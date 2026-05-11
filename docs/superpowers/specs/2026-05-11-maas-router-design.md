# MaaS Router — Design Spec

**Date:** 2026-05-11
**Status:** Draft, pending implementation plan
**Author:** Brainstorm session — vshanyiao@gmail.com

## 1. Overview

A Model-as-a-Service reseller platform offering frontier Western LLM access (OpenAI, Anthropic, Google) to mainland China users, differentiated from OpenRouter by **native Alipay/WeChat Pay support** and a **Chinese-language user experience**. Operated as an offshore (Hong Kong) entity to avoid ICP filing and Cyberspace Administration content-moderation overhead, hosted on AWS in `ap-east-1`.

Users sign up, top up credits via Stripe (card / Alipay / WeChat Pay), get an API key, and call frontier models through either an **OpenAI-compatible `/v1/chat/completions`** endpoint or an **Anthropic-compatible `/v1/messages`** endpoint. We charge a flat 18% markup on the underlying provider cost (admin-configurable per-model). Pre-paid credit model only; no postpaid invoicing.

This document is the design spec. A separate implementation plan (written via the `writing-plans` skill) follows.

## 2. Locked-in Product Decisions

| Decision | Choice |
|---|---|
| Scope | MVP (not POC, not full production-grade clone) |
| Audience | Reseller SaaS targeting mainland China users |
| Differentiation | Alipay/WeChat Pay + Chinese-language UX |
| Legal posture | Offshore Hong Kong entity; no ICP filing |
| Hosting | AWS `ap-east-1` (Hong Kong region) |
| Model coverage (MVP) | ~6–8 frontier Western chat models (no domestic CN models, no commodity inference hosts) |
| Specific launch models | GPT-4o, GPT-4o-mini, o1, o1-mini, Claude 4.6 Sonnet, Claude Haiku 4.5, Gemini 2.5 Pro, Gemini 2.5 Flash |
| API surface | OpenAI-compatible (`/v1/chat/completions`) **and** Anthropic-compatible (`/v1/messages`), with cross-format translation |
| Feature coverage (MVP) | Basic chat, streaming, tools / function calling, vision (image input), system prompts |
| Feature coverage (post-MVP) | JSON mode / structured output, logprobs, audio in/out, embeddings, image generation |
| Frontend stack | Next.js 14+ (App Router), TypeScript, shadcn/ui, Tailwind CSS |
| Backend stack | Go for the inference proxy (high-concurrency streaming) |
| Pricing model | Pre-paid credits, flat 18% markup (admin-configurable, per-model overridable) |
| BYOK | **Not in MVP** (deferred) |
| Payment integration | Stripe (card + Alipay + WeChat Pay as one-time methods) |
| Authentication | Email/password + GitHub OAuth (no WeChat OAuth — requires CN entity) |
| Trial credit | $1 (100 cents) granted only after email verification or GitHub OAuth signup |
| UI scope | Landing + dashboard + model catalog + playground + admin panel |
| UI languages | Bilingual (zh-CN default, EN toggle, stored in `users.locale`) |
| Component library | shadcn/ui + Tailwind |
| AWS orchestration | EKS (Kubernetes) — chosen as a learning exercise; ECS Fargate would otherwise be the MVP recommendation |
| AWS IaC | CloudFormation (nested stacks) — chosen as a learning exercise; Terraform would otherwise be the recommendation |
| Local dev | `docker compose up` for the full stack (web, proxy, postgres, redis, stripe-cli) |
| Async write pattern | Reactor pattern (in-process goroutine + buffered channel + batch insert) for `request_logs`, `api_keys.last_used_at` updates, `audit_logs` |
| Default markup | 18% (admin-configurable) |
| Default top-up presets | $5, $10, $20, $50, $100 (admin-configurable) |

## 3. System Architecture

### 3.1 Three deployables

1. **`web`** — Next.js (App Router). Serves all user-facing pages plus the **control-plane API** at `/api/*`:
   - Signup, login, OAuth callback
   - API key CRUD
   - Stripe Checkout session creation
   - Stripe webhook receiver (`/api/stripe-webhook`)
   - Model catalog read
   - Usage queries
   - Admin panel (`/admin/*`) and admin API (`/api/admin/*`)
   
   Does **not** serve the inference path. Does **not** hold long-running streaming connections.

2. **`proxy`** — Go binary. Serves **only** the inference API at `/v1/chat/completions` and `/v1/messages`. Responsibilities:
   - Authenticate by API key (Redis cache → Postgres fallback)
   - Reserve max cost from user credits (atomic SQL)
   - Translate request format if user surface ≠ upstream provider format
   - Stream from upstream provider, translate stream events back to user surface format
   - Finalize: deduct actual cost, refund the reservation difference, async-log via Reactor pattern

3. **`reaper`** — A goroutine running inside the `proxy` binary on a 60-second ticker (not a separate deployable for MVP). Sweeps stuck `credit_transactions` with `kind='reservation'` older than 5 minutes that have no matching `consumption` or `refund`, and refunds them.

### 3.2 Shared backing services

- **Postgres (RDS)** — source of truth for users, API keys, credits, ledger, request logs, model catalog, pricing config, audit logs. Multi-AZ in production.
- **ElastiCache Redis** — hot caches and counters:
  - `key:{hmac}` → API key lookup (5-min TTL)
  - `rl:user:{user_id}:{window}` → per-user rate limit counters
  - `rl:ip:{ip}:{window}` → per-IP signup / login / unauth rate limit
  - `idem:{stripe_event_id}` → Stripe webhook idempotency (24h TTL)

### 3.3 Boundary between `web` and `proxy`

- `web` writes: `users`, `api_keys`, `credit_transactions` (only for top-ups and admin adjustments), `payment_intents`, `app_config`, `audit_logs`, `model_catalog`
- `proxy` writes: `credit_transactions` (consumption/refund/reservation), `users.credits_cents` (atomic decrement), `request_logs` (async via Reactor), `api_keys.last_used_at` (async via Reactor)
- Both read: `model_catalog`, `app_config`, `users`, `api_keys`
- The services do **not** call each other over HTTP. They communicate only via shared Postgres + Redis.

### 3.4 Deployment topology (AWS, `ap-east-1`)

```
[User Browser] ──▶ CloudFront ──▶ ALB ──▶ ECS/EKS pods: web (Next.js)
[User API client] ──▶ ALB (api.domain) ──▶ EKS pods: proxy (Go)

   web pods   ──▶ RDS Postgres (private subnet, multi-AZ)
   proxy pods ──▶ RDS Postgres + ElastiCache Redis
   proxy pods ──▶ NAT Gateway ──▶ OpenAI / Anthropic / Google APIs

   Stripe ──▶ ALB ──▶ web pod /api/stripe-webhook
```

### 3.5 What we are NOT building

- No separate auth microservice (NextAuth in `web` is enough).
- No separate billing microservice (Stripe webhook → `web` → Postgres).
- No separate key-issuance service.
- No worker / job queue service for MVP (Reactor pattern in-process suffices; cron jobs run inside `proxy`).

