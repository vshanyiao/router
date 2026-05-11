# Change Memo — Phase 0/1 Code Review Fixes

**Date:** 2026-05-11
**Source:** Code review of PR #1 (Phase 0 Foundation) and PR #2 (Phase 1 Streaming + Multi-Provider)
**Reviewers:** Two independent reviewer subagents (sonnet model)
**Affected branches:** `phase-0-foundation`, `phase-1-streaming-providers`
**Suggested implementation branch:** `phase-1-bugfixes` (stacked on `phase-1-streaming-providers`)

This memo records all issues raised during code review of PR #1 and PR #2, with concrete solutions and effort estimates. Use this as the basis for a follow-up bugfix PR (#3) before either Phase 0 or Phase 1 work merges to `main`.

---

## Priority Summary

| Severity | Count | Should fix by |
|---|---|---|
| 🔴 Critical | 7 | Before any production traffic |
| 🟡 Important | 7 | Before public launch (Phase 6) |
| 🟢 Minor | 9 | Backlog / opportunistic |

---

## 🔴 Critical Issues

### C1 — Gemini streaming always bills $0 (revenue leak)

**Location:** `proxy/internal/provider/gemini/gemini.go:165-185` (`geminiStream.Recv`)

**Root cause:** When `body.Read()` returns `(n>0, io.EOF)` (the typical HTTP/1.1 end-of-stream pattern), the code appends `n` bytes to `s.buf` and then immediately returns the stop event. The final SSE event — which contains `usageMetadata` with `promptTokenCount` and `candidatesTokenCount` — sits unparsed in the buffer. `s.usage` stays at `{0, 0}`. Downstream `handleStream` finalizes with `actualCost=0`, fully refunding the reservation.

**Fix:** After appending bytes from a read that returned `io.EOF`, loop back to the buffer-search at the top so any complete events get drained before emitting the stop event. Only emit stop once the buffer contains no more `\n\n` boundaries.

```go
// Replace the existing structure:
for {
    if idx := bytes.Index(s.buf, []byte("\n\n")); idx >= 0 {
        // ... existing parse + emit content ...
        continue
    }
    n, err := s.body.Read(tmp)
    if n > 0 {
        s.buf = append(s.buf, tmp[:n]...)
    }
    if err != nil {
        if err == io.EOF {
            // Try one more drain of any pending complete events
            if idx := bytes.Index(s.buf, []byte("\n\n")); idx >= 0 {
                continue
            }
            s.done = true
            u := s.usage
            return provider.StreamEvent{Type: "stop", StopReason: "stop", Usage: &u}, io.EOF
        }
        return provider.StreamEvent{}, err
    }
}
```

**Test:** unit test feeding a recorded Gemini stream ending with a `usageMetadata`-bearing event followed by `EOF`; assert `Usage.PromptTokens > 0` in the emitted stop event.

**Effort:** ~30 min

---

### C2 — `billing.Reserve` swallows all DB errors as `ErrInsufficientCredits`

**Location:** `proxy/internal/billing/billing.go:62-65`

**Root cause:**

```go
err = tx.QueryRow(ctx, `UPDATE users SET credits_cents ... RETURNING ...`).Scan(&balanceAfter)
if err != nil {
    return ErrInsufficientCredits  // ← maps every error to this
}
```

`pgx` returns `pgx.ErrNoRows` when the UPDATE affected zero rows (the actual insufficient-credits signal). Any other error — connection drop, timeout, constraint violation — is also collapsed to `ErrInsufficientCredits`. The handler then returns HTTP 402 to the user, masking real outages.

**Fix:**

```go
import "github.com/jackc/pgx/v5"

err = tx.QueryRow(...).Scan(&balanceAfter)
if errors.Is(err, pgx.ErrNoRows) {
    return ErrInsufficientCredits
}
if err != nil {
    return err  // handler logs + 500s
}
```

**Test:** unit test with a mock pool that returns `pgx.ErrNoRows` (assert `ErrInsufficientCredits`) and one that returns an arbitrary error (assert non-`ErrInsufficientCredits` is returned).

**Effort:** ~15 min

---

### C3 — `Finalize` called with cancelled context on client disconnect

**Location:** `proxy/internal/server/openai.go:339` (`handleStream`)

