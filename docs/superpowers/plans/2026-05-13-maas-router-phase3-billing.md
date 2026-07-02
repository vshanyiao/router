# MaaS Router — Phase 3: Billing (Stripe Checkout + Top-up Flow)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax. Independent task groups are marked for parallel dispatch.

**Goal:** Let users top up their credit balance through Stripe Checkout with card / Alipay / WeChat Pay methods. Money in → credit transactions. Webhook handles success, refunds, chargebacks. Auto-suspend on dispute.

**Architecture:** All money flow happens in `web` (Next.js):
- `/dashboard/billing` UI shows balance + top-up button + history
- Top-up modal → POST `/api/topup/create-session` → Stripe Checkout Session → redirect
- Stripe → POST `/api/stripe-webhook` → idempotent credit grant + reactor-pattern audit
- Refund and dispute webhooks → adjust balance + auto-suspend

The proxy is untouched in Phase 3. The schema (`payment_intents`, `credit_transactions`) is already in place from Phase 0.

**Reference:** Design spec §5 (billing math), §7 (top-up flow & idempotency).

**Branched from:** `main` at `480c579` (PR #6 merged).

**Out of scope:**
- Subscription/recurring billing — pre-paid only per spec
- Admin manual credit adjustments — Phase 4
- BYOK — removed from MVP per spec
- Bilingual UI / playground — Phase 5

---

## File Structure

```
web/
  src/
    lib/
      stripe.ts                       + NEW  Stripe client singleton
      idempotency.ts                  + NEW  Redis SETNX helper for webhook idempotency
    app/
      api/
        topup/
          create-session/route.ts     + NEW  POST: create Stripe Checkout Session
          history/route.ts            + NEW  GET: list user's payment_intents
        stripe-webhook/route.ts       + NEW  POST: receive Stripe events
      dashboard/
        billing/
          page.tsx                    + NEW  Balance + Top Up button + history table
          topup/
            success/page.tsx          + NEW  Poll balance until webhook lands
            cancel/page.tsx           + NEW  Static "cancelled" page
    components/
      topup-modal.tsx                 + NEW  Preset buttons + payment method selector
      topup-history-table.tsx         + NEW  List of past top-ups
  prisma/
    seed.ts                           ⚠ MODIFY  Add stripe_webhook_secret reference

.env.example                          ⚠ MODIFY  Add STRIPE_SECRET_KEY + STRIPE_WEBHOOK_SECRET
```

Roughly +800 lines.

---

## Wave 1 (sequential, foundation) — 3 tasks

### Task 1: Add Stripe dependency + env

**Files:**
- Modify: `web/package.json`
- Modify: `.env.example`, `web/.env.local`
- Modify: `web/src/lib/env.ts` (Zod schema)

- [ ] **Step 1:** `cd web && pnpm add stripe`
- [ ] **Step 2:** Add to `.env.example`:
  ```
  # Stripe — https://dashboard.stripe.com/test/apikeys
  STRIPE_SECRET_KEY=sk_test_...
  STRIPE_WEBHOOK_SECRET=whsec_...   # from `stripe listen` or dashboard endpoint
  ```
- [ ] **Step 3:** Extend `web/src/lib/env.ts` to validate (both optional for now — only required when topup is exercised):
  ```typescript
  STRIPE_SECRET_KEY: z.string().optional(),
  STRIPE_WEBHOOK_SECRET: z.string().optional(),
  ```
- [ ] **Step 4:** `npx tsc --noEmit`
- [ ] **Step 5:** `git add . && git commit -m "billing: install stripe sdk + env vars"`

### Task 2: Stripe client + idempotency helper

**Files:**
- Create: `web/src/lib/stripe.ts`
- Create: `web/src/lib/idempotency.ts`

- [ ] **Step 1:** Write `stripe.ts`:
  ```typescript
  import Stripe from 'stripe'
  import { env } from './env'

  // Lazy: only instantiate when STRIPE_SECRET_KEY is set. Routes that require
  // Stripe should check `stripe !== null` and return 503 if missing.
  export const stripe = env.STRIPE_SECRET_KEY
    ? new Stripe(env.STRIPE_SECRET_KEY, { apiVersion: '2024-12-18.acacia' })
    : null
  ```
- [ ] **Step 2:** Write `idempotency.ts`:
  ```typescript
  import { redis } from './redis'

  /** Returns true if this is the first time we've seen the key (in 24h).
   *  Returns false if already processed — callers should ack and skip work. */
  export async function claimWebhookEvent(eventId: string): Promise<boolean> {
    const ok = await redis.set(`idem:${eventId}`, '1', 'EX', 86400, 'NX')
    return ok === 'OK'
  }
  ```
- [ ] **Step 3:** Build + commit

### Task 3: Schema migration for new fields (if any)

Audit current `payment_intents` schema — Phase 0 already added all required fields. **No migration needed.** Document this and move on.

- [ ] **Step 1:** Verify by `grep -A 20 "model PaymentIntent" web/prisma/schema.prisma` — confirm `stripeCheckoutSessionId`, `stripePaymentIntentId`, `status`, `currency` all exist
- [ ] **Step 2:** No code change. Skip commit.

---

## Wave 2 (parallel, 4 agents) — 4 tasks

These tasks touch independent files and can be dispatched concurrently.

### Task 4 (parallel A): POST /api/topup/create-session

**Files:** Create `web/src/app/api/topup/create-session/route.ts`

Logic:
1. Auth via `auth()` — 401 if no session
2. Parse + validate amount: must be in `app_config.topup_presets_cents` (default `[500, 1000, 2000, 5000, 10000]`)
3. `prisma.paymentIntent.create({ status: 'pending', amount, credits_added })`
4. `stripe.checkout.sessions.create({ payment_method_types: ['card', 'alipay', 'wechat_pay'], line_items: [...], metadata: { payment_intent_id }, success_url, cancel_url })`
5. Return `{ url: session.url }` for client redirect

```typescript
import { NextResponse } from 'next/server'
import { z } from 'zod'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { stripe } from '@/lib/stripe'
import { env } from '@/lib/env'

const schema = z.object({ amountCents: z.number().int().positive() })

export async function POST(req: Request) {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  if (!stripe) return NextResponse.json({ error: 'Billing not configured' }, { status: 503 })

  const body = await req.json().catch(() => null)
  const parsed = schema.safeParse(body)
  if (!parsed.success) return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })

  // Validate against allowed presets
  const cfg = await prisma.appConfig.findUnique({ where: { key: 'topup_presets_cents' } })
  const presets = (cfg?.value as number[]) ?? [500, 1000, 2000, 5000, 10000]
  if (!presets.includes(parsed.data.amountCents)) {
    return NextResponse.json({ error: 'amount must be one of the allowed presets' }, { status: 400 })
  }

  const pi = await prisma.paymentIntent.create({
    data: {
      userId: session.user.id,
      amountCents: BigInt(parsed.data.amountCents),
      creditsAddedCents: BigInt(parsed.data.amountCents),
      status: 'pending',
    },
  })

  const checkoutSession = await stripe.checkout.sessions.create({
    mode: 'payment',
    payment_method_types: ['card', 'alipay', 'wechat_pay'],
    line_items: [{
      price_data: {
        currency: 'usd',
        unit_amount: parsed.data.amountCents,
        product_data: { name: `MaaS Router credit top-up: $${(parsed.data.amountCents / 100).toFixed(2)}` },
      },
      quantity: 1,
    }],
    metadata: { payment_intent_id: pi.id },
    success_url: `${env.NEXTAUTH_URL}/dashboard/billing/topup/success?pi=${pi.id}`,
    cancel_url: `${env.NEXTAUTH_URL}/dashboard/billing/topup/cancel?pi=${pi.id}`,
  })

  await prisma.paymentIntent.update({
    where: { id: pi.id },
    data: { stripeCheckoutSessionId: checkoutSession.id },
  })

  return NextResponse.json({ url: checkoutSession.url })
}
```

Commit: `billing: add POST /api/topup/create-session`

### Task 5 (parallel B): POST /api/stripe-webhook

**Files:** Create `web/src/app/api/stripe-webhook/route.ts`

Logic:
1. Verify `stripe-signature` header via `stripe.webhooks.constructEvent` with the raw body
2. Claim idempotency on `event.id` (Redis SETNX). If already claimed, return 200 quietly.
3. Switch on `event.type`:
   - `checkout.session.completed` → mark payment_intent `succeeded`, UPDATE user.credits_cents += amount, INSERT credit_transactions(kind='topup')
   - `charge.refunded` → INSERT credit_transactions(kind='refund', amount negative), let balance go negative if needed
   - `charge.dispute.created` → INSERT credit_transactions(kind='chargeback'), set users.status='suspended', audit_log row
   - `checkout.session.expired` / `payment_intent.payment_failed` → mark payment_intent `failed`
4. All Postgres writes in ONE transaction per event.
5. Return 200 OK.

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { stripe } from '@/lib/stripe'
import { env } from '@/lib/env'
import { prisma } from '@/lib/db'
import { claimWebhookEvent } from '@/lib/idempotency'
import type Stripe from 'stripe'

export async function POST(req: NextRequest) {
  if (!stripe || !env.STRIPE_WEBHOOK_SECRET) {
    return NextResponse.json({ error: 'Webhook not configured' }, { status: 503 })
  }

  const sig = req.headers.get('stripe-signature')
  if (!sig) return NextResponse.json({ error: 'Missing stripe-signature' }, { status: 400 })

  const rawBody = await req.text()
  let event: Stripe.Event
  try {
    event = stripe.webhooks.constructEvent(rawBody, sig, env.STRIPE_WEBHOOK_SECRET)
  } catch (err) {
    return NextResponse.json({ error: 'Invalid signature' }, { status: 400 })
  }

  // First-line idempotency — fast Redis check
  const fresh = await claimWebhookEvent(event.id)
  if (!fresh) return NextResponse.json({ ok: true })

  try {
    switch (event.type) {
      case 'checkout.session.completed':
        await handleCheckoutCompleted(event.data.object as Stripe.Checkout.Session)
        break
      case 'checkout.session.expired':
      case 'payment_intent.payment_failed':
        await markFailed(event.data.object as any)
        break
      case 'charge.refunded':
        await handleRefund(event.data.object as Stripe.Charge)
        break
      case 'charge.dispute.created':
        await handleDispute(event.data.object as Stripe.Dispute)
        break
    }
  } catch (err) {
    console.error('Webhook handler failed:', err)
    return NextResponse.json({ error: 'Handler error' }, { status: 500 })
  }

  return NextResponse.json({ ok: true })
}

async function handleCheckoutCompleted(session: Stripe.Checkout.Session) {
  const piId = session.metadata?.payment_intent_id
  if (!piId) throw new Error('Missing payment_intent_id in session metadata')

  await prisma.$transaction(async (tx) => {
    // Conditional UPDATE — only credits if still 'pending'. Defense layer 3
    // against double-credit.
    const updated = await tx.paymentIntent.updateMany({
      where: { id: piId, status: 'pending' },
      data: {
        status: 'succeeded',
        stripePaymentIntentId: typeof session.payment_intent === 'string' ? session.payment_intent : null,
        completedAt: new Date(),
      },
    })
    if (updated.count === 0) return // already processed

    const pi = await tx.paymentIntent.findUnique({ where: { id: piId } })
    if (!pi) return

    const user = await tx.user.update({
      where: { id: pi.userId },
      data: { creditsCents: { increment: pi.creditsAddedCents } },
      select: { creditsCents: true },
    })

    await tx.creditTransaction.create({
      data: {
        userId: pi.userId,
        amountCents: pi.creditsAddedCents,
        kind: 'topup',
        paymentIntentId: pi.id,
        balanceAfterCents: user.creditsCents,
      },
    })
  })
}

async function markFailed(obj: any) {
  const piId = obj.metadata?.payment_intent_id
  if (!piId) return
  await prisma.paymentIntent.updateMany({
    where: { id: piId, status: 'pending' },
    data: { status: 'failed' },
  })
}

async function handleRefund(charge: Stripe.Charge) {
  const piId = charge.metadata?.payment_intent_id
  if (!piId) return
  const refunded = BigInt(charge.amount_refunded)
  await prisma.$transaction(async (tx) => {
    const pi = await tx.paymentIntent.findUnique({ where: { id: piId } })
    if (!pi) return
    const user = await tx.user.update({
      where: { id: pi.userId },
      data: { creditsCents: { decrement: refunded } },
      select: { creditsCents: true },
    })
    await tx.creditTransaction.create({
      data: {
        userId: pi.userId,
        amountCents: -refunded,
        kind: 'refund',
        paymentIntentId: pi.id,
        balanceAfterCents: user.creditsCents,
        description: 'stripe refund',
      },
    })
  })
}

async function handleDispute(dispute: Stripe.Dispute) {
  // Stripe sets the charge's metadata, not the dispute's. Fetch charge.
  if (!stripe) return
  const chargeId = typeof dispute.charge === 'string' ? dispute.charge : dispute.charge.id
  const charge = await stripe.charges.retrieve(chargeId)
  const piId = charge.metadata?.payment_intent_id
  if (!piId) return
  const disputed = BigInt(dispute.amount)
  await prisma.$transaction(async (tx) => {
    const pi = await tx.paymentIntent.findUnique({ where: { id: piId } })
    if (!pi) return
    const user = await tx.user.update({
      where: { id: pi.userId },
      data: { creditsCents: { decrement: disputed }, status: 'suspended' },
      select: { creditsCents: true },
    })
    await tx.creditTransaction.create({
      data: {
        userId: pi.userId,
        amountCents: -disputed,
        kind: 'chargeback',
        paymentIntentId: pi.id,
        balanceAfterCents: user.creditsCents,
        description: `stripe dispute ${dispute.id}`,
      },
    })
    await tx.auditLog.create({
      data: {
        targetUserId: pi.userId,
        kind: 'auto_suspend_chargeback',
        payload: { disputeId: dispute.id, chargeId, amount: dispute.amount },
      },
    })
  })
}
```

Commit: `billing: add POST /api/stripe-webhook with idempotency + refund/dispute handling`

### Task 6 (parallel C): GET /api/topup/history

**Files:** Create `web/src/app/api/topup/history/route.ts`

```typescript
import { NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const intents = await prisma.paymentIntent.findMany({
    where: { userId: session.user.id },
    orderBy: { createdAt: 'desc' },
    take: 20,
    select: {
      id: true,
      amountCents: true,
      creditsAddedCents: true,
      currency: true,
      status: true,
      createdAt: true,
      completedAt: true,
    },
  })
  // Convert BigInt to Number for JSON serialization (amounts fit safely in Number)
  return NextResponse.json({
    intents: intents.map((i) => ({
      ...i,
      amountCents: Number(i.amountCents),
      creditsAddedCents: Number(i.creditsAddedCents),
    })),
  })
}
```

Commit: `billing: add GET /api/topup/history`

### Task 7 (parallel D): top-up modal + history table components

**Files:** Create `web/src/components/topup-modal.tsx`, `web/src/components/topup-history-table.tsx`

`topup-modal.tsx`:

```tsx
'use client'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const PRESETS = [500, 1000, 2000, 5000, 10000]

export function TopUpModal() {
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState<number>(2000)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function checkout() {
    setLoading(true); setError('')
    const res = await fetch('/api/topup/create-session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ amountCents: selected }),
    })
    const data = await res.json()
    if (!res.ok) { setError(data.error || 'Failed'); setLoading(false); return }
    window.location.href = data.url
  }

  return (
    <>
      <Button onClick={() => setOpen(true)}>Top Up Credits</Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>Top Up</DialogTitle></DialogHeader>
          <div className="grid grid-cols-3 gap-2 my-4">
            {PRESETS.map((amt) => (
              <button
                key={amt}
                onClick={() => setSelected(amt)}
                className={`rounded border p-3 text-center ${selected === amt ? 'border-indigo-500 bg-indigo-50' : 'border-gray-200'}`}
              >
                <div className="font-bold">${(amt / 100).toFixed(0)}</div>
                <div className="text-xs text-gray-500">≈¥{((amt / 100) * 7.2).toFixed(0)}</div>
              </button>
            ))}
          </div>
          <p className="text-xs text-gray-600">Payment methods: Card, Alipay, WeChat Pay (chosen on next page).</p>
          {error && <p className="text-sm text-red-600">{error}</p>}
          <Button onClick={checkout} disabled={loading} className="w-full mt-4">
            {loading ? 'Redirecting…' : 'Continue to checkout'}
          </Button>
        </DialogContent>
      </Dialog>
    </>
  )
}
```

`topup-history-table.tsx`:

```tsx
'use client'
import { useEffect, useState } from 'react'