## 4. Data Model

### 4.1 Postgres tables

#### `users`
```
id                  uuid PK
email               text unique not null
password_hash       text nullable                -- null for OAuth-only users
github_id           text unique nullable
email_verified_at   timestamptz nullable
locale              text not null default 'zh-CN'  -- 'zh-CN' | 'en'
credits_cents       bigint not null default 0    -- atomic decrement field
status              text not null default 'active'  -- 'active' | 'suspended' | 'banned'
is_admin            bool not null default false
created_at          timestamptz not null default now()
updated_at          timestamptz not null default now()
```

#### `api_keys`
```
id              uuid PK
user_id         uuid not null FK → users(id)
name            text not null               -- user-defined label
key_prefix      text not null               -- first 12 chars for display
key_hash        text not null unique        -- HMAC-SHA256(key, server_secret)
last_used_at    timestamptz nullable        -- async-updated via Reactor
created_at      timestamptz not null default now()
revoked_at      timestamptz nullable
```

Index: `(user_id, revoked_at)` partial index where `revoked_at IS NULL`.

#### `credit_transactions` — append-only ledger
```
id                  uuid PK
user_id             uuid not null FK → users(id)
amount_cents        bigint not null            -- signed
kind                text not null
    -- 'topup'         (Stripe top-up)
    -- 'reservation'   (max-cost reserved at request start)
    -- 'consumption'   (actual cost at request end)
    -- 'refund'        (reservation - actual at request end, or admin refund)
    -- 'adjustment'    (admin manual)
    -- 'trial_credit'  (signup bonus)
    -- 'chargeback'    (Stripe dispute)
request_id          uuid nullable              -- NOT a FK; eventual consistency w/ Reactor
payment_intent_id   uuid nullable FK → payment_intents(id)
balance_after_cents bigint not null            -- snapshot for audit reconstruction
description         text nullable              -- e.g. admin adjustment reason
created_at          timestamptz not null default now()
```

Index: `(user_id, created_at desc)`. Append-only — never UPDATE this table.

#### `payment_intents` — Stripe top-up sessions
```
id                          uuid PK
user_id                     uuid not null FK → users(id)
stripe_payment_intent_id    text unique nullable
stripe_checkout_session_id  text unique nullable
amount_cents                bigint not null
credits_added_cents         bigint not null    -- usually == amount_cents
currency                    text not null default 'usd'
status                      text not null      -- 'pending' | 'succeeded' | 'failed' | 'expired'
created_at                  timestamptz not null default now()
completed_at                timestamptz nullable
```

#### `request_logs` — one row per inference request, async-written
```
id                  uuid PK                   -- returned to client as request ID
user_id             uuid not null FK → users(id)
api_key_id          uuid not null FK → api_keys(id)
api_surface         text not null             -- 'openai' | 'anthropic'
upstream_provider   text not null             -- 'openai' | 'anthropic' | 'google'
upstream_model      text not null
model_alias         text not null             -- as requested by client
prompt_tokens       int nullable
completion_tokens   int nullable
input_cost_cents    int not null default 0    -- provider cost (no markup)
output_cost_cents   int not null default 0
margin_cents        int not null default 0
total_charged_cents int not null default 0
streaming           bool not null
status              text not null             -- 'success' | 'provider_error' | 'cancelled' | 'insufficient_credits' | 'rate_limited'
error_message       text nullable
latency_ms          int nullable
created_at          timestamptz not null
completed_at        timestamptz nullable
```

Indexes: `(user_id, created_at desc)`, `(created_at desc)`. Monthly range-partition after first 12 months — deferred decision.

#### `model_catalog` — admin-configurable
```
id                                  uuid PK
alias                               text unique not null   -- "anthropic/claude-4.6-sonnet"
display_name                        text not null
upstream_provider                   text not null          -- 'openai' | 'anthropic' | 'google'
upstream_model_id                   text not null
context_window                      int not null
supports_streaming                  bool not null default true
supports_tools                      bool not null default false
supports_vision                     bool not null default false
input_cents_per_million_tokens      int not null
output_cents_per_million_tokens     int not null
markup_pct                          int not null default 18    -- per-model override
status                              text not null default 'active'  -- 'active' | 'deprecated' | 'disabled'
tags                                text[] not null default '{}'
description_zh                      text
description_en                      text
created_at                          timestamptz not null default now()
updated_at                          timestamptz not null default now()
```

#### `app_config` — admin-configurable runtime values
```
key         text PRIMARY KEY
value       jsonb not null
updated_at  timestamptz not null default now()
```

Seed rows:
- `default_markup_pct` → `18`
- `topup_presets_cents` → `[500, 1000, 2000, 5000, 10000]`
- `trial_credit_cents` → `100`
- `cny_per_usd_rate` → `7.20` (refreshed daily by cron)
- `rate_limit_per_user_per_minute` → `60`

#### `audit_logs` — security & admin actions, async-written
```
id          uuid PK
user_id     uuid nullable FK → users(id)    -- actor (admin id, or null for system events)
target_user_id  uuid nullable FK → users(id)   -- target of the action
kind        text not null              -- 'admin_credit_adjust' | 'admin_suspend' | 'admin_price_change' | 'login_failed' | etc.
payload     jsonb not null             -- before/after, request body, etc.
ip_address  text nullable
user_agent  text nullable
created_at  timestamptz not null default now()
```

#### NextAuth-managed tables
`sessions`, `accounts`, `verification_tokens` — managed by NextAuth Prisma adapter. We do not interact with these directly.

### 4.2 Non-obvious data model decisions

1. **API key hashing uses HMAC-SHA256, not bcrypt.** Keys are 190-bit random tokens, not low-entropy passwords. HMAC with a server-side secret prevents lookup attacks if the DB is leaked, while being ~10,000× faster than bcrypt. Bcrypt on every inference request would add ~100ms of CPU and is unnecessary.

2. **Cost stored as `cents_per_million_tokens` (integer).** Per-request cost calculated as `tokens × rate / 1_000_000`, rounded **up** at charge time. No floating-point math anywhere in the billing pipeline. The slight rounding favorable to us (≈$0.0001/request) is disclosed in the Terms of Service ("billed cost rounded up to the nearest cent").

3. **All credits stored in USD cents.** Stripe handles CNY → USD conversion at checkout. Single-currency math removes a whole class of reconciliation bugs. The UI displays "≈ ¥X" using `app_config.cny_per_usd_rate`.

4. **Atomic balance updates use conditional SQL:**
   ```sql
   UPDATE users
      SET credits_cents = credits_cents - $reserve_amount
    WHERE id = $user_id AND credits_cents >= $reserve_amount
   RETURNING credits_cents;
   ```
   Zero rows returned → insufficient credits, return 402. No race window between check and decrement at the default isolation level.

5. **`credit_transactions` is append-only** with `balance_after_cents` snapshot. Any user's balance history can be rebuilt by replay. Required for billing disputes and audits. Never UPDATE this table — only INSERT.

