# MaaS Router — Phase 0: Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a working end-to-end MaaS Router that lets a user sign up (email or GitHub), receive a $1 trial credit, create an API key, and successfully call `POST /v1/chat/completions` against GPT-4o (non-streaming) with full credit accounting.

**Architecture:** Two-service split. `web` (Next.js + Prisma + NextAuth + Tailwind/shadcn) handles UI, auth, and key management. `proxy` (Go + pgx + go-redis) handles inference at `/v1/chat/completions`. They share Postgres and Redis. Local development runs everything in `docker compose up`. Reference design spec: `docs/superpowers/specs/2026-05-11-maas-router-design.md`.

**Tech Stack:**
- Frontend: Next.js 14+ (App Router), TypeScript, Tailwind CSS, shadcn/ui, Prisma, NextAuth v5 (Auth.js), bcryptjs, Resend (email)
- Backend: Go 1.22+, pgx/v5 (Postgres), go-redis/v9, sashabaranov/go-openai, testify
- Infrastructure: Docker Compose (postgres:16, redis:7-alpine), Make
- Spec reference: §3 (architecture), §4 (data model), §5 (request flow), §9 (auth)

**Out of scope for Phase 0** (deferred to later phases per spec §11):
- Streaming (Phase 1)
- Anthropic + Google providers (Phase 1)
- Anthropic API surface `/v1/messages` (Phase 2)
- Stripe / top-up / billing UI (Phase 3)
- Admin panel (Phase 4)
- CAPTCHA, rate limiting, abuse prevention (Phase 4)
- Bilingual UI (Phase 5)
- Playground (Phase 5)
- AWS / EKS / CloudFormation (Phase 6)

---

## File Structure

```
maas-router/
├── .env.example
├── .gitignore                          (extend existing)
├── Makefile                            NEW
├── docker-compose.yml                  NEW
├── README.md                           NEW
├── docs/                               (existing)
│
├── web/                                NEW — Next.js app
│   ├── .env.example
│   ├── Dockerfile
│   ├── next.config.mjs
│   ├── package.json
│   ├── tsconfig.json
│   ├── tailwind.config.ts
│   ├── postcss.config.js
│   ├── components.json                 (shadcn config)
│   ├── prisma/
│   │   ├── schema.prisma               9 tables from spec §4.1
│   │   └── seed.ts                     seed 1 model + app_config
│   └── src/
│       ├── app/
│       │   ├── layout.tsx
│       │   ├── page.tsx                landing
│       │   ├── globals.css
│       │   ├── signup/page.tsx
│       │   ├── login/page.tsx
│       │   ├── auth/verify-email/[token]/page.tsx
│       │   ├── dashboard/
│       │   │   ├── layout.tsx          auth-gated
│       │   │   ├── page.tsx            balance + onboarding
│       │   │   └── keys/page.tsx
│       │   └── api/
│       │       ├── auth/[...nextauth]/route.ts
│       │       ├── signup/route.ts
│       │       ├── auth/verify-email/route.ts
│       │       ├── keys/route.ts       GET (list) + POST (create)
│       │       ├── keys/[id]/route.ts  DELETE (revoke)
│       │       └── credits/balance/route.ts
│       ├── lib/
│       │   ├── db.ts                   Prisma client singleton
│       │   ├── redis.ts                Redis client singleton
│       │   ├── auth.ts                 NextAuth config
│       │   ├── api-keys.ts             key gen, HMAC hash
│       │   ├── credits.ts              trial grant logic
│       │   ├── email.ts                Resend wrapper
│       │   └── env.ts                  env validation w/ Zod
│       ├── middleware.ts               dashboard auth gate
│       └── components/
│           ├── ui/                     shadcn primitives
│           ├── signup-form.tsx
│           ├── login-form.tsx
│           ├── balance-widget.tsx
│           └── create-key-modal.tsx
│
└── proxy/                              NEW — Go service
    ├── .air.toml                       hot reload
    ├── Dockerfile
    ├── go.mod
    ├── cmd/proxy/main.go
    └── internal/
        ├── server/
        │   ├── server.go               HTTP routing
        │   ├── health.go
        │   ├── openai.go               POST /v1/chat/completions
        │   └── openai_test.go
        ├── auth/
        │   ├── auth.go                 key lookup (Redis → PG)
        │   └── auth_test.go
        ├── billing/
        │   ├── billing.go              reserve + finalize
        │   └── billing_test.go
        ├── catalog/
        │   ├── catalog.go              in-memory model cache
        │   └── catalog_test.go
        ├── provider/
        │   ├── provider.go             interface
        │   └── openai/
        │       ├── openai.go           OpenAI adapter (non-streaming)
        │       └── openai_test.go
        ├── logging/
        │   ├── reactor.go              Reactor pattern goroutine
        │   └── reactor_test.go
        └── storage/
            ├── postgres.go             pgx pool
            └── redis.go                go-redis client
```

---

## Group A: Repository scaffolding

### Task 1: Create root files

**Files:**
- Create: `Makefile`
- Create: `README.md`
- Create: `.env.example`
- Modify: `.gitignore`

- [ ] **Step 1: Create Makefile**

```makefile
.PHONY: up down logs seed migrate test clean

up:
	docker compose up

down:
	docker compose down

logs:
	docker compose logs -f $(service)

seed:
	docker compose exec web pnpm prisma db seed

migrate:
	docker compose exec web pnpm prisma migrate dev

migrate-new:
	docker compose exec web pnpm prisma migrate dev --name $(name)

reset-db:
	docker compose exec web pnpm prisma migrate reset --force

test-web:
	cd web && pnpm test

test-proxy:
	cd proxy && go test ./...

test: test-web test-proxy

clean:
	docker compose down -v
	rm -rf web/node_modules web/.next proxy/tmp
```

- [ ] **Step 2: Create README.md**

```markdown
# MaaS Router

Reseller MaaS platform offering frontier LLM access to mainland China users.

## Quick start

1. Copy `.env.example` to `.env` and fill in values (or use defaults for local dev)
2. `make up` — starts postgres, redis, web (Next.js), proxy (Go)
3. `make migrate` — applies database migrations
4. `make seed` — seeds initial model catalog and app config
5. Open http://localhost:3000

## Architecture

See `docs/superpowers/specs/2026-05-11-maas-router-design.md`.

## Phase 0

Email/password + GitHub signup, $1 trial credit, API key management,
single model (GPT-4o non-streaming) end-to-end.
```

- [ ] **Step 3: Create root .env.example**

```bash
# Database
DATABASE_URL=postgres://app:dev@postgres:5432/maas

# Redis
REDIS_URL=redis://redis:6379

# NextAuth — generate with: openssl rand -base64 32
NEXTAUTH_URL=http://localhost:3000
NEXTAUTH_SECRET=replace-me-openssl-rand-base64-32

# GitHub OAuth — create at https://github.com/settings/applications/new
# Authorization callback URL: http://localhost:3000/api/auth/callback/github
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=

# Resend (email) — https://resend.com/api-keys; use test key for dev
RESEND_API_KEY=
EMAIL_FROM=noreply@example.com

# OpenAI provider key
OPENAI_API_KEY=

# Proxy — generate with: openssl rand -hex 32
HMAC_SERVER_SECRET=replace-me-openssl-rand-hex-32

# Public URL used by proxy in welcome emails / receipts (Phase 3+)
PUBLIC_URL=http://localhost:3000
```

- [ ] **Step 4: Extend .gitignore**

Append to existing `.gitignore`:

```gitignore

# Node / Next.js
node_modules/
.next/
*.log
.turbo/

# Go
proxy/tmp/
proxy/bin/
*.test

# Env files
.env
.env.local
.env.*.local

# Database
*.db
*.sqlite

# IDE
*.swp
*.swo
```

- [ ] **Step 5: Commit**

```bash
git add Makefile README.md .env.example .gitignore
git commit -m "scaffold: add Makefile, README, env example"
```

---

### Task 2: Create docker-compose.yml

**Files:**
- Create: `docker-compose.yml`

- [ ] **Step 1: Write docker-compose.yml**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: dev
      POSTGRES_DB: maas
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app -d maas"]
      interval: 2s
      timeout: 2s
      retries: 10

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 2s
      timeout: 2s
      retries: 10

  web:
    build:
      context: ./web
      dockerfile: Dockerfile
    command: pnpm dev
    volumes:
      - ./web:/app
      - /app/node_modules
      - /app/.next
    ports:
      - "3000:3000"
    environment:
      DATABASE_URL: postgres://app:dev@postgres:5432/maas
      REDIS_URL: redis://redis:6379
      NEXTAUTH_URL: ${NEXTAUTH_URL}
      NEXTAUTH_SECRET: ${NEXTAUTH_SECRET}
      GITHUB_CLIENT_ID: ${GITHUB_CLIENT_ID}
      GITHUB_CLIENT_SECRET: ${GITHUB_CLIENT_SECRET}
      RESEND_API_KEY: ${RESEND_API_KEY}
      EMAIL_FROM: ${EMAIL_FROM}
      HMAC_SERVER_SECRET: ${HMAC_SERVER_SECRET}
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

  proxy:
    build:
      context: ./proxy
      dockerfile: Dockerfile
    command: air
    volumes:
      - ./proxy:/app
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://app:dev@postgres:5432/maas
      REDIS_URL: redis://redis:6379
      HMAC_SERVER_SECRET: ${HMAC_SERVER_SECRET}
      OPENAI_API_KEY: ${OPENAI_API_KEY}
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

volumes:
  pgdata:
```

- [ ] **Step 2: Verify postgres + redis start cleanly**

Create `.env` from `.env.example` (set placeholder values for the secrets — the apps don't start yet). Then:

```bash
docker compose up postgres redis
```

Expected: both reach a healthy state within ~5 seconds. Logs show `database system is ready to accept connections` (postgres) and `Ready to accept connections` (redis). Ctrl+C to stop.

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "infra: add docker-compose with postgres + redis"
```

---

## Group B: Web app foundation (Next.js + Prisma + shadcn)

### Task 3: Initialize Next.js app

**Files:**
- Create: `web/package.json`, `web/tsconfig.json`, `web/next.config.mjs`, etc. (via create-next-app)
- Create: `web/Dockerfile`

- [ ] **Step 1: Scaffold Next.js**

```bash
cd web 2>/dev/null || mkdir web && cd web
pnpm create next-app@latest . --typescript --app --tailwind --eslint --no-src-dir=false --import-alias "@/*"
```

When prompted, answer:
- TypeScript: Yes (already set)
- ESLint: Yes
- Tailwind: Yes
- `src/` directory: Yes
- App Router: Yes (default)
- import alias: `@/*` (default)
- Turbopack for `next dev`: Yes

- [ ] **Step 2: Create web/Dockerfile**

```dockerfile
FROM node:20-alpine

WORKDIR /app

RUN corepack enable && corepack prepare pnpm@latest --activate

COPY package.json pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install

COPY . .

EXPOSE 3000

CMD ["pnpm", "dev"]
```

- [ ] **Step 3: Verify the web container builds**

From repo root:

```bash
docker compose build web
docker compose up web postgres redis
```

Expected: `web` logs show `Ready in <Xs>` and `Local: http://localhost:3000`. Open http://localhost:3000 in your browser — you should see the Next.js welcome page. Ctrl+C to stop.

- [ ] **Step 4: Commit**

```bash
cd .. && git add web/
git commit -m "web: scaffold Next.js app with TypeScript + Tailwind"
```

---

### Task 4: Install web dependencies

**Files:**
- Modify: `web/package.json`

- [ ] **Step 1: Install runtime deps**

```bash
cd web
pnpm add @prisma/client @auth/prisma-adapter next-auth@beta bcryptjs resend zod react-hook-form @hookform/resolvers lucide-react ioredis
pnpm add -D prisma @types/bcryptjs tsx vitest @testing-library/react @testing-library/jest-dom jsdom
```