**Root cause:** When the user closes their browser/connection mid-stream, Go's `net/http` cancels `r.Context()`. The streaming loop breaks, then `h.Billing.Finalize(ctx, ...)` is called with that same cancelled ctx. `pg.Begin(ctx)` immediately returns context-cancelled, the reservation never finalizes, and the user's credits stay locked until the reaper sweeps them ~5 minutes later. Same issue with `Reactor.Push` afterward (less serious because Push is non-blocking).

**Fix:** Detach a fresh context for cleanup work after the stream ends. Use `context.WithoutCancel` (Go 1.21+) if available, or `context.Background` with a derived deadline.

```go
// At the top of handleStream, save a detached context for cleanup:
cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
defer cleanupCancel()

// Later, replace the Finalize call:
_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, actual.TotalCents)
```

Apply same `cleanupCtx` to the non-streaming Finalize calls in the error paths (line 168) for consistency.

**Test:** integration test that opens a stream, cancels the request context mid-flight, asserts the reservation finalizes within 1s (not 5 min via reaper).

**Effort:** ~20 min

---

### C4 — Reaper's UPDATE + INSERT are not transactional

**Location:** `proxy/internal/reaper/reaper.go:69-76`

**Root cause:** Two separate `Exec` calls execute the balance restoration and the audit row insertion. Errors are silently discarded (`_, _ = ...`). Failure modes:

1. UPDATE succeeds, INSERT fails → balance restored without ledger row → next sweep finds same reservation, refunds it again = **double credit**
2. The INSERT's `balance_after_cents` uses `SELECT credits_cents FROM users WHERE id = $1` as a subquery — concurrent requests modifying balance between UPDATE and INSERT make the snapshot incorrect

**Fix:** Single transaction, capture balance via UPDATE's RETURNING clause, propagate errors.

```go
for _, s := range stucks {
    tx, err := r.pg.Begin(ctx)
    if err != nil {
        log.Printf("reaper: begin tx: %v", err)
        continue
    }
    var balanceAfter int64
    err = tx.QueryRow(ctx, `
        UPDATE users SET credits_cents = credits_cents + $1
        WHERE id = $2 RETURNING credits_cents
    `, s.RefundAmount, s.UserID).Scan(&balanceAfter)
    if err != nil {
        log.Printf("reaper: update balance for user %s: %v", s.UserID, err)
        tx.Rollback(ctx)
        continue
    }
    _, err = tx.Exec(ctx, `
        INSERT INTO credit_transactions
            (id, user_id, amount_cents, kind, request_id, balance_after_cents, description, created_at)
        VALUES (gen_random_uuid(), $1, $2, 'refund', $3, $4, 'reaper: stuck reservation', now())
    `, s.UserID, s.RefundAmount, s.RequestID, balanceAfter)
    if err != nil {
        log.Printf("reaper: insert refund txn for user %s: %v", s.UserID, err)
        tx.Rollback(ctx)
        continue
    }
    if err := tx.Commit(ctx); err != nil {
        log.Printf("reaper: commit for user %s: %v", s.UserID, err)
        continue
    }
    log.Printf("reaper: refunded %d cents to user %s for stuck request %s", s.RefundAmount, s.UserID, s.RequestID)
}
```

**Test:** integration test that inserts a stuck reservation row, runs `sweep` once, asserts exactly one refund row created, balance updated, sweep again is a no-op.

**Effort:** ~30 min

---

### C5 — Email verify + trial credit grant race

**Location:** `web/src/app/api/auth/verify-email/route.ts:11-28`

**Root cause:** Token lookup, user update, token delete, and credit grant are sequential awaited statements without a single transaction. A user double-clicking the verify link can pass two concurrent requests through the existence check before either deletes the token. Two `grantTrialCreditIfEligible` calls then race; the per-user DB check is a read-before-write with no serialization. Mitigated in practice by the per-IP Redis SETNX, but if the second request originates from a different IP (load balancer, VPN), both grants succeed.

**Fix:** Wrap the verify+grant in a Prisma transaction with the token delete as the first statement. The unique-constraint delete acts as the serialization point.