6. **`credit_transactions.request_id` is a plain UUID, NOT a foreign key.** Because the Reactor pattern writes `request_logs` asynchronously (after `credit_transactions` is committed synchronously), enforcing a FK would create write-order coupling. Application code maintains consistency; eventual consistency is acceptable since `credit_transactions` is the billing source of truth.

7. **No soft delete on `users`.** Use `status='banned'` to preserve FK integrity in `request_logs`, `credit_transactions`, etc. Same for `api_keys` — use `revoked_at`.

8. **`request_logs` is the busiest table.** At 1k req/day → 365k rows/year. Indexes designed for "user views their history" (`user_id, created_at`) and "admin views recent" (`created_at`). Monthly partitioning is recommended after 12 months but deferred for now.

## 5. Request Flow

### 5.1 Six phases of `POST /v1/chat/completions` (streaming)

**Phase 1 — Authenticate (≤2ms).** Read `Authorization: Bearer sk-or-...`. Compute `HMAC-SHA256(key, server_secret)`. Lookup `key:{hmac}` in Redis (cache hit: ~0.5ms). On miss: `SELECT user_id, credits_cents, status FROM api_keys JOIN users ON ... WHERE key_hash = $1 AND revoked_at IS NULL`; populate Redis with 5-min TTL. Reject 401 if not found / revoked; 403 if user banned.

**Phase 2 — Parse & route (≤2ms).** Parse JSON body. Extract `model` field. Look up in `model_catalog` (in-memory cache, refreshed from Postgres every 60s). Validate request matches API surface schema (e.g., `messages` array, `max_tokens`). Determine if cross-format translation is needed (user surface ≠ `upstream_provider`).