- [ ] **Step 2: Add prisma init**

```bash
pnpm prisma init --datasource-provider postgresql
```

This creates `prisma/schema.prisma` and adds `DATABASE_URL` to a `.env` (we'll override via docker-compose env).

- [ ] **Step 3: Initialize shadcn/ui**

```bash
pnpm dlx shadcn@latest init
```

Answer:
- TypeScript: Yes
- Style: Default
- Base color: Slate
- CSS variables: Yes

Then add the components we'll need in Phase 0:

```bash
pnpm dlx shadcn@latest add button input label card dialog table badge form alert
```

- [ ] **Step 4: Commit**

```bash
cd ..
git add web/
git commit -m "web: install Prisma, NextAuth, bcrypt, Resend, shadcn/ui"
```

---

### Task 5: Add Prisma schema with all 9 tables

**Files:**
- Modify: `web/prisma/schema.prisma`

- [ ] **Step 1: Replace prisma/schema.prisma with full schema**

```prisma
generator client {
  provider = "prisma-client-js"
}

datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model User {
  id              String    @id @default(uuid()) @db.Uuid
  email           String    @unique
  passwordHash    String?   @map("password_hash")
  githubId        String?   @unique @map("github_id")
  emailVerifiedAt DateTime? @map("email_verified_at")
  locale          String    @default("zh-CN")
  creditsCents    BigInt    @default(0) @map("credits_cents")
  status          String    @default("active") // 'active' | 'suspended' | 'banned'
  isAdmin         Boolean   @default(false) @map("is_admin")
  createdAt       DateTime  @default(now()) @map("created_at")
  updatedAt       DateTime  @updatedAt @map("updated_at")

  accounts            Account[]
  sessions            Session[]
  apiKeys             ApiKey[]
  creditTransactions  CreditTransaction[]
  paymentIntents      PaymentIntent[]
  requestLogs         RequestLog[]
  auditLogs           AuditLog[] @relation("AuditActor")
  targetedAuditLogs   AuditLog[] @relation("AuditTarget")

  @@map("users")
}

model Account {
  id                String  @id @default(cuid())
  userId            String  @map("user_id") @db.Uuid
  type              String
  provider          String
  providerAccountId String  @map("provider_account_id")
  refresh_token     String? @db.Text
  access_token      String? @db.Text
  expires_at        Int?
  token_type        String?
  scope             String?
  id_token          String? @db.Text
  session_state     String?

  user User @relation(fields: [userId], references: [id], onDelete: Cascade)

  @@unique([provider, providerAccountId])
  @@map("accounts")
}

model Session {
  id           String   @id @default(cuid())
  sessionToken String   @unique @map("session_token")
  userId       String   @map("user_id") @db.Uuid
  expires      DateTime
  user         User     @relation(fields: [userId], references: [id], onDelete: Cascade)

  @@map("sessions")
}

model VerificationToken {
  identifier String
  token      String   @unique
  expires    DateTime

  @@unique([identifier, token])
  @@map("verification_tokens")
}

model ApiKey {
  id          String    @id @default(uuid()) @db.Uuid
  userId      String    @map("user_id") @db.Uuid
  name        String
  keyPrefix   String    @map("key_prefix")
  keyHash     String    @unique @map("key_hash")
  lastUsedAt  DateTime? @map("last_used_at")
  createdAt   DateTime  @default(now()) @map("created_at")
  revokedAt   DateTime? @map("revoked_at")

  user        User      @relation(fields: [userId], references: [id], onDelete: Cascade)

  @@index([userId, revokedAt])
  @@map("api_keys")
}

model CreditTransaction {
  id                 String    @id @default(uuid()) @db.Uuid
  userId             String    @map("user_id") @db.Uuid
  amountCents        BigInt    @map("amount_cents")
  kind               String    // 'topup' | 'reservation' | 'consumption' | 'refund' | 'adjustment' | 'trial_credit' | 'chargeback'
  requestId          String?   @map("request_id") @db.Uuid // NOT a FK; eventual consistency w/ Reactor
  paymentIntentId    String?   @map("payment_intent_id") @db.Uuid
  balanceAfterCents  BigInt    @map("balance_after_cents")
  description        String?
  createdAt          DateTime  @default(now()) @map("created_at")

  user            User           @relation(fields: [userId], references: [id])
  paymentIntent   PaymentIntent? @relation(fields: [paymentIntentId], references: [id])

  @@index([userId, createdAt])
  @@map("credit_transactions")
}

model PaymentIntent {
  id                       String    @id @default(uuid()) @db.Uuid
  userId                   String    @map("user_id") @db.Uuid
  stripePaymentIntentId    String?   @unique @map("stripe_payment_intent_id")
  stripeCheckoutSessionId  String?   @unique @map("stripe_checkout_session_id")
  amountCents              BigInt    @map("amount_cents")
  creditsAddedCents        BigInt    @map("credits_added_cents")
  currency                 String    @default("usd")
  status                   String    // 'pending' | 'succeeded' | 'failed' | 'expired'
  createdAt                DateTime  @default(now()) @map("created_at")
  completedAt              DateTime? @map("completed_at")

  user        User                 @relation(fields: [userId], references: [id])
  transactions CreditTransaction[]

  @@map("payment_intents")
}

model RequestLog {
  id                  String    @id @default(uuid()) @db.Uuid
  userId              String    @map("user_id") @db.Uuid
  apiKeyId            String    @map("api_key_id") @db.Uuid
  apiSurface          String    @map("api_surface") // 'openai' | 'anthropic'
  upstreamProvider    String    @map("upstream_provider") // 'openai' | 'anthropic' | 'google'
  upstreamModel       String    @map("upstream_model")
  modelAlias          String    @map("model_alias")
  promptTokens        Int?      @map("prompt_tokens")
  completionTokens    Int?      @map("completion_tokens")
  inputCostCents      Int       @default(0) @map("input_cost_cents")
  outputCostCents     Int       @default(0) @map("output_cost_cents")
  marginCents         Int       @default(0) @map("margin_cents")
  totalChargedCents   Int       @default(0) @map("total_charged_cents")
  streaming           Boolean   @default(false)
  status              String    // 'success' | 'provider_error' | 'cancelled' | 'insufficient_credits' | 'rate_limited'
  errorMessage        String?   @map("error_message")
  latencyMs           Int?      @map("latency_ms")
  createdAt           DateTime  @map("created_at")
  completedAt         DateTime? @map("completed_at")

  user    User    @relation(fields: [userId], references: [id])

  @@index([userId, createdAt])
  @@index([createdAt])
  @@map("request_logs")
}

model ModelCatalog {
  id                            String   @id @default(uuid()) @db.Uuid
  alias                         String   @unique
  displayName                   String   @map("display_name")
  upstreamProvider              String   @map("upstream_provider")
  upstreamModelId               String   @map("upstream_model_id")
  contextWindow                 Int      @map("context_window")
  supportsStreaming             Boolean  @default(true) @map("supports_streaming")
  supportsTools                 Boolean  @default(false) @map("supports_tools")
  supportsVision                Boolean  @default(false) @map("supports_vision")
  inputCentsPerMillionTokens    Int      @map("input_cents_per_million_tokens")
  outputCentsPerMillionTokens   Int      @map("output_cents_per_million_tokens")
  markupPct                     Int      @default(18) @map("markup_pct")
  status                        String   @default("active")
  tags                          String[] @default([])
  descriptionZh                 String?  @map("description_zh")
  descriptionEn                 String?  @map("description_en")
  createdAt                     DateTime @default(now()) @map("created_at")
  updatedAt                     DateTime @updatedAt @map("updated_at")

  @@map("model_catalog")
}

model AppConfig {
  key       String   @id
  value     Json
  updatedAt DateTime @updatedAt @map("updated_at")

  @@map("app_config")
}

model AuditLog {
  id           String   @id @default(uuid()) @db.Uuid
  userId       String?  @map("user_id") @db.Uuid // actor
  targetUserId String?  @map("target_user_id") @db.Uuid // target
  kind         String
  payload      Json
  ipAddress    String?  @map("ip_address")
  userAgent    String?  @map("user_agent")
  createdAt    DateTime @default(now()) @map("created_at")

  actor   User? @relation("AuditActor", fields: [userId], references: [id])
  target  User? @relation("AuditTarget", fields: [targetUserId], references: [id])

  @@map("audit_logs")
}
```

- [ ] **Step 2: Generate the migration**

From repo root:

```bash
docker compose run --rm web pnpm prisma migrate dev --name initial_schema
```

Expected: prisma creates `web/prisma/migrations/<timestamp>_initial_schema/migration.sql`, then applies it to the `maas` database. Output ends with `Your database is now in sync with your schema.`

- [ ] **Step 3: Verify tables exist**

```bash
docker compose exec postgres psql -U app -d maas -c '\dt'
```

Expected: lists all tables including `users`, `accounts`, `sessions`, `verification_tokens`, `api_keys`, `credit_transactions`, `payment_intents`, `request_logs`, `model_catalog`, `app_config`, `audit_logs`, plus prisma's `_prisma_migrations`.

- [ ] **Step 4: Commit**

```bash
git add web/prisma/
git commit -m "db: add initial schema (9 app tables + NextAuth tables)"
```

---

### Task 6: Add database seed script

**Files:**
- Create: `web/prisma/seed.ts`
- Modify: `web/package.json` (add `prisma.seed` field)

- [ ] **Step 1: Write seed script**

`web/prisma/seed.ts`:

```typescript
import { PrismaClient } from '@prisma/client'

const prisma = new PrismaClient()

async function main() {
  // app_config defaults
  await prisma.appConfig.upsert({
    where: { key: 'default_markup_pct' },
    update: {},
    create: { key: 'default_markup_pct', value: 18 },
  })
  await prisma.appConfig.upsert({
    where: { key: 'topup_presets_cents' },
    update: {},
    create: { key: 'topup_presets_cents', value: [500, 1000, 2000, 5000, 10000] },
  })
  await prisma.appConfig.upsert({
    where: { key: 'trial_credit_cents' },
    update: {},
    create: { key: 'trial_credit_cents', value: 100 },
  })
  await prisma.appConfig.upsert({
    where: { key: 'cny_per_usd_rate' },
    update: {},
    create: { key: 'cny_per_usd_rate', value: 7.20 },
  })
  await prisma.appConfig.upsert({
    where: { key: 'rate_limit_per_user_per_minute' },
    update: {},
    create: { key: 'rate_limit_per_user_per_minute', value: 60 },
  })

  // Phase 0: seed exactly one model — GPT-4o
  await prisma.modelCatalog.upsert({
    where: { alias: 'openai/gpt-4o' },
    update: {},
    create: {
      alias: 'openai/gpt-4o',
      displayName: 'GPT-4o',
      upstreamProvider: 'openai',
      upstreamModelId: 'gpt-4o',
      contextWindow: 128000,
      supportsStreaming: true,
      supportsTools: true,
      supportsVision: true,
      inputCentsPerMillionTokens: 250,  // $2.50/M
      outputCentsPerMillionTokens: 1000, // $10.00/M
      markupPct: 18,
      status: 'active',
      tags: ['frontier', 'vision', 'tools'],
      descriptionZh: 'OpenAI 最新前沿模型, 128K 上下文, 支持视觉和工具调用',
      descriptionEn: 'OpenAI flagship model, 128K context, supports vision and tools',
    },
  })

  console.log('Seed complete.')
}

main()
  .catch((e) => { console.error(e); process.exit(1) })
  .finally(async () => { await prisma.$disconnect() })
```

- [ ] **Step 2: Wire seed into package.json**

In `web/package.json`, add at the top level (sibling to `"scripts"`):

```json
"prisma": {
  "seed": "tsx prisma/seed.ts"
}
```

- [ ] **Step 3: Run the seed**

```bash
docker compose run --rm web pnpm prisma db seed
```

Expected: console output `Seed complete.`

- [ ] **Step 4: Verify the seed**

```bash
docker compose exec postgres psql -U app -d maas -c "SELECT alias FROM model_catalog; SELECT key FROM app_config;"
```

Expected: one row in `model_catalog` (`openai/gpt-4o`), five rows in `app_config`.

- [ ] **Step 5: Commit**

```bash
git add web/prisma/seed.ts web/package.json
git commit -m "db: add seed script with GPT-4o and app_config defaults"
```

---

## Group C: Library infrastructure

### Task 7: Add env validation, Prisma client, Redis client

**Files:**
- Create: `web/src/lib/env.ts`
- Create: `web/src/lib/db.ts`
- Create: `web/src/lib/redis.ts`

- [ ] **Step 1: Write env.ts**

```typescript
// web/src/lib/env.ts
import { z } from 'zod'

const schema = z.object({
  DATABASE_URL: z.string().url(),
  REDIS_URL: z.string().url(),
  NEXTAUTH_URL: z.string().url(),
  NEXTAUTH_SECRET: z.string().min(32),
  GITHUB_CLIENT_ID: z.string().optional(),
  GITHUB_CLIENT_SECRET: z.string().optional(),
  RESEND_API_KEY: z.string().optional(),
  EMAIL_FROM: z.string().email().optional(),
  HMAC_SERVER_SECRET: z.string().min(32),
})

export const env = schema.parse(process.env)
```

- [ ] **Step 2: Write db.ts (Prisma singleton)**

```typescript
// web/src/lib/db.ts
import { PrismaClient } from '@prisma/client'

const globalForPrisma = globalThis as unknown as { prisma?: PrismaClient }

export const prisma = globalForPrisma.prisma ?? new PrismaClient({
  log: process.env.NODE_ENV === 'development' ? ['error', 'warn'] : ['error'],
})

if (process.env.NODE_ENV !== 'production') globalForPrisma.prisma = prisma
```

- [ ] **Step 3: Write redis.ts**

```typescript
// web/src/lib/redis.ts
import Redis from 'ioredis'
import { env } from './env'

const globalForRedis = globalThis as unknown as { redis?: Redis }

export const redis = globalForRedis.redis ?? new Redis(env.REDIS_URL, {
  maxRetriesPerRequest: 3,
})

if (process.env.NODE_ENV !== 'production') globalForRedis.redis = redis
```

- [ ] **Step 4: Smoke-test that they import without error**

Create a temporary `web/src/lib/__smoke__.ts`:

```typescript
import { env } from './env'
import { prisma } from './db'
import { redis } from './redis'

async function smoke() {
  console.log('Env OK:', !!env.DATABASE_URL)
  const users = await prisma.user.count()
  console.log('Users in DB:', users)
  await redis.set('smoke', 'ok', 'EX', 10)
  const v = await redis.get('smoke')
  console.log('Redis OK:', v)
  await prisma.$disconnect()
  await redis.quit()
}

smoke()
```

```bash
docker compose run --rm web pnpm tsx src/lib/__smoke__.ts
```

Expected: `Env OK: true`, `Users in DB: 0`, `Redis OK: ok`.

Delete the smoke file:

```bash
rm web/src/lib/__smoke__.ts
```

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/
git commit -m "web: add env validation, Prisma + Redis singletons"
```

---

### Task 8: Add API key library (gen + hash)

**Files:**
- Create: `web/src/lib/api-keys.ts`
- Create: `web/src/lib/api-keys.test.ts`

- [ ] **Step 1: Write the failing test first**

`web/src/lib/api-keys.test.ts`:

```typescript
import { describe, it, expect } from 'vitest'
import { generateApiKey, hashApiKey, keyPrefix } from './api-keys'

describe('api-keys', () => {
  it('generates a key with sk-or- prefix and 32 random chars', () => {
    const { plaintext } = generateApiKey()
    expect(plaintext).toMatch(/^sk-or-[A-Za-z0-9]{32}$/)
  })

  it('generates unique keys on consecutive calls', () => {
    const a = generateApiKey().plaintext
    const b = generateApiKey().plaintext
    expect(a).not.toBe(b)
  })

  it('returns prefix matching first 12 chars', () => {
    const { plaintext, prefix } = generateApiKey()
    expect(prefix).toBe(plaintext.slice(0, 12))
    expect(prefix.length).toBe(12)
  })

  it('hashes deterministically with HMAC-SHA256', () => {
    const hash1 = hashApiKey('sk-or-abc')
    const hash2 = hashApiKey('sk-or-abc')
    expect(hash1).toBe(hash2)
    expect(hash1).toMatch(/^[a-f0-9]{64}$/)
  })

  it('produces different hashes for different keys', () => {
    expect(hashApiKey('sk-or-a')).not.toBe(hashApiKey('sk-or-b'))
  })

  it('keyPrefix returns first 12 chars', () => {
    expect(keyPrefix('sk-or-abcdef1234567890')).toBe('sk-or-abcdef')
  })
})
```

- [ ] **Step 2: Configure Vitest**

Add `web/vitest.config.ts`:

```typescript
import { defineConfig } from 'vitest/config'
import path from 'path'

export default defineConfig({
  test: {
    environment: 'node',
  },
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
})
```

Add to `web/package.json` scripts:

```json
"test": "vitest run",
"test:watch": "vitest"
```

- [ ] **Step 3: Run the test, watch it fail**

```bash
cd web && pnpm test src/lib/api-keys.test.ts
```

Expected: FAIL — module not found `./api-keys`.

- [ ] **Step 4: Implement api-keys.ts**

`web/src/lib/api-keys.ts`:

```typescript
import { randomBytes, createHmac } from 'node:crypto'
import { env } from './env'

const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'

export function generateApiKey(): { plaintext: string; prefix: string; hash: string } {
  const bytes = randomBytes(32)
  let body = ''
  for (let i = 0; i < 32; i++) {
    body += ALPHABET[bytes[i] % ALPHABET.length]
  }
  const plaintext = `sk-or-${body}`
  return {
    plaintext,
    prefix: keyPrefix(plaintext),
    hash: hashApiKey(plaintext),
  }
}

export function hashApiKey(plaintext: string): string {
  return createHmac('sha256', env.HMAC_SERVER_SECRET).update(plaintext).digest('hex')
}

export function keyPrefix(plaintext: string): string {
  return plaintext.slice(0, 12)
}
```

- [ ] **Step 5: Run the test again**

```bash
pnpm test src/lib/api-keys.test.ts
```

Expected: PASS — all 6 tests green.

- [ ] **Step 6: Commit**

```bash
cd .. && git add web/src/lib/api-keys.ts web/src/lib/api-keys.test.ts web/vitest.config.ts web/package.json
git commit -m "web: add API key generation and HMAC hashing with tests"
```

---

### Task 9: Add trial credit grant library

**Files:**
- Create: `web/src/lib/credits.ts`
- Create: `web/src/lib/credits.test.ts`

- [ ] **Step 1: Write the failing test**

`web/src/lib/credits.test.ts`:

```typescript
import { describe, it, expect, beforeEach, vi } from 'vitest'

const mockSetNx = vi.fn()
const mockTransaction = vi.fn()
const mockAppConfigFindUnique = vi.fn()

vi.mock('./redis', () => ({
  redis: { set: mockSetNx },
}))

vi.mock('./db', () => ({
  prisma: {
    appConfig: { findUnique: mockAppConfigFindUnique },
    $transaction: mockTransaction,
  },
}))

import { grantTrialCreditIfEligible } from './credits'

describe('grantTrialCreditIfEligible', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockAppConfigFindUnique.mockResolvedValue({ value: 100 })
  })

  it('grants 100 cents to a new user from a fresh IP', async () => {
    mockSetNx.mockResolvedValue('OK')
    mockTransaction.mockResolvedValue([{ creditsCents: 100n }])

    const result = await grantTrialCreditIfEligible('user-1', '1.2.3.4')
    expect(result).toBe(true)
    expect(mockSetNx).toHaveBeenCalledWith('trial:ip:1.2.3.4', 'user-1', 'EX', 86400, 'NX')
  })

  it('does not grant if IP already used in last 24h', async () => {
    mockSetNx.mockResolvedValue(null)

    const result = await grantTrialCreditIfEligible('user-1', '1.2.3.4')
    expect(result).toBe(false)
    expect(mockTransaction).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run, watch it fail**

```bash
cd web && pnpm test src/lib/credits.test.ts
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement credits.ts**

`web/src/lib/credits.ts`:

```typescript
import { prisma } from './db'
import { redis } from './redis'

/**
 * Grant trial credit if the user is eligible.
 *
 * Eligibility rules:
 *   1. User must not already have a trial_credit transaction (per-user dedup, checked first
 *      so re-visits don't pollute Redis IP claims)
 *   2. The IP must not have been used to claim trial credit in the last 24h (Redis SETNX)
 *
 * Idempotent — safe to call from dashboard layout on every render.
 * Returns true if credit was granted.
 */
export async function grantTrialCreditIfEligible(
  userId: string,
  ipAddress: string,
): Promise<boolean> {
  // Per-user check FIRST: if user already has trial credit, return early without touching Redis.
  // This prevents polluting Redis IP claims when an existing user revisits from a new IP.
  const existing = await prisma.creditTransaction.findFirst({
    where: { userId, kind: 'trial_credit' },
    select: { id: true },
  })
  if (existing) return false

  // Per-IP atomic dedup: only first user per IP per 24h gets the bonus.
  const ok = await redis.set(`trial:ip:${ipAddress}`, userId, 'EX', 86400, 'NX')
  if (ok !== 'OK') return false

  const cfg = await prisma.appConfig.findUnique({ where: { key: 'trial_credit_cents' } })
  const amount = BigInt(typeof cfg?.value === 'number' ? cfg.value : 100)

  await prisma.$transaction(async (tx) => {
    const user = await tx.user.update({
      where: { id: userId },
      data: { creditsCents: { increment: amount } },
      select: { creditsCents: true },
    })
    await tx.creditTransaction.create({
      data: {
        userId,
        amountCents: amount,
        kind: 'trial_credit',
        balanceAfterCents: user.creditsCents,
      },
    })
  })

  return true
}
```

- [ ] **Step 4: Run the test**

```bash
pnpm test src/lib/credits.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd .. && git add web/src/lib/credits.ts web/src/lib/credits.test.ts
git commit -m "web: add trial credit grant with per-IP dedup"
```

---

### Task 10: Add email library (Resend wrapper)

**Files:**
- Create: `web/src/lib/email.ts`

- [ ] **Step 1: Write email.ts**

```typescript
// web/src/lib/email.ts
import { Resend } from 'resend'
import { env } from './env'

const resend = env.RESEND_API_KEY ? new Resend(env.RESEND_API_KEY) : null

export async function sendVerificationEmail(to: string, verifyUrl: string): Promise<void> {
  if (!resend || !env.EMAIL_FROM) {
    // Dev fallback: print the link to stdout so the developer can verify
    console.log(`\n[DEV] Verification email to ${to}:\n  ${verifyUrl}\n`)
    return
  }

  const { error } = await resend.emails.send({
    from: env.EMAIL_FROM,
    to,
    subject: 'Verify your MaaS Router email',
    html: `
      <p>Welcome to MaaS Router.</p>
      <p>Click the link below to verify your email and get your $1 free trial credit:</p>
      <p><a href="${verifyUrl}">${verifyUrl}</a></p>
      <p>This link expires in 24 hours.</p>
    `,
  })
  if (error) throw new Error(`Failed to send verification email: ${error.message}`)
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/lib/email.ts
git commit -m "web: add email lib with Resend + dev console fallback"
```

---

## Group D: NextAuth + signup/login

### Task 11: Configure NextAuth with Prisma adapter

**Files:**
- Create: `web/src/lib/auth.ts`
- Create: `web/src/app/api/auth/[...nextauth]/route.ts`

- [ ] **Step 1: Write auth.ts**

```typescript
// web/src/lib/auth.ts
import NextAuth from 'next-auth'
import GitHub from 'next-auth/providers/github'
import Credentials from 'next-auth/providers/credentials'
import { PrismaAdapter } from '@auth/prisma-adapter'
import bcrypt from 'bcryptjs'
import { prisma } from './db'
import { env } from './env'
import { grantTrialCreditIfEligible } from './credits'

export const { handlers, signIn, signOut, auth } = NextAuth({
  adapter: PrismaAdapter(prisma) as any,
  session: { strategy: 'database' },
  pages: { signIn: '/login' },
  providers: [
    GitHub({
      clientId: env.GITHUB_CLIENT_ID,
      clientSecret: env.GITHUB_CLIENT_SECRET,
    }),
    Credentials({
      credentials: {
        email: { label: 'Email', type: 'email' },
        password: { label: 'Password', type: 'password' },
      },
      async authorize(credentials) {
        const email = credentials?.email as string
        const password = credentials?.password as string
        if (!email || !password) return null

        const user = await prisma.user.findUnique({ where: { email } })
        if (!user?.passwordHash) return null
        const ok = await bcrypt.compare(password, user.passwordHash)
        if (!ok) return null
        if (user.status !== 'active') return null
        return { id: user.id, email: user.email, name: user.email }
      },
    }),
  ],
  callbacks: {
    async signIn({ user, account, profile }) {
      // For GitHub signups, set github_id and grant trial credit if new
      if (account?.provider === 'github' && user.email) {
        const existing = await prisma.user.findUnique({ where: { email: user.email } })
        if (!existing) {
          // PrismaAdapter will create the user. We mark email_verified and grant credit
          // in the events.createUser callback below.
        } else if (!existing.githubId && profile?.id) {
          await prisma.user.update({
            where: { id: existing.id },
            data: { githubId: String(profile.id), emailVerifiedAt: existing.emailVerifiedAt ?? new Date() },
          })
        }
      }
      return true
    },
  },
  events: {
    async createUser({ user }) {
      // Fires after PrismaAdapter creates a new user (i.e., GitHub signup)
      if (user.id) {
        await prisma.user.update({
          where: { id: user.id },
          data: { emailVerifiedAt: new Date() },
        })
        // Trial credit grant happens in route handler with IP context — not here.
      }
    },
  },
})
```

- [ ] **Step 2: Wire the route handler**

`web/src/app/api/auth/[...nextauth]/route.ts`:

```typescript
import { handlers } from '@/lib/auth'
export const { GET, POST } = handlers
```

- [ ] **Step 3: Generate Prisma client + verify imports**

```bash
docker compose run --rm web pnpm prisma generate
docker compose up web postgres redis
```

Expected: web logs show `Ready in <Xs>` without errors. Visit `http://localhost:3000/api/auth/signin` — should redirect to GitHub OAuth flow (if GITHUB_CLIENT_ID set) or show a sign-in page. Ctrl+C to stop.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/auth.ts web/src/app/api/auth
git commit -m "auth: configure NextAuth with Prisma adapter, GitHub + Credentials providers"
```

---

### Task 12: Build signup endpoint (email/password)

**Files:**
- Create: `web/src/app/api/signup/route.ts`

- [ ] **Step 1: Write signup route**

```typescript
// web/src/app/api/signup/route.ts
import { NextRequest, NextResponse } from 'next/server'
import { z } from 'zod'
import bcrypt from 'bcryptjs'
import { randomBytes } from 'node:crypto'
import { prisma } from '@/lib/db'
import { sendVerificationEmail } from '@/lib/email'
import { env } from '@/lib/env'

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(10, 'Password must be at least 10 characters'),
})

export async function POST(req: NextRequest) {
  const body = await req.json().catch(() => null)
  const parsed = schema.safeParse(body)
  if (!parsed.success) {
    return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })
  }
  const { email, password } = parsed.data

  const existing = await prisma.user.findUnique({ where: { email } })
  if (existing) {
    return NextResponse.json({ error: 'Email already registered' }, { status: 409 })
  }

  const passwordHash = await bcrypt.hash(password, 12)
  const user = await prisma.user.create({
    data: { email, passwordHash },
  })

  // Email verification token (24h)
  const token = randomBytes(32).toString('hex')
  const expires = new Date(Date.now() + 24 * 60 * 60 * 1000)
  await prisma.verificationToken.create({
    data: { identifier: email, token, expires },
  })

  const verifyUrl = `${env.NEXTAUTH_URL}/auth/verify-email/${token}`
  await sendVerificationEmail(email, verifyUrl)

  return NextResponse.json({ ok: true, message: 'Check your email for the verification link.' })
}
```

- [ ] **Step 2: Test manually**

Restart `docker compose up`, then:

```bash
curl -X POST http://localhost:3000/api/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"correct-horse-battery-staple"}'
```

Expected: `{"ok":true,"message":"Check your email for the verification link."}`. Check the `web` container logs — if Resend isn't configured, you'll see `[DEV] Verification email to test@example.com: http://localhost:3000/auth/verify-email/<token>`. Copy the URL for the next task.