```typescript
import { Prisma } from '@prisma/client'

export async function POST(req: NextRequest) {
  const { token } = await req.json().catch(() => ({}))
  if (!token || typeof token !== 'string') {
    return NextResponse.json({ error: 'Missing token' }, { status: 400 })
  }

  const ip = req.headers.get('x-forwarded-for')?.split(',')[0].trim() || '0.0.0.0'

  let userId: string | null = null
  try {
    userId = await prisma.$transaction(async (tx) => {
      // Delete-first; if already deleted, second concurrent request fails here.
      let deleted
      try {
        deleted = await tx.verificationToken.delete({ where: { token } })
      } catch (e) {
        if (e instanceof Prisma.PrismaClientKnownRequestError && e.code === 'P2025') {
          throw new Error('TOKEN_NOT_FOUND')
        }
        throw e
      }
      if (deleted.expires < new Date()) throw new Error('TOKEN_EXPIRED')

      const user = await tx.user.findUnique({ where: { email: deleted.identifier } })
      if (!user) throw new Error('USER_NOT_FOUND')

      await tx.user.update({
        where: { id: user.id },
        data: { emailVerifiedAt: new Date() },
      })
      return user.id
    })
  } catch (e) {
    const msg = (e as Error).message
    return NextResponse.json({ error: msg === 'TOKEN_EXPIRED' ? 'Token expired' : 'Invalid token' }, { status: 400 })
  }

  // Grant trial credit OUTSIDE the verify transaction — it has its own dedup.
  await grantTrialCreditIfEligible(userId!, ip)
  return NextResponse.json({ ok: true })
}
```

**Test:** integration test firing two concurrent verify requests with the same token; assert exactly one succeeds and exactly one `trial_credit` row exists.

**Effort:** ~45 min (the transaction restructure + error handling)

---

### C6 — Partial stream on provider error gives the user free tokens

**Location:** `proxy/internal/server/openai.go:316-323, 326-345`

**Root cause:** If `reader.Recv()` returns a non-EOF error after some content deltas have already been streamed to the client, the loop breaks with `finalUsage == nil`. The code calls `Finalize(ctx, ..., maxCost, 0)` → full refund. The user received real generated tokens (which we paid the upstream for), but pays nothing. Additionally, the `request_log` status is hardcoded `"success"` regardless of how the loop exited.

**Fix:** Track a running completion-token count as content deltas are streamed (best-effort proxy-side, since the provider may not send per-chunk usage). Use the higher of running estimate vs. `finalUsage` for billing. Also propagate a real status.

```go
streamStatus := "success"
var streamedChars int
var finalUsage *provider.Usage

for {
    evt, recvErr := reader.Recv()
    if evt.Type == "content" && evt.ContentDelta != "" {
        streamedChars += len(evt.ContentDelta)
        chunk := streamingChunk{ ... }
        if writeErr := sse.SendJSON(chunk); writeErr != nil {
            log.Printf("ERROR: sse write: %v", writeErr)
            streamStatus = "cancelled"
            break
        }
    }
    if evt.Usage != nil {
        finalUsage = evt.Usage
    }
    if recvErr == io.EOF { break }
    if recvErr != nil {
        log.Printf("ERROR: stream recv: %v", recvErr)
        streamStatus = "provider_error"
        break
    }
}

// Estimate completion tokens if upstream didn't report usage
var promptTokens, completionTokens int
if finalUsage != nil {
    promptTokens = finalUsage.PromptTokens
    completionTokens = finalUsage.CompletionTokens
} else {
    promptTokens = int(estPromptTokens)             // from earlier reservation
    completionTokens = streamedChars / 4            // crude fallback
}
```

And in the Reactor.Push call, replace `Status: "success"` with `Status: streamStatus`.

**Test:** unit test of `handleStream` with a fake stream that emits 2 content events then returns a non-EOF error; assert `actualCost > 0` was finalized and request_log status is `"provider_error"`.

**Effort:** ~45 min

---

### C7 — Gemini API key embedded in URL query string

**Location:**
- `proxy/internal/provider/gemini/gemini.go:74` (`generateContent`)
- `proxy/internal/provider/gemini/gemini.go:109` (`streamGenerateContent`)
- `proxy/internal/tokenizer/gemini_tokenizer.go:56` (`countTokens`)

**Root cause:** Google's API supports `?key=` in URL or `X-Goog-Api-Key` in header. URL form leaks the key to any access log, load balancer, CDN, or HTTP trace that records URLs.