**Phase 3 — Cost reservation (5–15ms, one DB roundtrip).** Count prompt tokens with the appropriate tokenizer:
- OpenAI models: `tiktoken-go` (BPE encoding matching the model's encoder, e.g. `o200k_base` for GPT-4o).
- Anthropic models: `anthropic-tokenizer-go` (or call Anthropic's `/v1/messages/count_tokens` endpoint as fallback).
- Gemini models: call Google's `count_tokens` API once at request start (adds ~50ms; cache result by hash of prompt for retries).

Estimate max cost:
```
max_cost = ceilDiv(prompt_tokens × input_rate, 1_000_000)
         + ceilDiv(max_completion_tokens × output_rate, 1_000_000)
margin   = ceilDiv(max_cost × markup_pct, 100)
reserve  = (max_cost + margin)
```
(If client doesn't specify `max_tokens`, default to model's context window minus prompt tokens.)

Atomic SQL:
```sql
UPDATE users SET credits_cents = credits_cents - $reserve
 WHERE id = $user_id AND credits_cents >= $reserve
RETURNING credits_cents;
```
- 0 rows → 402 Payment Required (insufficient credits).
- INSERT `credit_transactions(kind='reservation', amount=-$reserve, request_id=$id, balance_after_cents=...)` in the same transaction.

**Phase 4 — Translate & dispatch (1–3ms).** If surface format ≠ upstream format, run the appropriate translator (e.g., `translate/oai/to_canonical` → `provider/anthropic/from_canonical`). Construct upstream HTTP request with our provider API key (from AWS Secrets Manager, cached in process).

**Phase 5 — Stream from upstream (provider-dependent: 100ms – 30s+).** Open SSE / chunked HTTP. For each chunk: parse as canonical `StreamEvent`, translate to user surface format if needed, write to client via SSE. Accumulate token counts from chunk metadata. On client disconnect, propagate `ctx.Cancel()` to upstream.

**Phase 6 — Finalize.**
- **Synchronous (billing source of truth):**
  - `actual_cost = ceilDiv(prompt_tokens × input_rate, 1_000_000) + ceilDiv(completion_tokens × output_rate, 1_000_000) + ceilDiv(provider_cost × markup_pct, 100)`
  - `refund = reserve - actual_cost` (always ≥ 0)
  - One Postgres transaction:
    ```sql
    UPDATE users SET credits_cents = credits_cents + $refund WHERE id = $user_id;
    INSERT credit_transactions(kind='consumption', amount=-$actual_cost, ..., balance_after_cents=...);
    INSERT credit_transactions(kind='refund', amount=+$refund, ...);  -- only if refund > 0
    ```
- **Asynchronous (Reactor pattern, non-blocking):**
  - Push `RequestLogEntry` to `logActor.inbox` channel
  - Push `KeyActivity` update to `keyActivityActor.inbox` (coalesced, latest-wins per key)
  - Return final SSE chunk to client and close connection

### 5.2 Error paths

| Failure | HTTP response | Credit handling |
|---|---|---|
| Bad / revoked API key | 401 | No charge (never reserved) |
| Insufficient credits | 402 | No charge |
| Rate limit exceeded | 429 | No charge |
| Provider 5xx before any tokens | 502 | Full refund of reservation |
| Provider error mid-stream | Stream ends with error event | Charge for tokens generated; refund remainder |
| Client disconnect mid-stream | (connection closed) | Charge for tokens generated up to cancellation; refund remainder |
| Proxy crash after reservation | (connection drops) | Reaper job refunds within 5 min |

### 5.3 Stuck-reservation reaper

A goroutine in the `proxy` binary runs every 60 seconds:

```sql
SELECT user_id, request_id, -amount_cents AS refund_amount
  FROM credit_transactions
 WHERE kind = 'reservation'
   AND created_at < now() - interval '5 minutes'
   AND request_id NOT IN (
       SELECT request_id FROM credit_transactions
        WHERE kind IN ('consumption', 'refund')
          AND request_id IS NOT NULL
   );
```

For each row, refund the reservation amount via `UPDATE users SET credits_cents += refund_amount` + `INSERT credit_transactions(kind='refund', amount=+refund_amount)`. Safety net for crashes only — not the normal path.

## 6. Provider Abstraction

### 6.1 Five-layer architecture

```
Layer 1: Surface handler        e.g. server/openai.go         (parse user request)
                  ↓
Layer 2: Translator             translate/oai/to_canonical    (only if cross-format)
                  ↓
Layer 3: Canonical IR           canonical.Request             (provider-neutral)
                  ↓
Layer 4: Provider adapter       provider/anthropic/           (canonical → Anthropic HTTP)
                  ↓
Layer 5: Upstream provider      Anthropic API
```

The stream returns through the reverse path: provider adapter parses provider's SSE → emits `canonical.StreamEvent` → translator converts to user surface format → SSE written to client.

### 6.2 Fast path

When **surface format == upstream provider format** (e.g., OAI surface → OpenAI upstream), skip canonical conversion entirely:

1. Parse just enough of the request to extract model and count prompt tokens for cost reservation.
2. Replace `Authorization` header (their key → our provider key) and swap `model` if aliased.
3. Forward raw bytes upstream.
4. Stream upstream bytes back; only parse the final chunk for `usage` data.

Saves ~5ms per request and ensures we never lose data on edge-case fields we haven't modeled in canonical IR yet. Expected to be the most common code path.

### 6.3 Feature support matrix (MVP)

|Feature                 | OAI→OAI | OAI→Anthropic | OAI→Gemini | Anthropic→Anthropic | Anthropic→OAI | Anthropic→Gemini |
|------------------------|---------|---------------|------------|---------------------|---------------|------------------|
| Basic chat             | native  | translate     | translate  | native              | translate     | translate        |
| Streaming              | native  | SSE remap     | SSE remap  | native              | SSE remap     | SSE remap        |
| Tools / function calls | native  | schema map    | schema map | native              | schema map    | schema map       |
| Vision (image input)   | native  | content map   | inlineData | native              | content map   | inlineData       |
| System prompt          | native  | to `system`   | to `systemInstruction` | native | to `role:system` | to `systemInstruction` |
| JSON mode / structured | native  | **deferred to v2** | **deferred** | native (prefill) | **deferred** | **deferred** |
| Logprobs               | native  | **N/A — provider doesn't support** | **N/A** | N/A | N/A | N/A |
| Audio in/out           | **deferred to v3** | **deferred** | **deferred** | **deferred** | **deferred** | **deferred** |

Red cells return HTTP 400 with a clear error message: `"feature X is not supported on model Y; try model Z"`.

### 6.4 Go package layout

```
proxy/
  cmd/proxy/main.go
  internal/
    server/                    # HTTP handlers for /v1/* routes
      openai.go                # POST /v1/chat/completions
      anthropic.go             # POST /v1/messages
    auth/                      # API key validation, Redis cache
    billing/                   # reserve, finalize, refund
    catalog/                   # model_catalog in-memory cache
    canonical/                 # IR types
    translate/
      oai/                     # OAI surface ↔ canonical
      anthropic/               # Anthropic surface ↔ canonical
    provider/
      provider.go              # Provider, StreamReader interfaces
      openai/
      anthropic/
      gemini/
    stream/                    # SSE writer, chunk relay
    logging/                   # Reactor-pattern actors
    reaper/                    # stuck-reservation sweeper
    storage/                   # Postgres + Redis clients
    metrics/                   # CloudWatch EMF emitter
```

### 6.5 Canonical IR (sketch)

```go
package canonical

type Request struct {
    Model         string
    System        string          // separated from messages
    Messages      []Message
    Tools         []Tool
    ToolChoice    ToolChoice
    MaxTokens     int
    Temperature   *float64
    StopSequences []string
    Stream        bool
}

type Message struct {
    Role    string          // "user" | "assistant" | "tool"
    Content []ContentBlock  // always array; strings get wrapped
}

type ContentBlock struct {
    Type       string  // "text" | "image" | "tool_use" | "tool_result"
    Text       string
    ImageURL   string  // or ImageData []byte
    ToolUseID  string
    ToolName   string
    ToolInput  json.RawMessage
    ToolResult string
}

type StreamEvent struct {
    Type          string  // "content" | "tool_call_delta" | "usage" | "stop" | "error"
    ContentDelta  string
    ToolCallDelta ToolCallDelta
    Usage         *Usage
    StopReason    string
    Error         *ErrorInfo
}
```

`Content` is always `[]ContentBlock`. OAI's string-or-array ambiguity is normalized at the surface boundary.

### 6.6 Provider interface

```go
package provider

type Provider interface {
    Name() string
    Send(ctx context.Context, req canonical.Request, key Credential) (StreamReader, error)
    SendRaw(ctx context.Context, body []byte, key Credential) (io.ReadCloser, error)
}

type StreamReader interface {
    Recv() (canonical.StreamEvent, error)  // io.EOF on completion
    Close() error
}

type Credential struct {
    APIKey string
    // Plus provider-specific extras (Azure deployment, GCP project) — deferred to v2.
}
```

### 6.7 Testing strategy

1. **Pure-function golden tests** for every translator. Fixture in → struct out. Coverage: every supported feature × every surface.
2. **Recorded conformance tests** using `go-vcr` — record real provider responses once, replay deterministically. One recording per (provider, feature). Re-recorded quarterly to catch provider drift.
3. **Cross-format integration tests** — for every (surface, upstream) combination, run a synthetic request through the full stack against a mock upstream, assert response shape. Catches translation regressions.
4. **Live smoke tests in CI** — hit each real provider with one canary request before every deploy. Fails CI if a provider has changed shape.

### 6.8 Notable translation challenges

- **OAI tools vs Anthropic tools.** OAI: `{"function":{"name", "parameters":<schema>}}`. Anthropic: `{"name", "input_schema":<schema>}`. Schemas are interchangeable; the wrapper differs.
- **Tool results in multi-turn agentic flows.** OAI puts tool results in messages with `role:"tool"`. Anthropic puts them in user messages as `tool_result` content blocks. Translation must be precise or the model loses context.
- **Streaming SSE event shapes.** OAI: single `data: {...}` deltas. Anthropic: typed events (`content_block_start`, `content_block_delta`, `message_delta`, etc.). Gemini: not SSE — chunked JSON arrays. Each provider needs its own framing parser.
- **JSON mode.** OAI has `response_format`. Anthropic uses an assistant-message-prefill trick (`{`). Gemini has `responseSchema`. **Deferred to v2** in MVP.

## 7. Billing, Credits & Top-up Flow

### 7.1 Top-up flow

```
1. User clicks "Top up $20" on /dashboard/billing
2. Browser → POST /api/topup/create-session  (web)
     - Validates: authenticated user, amount in app_config.topup_presets_cents
     - INSERT payment_intents (status='pending', amount_cents=2000, credits_added_cents=2000)
     - Stripe.checkout.sessions.create({
         payment_method_types: ['card', 'alipay', 'wechat_pay'],
         line_items: [{price_data: {currency: 'usd', unit_amount: 2000, ...}}],
         metadata: {payment_intent_id: <our uuid>},
         success_url: '/dashboard/billing/topup/success',
         cancel_url:  '/dashboard/billing/topup/cancel',
       })
     - Return session.url
3. Browser navigates to Stripe Checkout
4. User pays (card / Alipay QR / WeChat Pay QR)
5. Stripe → POST /api/stripe-webhook  (signature-verified, no auth)
     - Verify HMAC-SHA256 of body using STRIPE_WEBHOOK_SECRET
     - Redis SETNX idem:{stripe_event_id}=1 TTL 24h
         → if exists, return 200 (already processed)
     - On checkout.session.completed:
         BEGIN;
         UPDATE payment_intents SET status='succeeded', completed_at=now()
           WHERE id=$id AND status='pending';
         UPDATE users SET credits_cents = credits_cents + $credits_added
           WHERE id=$user_id;
         INSERT credit_transactions(kind='topup', amount=+$credits_added,
                                    balance_after_cents=..., payment_intent_id=$id);
         COMMIT;
     - Return 200
6. User is redirected back to /dashboard/billing/topup/success
7. Dashboard polls /api/credits/balance every 2s for 30s waiting for webhook to land
8. Email receipt sent via Resend
```

### 7.2 Pricing math (per request)

```go
inputCost    := ceilDiv(promptTokens     * model.InputCentsPerMillion,  1_000_000)
outputCost   := ceilDiv(completionTokens * model.OutputCentsPerMillion, 1_000_000)
providerCost := inputCost + outputCost
margin       := ceilDiv(providerCost * model.MarkupPct, 100)
totalCharge  := providerCost + margin
```

`ceilDiv(a, b) := (a + b - 1) / b` (integer division). Always rounds up. Disclosed in ToS.

**Worked example — Claude 4.6 Sonnet** at $3/M input, $15/M output, 18% markup, 1000 prompt + 500 completion tokens:
- Input cost: `ceil(1000 × 300 / 1_000_000)` = 1 cent
- Output cost: `ceil(500 × 1500 / 1_000_000)` = 1 cent
- Provider total: 2 cents
- Margin: `ceil(2 × 18 / 100)` = 1 cent (rounds up from 0.36)
- Total charge: 3 cents

For tiny requests the rounding-up of margin can briefly exceed 18%. For any request of meaningful size, it settles at exactly 18%.

### 7.3 Webhook idempotency — three layers

1. **Redis SETNX** on `idem:{stripe_event_id}` — first-line check (sub-ms).
2. **Postgres unique constraint** on `payment_intents.stripe_payment_intent_id` — second line if Redis is down.
3. **Conditional UPDATE** with `WHERE status='pending'` — third line; affects zero rows if already succeeded, so the credit increment is skipped.

All three layers because **double-crediting is the single highest-risk billing bug.**

### 7.4 Edge cases

- **Refund (admin via Stripe dashboard).** Stripe sends `charge.refunded`. Deduct from `users.credits_cents` (allowed to go negative). User cannot make new requests until they top up. Audit log entry.
- **Chargeback (user disputes via bank).** Stripe sends `charge.dispute.created`. Auto-suspend user, post Slack alert to admin, lose credits already consumed (write-off).
- **Currency.** User pays in CNY via Alipay/WeChat; Stripe converts to USD at their daily rate. We credit USD cents. UI shows "≈ ¥X" via `app_config.cny_per_usd_rate`.
- **Failed top-up.** `checkout.session.expired` or `payment_intent.payment_failed` webhook → set `payment_intents.status='failed'`. User can retry.

### 7.5 Known Risks

- **Stripe Checkout asset loading from mainland CN can be flaky** due to GFW behavior. Top-up frequency is low (1× per top-up), so most users will succeed eventually. Document this in user-facing FAQ; consider migrating to Airwallex post-MVP if user feedback shows persistent issues.
- **Stripe Alipay/WeChat = one-time payments only, no saved-method auto-topup.** Users will need to repeat the full flow each top-up. Acceptable for MVP.

## 8. Web UI Pages & Flows

### 8.1 Page inventory

**Public / marketing**
- `/` — landing, hero, value pitch, pricing, model list, CTA → signup
- `/pricing` — top-up presets, 18% markup explanation, FAQ
- `/models` — public model catalog (zh/en descriptions, prices, capabilities)
- `/docs` — API quickstart, OAI example, Anthropic example, errors, rate limits
- `/legal/terms`, `/legal/privacy`

**Auth**
- `/signup`, `/login`
- `/auth/github/callback`
- `/auth/verify-email/[token]`
- `/auth/forgot-password`, `/auth/reset-password/[token]`

**Authenticated dashboard**
- `/dashboard` — balance widget, 7-day usage chart, quick actions
- `/dashboard/keys` — API key CRUD; create modal shows raw key once
- `/dashboard/usage` — per-request history, filters, CSV export, model breakdown chart
- `/dashboard/billing` — balance, top-up history, top-up button, transactions ledger
- `/dashboard/billing/topup/success`, `/dashboard/billing/topup/cancel`
- `/dashboard/playground` — in-browser chat, model picker, cost-per-message, "show as code"
- `/dashboard/settings` — email, password, locale toggle, delete account

**Admin** (gated by `users.is_admin = true`)
- `/admin` — overview metrics
- `/admin/pricing` — `app_config` form (markup, presets, trial credit, FX rate)
- `/admin/models` — `model_catalog` CRUD
- `/admin/users` — search, suspend, manual credit adjustment (with audit-log reason)
- `/admin/transactions` — top-ups, refunds, disputes, CSV export
- `/admin/requests` — recent inference logs (read-only)
- `/admin/audit` — admin action log

Total page count: **~22**.

### 8.2 Critical flows

**Flow A — First-time user activation funnel:**
```
landing → "Get Started" → /signup (GitHub OAuth recommended path)
       → email verified (or skipped for GitHub) → /dashboard
       → $1 trial credit visible → "Create your first key" CTA
       → /dashboard/keys → "New Key" modal → key shown ONCE, copy button + warning
       → /dashboard/playground → pick model → send message → streaming response visible
       → balance updates from $1.00 → $0.97
       → CTA "Like it? Top up to keep using" → /dashboard/billing
```
**Goal:** time-to-first-token ≤ 30s from signup completion.

**Flow B — Top-up:** detailed in §7.1.

**Flow C — Playground:**
- Model picker dropdown grouped by provider (OpenAI / Anthropic / Google), showing price per 1k tokens next to each.
- Single conversation, no persistence — deliberately minimal.
- Per-message cost shown below each assistant bubble after stream completes.
- Running cost tally at top.
- "Show as code" toggle reveals a TS / Python / cURL snippet that reproduces the call.
- Playground requests use an auto-created API key named `playground-{user_id}`.
- Hard caps: max 50 messages per conversation, max 4k output tokens per message.

### 8.3 Cross-cutting UI requirements

- **Bilingual (zh-CN / en)** via `next-intl`. Strings in `messages/zh.json` and `messages/en.json`. Toggle in header, persisted to `users.locale`, defaults to `zh-CN` for new users (also detects from `Accept-Language` header).
- **Balance widget** in the header of every authenticated page: `$X.XX (≈¥Y.YY)`. Click → `/dashboard/billing`.
- **Empty states** designed for: zero usage, zero keys, zero top-ups. Skipping creates a "broken-looking" first-run.
- **Error states** for Stripe failure, rate limited, network error — clear zh/en messages, not raw error codes.
- **Dark mode toggle** — deferred to v2.

### 8.4 Component library

**shadcn/ui + Tailwind CSS.** Components copy-pasted into the repo, fully owned, no version-bump churn. Good zh-CN font handling out of the box. Material-UI and Ant Design are explicitly rejected — too opinionated, fight with custom styling.

### 8.5 Visual design direction

Low-fidelity wireframes for the 7 most-critical pages (landing, dashboard, playground, top-up modal, API keys, admin overview, admin users) are committed alongside this spec at `docs/superpowers/specs/2026-05-11-maas-router-wireframes.html` (open in a browser) and serve as the layout reference for implementation. Real visual polish comes at implementation time via shadcn/ui defaults + Tailwind tokens.

**Reference style:** Linear, Vercel, Stripe — clean, modern, generous whitespace, restrained color, strong typography hierarchy.

**Color palette:**
- Primary accent: `indigo-600` (#6366f1) — CTAs, links, selected states, charts
- Surface: white for cards, `gray-50` (#fafafa) for page background
- Borders: `gray-200` (#e5e5e5)
- Text: `gray-900` primary, `gray-600` secondary, `gray-400` tertiary
- Success: `green-600`; Warning: `amber-500`; Danger: `red-600`
- **Admin context visually distinct:** dark `gray-800` sidebar + `red-50` top strip, so admin pages can't be confused with user pages

**Typography:**
- Latin: Inter (variable, weights 400/500/600/700)
- Chinese: Noto Sans SC web font with fallback to PingFang SC (Apple) and Microsoft YaHei (Windows)
- Stack: `font-family: Inter, "Noto Sans SC", "PingFang SC", "Microsoft YaHei", system-ui, sans-serif;`
- Use Tailwind's type scale; no custom font sizes

**Density and spacing:**
- Marketing (landing, pricing, docs): spacious — section padding `py-16` to `py-24`
- Dashboard / settings: medium — `py-6` to `py-8`
- Tables / data-dense areas: compact — Tailwind defaults

**Iconography:** `lucide-react` (bundled with shadcn/ui). Meaningful icons only, never decorative.

**Light mode only for MVP.** Dark mode deferred to v2.

**Brand:** placeholder wordmark `⚡ MaaS Router` until a designer is engaged. **The actual brand name is not finalized — pick before going public.**

**Bilingual layout considerations:**
- Chinese characters occupy ~30% less horizontal space than equivalent English. Test every page in both languages — English can overflow buttons designed for Chinese.
- Numbers and prices always use Latin numerals (`$20`, `¥144`), never Chinese numerals.

**Accessibility (WCAG 2.1 AA target):**
- All interactive elements keyboard-reachable
- Color contrast ≥ 4.5:1 for normal text, ≥ 3:1 for large text
- Form labels properly associated; error messages tied via `aria-describedby`
- Focus rings always visible — never `outline: none` without a replacement

### 8.6 Explicitly out of scope (deferred to v2+)

- Team / organization accounts
- Per-API-key rate limits or spending caps
- Prompt library / templates
- Conversation history persistence in playground
- Model leaderboards / benchmarks
- Public usage analytics
- Dark mode
- 2FA / TOTP / WebAuthn
- Magic-link / passwordless login
- IP allowlists per API key
- Custom domains / vanity URLs

## 9. Auth, Onboarding & Abuse Prevention

### 9.1 Auth library

**NextAuth.js v5 (Auth.js) with Prisma adapter.** Database-backed sessions (not JWT) for revocation control. **bcrypt** for password hashing at cost factor 12 (~250ms). Email/password and GitHub OAuth providers enabled.

### 9.2 Signup flows

**Email/password:**
```
1. POST /api/signup { email, password, turnstile_token }
2. Validate password ≥ 10 chars, email regex, Turnstile token
3. Rate limit: rl:signup:ip:{ip} < 5 in 1h, else 429
4. INSERT user (email, bcrypt(password), email_verified_at=NULL, credits=0)
5. INSERT verification_token (token, expires_at = now() + 24h)
6. Send Resend email "Verify your email"
7. Redirect → /signup/check-email
8. User clicks link → /auth/verify-email/{token}
   → mark email_verified_at = now(); delete token
   → grant $1 trial credit (INSERT credit_transactions kind='trial_credit', amount=+100)
   → auto-login → /dashboard
```

**GitHub OAuth:**
```
1. /signup → "Continue with GitHub"
2. /api/auth/signin/github → GitHub authorize
3. Callback /api/auth/callback/github
4. NextAuth upserts user by github_id; if new: email from GitHub, email_verified_at=now()
5. Grant $1 trial credit immediately
6. Redirect → /dashboard
```

**Trial credit grant logic:**
```
IF user.is_new
   AND (user.email_verified_at IS NOT NULL OR user.github_id IS NOT NULL)
   AND Redis SETNX trial:ip:{client_ip} = user_id with TTL 24h succeeds:
   THEN grant 100 cents (INSERT credit_transactions kind='trial_credit', amount=+100)
        UPDATE users SET credits_cents = credits_cents + 100 WHERE id = user_id;
```
The Redis SETNX makes the per-IP dedup atomic. If the key already exists, skip the grant silently — the new account still works, just without the bonus. Belt-and-suspenders: also enforce one trial per email by checking for an existing `kind='trial_credit'` row for that user before granting.

### 9.3 CAPTCHA

**Cloudflare Turnstile** on `/signup` and `/auth/forgot-password`. Free, privacy-friendly, often invisible. Skipped for GitHub OAuth path. Site key in env; server-side verify on POST.

### 9.4 Email delivery

**Resend** for transactional email. Templates: `verify-email`, `password-reset`, `topup-receipt`, `low-balance` (optional), `account-suspended`.

**CN deliverability checklist:**
- SPF, DKIM, DMARC records configured on the domain via Route 53.
- "From" address: `support@<yourdomain>`, not `noreply@notifications.amazonses.com`.
- First 100 emails will land in qq.com / 163.com junk folders — warmup is real.
- **Push GitHub OAuth as the primary CTA** to bypass email entirely.
- "Didn't receive the email?" prompt offers re-send and a "different email" fallback.

### 9.5 API key lifecycle

**Format:** `sk-or-` + 32 random base62 chars (~190 bits entropy).

**Storage:**
- `key_prefix` (text) — first 12 chars for display
- `key_hash` (text unique) — `HMAC-SHA256(full_key, HMAC_SERVER_SECRET)` hex-encoded

`HMAC_SERVER_SECRET` lives in AWS Secrets Manager. Rotating it invalidates all keys (recoverable from DB).

**Lookup:**
```
1. Compute hmac = HMAC-SHA256(incoming_key, HMAC_SERVER_SECRET)
2. Redis GET key:{hmac} — cache hit returns user/balance/status (≤1ms)
3. On miss: SELECT FROM api_keys JOIN users WHERE key_hash=$1 AND revoked_at IS NULL
4. Populate Redis with 5min TTL
5. On revoke: DELETE key:{hmac} from Redis
```

**Display:** key shown to user **only once** on creation modal, with copy button and warning. List views show only `sk-or-ABCD...` prefix.

### 9.6 Rate limiting

| Layer | Scope | Limit | Backing |
|---|---|---|---|
| AWS WAF | per-IP HTTP req | ~1000/min | ALB rule |
| Proxy (auth) | per-IP unauth | 100/min | Redis |
| Proxy (signup) | per-IP signup | 5/hour | Redis |
| Proxy (login) | per-email failed login | 10/hour with backoff | Redis |
| Proxy (inference) | per-user req | 60/min default | Redis |
| Proxy (inference) | per-key req | same as per-user | Redis |

**Algorithm:** sliding-window counter via Redis `INCR` + `EXPIRE`. Approximate but cluster-safe. On hit: HTTP 429 with `Retry-After` header.

Admin can override per-user limits via SQL (`users.rate_limit_overrides jsonb` — deferred to v2 if not needed sooner).

### 9.7 Abuse prevention layers

1. **Trial credit gating** — only after email_verified_at OR github_id. One per email, one per IP per 24h.
2. **Signup velocity** — >5 new accounts per IP in 1h or >15 in 24h → block.
3. **Spend velocity flag** — new user spends >$50 in first 24h → Slack alert (not auto-block; might be legitimate).
4. **Active key cap** — max 5 active keys per user.
5. **Turnstile** on signup + password reset.
6. **Stripe Radar** auto-detects card fraud.
7. **Auto-suspend on chargeback** — §7.4.

### 9.8 Admin access bootstrap

No admin-creating UI. First admin promoted manually after first deploy:

```sql
UPDATE users SET is_admin = true WHERE email = 'you@yourdomain.com';
```

Subsequent admins promoted via SQL only (deliberately — no "Make Admin" button to avoid social engineering). All admin actions auto-logged to `audit_logs` with before/after diffs.

## 10. Deployment & Operations

### 10.1 Repository layout

```
maas-router/
├── web/                          # Next.js app
│   ├── Dockerfile                # node:20-alpine, standalone output
│   ├── prisma/schema.prisma
│   ├── messages/{zh,en}.json
│   ├── src/
│   └── package.json
├── proxy/                        # Go service
│   ├── Dockerfile                # multi-stage, distroless final (~20MB)
│   ├── cmd/proxy/main.go
│   ├── internal/...
│   ├── migrations/               # golang-migrate format
│   └── go.mod
├── infra/
│   ├── cloudformation/
│   │   ├── root.yaml
│   │   ├── 01-network.yaml
│   │   ├── 02-data.yaml
│   │   ├── 03-eks.yaml
│   │   ├── 04-app.yaml
│   │   └── 05-dns.yaml
│   └── k8s/
│       ├── namespace.yaml
│       ├── external-secrets.yaml
│       ├── deployment-web.yaml
│       ├── deployment-proxy.yaml
│       ├── service-web.yaml
│       ├── service-proxy.yaml
│       ├── ingress.yaml
│       ├── hpa.yaml
│       └── cronjob-reaper.yaml   # if running reaper as separate CronJob
├── docker-compose.yml
├── Makefile
├── .github/workflows/
│   ├── ci.yml
│   ├── build.yml
│   └── deploy-prod.yml
└── docs/
    ├── superpowers/specs/
    └── runbook.md
```

### 10.2 Local development

```yaml
# docker-compose.yml (sketch)
services:
  web:
    build: ./web
    command: pnpm dev
    volumes: ["./web:/app", "/app/node_modules"]
    ports: ["3000:3000"]
    environment:
      DATABASE_URL: postgres://app:dev@postgres:5432/maas
      REDIS_URL: redis://redis:6379
      STRIPE_SECRET_KEY: ${STRIPE_TEST_KEY}
    depends_on: [postgres, redis]
  proxy:
    build: ./proxy
    command: air                 # Go hot-reload via cosmtrek/air
    volumes: ["./proxy:/app"]
    ports: ["8080:8080"]
    environment:
      DATABASE_URL: ...
      REDIS_URL: ...
      OPENAI_API_KEY: ${OPENAI_API_KEY}
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
      GOOGLE_API_KEY: ${GOOGLE_API_KEY}
      HMAC_SERVER_SECRET: dev-secret-not-for-prod
  postgres:
    image: postgres:16
    environment: { POSTGRES_USER: app, POSTGRES_PASSWORD: dev, POSTGRES_DB: maas }
    volumes: ["pgdata:/var/lib/postgresql/data"]
    ports: ["5432:5432"]
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
  stripe-cli:
    image: stripe/stripe-cli
    command: listen --forward-to web:3000/api/stripe-webhook
    environment: { STRIPE_API_KEY: ${STRIPE_TEST_KEY} }
volumes:
  pgdata:
```

`.env.example` checked in with placeholders; `.env.local` in `.gitignore`.

**Make shortcuts:** `make up`, `make seed`, `make migrate-new name=X`, `make test`, `make logs service=proxy`.

### 10.3 CloudFormation structure

Nested stacks, ordered by output → input flow:

```
root.yaml
  └── 01-network    → outputs: VpcId, PrivateSubnetIds, PublicSubnetIds
  └── 02-data       → outputs: RdsEndpoint, RedisEndpoint, SecretsArn
  └── 03-eks        → outputs: ClusterName, NodeGroupRole
  └── 04-app        → outputs: AlbDnsName, EcrRepoUris
  └── 05-dns        → outputs: AcmCertArn, ApexDomain
```

Total ~600–900 lines of YAML. **Note:** CloudFormation cannot create K8s resources or external services. Apply K8s manifests separately via `kubectl apply -f infra/k8s/` after cluster is up; configure Stripe webhooks, Cloudflare DNS (if any), and GitHub OAuth apps by hand in respective consoles.

### 10.4 EKS specifics

- **EKS version:** latest stable at time of deploy.
- **Node group:** 2× `t3.medium` Spot in 2 AZs, auto-scaling group 2–5 nodes.
- **Add-ons:** AWS Load Balancer Controller, External Secrets Operator, `metrics-server`, `cluster-autoscaler`.
- **CNI:** AWS VPC CNI. No service mesh for MVP.
- **HPA for proxy:** CPU > 70%, min 2 / max 10. Custom-metric scaling (in-flight requests) deferred until needed.
- **Pod resources:** proxy 500m CPU / 512Mi; web 250m CPU / 512Mi. Tune after first load test.

### 10.5 Secrets management

AWS Secrets Manager holds: DB password, Stripe secret key, Stripe webhook secret, provider API keys (OpenAI / Anthropic / Google), GitHub OAuth client secret, NextAuth secret, HMAC server secret, Resend API key, Turnstile secret key.

External Secrets Operator (ESO) in cluster syncs them into K8s `Secret` resources; pods mount as env vars.

**No secrets in CloudFormation, no secrets in git, ever.**

### 10.6 CI/CD

| Trigger | Workflow | Steps |
|---|---|---|
| PR opened | `ci.yml` | lint, test (web + proxy), build images locally (no push) |
| Merge to `main` | `build.yml` | build, tag with git SHA, push to ECR, run DB migrations as one-shot K8s Job, `kubectl set image` on web + proxy deployments, wait for rollout success |
| Manual trigger | `deploy-prod.yml` | same as above, prod cluster, requires GitHub Environment approval |

DB migrations: tested in staging first, run as a K8s `Job` before app rollout. Migration failure aborts the deploy.

### 10.7 Observability

**Logs:** stdout → Container Insights → CloudWatch Logs. JSON-structured: `{time, level, service, request_id, user_id, msg}`. Inference adds: `model, provider, prompt_tokens, completion_tokens, latency_ms, total_charged_cents`.

**Metrics:** CloudWatch via aws-cloudwatch-embedded-metric-format (EMF) emitted directly in log lines. No Prometheus operator for MVP.

Key custom metrics:
- `proxy.requests` [provider, status, surface]
- `proxy.duration_ms` [provider]
- `proxy.tokens_in/out` [model]
- `proxy.charged_cents` [model]
- `proxy.log_inbox_depth` [gauge] — Reactor backlog
- `proxy.provider_errors` [provider, code]
- `web.signups`
- `web.topup_completed_cents`
- `web.api_errors` [endpoint, code]

**Errors:** Sentry for both `web` and `proxy`. Free tier covers MVP.

**Alarms** (CloudWatch → SNS → Slack/email):

| Alarm | Threshold | Severity |
|---|---|---|
| Proxy p99 latency | > 30s for 5min | notify |
| Proxy 5xx rate | > 5% for 5min | page |
| Provider error rate (any) | > 20% for 10min | notify |
| `log_inbox_depth` | > 8000 sustained | notify |
| Stripe webhook failures | any in 10min | page |
| RDS CPU | > 80% for 10min | notify |
| Hourly revenue | drop > 50% vs last hour | notify |
| EKS node count | below expected min | page |

### 10.8 Backup & recovery

- **RDS:** automatic daily snapshots, 7-day retention. **Manual snapshot before every migration.** Point-in-time recovery covers last 7 days.
- **ElastiCache:** not backed up (rebuildable).
- **ECR:** lifecycle policy retains last 30 images per repo.
- **Disaster scenarios** documented in `docs/runbook.md`: RDS failover, accidental migration, provider key compromise, EKS destruction, chargeback storm.

### 10.9 Cost monitoring

- **AWS Budget alert** at $200/month threshold.
- **Admin overview** (`/admin`) shows real-time unit economics: revenue today, COGS (provider spend) today, gross margin %. Catches pricing-config errors within hours.

## 11. Phased Rollout

This is a sketch; a detailed implementation plan will be produced via the `writing-plans` skill.

| Phase | Scope | Duration estimate |
|---|---|---|
| 0 — Foundation | Repo scaffold, docker-compose, schema migrations, signup/login (email + GitHub), one model (GPT-4o non-streaming) end-to-end | 2 weeks |
| 1 — Streaming & providers | Streaming, all 3 providers, OAI surface only, basic dashboard | 3 weeks |
| 2 — Anthropic surface & translation | `/v1/messages` endpoint, full translation matrix, tools, vision | 3 weeks |
| 3 — Billing | Stripe Checkout, Alipay/WeChat methods, top-up flow, credit accounting, refund handling | 2 weeks |
| 4 — Admin panel & abuse prevention | All `/admin/*` pages, Turnstile, rate limits, audit logs | 2 weeks |
| 5 — i18n & polish | Bilingual, playground, model catalog, public landing, docs site, empty states | 2 weeks |
| 6 — Production deploy | CloudFormation stacks, EKS rollout, monitoring, alarm tuning, first 100 users | 2 weeks |

**Total: ~16 weeks (4 months)** of focused full-time work for one person, including the learning curve on EKS + CloudFormation. Faster with help or by accepting different infra choices (ECS Fargate + Terraform would shave ~3 weeks).

## 12. Known Risks

| Risk | Mitigation |
|---|---|
| Stripe Checkout / Alipay loading from mainland CN can be flaky due to GFW | Top-up frequency is low (≤1/week typical); document in user FAQ; consider Airwallex post-MVP if persistent |
| Stripe Alipay/WeChat is one-time only — no saved-method auto-topup | Acceptable for MVP; migrate to Airwallex when user friction shows |
| Email deliverability to qq.com / 163.com requires SPF/DKIM/DMARC + warmup | Push GitHub OAuth as primary CTA; provide "different email" fallback |
| Chargeback storm could be costly | $1 trial gated by email-verified or GitHub-OAuth; per-IP signup velocity; Stripe Radar; auto-suspend on dispute |
| Provider format drift breaks translation | Quarterly re-recording of `go-vcr` fixtures; live smoke tests on every deploy |
| Stuck reservations on proxy crash | 60-second reaper; alarm if reservation > 5min unresolved count > 50 |
| Sub-cent rounding favorable to us | Disclosed in ToS; rounds only up to next cent; impact is < $0.01 per request |
| EKS + CloudFormation learning curve extends timeline | Accepted as part of learning goal |
| AWS baseline cost (~$130/mo) higher than Fly.io equivalent (~$30/mo) | Accepted as part of AWS-standardization goal |
| 2FA not in MVP — password compromise drains credits | Limited blast radius (max user balance); add 2FA in v1.1 |
| Single admin user manually promoted via SQL | Acceptable for solo-operator MVP; revisit when team > 1 |

## 13. Out of Scope

Explicitly **not** part of MVP, in approximate priority order for v2+:

- Team / organization accounts (multi-user orgs, shared credits, role-based access)
- 2FA (TOTP, WebAuthn)
- Per-API-key spend / rate limits
- BYOK (bring your own provider key)
- JSON mode / structured output across providers
- Logprobs support
- Embeddings, image generation, audio (in or out)
- Domestic Chinese models (DeepSeek, Kimi, GLM, Qwen)
- Cheap-inference hosts (Groq, DeepInfra, Together, Fireworks)
- Prompt library / templates
- Playground conversation persistence
- Model leaderboards or public benchmarks
- Public usage analytics
- Dark mode
- Magic-link / passwordless login
- WeChat OAuth (requires CN entity)
- IP allowlists per key
- Custom / vanity API domains
- Subscription billing (recurring) instead of pre-paid credits
- Per-region routing / multi-region deployment
- Service mesh (Istio / Linkerd)
- Migration to Airwallex for Alipay/WeChat (if Stripe friction proves unacceptable)

## 14. Open Questions / Future Decisions

- **When to add domestic CN models.** DeepSeek pricing alone is compelling. Add after MVP launch validates demand, or earlier if user research surfaces specific demand.
- **When to migrate Stripe → Airwallex.** Trigger metric: if > 5% of top-up attempts fail at checkout due to loading issues, prioritize migration.
- **When to introduce monthly subscription tiers.** Only if data shows high-volume users would prefer it over flat-markup pre-paid. Not before v2.
- **Whether to add an `inference-worker` deployable.** Current design folds the stuck-reservation reaper into the proxy binary. If we add async jobs (welcome emails, daily rollups, FX rate refresh), promote to a separate `worker` service.
- **Database partitioning strategy.** `request_logs` monthly partitions after ~12 months. Plan now, execute when growth justifies.
- **Region expansion.** If non-CN demand grows, consider `ap-southeast-1` as a second region with cross-region replication.

---

**End of design spec.** Implementation plan to follow via the `writing-plans` skill.