- [ ] **Step 3: Commit**

```bash
git add web/src/app/api/signup
git commit -m "auth: add /api/signup with email verification flow"
```

---

### Task 13: Build email verification handler

**Files:**
- Create: `web/src/app/auth/verify-email/[token]/page.tsx`
- Create: `web/src/app/api/auth/verify-email/route.ts`

- [ ] **Step 1: Write the API route**

`web/src/app/api/auth/verify-email/route.ts`:

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { prisma } from '@/lib/db'
import { grantTrialCreditIfEligible } from '@/lib/credits'

export async function POST(req: NextRequest) {
  const { token } = await req.json().catch(() => ({}))
  if (!token || typeof token !== 'string') {
    return NextResponse.json({ error: 'Missing token' }, { status: 400 })
  }

  const vt = await prisma.verificationToken.findUnique({ where: { token } })
  if (!vt || vt.expires < new Date()) {
    return NextResponse.json({ error: 'Invalid or expired token' }, { status: 400 })
  }

  const user = await prisma.user.findUnique({ where: { email: vt.identifier } })
  if (!user) {
    return NextResponse.json({ error: 'User not found' }, { status: 400 })
  }

  await prisma.user.update({
    where: { id: user.id },
    data: { emailVerifiedAt: new Date() },
  })
  await prisma.verificationToken.delete({ where: { token } })

  const ip = req.headers.get('x-forwarded-for')?.split(',')[0].trim() || '0.0.0.0'
  await grantTrialCreditIfEligible(user.id, ip)

  return NextResponse.json({ ok: true })
}
```

- [ ] **Step 2: Write the page that calls the API**

`web/src/app/auth/verify-email/[token]/page.tsx`:

```tsx
'use client'
import { useEffect, useState } from 'react'
import { useParams, useRouter } from 'next/navigation'