**Fix:** Move key to header in all three places.

```go
// Before:
url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, req.Model, apiKey)
r, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
r.Header.Set("Content-Type", "application/json")

// After:
url := fmt.Sprintf("%s/%s:generateContent", baseURL, req.Model)
r, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
r.Header.Set("Content-Type", "application/json")
r.Header.Set("X-Goog-Api-Key", apiKey)
```

Repeat for the streaming endpoint (keep `?alt=sse`) and the countTokens endpoint.

**Effort:** ~10 min

---

## 🟡 Important Issues

### I1 — Anthropic tokenizer converts system role to user

**Location:** `proxy/internal/tokenizer/anthropic_tokenizer.go:41-43`

**Root cause:** Anthropic's `count_tokens` endpoint accepts a top-level `system` field (matching the inference call). The tokenizer instead converts system messages to `user` role in the `messages` array. This creates two problems: (1) it can produce non-alternating user/assistant role sequences, which the Anthropic API rejects with a 400; (2) the token count for system-in-user differs from system-in-system-field, so reservation diverges from actual charged cost.

**Fix:** Mirror `buildBody` from the inference adapter: extract `system` to its own field, only pass user/assistant turns in the messages array.

**Effort:** ~20 min

### I2 — Gemini tokenizer converts system role to user

**Location:** `proxy/internal/tokenizer/gemini_tokenizer.go:46-48`

**Root cause:** Same class of bug. Gemini's inference call uses `systemInstruction` for the system role; the tokenizer flattens it to `user`.

**Fix:** Mirror `buildBody` from `gemini.go`: send system content as `systemInstruction` in the countTokens request payload.

**Effort:** ~20 min

### I3 — Anthropic SSE scanner uses default 64KB buffer

**Location:** `proxy/internal/provider/anthropic/anthropic.go:118`

**Root cause:** `bufio.NewScanner(resp.Body)` uses the default 64KB max per token (per line). Anthropic sends each SSE event as one `data: ` line; a single content_block_delta containing a long code block can exceed 64KB → `scanner.Err() == bufio.ErrTooLong` → stream silently terminates mid-response.

**Fix:** Increase scanner buffer to 1MB right after creation.

```go
scanner := bufio.NewScanner(resp.Body)
scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
return &anthropicStream{body: resp.Body, scanner: scanner}, nil
```

**Effort:** ~5 min

### I4 — Reactor's shutdown drain has only 200ms grace period

**Location:** `proxy/cmd/proxy/main.go:114-120`

**Root cause:** After `cancel()` is called, `main` sleeps 200ms then returns. The Reactor's drain loop runs in `ctx.Done()` branch — it drains the inbox channel and flushes — but has no synchronization back to main. Under load (e.g., 10k entries in the inbox), 200ms is not enough to flush.

**Fix:** Use a WaitGroup or done channel; main waits for actual reactor completion.

```go
// In main:
reactorDone := make(chan struct{})
go func() { defer close(reactorDone); reactor.Run(ctx) }()
// ...
<-sigs
log.Println("shutting down")
_ = srv.Shutdown(shutdownCtx)
cancel()
<-reactorDone  // wait for actual drain
```

**Effort:** ~15 min

### I5 — Admin suspend doesn't propagate to proxy (5-minute window of free service)

**Location:** `proxy/internal/auth/auth.go:38` (cacheTTL) and missing invalidation paths

**Root cause:** The Redis auth cache (`key:{hmac}` → `{user_id, credits_cents, status}`) has 5-minute TTL. When an admin suspends a user (writes `users.status='suspended'` via web), the proxy keeps accepting requests for up to 5 minutes. The web app correctly invalidates on key revoke (`web/src/app/api/keys/[id]/route.ts:23`) but no path invalidates on user status change.

**Fix options (pick one for Phase 1 bugfixes):**
1. Short-term: reduce `cacheTTL` from 5min to 30s. Trade more DB load for faster propagation.
2. Better: on suspension (Phase 4 admin panel), iterate the user's `api_keys`, compute their HMACs, and `DEL key:{hmac}` for each. Requires a helper in web/lib.
3. Best: store a per-user invalidation flag in Redis with longer TTL; proxy checks it on cache hit before trusting the cached status.