type Intent = {
  id: string
  amountCents: number
  status: string
  createdAt: string
  completedAt: string | null
}

export function TopUpHistoryTable() {
  const [intents, setIntents] = useState<Intent[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/topup/history').then(r => r.json()).then(d => setIntents(d.intents || [])).finally(() => setLoading(false))
  }, [])

  if (loading) return <p className="text-sm text-gray-500">Loading…</p>
  if (intents.length === 0) return <p className="text-sm text-gray-500">No top-ups yet.</p>

  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs uppercase text-gray-500">
        <tr><th className="pb-2">Date</th><th>Amount</th><th>Status</th></tr>
      </thead>
      <tbody>
        {intents.map(i => (
          <tr key={i.id} className="border-t">
            <td className="py-2">{new Date(i.createdAt).toLocaleString()}</td>
            <td>${(i.amountCents / 100).toFixed(2)}</td>
            <td>
              <span className={`text-xs px-2 py-1 rounded ${
                i.status === 'succeeded' ? 'bg-green-100 text-green-700' :
                i.status === 'failed' ? 'bg-red-100 text-red-700' :
                'bg-yellow-100 text-yellow-700'
              }`}>{i.status}</span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
```

Commit: `billing: add top-up modal + history table components`

---

## Wave 3 (sequential, ties it together) — 3 tasks

### Task 8: /dashboard/billing page

**Files:** Create `web/src/app/dashboard/billing/page.tsx`

```tsx
import { TopUpModal } from '@/components/topup-modal'
import { TopUpHistoryTable } from '@/components/topup-history-table'
import { BalanceWidget } from '@/components/balance-widget'

export default function BillingPage() {
  return (
    <div className="max-w-3xl space-y-6">
      <h1 className="text-2xl font-bold">Billing</h1>
      <div className="rounded-lg bg-white border p-6">
        <BalanceWidget />
        <div className="mt-4"><TopUpModal /></div>
      </div>
      <div className="rounded-lg bg-white border p-6">
        <h2 className="font-semibold mb-4">Top-up history</h2>
        <TopUpHistoryTable />
      </div>
    </div>
  )
}
```

Update `dashboard/layout.tsx` to add a `Billing` link in the sidebar nav between `API Keys` and `Usage`.

Commit: `billing: add /dashboard/billing page + sidebar link`

### Task 9: success + cancel pages

**Files:** Create `web/src/app/dashboard/billing/topup/success/page.tsx`, `web/src/app/dashboard/billing/topup/cancel/page.tsx`

Success page polls `/api/credits/balance` every 2s for up to 30s, then shows "Payment received, balance updated". Cancel page is static.

```tsx
// success/page.tsx
'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'

export default function TopupSuccessPage() {
  const [balance, setBalance] = useState<number | null>(null)
  const [polled, setPolled] = useState(0)
  const max = 15 // 30s at 2s intervals

  useEffect(() => {
    let cancelled = false
    let initialBalance: number | null = null

    async function tick() {
      if (cancelled) return
      const r = await fetch('/api/credits/balance')
      if (!r.ok) return
      const data = await r.json()
      if (initialBalance === null) initialBalance = data.creditsCents
      setBalance(data.creditsCents)
      // If balance changed from initial, we've seen the credit land.
      if (data.creditsCents > (initialBalance ?? 0)) return
      setPolled((p) => p + 1)
    }

    tick()
    const interval = setInterval(() => {
      if (polled >= max) { clearInterval(interval); return }
      tick()
    }, 2000)
    return () => { cancelled = true; clearInterval(interval) }
  }, [polled])

  return (
    <div className="max-w-md mx-auto p-8 text-center">
      <h1 className="text-2xl font-bold mb-2">Payment received</h1>
      <p className="text-gray-600 mb-6">
        {polled < max ? 'Waiting for credits to land in your account…' : 'Webhook still processing — your balance will update shortly.'}
      </p>
      {balance !== null && (
        <div className="rounded-lg border p-4 mb-6">
          <div className="text-xs uppercase text-gray-500">Current balance</div>
          <div className="text-3xl font-bold">${(balance / 100).toFixed(2)}</div>
        </div>
      )}
      <Link href="/dashboard/billing" className="text-indigo-600 hover:underline">Back to billing</Link>
    </div>
  )
}
```

```tsx
// cancel/page.tsx
import Link from 'next/link'

export default function TopupCancelPage() {
  return (
    <div className="max-w-md mx-auto p-8 text-center">
      <h1 className="text-2xl font-bold mb-2">Payment cancelled</h1>
      <p className="text-gray-600 mb-6">No charges were made. You can try again whenever you're ready.</p>
      <Link href="/dashboard/billing" className="text-indigo-600 hover:underline">Back to billing</Link>
    </div>
  )
}
```

Commit: `billing: add top-up success (polling) + cancel pages`

### Task 10: docker-compose stripe-cli forwarder

**Files:** Modify `docker-compose.yml`

Add the optional `stripe-cli` service from the original plan so local dev can forward Stripe events to the webhook:

```yaml
stripe-cli:
  image: stripe/stripe-cli:latest
  profiles: ["billing"]   # opt-in: docker compose --profile billing up
  command: listen --forward-to web:3000/api/stripe-webhook
  environment:
    STRIPE_API_KEY: ${STRIPE_SECRET_KEY}
```

Document in README how to use: `docker compose --profile billing up` plus copying the printed `whsec_…` value into `.env`.

Commit: `infra: add stripe-cli forwarder service (opt-in profile)`

---

## Wave 4 (final verification) — 1 task

### Task 11: Webhook idempotency test

**Files:** Create `web/src/app/api/stripe-webhook/idempotency_test.ts` (Vitest)

Mock `redis.set` and verify: first call returns true, second call returns false. Confirms the `claimWebhookEvent` function works.

```typescript
import { describe, it, expect, vi } from 'vitest'

const { mockSet } = vi.hoisted(() => ({ mockSet: vi.fn() }))
vi.mock('@/lib/redis', () => ({ redis: { set: mockSet } }))

import { claimWebhookEvent } from '@/lib/idempotency'

describe('claimWebhookEvent', () => {
  it('returns true on first call', async () => {
    mockSet.mockResolvedValueOnce('OK')
    expect(await claimWebhookEvent('evt_1')).toBe(true)
    expect(mockSet).toHaveBeenCalledWith('idem:evt_1', '1', 'EX', 86400, 'NX')
  })

  it('returns false when key already exists', async () => {
    mockSet.mockResolvedValueOnce(null)
    expect(await claimWebhookEvent('evt_1')).toBe(false)
  })
})
```

Commit: `billing: tests for webhook idempotency helper`

---

## Self-review

Coverage against spec §7 (Billing flow):

- [x] §7.1 Top-up flow: create-session → Stripe Checkout → webhook → credits added — Tasks 4, 5, 8, 9
- [x] §7.2 Pricing math: unchanged (existing CalculateCost in proxy)
- [x] §7.3 Three-layer idempotency:
  - Layer 1: Redis SETNX in `claimWebhookEvent` — Task 5
  - Layer 2: Postgres unique constraint on `stripe_payment_intent_id` — already in schema from Phase 0
  - Layer 3: Conditional UPDATE with `WHERE status='pending'` — Task 5 `handleCheckoutCompleted`
- [x] §7.4 Refund — `charge.refunded` handler in Task 5
- [x] §7.4 Chargeback auto-suspend — `charge.dispute.created` handler in Task 5 (sets `status='suspended'` + audit_log)
- [x] §7.4 Failed top-up — `checkout.session.expired` / `payment_intent.payment_failed` mark `failed`
- [x] Currency: stored in USD cents throughout; Stripe handles user-facing CNY display at checkout

Out of scope (deferred to Phase 4 admin panel or later):
- Admin-initiated manual refund
- Admin view of disputes
- CNY/USD display rate (uses `app_config.cny_per_usd_rate` from Phase 0)

## Risks & known gaps

- **Stripe API version pinning**: Using `2024-12-18.acacia`. Verify this is current. Bump if needed.
- **Webhook secret rotation**: not handled; single secret in env. Acceptable for Phase 3; revisit when ops becomes a priority.
- **Test mode vs live**: Phase 3 ships test-mode only. Production rollout (Phase 6) needs separate prod webhook + secret.
- **Alipay/WeChat one-time payments only**: Stripe's Alipay/WeChat methods don't support saved payment methods. Users repeat the full flow each top-up. Accepted per spec §7.5.
- **Race between checkout success and webhook**: success page polls `/api/credits/balance` for 30s. If the webhook is delayed >30s, user sees "still processing" and can refresh later.

---

## Parallelism summary

| Wave | Tasks | Mode |
|---|---|---|
| 1 | 1, 2, 3 | sequential (foundation: SDK install → client → schema audit) |
| 2 | 4, 5, 6, 7 | **4 parallel agents** (independent files, no shared state) |
| 3 | 8, 9, 10 | sequential (UI integration depends on Wave 2 components) |
| 4 | 11 | single task |

Total: 11 tasks. Estimated solo dev: 2 weeks (matches spec §11). Subagent-driven: ~2 hours.

---

**End of Phase 3 plan.**