export default function VerifyEmailPage() {
  const { token } = useParams<{ token: string }>()
  const router = useRouter()
  const [status, setStatus] = useState<'pending' | 'ok' | 'error'>('pending')
  const [message, setMessage] = useState('')

  useEffect(() => {
    fetch('/api/auth/verify-email', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    })
      .then(async (r) => {
        const data = await r.json()
        if (r.ok) {
          setStatus('ok')
          setMessage('Email verified — $1 trial credit added.')
          setTimeout(() => router.push('/login'), 2000)
        } else {
          setStatus('error')
          setMessage(data.error || 'Verification failed')
        }
      })
      .catch(() => { setStatus('error'); setMessage('Network error') })
  }, [token, router])

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="rounded border bg-white p-8 shadow-sm">
        {status === 'pending' && <p>Verifying…</p>}
        {status === 'ok' && <p className="text-green-700">{message}</p>}
        {status === 'error' && <p className="text-red-700">{message}</p>}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Test the full flow**

1. POST `/api/signup` as in Task 12 with a fresh email
2. Copy the `[DEV] Verification email` URL from container logs
3. Open it in your browser
4. Expected: page shows "Email verified — $1 trial credit added." and redirects to `/login` after 2 seconds.

Verify the trial credit was granted:

```bash
docker compose exec postgres psql -U app -d maas -c \
  "SELECT email, credits_cents, email_verified_at FROM users WHERE email='test@example.com';"
```

Expected: `credits_cents = 100`, `email_verified_at` not null.

- [ ] **Step 4: Commit**

```bash
git add web/src/app/auth/verify-email web/src/app/api/auth/verify-email
git commit -m "auth: add email verification page + trial credit grant on success"
```

---

### Task 14: Build signup + login pages

**Files:**
- Create: `web/src/app/signup/page.tsx`
- Create: `web/src/components/signup-form.tsx`
- Create: `web/src/app/login/page.tsx`
- Create: `web/src/components/login-form.tsx`

- [ ] **Step 1: Write signup form component**

`web/src/components/signup-form.tsx`:

```tsx
'use client'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { signIn } from 'next-auth/react'

export function SignupForm() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [status, setStatus] = useState<'idle' | 'submitting' | 'sent' | 'error'>('idle')
  const [message, setMessage] = useState('')

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setStatus('submitting')
    const res = await fetch('/api/signup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    })
    const data = await res.json()
    if (res.ok) {
      setStatus('sent')
      setMessage(data.message)
    } else {
      setStatus('error')
      setMessage(data.error || 'Signup failed')
    }
  }

  if (status === 'sent') {
    return <p className="text-green-700">{message}</p>
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <div>
        <Label htmlFor="email">Email</Label>
        <Input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
      </div>
      <div>
        <Label htmlFor="password">Password (10+ characters)</Label>
        <Input id="password" type="password" minLength={10} value={password} onChange={(e) => setPassword(e.target.value)} required />
      </div>
      {status === 'error' && <p className="text-sm text-red-700">{message}</p>}
      <Button type="submit" disabled={status === 'submitting'} className="w-full">
        {status === 'submitting' ? 'Creating account…' : 'Sign up'}
      </Button>
      <div className="text-center text-sm text-gray-500">or</div>
      <Button type="button" variant="outline" onClick={() => signIn('github', { callbackUrl: '/dashboard' })} className="w-full">
        Continue with GitHub
      </Button>
    </form>
  )
}
```

- [ ] **Step 2: Write signup page**

`web/src/app/signup/page.tsx`:

```tsx
import { SignupForm } from '@/components/signup-form'
import Link from 'next/link'

export default function SignupPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md rounded-lg border bg-white p-8 shadow-sm">
        <h1 className="mb-6 text-2xl font-bold">Create an account</h1>
        <SignupForm />
        <p className="mt-6 text-center text-sm text-gray-600">
          Already have an account? <Link href="/login" className="text-indigo-600 hover:underline">Log in</Link>
        </p>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Write login form + page**

`web/src/components/login-form.tsx`:

```tsx
'use client'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { signIn } from 'next-auth/react'
import { useRouter } from 'next/navigation'

export function LoginForm() {
  const router = useRouter()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    const res = await signIn('credentials', { email, password, redirect: false })
    if (res?.error) {
      setError('Invalid email or password')
      setLoading(false)
    } else {
      router.push('/dashboard')
    }
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <div>
        <Label htmlFor="email">Email</Label>
        <Input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
      </div>
      <div>
        <Label htmlFor="password">Password</Label>
        <Input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
      </div>
      {error && <p className="text-sm text-red-700">{error}</p>}
      <Button type="submit" disabled={loading} className="w-full">
        {loading ? 'Logging in…' : 'Log in'}
      </Button>
      <div className="text-center text-sm text-gray-500">or</div>
      <Button type="button" variant="outline" onClick={() => signIn('github', { callbackUrl: '/dashboard' })} className="w-full">
        Continue with GitHub
      </Button>
    </form>
  )
}
```

`web/src/app/login/page.tsx`:

```tsx
import { LoginForm } from '@/components/login-form'
import Link from 'next/link'