For Phase 1 bugfixes, do option 1 (drop TTL to 30s). Revisit when Phase 4 builds the admin panel.

**Effort:** ~5 min (option 1) / ~1 hour (option 2)

### I6 — `http.DefaultClient` has no timeout

**Location:**
- `proxy/internal/provider/anthropic/anthropic.go:23`
- `proxy/internal/provider/gemini/gemini.go:19`
- `proxy/internal/tokenizer/anthropic_tokenizer.go:17`
- `proxy/internal/tokenizer/gemini_tokenizer.go:17`

**Root cause:** A hung upstream connection holds a goroutine indefinitely (Go's default kernel-level TCP timeout is minutes).

**Fix:** Use a shared timed client in each adapter:

```go
var defaultHTTPClient = &http.Client{Timeout: 90 * time.Second}

func New() *Adapter { return &Adapter{HTTP: defaultHTTPClient} }
```

(For streaming requests, 90s is too short — set timeout on the request context instead, or use no timeout on the client and rely on ctx propagation from the handler.)

**Effort:** ~15 min

### I7 — `handleStream` always logs status as `"success"`

**Location:** `proxy/internal/server/openai.go:351`

**Root cause:** Covered by C6's fix (track `streamStatus`). Listed separately because the request_log status is also useful even when no revenue impact exists — e.g., to detect provider degradation.

**Fix:** Combined with C6 fix.

**Effort:** Covered in C6

### I8 — Signup endpoint leaks account existence

**Location:** `web/src/app/api/signup/route.ts:22-24`

**Root cause:** Returning HTTP 409 with `"Email already registered"` lets an attacker enumerate valid emails.

**Fix:** Return HTTP 200 with the same "Check your email for verification link" message whether the account is new or existing. Don't actually send any email or create any record in the existing-account branch.

```typescript
if (existing) {
  return NextResponse.json({
    ok: true,
    message: 'Check your email for the verification link.',
  })
}
```

**Effort:** ~10 min

---

## 🟢 Minor Issues (backlog)

### M1 — `stopReason` hardcoded to `"stop"` in final SSE chunk
**Location:** `proxy/internal/server/openai.go:321`
**Fix:** Propagate `StreamEvent.StopReason` from the stop event through to `FinishReason`.
**Effort:** ~15 min

### M2 — O(n²) reaper query (`NOT IN` subquery)
**Location:** `proxy/internal/reaper/reaper.go:38-47`
**Fix:** Rewrite as `NOT EXISTS` correlated subquery + add index `CREATE INDEX idx_credit_tx_kind_request ON credit_transactions(kind, request_id) WHERE request_id IS NOT NULL`. Defer until volume justifies.
**Effort:** ~30 min

### M3 — Provider tests only check constructors
**Location:** `proxy/internal/provider/anthropic/anthropic_test.go`, `gemini_test.go`
**Fix:** Add table-driven tests for `*Stream.Recv()` using `io.NopCloser(bytes.NewReader(...))` as the response body. Cover: clean EOF, EOF-with-leftover-bytes, partial reads, large content, malformed JSON.
**Effort:** ~1.5 hours

### M4 — Magic numbers (Reactor flush/batch, Reaper tick/threshold)
**Locations:** `proxy/internal/logging/reactor.go:45-46`, `proxy/cmd/proxy/main.go:56`
**Fix:** Move to `app_config` table reads on startup, or to env vars. The 5-minute reaper threshold is particularly relevant — a slow Gemini generation could exceed it and get prematurely refunded mid-flight. Bump to 10-15 min once admin panel exists.
**Effort:** ~30 min

### M5 — `docker-compose.yml` omits Anthropic + Gemini keys from proxy service
**Location:** `docker-compose.yml:65-73` (proxy service env)
**Fix:** Add `ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}` and `GEMINI_API_KEY: ${GEMINI_API_KEY}` to the proxy service. Phase 1 seeds Anthropic/Gemini models, so without these keys, calls to those models 500 with "provider key not configured."
**Effort:** ~5 min

### M6 — `generateApiKey` has modulo bias
**Location:** `web/src/lib/api-keys.ts:8-11`
**Fix:** Use rejection sampling (~190 bits effective entropy → ~192+ bits with rejection sampling). Academic — not a real security risk for 32-char keys, but worth fixing if doing other crypto polish.
**Effort:** ~20 min

### M7 — `len(content)` byte-count for non-Latin fallback tokenizer
**Location:** `proxy/internal/server/openai.go:134-138`
**Fix:** The 4-chars-per-token fallback massively underestimates CJK text (3 bytes per Chinese character ≈ 0.6 tokens, so `bytes/4` underestimates by ~5×). Since the target market is mainland China, this is the common case. Replace `len(m.Content)` with `utf8.RuneCountInString(m.Content)` and adjust ratio to `runes / 2.5` for CJK-heavy content. Or just over-reserve aggressively (e.g., `runes * 1.5`) on the fallback path. Best: don't ever fall back — make tokenizer failures hard errors instead of silent degradation.
**Effort:** ~20 min

### M8 — `openai.go` (server package) is 350+ lines
**Location:** `proxy/internal/server/openai.go`
**Fix:** Extract `handleStream` and its helper types to `proxy/internal/server/openai_stream.go` (same package). Trivial mechanical move; reduces cognitive load.
**Effort:** ~15 min

### M9 — Catalog auto-refresh goroutine leak
**Location:** `proxy/internal/catalog/catalog.go:89`, `proxy/cmd/proxy/main.go`
**Fix:** The catalog's `StartAutoRefresh` goroutine listens on a `stop` channel that's never closed (main only calls `cancel()`, which doesn't notify the catalog). Refactor `StartAutoRefresh` to use the context's `Done` channel instead of the separate `stop` channel. Then drop the `Stop` method.
**Effort:** ~15 min

---

## Sequencing

Recommended order for a `phase-1-bugfixes` PR:

**Round 1 (quick wins, ~1.5 hours):**
- C7 Gemini key in URL → header
- C2 Reserve error mapping
- C5 Email verify race
- I3 Anthropic scanner buffer
- I5 (option 1) Cache TTL to 30s
- I6 HTTP timeouts
- I8 Signup enumeration

**Round 2 (the streaming/billing fixes, ~2.5 hours):**
- C1 Gemini buffer drain
- C3 Detached context for Finalize
- C6 + I7 Partial stream tracking + status propagation
- C4 Reaper transactional refund

**Round 3 (tokenizer fixes, ~45 min):**
- I1 Anthropic tokenizer system handling
- I2 Gemini tokenizer system handling
- I4 Reactor shutdown WaitGroup

**Round 4 (polish, optional before merge — could defer to next PR):**
- M1, M5, M8, M9 — small, mechanical, low-risk

**Total Round 1+2+3 effort: ~5 hours of focused work.**

## Verification plan

After all Round 1-3 fixes:

1. **Unit tests:** add the tests called out in each fix (Gemini Recv with EOF+data, Reserve error mapping, partial stream tracking, reaper concurrency)
2. **Integration smoke test:** the existing `scripts/smoke-test.sh` should still pass for all three providers in both streaming and non-streaming modes
3. **Manual disconnect test:** open a streaming request via `curl`, kill it mid-stream with Ctrl+C, verify the credit balance returns to its pre-request value within 1 second (not 5 minutes)
4. **Manual concurrent verify test:** double-click an email verification link; verify only one trial credit is granted
5. **`docker compose logs proxy`:** no Gemini API keys visible in URL logs

## Open questions

- **Suspension propagation strategy (I5):** option 1 (short TTL) is quick but loads the DB. Should we plan option 2 (active invalidation on suspend) for Phase 4 (admin panel) instead? Decision: defer to Phase 4 plan.
- **CJK fallback tokenizer (M7):** should the fallback even exist, or should tokenizer failure be a hard 500? Argument for hard failure: silent underestimation lets users go slightly negative. Argument for fallback: provider tokenizer endpoint may be flaky. Decision: keep fallback but use rune count + 1.5× multiplier to over-reserve.
- **Reaper threshold (M4):** 5 min is too short for slow Gemini Pro responses with large prompts. Bump to 10 min as a band-aid; revisit once Phase 4 admin panel provides observability into actual finalize latencies.

---

**End of memo.** When Round 1-3 fixes are implemented, this memo can be closed and the issues marked as resolved in the PR descriptions.