export default function LoginPage() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md rounded-lg border bg-white p-8 shadow-sm">
        <h1 className="mb-6 text-2xl font-bold">Log in</h1>
        <LoginForm />
        <p className="mt-6 text-center text-sm text-gray-600">
          No account? <Link href="/signup" className="text-indigo-600 hover:underline">Sign up</Link>
        </p>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Verify in browser**

Restart `docker compose up`. Visit:
- http://localhost:3000/signup — sign up with a fresh email
- Verify email via the URL in container logs
- Visit http://localhost:3000/login and log in

Expected: login succeeds and redirects to `/dashboard` (which will 404 for now — that's the next task).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/signup-form.tsx web/src/components/login-form.tsx web/src/app/signup web/src/app/login
git commit -m "auth: add signup + login pages with shadcn forms"
```

---

### Task 15: Build dashboard layout + middleware

**Files:**
- Create: `web/src/middleware.ts`
- Create: `web/src/app/dashboard/layout.tsx`
- Create: `web/src/app/dashboard/page.tsx`
- Create: `web/src/components/balance-widget.tsx`

- [ ] **Step 1: Add middleware to gate /dashboard**

`web/src/middleware.ts`:

```typescript
import { auth } from '@/lib/auth'

export default auth((req) => {
  if (!req.auth && req.nextUrl.pathname.startsWith('/dashboard')) {
    const loginUrl = new URL('/login', req.url)
    return Response.redirect(loginUrl)
  }
})

export const config = {
  matcher: ['/dashboard/:path*'],
}
```

- [ ] **Step 2: Write the balance widget**

`web/src/components/balance-widget.tsx`:

```tsx
'use client'
import { useEffect, useState } from 'react'

export function BalanceWidget() {
  const [balance, setBalance] = useState<number | null>(null)

  useEffect(() => {
    fetch('/api/credits/balance').then(async (r) => {
      if (r.ok) {
        const data = await r.json()
        setBalance(data.creditsCents)
      }
    })
  }, [])

  if (balance === null) return <div className="text-sm text-gray-500">…</div>
  const dollars = (balance / 100).toFixed(2)
  return (
    <div className="rounded-lg bg-indigo-50 p-4">
      <div className="text-xs uppercase text-gray-600">Balance</div>
      <div className="text-2xl font-bold">${dollars}</div>
    </div>
  )
}
```

- [ ] **Step 3: Write the balance API route**

`web/src/app/api/credits/balance/route.ts`:

```typescript
import { NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }
  const user = await prisma.user.findUnique({
    where: { id: session.user.id },
    select: { creditsCents: true },
  })
  if (!user) return NextResponse.json({ error: 'Not found' }, { status: 404 })
  return NextResponse.json({ creditsCents: Number(user.creditsCents) })
}
```

- [ ] **Step 4: Write dashboard layout + page**

`web/src/app/dashboard/layout.tsx`:

```tsx
import Link from 'next/link'
import { headers } from 'next/headers'
import { BalanceWidget } from '@/components/balance-widget'
import { auth } from '@/lib/auth'
import { grantTrialCreditIfEligible } from '@/lib/credits'

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  // Idempotent trial credit grant: covers both GitHub OAuth users (who skip the
  // email-verification path) and as a safety net for email users.
  const session = await auth()
  if (session?.user?.id) {
    const ip = (await headers()).get('x-forwarded-for')?.split(',')[0].trim() || '0.0.0.0'
    await grantTrialCreditIfEligible(session.user.id, ip)
  }

  return (
    <div className="flex min-h-screen bg-gray-50">
      <aside className="w-56 border-r bg-white p-4">
        <div className="mb-6 font-bold">⚡ MaaS Router</div>
        <BalanceWidget />
        <nav className="mt-6 space-y-1 text-sm">
          <Link href="/dashboard" className="block rounded px-3 py-2 hover:bg-gray-100">Overview</Link>
          <Link href="/dashboard/keys" className="block rounded px-3 py-2 hover:bg-gray-100">API Keys</Link>
        </nav>
      </aside>
      <main className="flex-1 p-8">{children}</main>
    </div>
  )
}
```

`web/src/app/dashboard/page.tsx`:

```tsx
import Link from 'next/link'

export default function DashboardPage() {
  return (
    <div>
      <h1 className="mb-6 text-2xl font-bold">Welcome to MaaS Router</h1>
      <div className="rounded-lg border-2 border-dashed border-indigo-300 bg-indigo-50/50 p-6">
        <div className="mb-2 font-semibold">Get started:</div>
        <ol className="space-y-2 text-sm">
          <li>✅ You've been granted $1.00 trial credit</li>
          <li>
            ⬜ <Link href="/dashboard/keys" className="text-indigo-600 underline">Create your first API key</Link>
          </li>
          <li>⬜ Make your first API call (see <Link href="/docs" className="text-indigo-600 underline">docs</Link>)</li>
        </ol>
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Verify in browser**

Visit http://localhost:3000/dashboard. Expected:
- Unauthenticated users redirected to `/login`
- Authenticated users see "Welcome to MaaS Router", balance widget showing $1.00 (if trial granted)

- [ ] **Step 6: Commit**

```bash
git add web/src/middleware.ts web/src/app/dashboard web/src/components/balance-widget.tsx web/src/app/api/credits
git commit -m "web: add dashboard layout, balance widget, auth-gating middleware"
```

---

## Group E: API key management

### Task 16: Build keys API routes

**Files:**
- Create: `web/src/app/api/keys/route.ts`
- Create: `web/src/app/api/keys/[id]/route.ts`

- [ ] **Step 1: Write POST + GET /api/keys**

`web/src/app/api/keys/route.ts`:

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { z } from 'zod'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { generateApiKey } from '@/lib/api-keys'

const createSchema = z.object({ name: z.string().min(1).max(64) })

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const keys = await prisma.apiKey.findMany({
    where: { userId: session.user.id, revokedAt: null },
    orderBy: { createdAt: 'desc' },
    select: { id: true, name: true, keyPrefix: true, lastUsedAt: true, createdAt: true },
  })
  return NextResponse.json({ keys })
}

export async function POST(req: NextRequest) {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const body = await req.json().catch(() => null)
  const parsed = createSchema.safeParse(body)
  if (!parsed.success) return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })

  // Max 5 active keys per user
  const count = await prisma.apiKey.count({ where: { userId: session.user.id, revokedAt: null } })
  if (count >= 5) {
    return NextResponse.json({ error: 'Maximum 5 active keys. Revoke an existing key first.' }, { status: 400 })
  }

  const { plaintext, prefix, hash } = generateApiKey()
  const key = await prisma.apiKey.create({
    data: {
      userId: session.user.id,
      name: parsed.data.name,
      keyPrefix: prefix,
      keyHash: hash,
    },
    select: { id: true, name: true, keyPrefix: true, createdAt: true },
  })

  // Return the plaintext key ONLY in this response. Never stored.
  return NextResponse.json({ key: { ...key, plaintext } })
}
```

- [ ] **Step 2: Write DELETE /api/keys/[id]**

`web/src/app/api/keys/[id]/route.ts`:

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { redis } from '@/lib/redis'

export async function DELETE(_req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const { id } = await params
  const key = await prisma.apiKey.findUnique({ where: { id } })
  if (!key || key.userId !== session.user.id) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 })
  }
  if (key.revokedAt) {
    return NextResponse.json({ error: 'Already revoked' }, { status: 400 })
  }

  await prisma.apiKey.update({
    where: { id },
    data: { revokedAt: new Date() },
  })
  // Invalidate Redis cache so the proxy can't keep using the key
  await redis.del(`key:${key.keyHash}`)

  return NextResponse.json({ ok: true })
}
```

- [ ] **Step 3: Manual smoke test**

Log in as your test user, then in the browser DevTools console (or via `curl` with cookies):

```javascript
// In DevTools while on /dashboard:
fetch('/api/keys', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'test-key' }) })
  .then(r => r.json()).then(console.log)
```

Expected: response includes `key.plaintext` like `sk-or-<32 chars>` plus metadata. **Save this plaintext for Task 22 smoke test.**

- [ ] **Step 4: Commit**

```bash
git add web/src/app/api/keys
git commit -m "keys: add create/list/revoke API routes with 5-key cap"
```

---

### Task 17: Build keys page UI

**Files:**
- Create: `web/src/app/dashboard/keys/page.tsx`
- Create: `web/src/components/create-key-modal.tsx`
- Create: `web/src/components/keys-table.tsx`

- [ ] **Step 1: Write create-key modal**

`web/src/components/create-key-modal.tsx`:

```tsx
'use client'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'

export function CreateKeyModal({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [plaintext, setPlaintext] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    const res = await fetch('/api/keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
    const data = await res.json()
    setLoading(false)
    if (res.ok) {
      setPlaintext(data.key.plaintext)
    } else {
      setError(data.error || 'Failed to create key')
    }
  }

  function close() {
    setOpen(false)
    setName('')
    setPlaintext(null)
    setError('')
    onCreated()
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>+ New Key</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{plaintext ? 'Save this key — it will not be shown again' : 'Create API key'}</DialogTitle>
        </DialogHeader>
        {!plaintext ? (
          <form onSubmit={onSubmit} className="space-y-4">
            <div>
              <Label htmlFor="key-name">Name</Label>
              <Input id="key-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. production-app" required maxLength={64} />
            </div>
            {error && <p className="text-sm text-red-700">{error}</p>}
            <Button type="submit" disabled={loading} className="w-full">{loading ? 'Creating…' : 'Create key'}</Button>
          </form>
        ) : (
          <div className="space-y-4">
            <div className="rounded border bg-yellow-50 p-3 text-sm text-yellow-900">
              ⚠️ Copy this key now. We will not show it again.
            </div>
            <code className="block break-all rounded bg-gray-100 p-3 text-sm">{plaintext}</code>
            <Button onClick={() => navigator.clipboard.writeText(plaintext)} className="w-full">Copy to clipboard</Button>
            <Button onClick={close} variant="outline" className="w-full">Done</Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 2: Write keys table**

`web/src/components/keys-table.tsx`:

```tsx
'use client'
import { Button } from '@/components/ui/button'

type Key = { id: string; name: string; keyPrefix: string; createdAt: string; lastUsedAt: string | null }

export function KeysTable({ keys, onRevoke }: { keys: Key[]; onRevoke: () => void }) {
  async function revoke(id: string) {
    if (!confirm('Revoke this key? Existing API calls using it will start failing.')) return
    await fetch(`/api/keys/${id}`, { method: 'DELETE' })
    onRevoke()
  }

  if (keys.length === 0) {
    return <div className="rounded border bg-white p-8 text-center text-sm text-gray-500">No API keys yet. Click "New Key" to create one.</div>
  }

  return (
    <div className="overflow-hidden rounded-lg border bg-white">
      <table className="w-full text-sm">
        <thead className="bg-gray-50 text-xs uppercase text-gray-600">
          <tr>
            <th className="px-4 py-3 text-left">Name</th>
            <th className="px-4 py-3 text-left">Key</th>
            <th className="px-4 py-3 text-left">Created</th>
            <th className="px-4 py-3 text-left">Last used</th>
            <th className="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          {keys.map((k) => (
            <tr key={k.id} className="border-t">
              <td className="px-4 py-3">{k.name}</td>
              <td className="px-4 py-3 font-mono text-gray-600">{k.keyPrefix}...</td>
              <td className="px-4 py-3 text-gray-600">{new Date(k.createdAt).toLocaleDateString()}</td>
              <td className="px-4 py-3 text-gray-600">{k.lastUsedAt ? new Date(k.lastUsedAt).toLocaleString() : 'Never'}</td>
              <td className="px-4 py-3 text-right">
                <button onClick={() => revoke(k.id)} className="text-sm text-red-600 hover:underline">Revoke</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
```

- [ ] **Step 3: Write keys page**

`web/src/app/dashboard/keys/page.tsx`:

```tsx
'use client'
import { useEffect, useState } from 'react'
import { CreateKeyModal } from '@/components/create-key-modal'
import { KeysTable } from '@/components/keys-table'

export default function KeysPage() {
  const [keys, setKeys] = useState<any[]>([])

  async function load() {
    const r = await fetch('/api/keys')
    const data = await r.json()
    setKeys(data.keys || [])
  }

  useEffect(() => { load() }, [])

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">API Keys</h1>
          <p className="mt-1 text-sm text-gray-600">Up to 5 active keys per account.</p>
        </div>
        <CreateKeyModal onCreated={load} />
      </div>
      <KeysTable keys={keys} onRevoke={load} />
    </div>
  )
}
```

- [ ] **Step 4: Verify in browser**

Visit http://localhost:3000/dashboard/keys. Create a key via the modal. Verify:
- Key shows once with copy button
- After "Done", returns to list with masked key
- Revoke button works

- [ ] **Step 5: Commit**

```bash
git add web/src/app/dashboard/keys web/src/components/create-key-modal.tsx web/src/components/keys-table.tsx
git commit -m "keys: add UI for create/list/revoke with one-time-show modal"
```

---

## Group F: Go proxy — scaffold

### Task 18: Initialize Go module + scaffolding

**Files:**
- Create: `proxy/go.mod`, `proxy/cmd/proxy/main.go`, `proxy/.air.toml`, `proxy/Dockerfile`

- [ ] **Step 1: Initialize Go module**

```bash
mkdir -p proxy/cmd/proxy
cd proxy
go mod init github.com/your-org/maas-router/proxy
```

(Replace `your-org` with whatever you want — it's just an import path; can be anything.)

- [ ] **Step 2: Add core dependencies**

```bash
go get github.com/jackc/pgx/v5 \
       github.com/jackc/pgx/v5/pgxpool \
       github.com/redis/go-redis/v9 \
       github.com/google/uuid \
       github.com/sashabaranov/go-openai \
       github.com/stretchr/testify
```

- [ ] **Step 3: Write minimal main.go**

`proxy/cmd/proxy/main.go`:

```go
package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	log.Printf("proxy listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Write Dockerfile**

`proxy/Dockerfile`:

```dockerfile
FROM golang:1.22-alpine

WORKDIR /app

RUN go install github.com/cosmtrek/air@latest

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

EXPOSE 8080

CMD ["air"]
```

- [ ] **Step 5: Write .air.toml**

`proxy/.air.toml`:

```toml
root = "."
testdata_dir = "testdata"
tmp_dir = "tmp"

[build]
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main ./cmd/proxy"
  delay = 1000
  exclude_dir = ["tmp", "vendor"]
  include_ext = ["go"]
```

- [ ] **Step 6: Verify it builds and runs**

From repo root:

```bash
docker compose up proxy
```

In another terminal:

```bash
curl http://localhost:8080/healthz
```

Expected: `ok`. Ctrl+C the compose command.

- [ ] **Step 7: Commit**

```bash
cd .. && git add proxy/
git commit -m "proxy: scaffold Go service with healthz, air hot-reload"
```

---

### Task 19: Add storage (Postgres + Redis) clients

**Files:**
- Create: `proxy/internal/storage/postgres.go`
- Create: `proxy/internal/storage/redis.go`

- [ ] **Step 1: Write postgres.go**

`proxy/internal/storage/postgres.go`:

```go
package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPostgres(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, fmt.Errorf("DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
```

- [ ] **Step 2: Write redis.go**

`proxy/internal/storage/redis.go`:

```go
package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
)

func NewRedis(ctx context.Context) (*redis.Client, error) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		return nil, fmt.Errorf("REDIS_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
```

- [ ] **Step 3: Wire into main.go**

Replace `proxy/cmd/proxy/main.go`:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/your-org/maas-router/proxy/internal/storage"
)

func main() {
	ctx := context.Background()

	pg, err := storage.NewPostgres(ctx)
	if err != nil {
		log.Fatalf("postgres init: %v", err)
	}
	defer pg.Close()

	rdb, err := storage.NewRedis(ctx)
	if err != nil {
		log.Fatalf("redis init: %v", err)
	}
	defer rdb.Close()

	_ = pg
	_ = rdb

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("proxy listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
```

**Note:** Replace `github.com/your-org/maas-router/proxy` with whatever you used in `go.mod`.

- [ ] **Step 4: Restart and verify**

```bash
docker compose up proxy
```

Expected: logs show `proxy listening on :8080` with no errors. `curl localhost:8080/healthz` returns `ok`.

- [ ] **Step 5: Commit**

```bash
cd .. && git add proxy/
git commit -m "proxy: wire Postgres + Redis clients with graceful shutdown"
```

---

## Group G: Proxy auth + billing libraries

### Task 20: Implement HMAC key lookup (auth library)

**Files:**
- Create: `proxy/internal/auth/auth.go`
- Create: `proxy/internal/auth/auth_test.go`

- [ ] **Step 1: Write the failing test**

`proxy/internal/auth/auth_test.go`:

```go
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashKey(t *testing.T) {
	secret := "test-secret-32-bytes-long-padded-x"

	h1 := HashKey("sk-or-abc", secret)
	h2 := HashKey("sk-or-abc", secret)
	assert.Equal(t, h1, h2, "same input must hash to same output")

	h3 := HashKey("sk-or-xyz", secret)
	assert.NotEqual(t, h1, h3, "different inputs must hash differently")

	assert.Len(t, h1, 64, "HMAC-SHA256 hex is 64 chars")
}
```

- [ ] **Step 2: Run test, watch fail**

```bash
cd proxy && go test ./internal/auth/...
```

Expected: FAIL (package doesn't exist yet).

- [ ] **Step 3: Implement auth.go (HashKey first)**

`proxy/internal/auth/auth.go`:

```go
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func HashKey(plaintext, serverSecret string) string {
	mac := hmac.New(sha256.New, []byte(serverSecret))
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// LookupResult is the cached/db-backed view of an API key + its owner
type LookupResult struct {
	APIKeyID     uuid.UUID `json:"api_key_id"`
	UserID       uuid.UUID `json:"user_id"`
	CreditsCents int64     `json:"credits_cents"`
	UserStatus   string    `json:"user_status"`
}

var (
	ErrKeyNotFound = errors.New("api key not found or revoked")
	ErrUserBanned  = errors.New("user not active")
)

const cacheTTL = 5 * time.Minute

type Auth struct {
	PG     *pgxpool.Pool
	RDB    *redis.Client
	Secret string
}

func New(pg *pgxpool.Pool, rdb *redis.Client, secret string) *Auth {
	return &Auth{PG: pg, RDB: rdb, Secret: secret}
}

// LookupBearer extracts "sk-or-..." from "Bearer ..." and looks it up.
func (a *Auth) LookupBearer(ctx context.Context, header string) (*LookupResult, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, ErrKeyNotFound
	}
	plaintext := strings.TrimPrefix(header, "Bearer ")
	return a.Lookup(ctx, plaintext)
}

func (a *Auth) Lookup(ctx context.Context, plaintext string) (*LookupResult, error) {
	hash := HashKey(plaintext, a.Secret)

	// Redis fast path
	cacheKey := "key:" + hash
	cached, err := a.RDB.Get(ctx, cacheKey).Result()
	if err == nil {
		var r LookupResult
		if jerr := json.Unmarshal([]byte(cached), &r); jerr == nil {
			if r.UserStatus != "active" {
				return nil, ErrUserBanned
			}
			return &r, nil
		}
	}

	// Postgres fallback
	const q = `
		SELECT k.id, k.user_id, u.credits_cents, u.status
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = $1 AND k.revoked_at IS NULL
		LIMIT 1
	`
	var r LookupResult
	err = a.PG.QueryRow(ctx, q, hash).Scan(&r.APIKeyID, &r.UserID, &r.CreditsCents, &r.UserStatus)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if r.UserStatus != "active" {
		return nil, ErrUserBanned
	}

	// Populate cache
	if b, jerr := json.Marshal(&r); jerr == nil {
		_ = a.RDB.Set(ctx, cacheKey, b, cacheTTL).Err()
	}

	return &r, nil
}

// InvalidateCache wipes the Redis entry for a given hash. Called on key revoke from `web`.
// (Phase 0: not used in proxy; relevant for tests.)
func (a *Auth) InvalidateCache(ctx context.Context, hash string) error {
	return a.RDB.Del(ctx, "key:"+hash).Err()
}

// for use elsewhere
func _() { _ = fmt.Sprint("") }
```

- [ ] **Step 4: Run tests again**

```bash
go test ./internal/auth/...
```

Expected: PASS for `TestHashKey`.

- [ ] **Step 5: Commit**

```bash
cd .. && git add proxy/internal/auth
git commit -m "proxy: add API key lookup with Redis cache + Postgres fallback"
```

---

### Task 21: Implement billing library (reserve + finalize)

**Files:**
- Create: `proxy/internal/billing/billing.go`
- Create: `proxy/internal/billing/billing_test.go`

- [ ] **Step 1: Write the failing test (CeilDiv only — DB tests later)**

`proxy/internal/billing/billing_test.go`:

```go
package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCeilDiv(t *testing.T) {
	assert.Equal(t, int64(1), CeilDiv(1, 1))
	assert.Equal(t, int64(1), CeilDiv(1, 2))
	assert.Equal(t, int64(2), CeilDiv(3, 2))
	assert.Equal(t, int64(0), CeilDiv(0, 1))
	assert.Equal(t, int64(1), CeilDiv(999_999, 1_000_000))
	assert.Equal(t, int64(1), CeilDiv(1_000_000, 1_000_000))
	assert.Equal(t, int64(2), CeilDiv(1_000_001, 1_000_000))
}

func TestCalculateCost(t *testing.T) {
	// GPT-4o: $2.50/M input, $10/M output, 18% markup
	// 1000 prompt + 500 completion
	cost := CalculateCost(1000, 500, 250, 1000, 18)
	// input: ceil(1000*250 / 1_000_000) = 1
	// output: ceil(500*1000 / 1_000_000) = 1
	// provider = 2; margin = ceil(2*18/100) = 1
	// total = 3
	assert.Equal(t, int64(1), cost.InputCents)
	assert.Equal(t, int64(1), cost.OutputCents)
	assert.Equal(t, int64(2), cost.ProviderCents)
	assert.Equal(t, int64(1), cost.MarginCents)
	assert.Equal(t, int64(3), cost.TotalCents)
}
```

- [ ] **Step 2: Run, watch fail**

```bash
cd proxy && go test ./internal/billing/...
```

Expected: FAIL.

- [ ] **Step 3: Implement billing.go**

`proxy/internal/billing/billing.go`:

```go
package billing

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInsufficientCredits = errors.New("insufficient credits")

// CeilDiv = ceiling division for non-negative integers.
func CeilDiv(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

type Cost struct {
	InputCents    int64
	OutputCents   int64
	ProviderCents int64
	MarginCents   int64
	TotalCents    int64
}

func CalculateCost(promptTokens, completionTokens, inputCentsPerMillion, outputCentsPerMillion int64, markupPct int) Cost {
	input := CeilDiv(promptTokens*inputCentsPerMillion, 1_000_000)
	output := CeilDiv(completionTokens*outputCentsPerMillion, 1_000_000)
	provider := input + output
	margin := CeilDiv(provider*int64(markupPct), 100)
	return Cost{
		InputCents:    input,
		OutputCents:   output,
		ProviderCents: provider,
		MarginCents:   margin,
		TotalCents:    provider + margin,
	}
}

type Service struct {
	PG *pgxpool.Pool
}

func New(pg *pgxpool.Pool) *Service {
	return &Service{PG: pg}
}

// Reserve atomically deducts $amount from the user's balance and inserts a reservation row.
// Returns ErrInsufficientCredits if balance < amount.
func (s *Service) Reserve(ctx context.Context, userID, requestID uuid.UUID, amount int64) error {
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balanceAfter int64
	err = tx.QueryRow(ctx, `
		UPDATE users SET credits_cents = credits_cents - $1
		WHERE id = $2 AND credits_cents >= $1
		RETURNING credits_cents
	`, amount, userID).Scan(&balanceAfter)
	if err != nil {
		return ErrInsufficientCredits
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO credit_transactions (id, user_id, amount_cents, kind, request_id, balance_after_cents, created_at)
		VALUES (gen_random_uuid(), $1, $2, 'reservation', $3, $4, now())
	`, userID, -amount, requestID, balanceAfter)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Finalize records the actual cost and refunds the difference back to the user.
// reserveAmount is what Reserve charged; actualCost is what the request actually consumed.
func (s *Service) Finalize(ctx context.Context, userID, requestID uuid.UUID, reserveAmount, actualCost int64) error {
	refund := reserveAmount - actualCost
	if refund < 0 {
		refund = 0 // shouldn't happen but be defensive
	}

	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balanceAfter int64
	err = tx.QueryRow(ctx, `
		UPDATE users SET credits_cents = credits_cents + $1
		WHERE id = $2
		RETURNING credits_cents
	`, refund, userID).Scan(&balanceAfter)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO credit_transactions (id, user_id, amount_cents, kind, request_id, balance_after_cents, created_at)
		VALUES (gen_random_uuid(), $1, $2, 'consumption', $3, $4, now())
	`, userID, -actualCost, requestID, balanceAfter-refund)
	if err != nil {
		return err
	}

	if refund > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO credit_transactions (id, user_id, amount_cents, kind, request_id, balance_after_cents, created_at)
			VALUES (gen_random_uuid(), $1, $2, 'refund', $3, $4, now())
		`, userID, refund, requestID, balanceAfter)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/billing/...
```

Expected: PASS — both `TestCeilDiv` and `TestCalculateCost`.

- [ ] **Step 5: Commit**

```bash
cd .. && git add proxy/internal/billing
git commit -m "proxy: add billing reserve/finalize with ceil-div cost math"
```

---

### Task 22: Implement model catalog (in-memory cache)

**Files:**
- Create: `proxy/internal/catalog/catalog.go`

- [ ] **Step 1: Write catalog.go**

```go
package catalog

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Model struct {
	Alias                       string
	UpstreamProvider            string
	UpstreamModelID             string
	ContextWindow               int
	InputCentsPerMillionTokens  int64
	OutputCentsPerMillionTokens int64
	MarkupPct                   int
	Status                      string
}

var ErrModelNotFound = errors.New("model not found")

type Catalog struct {
	pg      *pgxpool.Pool
	mu      sync.RWMutex
	byAlias map[string]Model
	stop    chan struct{}
}

func New(pg *pgxpool.Pool) *Catalog {
	return &Catalog{pg: pg, byAlias: map[string]Model{}, stop: make(chan struct{})}
}

func (c *Catalog) Refresh(ctx context.Context) error {
	const q = `
		SELECT alias, upstream_provider, upstream_model_id, context_window,
		       input_cents_per_million_tokens, output_cents_per_million_tokens,
		       markup_pct, status
		FROM model_catalog
	`
	rows, err := c.pg.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()

	next := map[string]Model{}
	for rows.Next() {
		var m Model
		if err := rows.Scan(&m.Alias, &m.UpstreamProvider, &m.UpstreamModelID, &m.ContextWindow,
			&m.InputCentsPerMillionTokens, &m.OutputCentsPerMillionTokens, &m.MarkupPct, &m.Status); err != nil {
			return err
		}
		next[m.Alias] = m
	}
	c.mu.Lock()
	c.byAlias = next
	c.mu.Unlock()
	return nil
}

func (c *Catalog) Get(alias string) (Model, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.byAlias[alias]
	if !ok || m.Status != "active" {
		return Model{}, ErrModelNotFound
	}
	return m, nil
}

func (c *Catalog) StartAutoRefresh(ctx context.Context, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_ = c.Refresh(ctx)
			case <-c.stop:
				return
			}
		}
	}()
}

func (c *Catalog) Stop() { close(c.stop) }
```

- [ ] **Step 2: Commit**

```bash
git add proxy/internal/catalog
git commit -m "proxy: add in-memory model catalog with 60s auto-refresh"
```

---

## Group H: Provider adapter + completions endpoint

### Task 23: Implement OpenAI provider adapter

**Files:**
- Create: `proxy/internal/provider/provider.go`
- Create: `proxy/internal/provider/openai/openai.go`

- [ ] **Step 1: Define Provider interface**

`proxy/internal/provider/provider.go`:

```go
package provider

import "context"

// Phase 0: non-streaming only.
type Request struct {
	Model    string
	Messages []Message
	MaxTokens int
}

type Message struct {
	Role    string
	Content string
}

type Response struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
}

type Provider interface {
	Send(ctx context.Context, req Request, apiKey string) (*Response, error)
}
```

- [ ] **Step 2: Implement OpenAI adapter**

`proxy/internal/provider/openai/openai.go`:

```go
package openai

import (
	"context"
	"errors"

	openaisdk "github.com/sashabaranov/go-openai"
	"github.com/your-org/maas-router/proxy/internal/provider"
)

// Replace the import path above to match your go.mod module name.

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Send(ctx context.Context, req provider.Request, apiKey string) (*provider.Response, error) {
	if apiKey == "" {
		return nil, errors.New("openai api key not configured")
	}
	client := openaisdk.NewClient(apiKey)

	var msgs []openaisdk.ChatCompletionMessage
	for _, m := range req.Messages {
		msgs = append(msgs, openaisdk.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}

	resp, err := client.CreateChatCompletion(ctx, openaisdk.ChatCompletionRequest{
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices returned")
	}
	return &provider.Response{
		Content:          resp.Choices[0].Message.Content,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}, nil
}
```

- [ ] **Step 3: Commit**

```bash
git add proxy/internal/provider
git commit -m "proxy: add Provider interface + OpenAI non-streaming adapter"
```

---

### Task 24: Implement Reactor pattern for async request_logs

**Files:**
- Create: `proxy/internal/logging/reactor.go`

- [ ] **Step 1: Write reactor.go**

```go
package logging

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RequestLogEntry struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	APIKeyID          uuid.UUID
	APISurface        string
	UpstreamProvider  string
	UpstreamModel     string
	ModelAlias        string
	PromptTokens      *int
	CompletionTokens  *int
	InputCostCents    int
	OutputCostCents   int
	MarginCents       int
	TotalChargedCents int
	Streaming         bool
	Status            string
	ErrorMessage      *string
	LatencyMs         *int
	CreatedAt         time.Time
	CompletedAt       *time.Time
}

type Reactor struct {
	pg       *pgxpool.Pool
	inbox    chan RequestLogEntry
	flushAt  time.Duration
	maxBatch int
}

func New(pg *pgxpool.Pool) *Reactor {
	return &Reactor{
		pg:       pg,
		inbox:    make(chan RequestLogEntry, 10_000),
		flushAt:  100 * time.Millisecond,
		maxBatch: 50,
	}
}

func (r *Reactor) Push(entry RequestLogEntry) {
	select {
	case r.inbox <- entry:
	default:
		log.Printf("WARN: log inbox full, dropping entry id=%s", entry.ID)
	}
}

func (r *Reactor) Run(ctx context.Context) {
	batch := make([]RequestLogEntry, 0, r.maxBatch)
	ticker := time.NewTicker(r.flushAt)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := r.insertBatch(ctx, batch); err != nil {
			log.Printf("WARN: failed to insert log batch (%d entries): %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// drain remaining on shutdown
			for {
				select {
				case e := <-r.inbox:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		case e := <-r.inbox:
			batch = append(batch, e)
			if len(batch) >= r.maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (r *Reactor) insertBatch(ctx context.Context, entries []RequestLogEntry) error {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO request_logs
			(id, user_id, api_key_id, api_surface, upstream_provider, upstream_model, model_alias,
			 prompt_tokens, completion_tokens, input_cost_cents, output_cost_cents, margin_cents,
			 total_charged_cents, streaming, status, error_message, latency_ms, created_at, completed_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`
	for _, e := range entries {
		if _, err := tx.Exec(ctx, q,
			e.ID, e.UserID, e.APIKeyID, e.APISurface, e.UpstreamProvider, e.UpstreamModel, e.ModelAlias,
			e.PromptTokens, e.CompletionTokens, e.InputCostCents, e.OutputCostCents, e.MarginCents,
			e.TotalChargedCents, e.Streaming, e.Status, e.ErrorMessage, e.LatencyMs, e.CreatedAt, e.CompletedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
```

- [ ] **Step 2: Commit**

```bash
git add proxy/internal/logging
git commit -m "proxy: add Reactor pattern for async batched request_logs"
```

---

### Task 25: Wire POST /v1/chat/completions

**Files:**
- Create: `proxy/internal/server/openai.go`
- Modify: `proxy/cmd/proxy/main.go`

- [ ] **Step 1: Write the completions handler**

`proxy/internal/server/openai.go`:

```go
package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/maas-router/proxy/internal/auth"
	"github.com/your-org/maas-router/proxy/internal/billing"
	"github.com/your-org/maas-router/proxy/internal/catalog"
	"github.com/your-org/maas-router/proxy/internal/logging"
	"github.com/your-org/maas-router/proxy/internal/provider"
)

// Replace import path to match your go.mod.

type ChatCompletionsRequest struct {
	Model     string                          `json:"model"`
	Messages  []ChatCompletionMessage         `json:"messages"`
	MaxTokens int                             `json:"max_tokens,omitempty"`
	Stream    bool                            `json:"stream,omitempty"`
}

type ChatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionsResponse struct {
	ID      string                          `json:"id"`
	Object  string                          `json:"object"`
	Created int64                           `json:"created"`
	Model   string                          `json:"model"`
	Choices []ChatCompletionChoice          `json:"choices"`
	Usage   ChatCompletionUsage             `json:"usage"`
}

type ChatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      ChatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIHandler struct {
	Auth        *auth.Auth
	Billing     *billing.Service
	Catalog     *catalog.Catalog
	OpenAI      provider.Provider
	OpenAIKey   string
	Reactor     *logging.Reactor
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": "invalid_request_error"},
	})
}

func (h *OpenAIHandler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	started := time.Now()

	// Phase 0: streaming not supported
	var req ChatCompletionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Stream {
		writeError(w, http.StatusBadRequest, "streaming not yet supported (Phase 0 limitation)")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages required")
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024
	}

	// Auth
	result, err := h.Auth.LookupBearer(ctx, r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, auth.ErrUserBanned) {
			writeError(w, http.StatusForbidden, "account suspended or banned")
		} else {
			writeError(w, http.StatusUnauthorized, "invalid API key")
		}
		return
	}

	// Catalog lookup
	model, err := h.Catalog.Get(req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "model '"+req.Model+"' not found")
		return
	}
	if model.UpstreamProvider != "openai" {
		writeError(w, http.StatusBadRequest, "Phase 0 supports OpenAI models only")
		return
	}

	// Cost reservation — estimate worst case
	requestID := uuid.New()
	// Naive prompt-token estimate: 4 chars ≈ 1 token (Phase 0 placeholder; real tokenizer in Phase 1)
	var promptChars int
	for _, m := range req.Messages {
		promptChars += len(m.Content)
	}
	estPromptTokens := int64(promptChars / 4)
	maxCost := billing.CalculateCost(estPromptTokens, int64(req.MaxTokens), model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)
	if err := h.Billing.Reserve(ctx, result.UserID, requestID, maxCost.TotalCents); err != nil {
		if errors.Is(err, billing.ErrInsufficientCredits) {
			writeError(w, http.StatusPaymentRequired, "insufficient credits")
		} else {
			log.Printf("ERROR: reservation failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	// Call upstream
	var messages []provider.Message
	for _, m := range req.Messages {
		messages = append(messages, provider.Message{Role: m.Role, Content: m.Content})
	}
	upstream, err := h.OpenAI.Send(ctx, provider.Request{
		Model:     model.UpstreamModelID,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
	}, h.OpenAIKey)
	if err != nil {
		// Refund full reservation
		_ = h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, 0)
		log.Printf("ERROR: upstream call failed: %v", err)
		// Async log the failure
		now := time.Now()
		latency := int(time.Since(started).Milliseconds())
		errMsg := err.Error()
		h.Reactor.Push(logging.RequestLogEntry{
			ID: requestID, UserID: result.UserID, APIKeyID: result.APIKeyID,
			APISurface: "openai", UpstreamProvider: model.UpstreamProvider, UpstreamModel: model.UpstreamModelID,
			ModelAlias: model.Alias, Streaming: false, Status: "provider_error",
			ErrorMessage: &errMsg, LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
		})
		writeError(w, http.StatusBadGateway, "upstream provider error")
		return
	}

	// Compute actual cost
	actual := billing.CalculateCost(int64(upstream.PromptTokens), int64(upstream.CompletionTokens),
		model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)

	// Finalize (refund difference)
	if err := h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, actual.TotalCents); err != nil {
		log.Printf("ERROR: finalize failed: %v", err)
		// Continue — user got their response. Reaper will fix any stuck reservation.
	}

	// Build response
	now := time.Now()
	latency := int(time.Since(started).Milliseconds())
	out := ChatCompletionsResponse{
		ID:      "chatcmpl-" + requestID.String(),
		Object:  "chat.completion",
		Created: now.Unix(),
		Model:   req.Model,
		Choices: []ChatCompletionChoice{{
			Index:        0,
			Message:      ChatCompletionMessage{Role: "assistant", Content: upstream.Content},
			FinishReason: "stop",
		}},
		Usage: ChatCompletionUsage{
			PromptTokens:     upstream.PromptTokens,
			CompletionTokens: upstream.CompletionTokens,
			TotalTokens:      upstream.PromptTokens + upstream.CompletionTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)

	// Async log
	promptT := upstream.PromptTokens
	compT := upstream.CompletionTokens
	h.Reactor.Push(logging.RequestLogEntry{
		ID:                requestID,
		UserID:            result.UserID,
		APIKeyID:          result.APIKeyID,
		APISurface:        "openai",
		UpstreamProvider:  model.UpstreamProvider,
		UpstreamModel:     model.UpstreamModelID,
		ModelAlias:        model.Alias,
		PromptTokens:      &promptT,
		CompletionTokens: &compT,
		InputCostCents:    int(actual.InputCents),
		OutputCostCents:   int(actual.OutputCents),
		MarginCents:       int(actual.MarginCents),
		TotalChargedCents: int(actual.TotalCents),
		Streaming:         false,
		Status:            "success",
		LatencyMs:         &latency,
		CreatedAt:         started,
		CompletedAt:       &now,
	})
}
```

- [ ] **Step 2: Wire everything together in main.go**

Replace `proxy/cmd/proxy/main.go`:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/your-org/maas-router/proxy/internal/auth"
	"github.com/your-org/maas-router/proxy/internal/billing"
	"github.com/your-org/maas-router/proxy/internal/catalog"
	"github.com/your-org/maas-router/proxy/internal/logging"
	"github.com/your-org/maas-router/proxy/internal/provider/openai"
	"github.com/your-org/maas-router/proxy/internal/server"
	"github.com/your-org/maas-router/proxy/internal/storage"
)

// Replace import path to match your go.mod.

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pg, err := storage.NewPostgres(ctx)
	if err != nil {
		log.Fatalf("postgres init: %v", err)
	}
	defer pg.Close()

	rdb, err := storage.NewRedis(ctx)
	if err != nil {
		log.Fatalf("redis init: %v", err)
	}
	defer rdb.Close()

	hmacSecret := os.Getenv("HMAC_SERVER_SECRET")
	if hmacSecret == "" {
		log.Fatal("HMAC_SERVER_SECRET not set")
	}

	cat := catalog.New(pg)
	if err := cat.Refresh(ctx); err != nil {
		log.Fatalf("catalog refresh: %v", err)
	}
	cat.StartAutoRefresh(ctx, 60*time.Second)

	reactor := logging.New(pg)
	go reactor.Run(ctx)

	handler := &server.OpenAIHandler{
		Auth:      auth.New(pg, rdb, hmacSecret),
		Billing:   billing.New(pg),
		Catalog:   cat,
		OpenAI:    openai.New(),
		OpenAIKey: os.Getenv("OPENAI_API_KEY"),
		Reactor:   reactor,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/v1/chat/completions", handler.ChatCompletions)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("proxy listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("shutting down")

	shutdownCtx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	_ = srv.Shutdown(shutdownCtx)
	cancel() // signals reactor + catalog to stop
	time.Sleep(200 * time.Millisecond) // brief drain window
}
```

- [ ] **Step 3: Verify the build**

```bash
cd proxy && go build ./... && cd ..
```

Expected: no compile errors.

- [ ] **Step 4: Commit**

```bash
git add proxy/
git commit -m "proxy: wire POST /v1/chat/completions end-to-end (auth, reserve, call, finalize, log)"
```

---

## Group I: End-to-end integration test

### Task 26: Manual end-to-end smoke test

**Files:**
- Create: `scripts/smoke-test.sh` (helper script)

- [ ] **Step 1: Write the smoke test script**

`scripts/smoke-test.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Reads $API_KEY from environment. Pass the plaintext key created in /dashboard/keys.

if [ -z "${API_KEY:-}" ]; then
  echo "Usage: API_KEY=sk-or-... ./scripts/smoke-test.sh"
  exit 1
fi

echo "=== Checking proxy health ==="
curl -fsS http://localhost:8080/healthz && echo

echo
echo "=== Making chat completion request ==="
curl -sS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o",
    "messages": [{"role": "user", "content": "Say hello in one word."}],
    "max_tokens": 20
  }' | tee /tmp/maas-response.json

echo
echo
echo "=== Response details ==="
echo "Status: $(jq -r '.choices[0].finish_reason' /tmp/maas-response.json)"
echo "Tokens: $(jq -r '.usage.prompt_tokens' /tmp/maas-response.json) in / $(jq -r '.usage.completion_tokens' /tmp/maas-response.json) out"

echo
echo "=== Asking DB for the request log (after 1s drain) ==="
sleep 1
docker compose exec -T postgres psql -U app -d maas -c "
  SELECT model_alias, status, prompt_tokens, completion_tokens, total_charged_cents, latency_ms
  FROM request_logs ORDER BY created_at DESC LIMIT 1;
"

echo
echo "=== Current balance ==="
docker compose exec -T postgres psql -U app -d maas -c "
  SELECT email, credits_cents FROM users
  WHERE id = (SELECT user_id FROM request_logs ORDER BY created_at DESC LIMIT 1);
"
```

- [ ] **Step 2: Make it executable and run**

```bash
chmod +x scripts/smoke-test.sh
```

First, complete the manual setup:

1. `docker compose up` (in another terminal — leave running)
2. Visit http://localhost:3000/signup, create an account, verify email
3. Visit http://localhost:3000/dashboard/keys, create a key, copy plaintext
4. Run: `API_KEY=sk-or-... ./scripts/smoke-test.sh`

Expected output:
- Proxy returns `ok`
- Chat completion returns a JSON with assistant message
- DB query shows one row in `request_logs` with `status='success'`
- User balance is `100 - total_charged_cents` (the trial credit minus the request cost)

If anything fails, check `docker compose logs proxy` and `docker compose logs web`.

- [ ] **Step 3: Commit**

```bash
git add scripts/smoke-test.sh
git commit -m "scripts: add manual end-to-end smoke test"
```

---

### Task 27: Add landing page (minimal)

**Files:**
- Modify: `web/src/app/page.tsx`

- [ ] **Step 1: Replace the default Next.js landing**

`web/src/app/page.tsx`:

```tsx
import Link from 'next/link'
import { Button } from '@/components/ui/button'

export default function HomePage() {
  return (
    <div className="min-h-screen bg-gray-50">
      <header className="border-b bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <div className="font-bold">⚡ MaaS Router</div>
          <div className="flex items-center gap-3">
            <Link href="/login" className="text-sm text-gray-600 hover:underline">Log in</Link>
            <Button asChild>
              <Link href="/signup">Get Started</Link>
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-3xl px-6 py-24 text-center">
        <h1 className="text-4xl font-bold leading-tight">
          Call frontier LLMs through one API.<br />
          <span className="text-indigo-600">Pay with Alipay or WeChat.</span>
        </h1>
        <p className="mx-auto mt-6 max-w-xl text-lg text-gray-600">
          GPT-4o, Claude, Gemini — one OpenAI-compatible endpoint, transparent 18% markup,
          $1 free trial to get started.
        </p>
        <div className="mt-10">
          <Button asChild size="lg">
            <Link href="/signup">Start with $1 free →</Link>
          </Button>
        </div>
        <p className="mt-4 text-xs text-gray-500">No credit card required for signup.</p>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Verify and commit**

Visit http://localhost:3000 — landing page should render with hero + CTA.

```bash
git add web/src/app/page.tsx
git commit -m "web: replace default landing with phase 0 hero"
```

---

## Self-review checklist

Run through the spec sections relevant to Phase 0 (§3 architecture, §4 data model, §5 request flow, §9 auth) and confirm coverage:

- [ ] **§3.1 Two deployables (web + proxy)** — Tasks 3, 18 ✓
- [ ] **§3.2 Postgres + Redis shared** — Tasks 5, 7, 19 ✓
- [ ] **§3.3 Service boundaries (web writes users/keys, proxy writes credit_transactions atomically)** — Tasks 11–17, 21 ✓
- [ ] **§4.1 All 9 tables + NextAuth tables** — Task 5 ✓
- [ ] **§4.2 Non-obvious decisions:**
  - HMAC-SHA256 key hashing (not bcrypt) — Tasks 8, 20 ✓
  - cents_per_million_tokens with ceilDiv — Task 21 ✓
  - USD-only credits — schema in Task 5 ✓
  - Atomic balance update — Task 21 ✓
  - credit_transactions.request_id NOT a FK — schema in Task 5 ✓
  - No soft delete — schema uses status/revoked_at ✓
  - HMAC server secret in env — Task 1 ✓
- [ ] **§5.1 Six-phase request flow** — Task 25 ✓ (non-streaming variant for Phase 0)
- [ ] **§5.2 Error paths** — Task 25 covers 401, 402, 502, provider error ✓
- [ ] **§9.1 NextAuth + Prisma + bcrypt + DB sessions** — Task 11 ✓
- [ ] **§9.2 Signup flows (email + GitHub)** — Tasks 11–14 ✓
- [ ] **§9.5 API key lifecycle (HMAC, prefix, show once)** — Tasks 8, 16, 17 ✓
- [ ] **§9.7 Trial credit gating (verified email, IP dedup)** — Task 9, 13 ✓

**Coverage gaps for Phase 0 (intentionally deferred):**
- Streaming, multiple providers, Anthropic surface → Phases 1–2
- Stripe / top-ups → Phase 3
- Admin panel, CAPTCHA, rate limits, audit log writes → Phase 4
- Bilingual UI, playground → Phase 5
- AWS / EKS / CloudFormation → Phase 6

**Stuck-reservation reaper** (§5.3): not in Phase 0 task list. Trade-off: in Phase 0 the system has tiny volume; reservations only get stuck if the proxy crashes between Reserve and Finalize. **Add to Phase 1 plan** along with streaming, since that's when stream interruptions make the reaper genuinely useful.

---

**End of Phase 0 plan.** Total tasks: 27. Estimated time: 2 weeks for solo developer including learning curve and debugging.

Next: when Phase 0 is shipped, brainstorm/plan Phase 1 (streaming + Anthropic & Google providers).
