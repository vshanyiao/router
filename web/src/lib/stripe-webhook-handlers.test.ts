import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  handleCheckoutCompleted,
  handleSessionExpired,
  handleRefund,
  handleDispute,
} from './stripe-webhook-handlers'

/**
 * In-memory fake of the slice of Prisma these handlers touch. Models the two
 * behaviors the bug fixes depend on: conditional updateMany returning a count,
 * and $transaction running the callback against the same store.
 */
function makeFakePrisma(seed: {
  paymentIntents?: any[]
  users?: any[]
} = {}) {
  const store = {
    paymentIntents: seed.paymentIntents ?? [],
    users: seed.users ?? [{ id: 'user-1', creditsCents: 1000n, status: 'active' }],
    creditTransactions: [] as any[],
    auditLogs: [] as any[],
  }

  const client: any = {
    paymentIntent: {
      findUnique: vi.fn(async ({ where }: any) => {
        return (
          store.paymentIntents.find(
            (p) =>
              (where.id && p.id === where.id) ||
              (where.stripePaymentIntentId &&
                p.stripePaymentIntentId === where.stripePaymentIntentId),
          ) ?? null
        )
      }),
      updateMany: vi.fn(async ({ where, data }: any) => {
        let count = 0
        for (const p of store.paymentIntents) {
          if (where.id && p.id !== where.id) continue
          if (where.status) {
            if (where.status.in && !where.status.in.includes(p.status)) continue
            if (where.status.not && p.status === where.status.not) continue
            if (typeof where.status === 'string' && p.status !== where.status) continue
          }
          Object.assign(p, data)
          count++
        }
        return { count }
      }),
      update: vi.fn(async ({ where, data }: any) => {
        const p = store.paymentIntents.find((x) => x.id === where.id)
        if (!p) throw new Error('not found')
        for (const [k, v] of Object.entries<any>(data)) {
          if (v && typeof v === 'object' && 'increment' in v) p[k] = (p[k] ?? 0n) + v.increment
          else if (v && typeof v === 'object' && 'decrement' in v) p[k] = (p[k] ?? 0n) - v.decrement
          else p[k] = v
        }
        return p
      }),
    },
    user: {
      update: vi.fn(async ({ where, data }: any) => {
        const u = store.users.find((x) => x.id === where.id)
        if (!u) throw new Error('user not found')
        for (const [k, v] of Object.entries<any>(data)) {
          if (v && typeof v === 'object' && 'increment' in v) u[k] = (u[k] ?? 0n) + v.increment
          else if (v && typeof v === 'object' && 'decrement' in v) u[k] = (u[k] ?? 0n) - v.decrement
          else u[k] = v
        }
        return u
      }),
    },
    creditTransaction: {
      create: vi.fn(async ({ data }: any) => {
        store.creditTransactions.push(data)
        return data
      }),
    },
    auditLog: {
      create: vi.fn(async ({ data }: any) => {
        store.auditLogs.push(data)
        return data
      }),
    },
    $transaction: vi.fn(async (fn: any) => fn(client)),
  }

  return { client, store }
}

const stripeStub: any = { charges: { retrieve: vi.fn() } }

beforeEach(() => vi.clearAllMocks())

describe('handleCheckoutCompleted', () => {
  it('credits the user once and writes a topup ledger row', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [
        { id: 'pi-1', userId: 'user-1', status: 'pending', creditsAddedCents: 2000n, refundedCents: 0n },
      ],
    })
    const session: any = { metadata: { payment_intent_id: 'pi-1' }, payment_status: 'paid', payment_intent: 'pi_stripe_1' }
    await handleCheckoutCompleted({ prisma: client, stripe: stripeStub }, session)

    expect(store.users[0].creditsCents).toBe(3000n) // 1000 + 2000
    expect(store.creditTransactions).toHaveLength(1)
    expect(store.creditTransactions[0].kind).toBe('topup')
    expect(store.paymentIntents[0].status).toBe('succeeded')
  })

  it('is idempotent on duplicate delivery (already succeeded → no double credit)', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [
        { id: 'pi-1', userId: 'user-1', status: 'succeeded', creditsAddedCents: 2000n, refundedCents: 0n },
      ],
    })
    const session: any = { metadata: { payment_intent_id: 'pi-1' }, payment_status: 'paid' }
    await handleCheckoutCompleted({ prisma: client, stripe: stripeStub }, session)

    expect(store.users[0].creditsCents).toBe(1000n) // unchanged
    expect(store.creditTransactions).toHaveLength(0)
  })

  it('credits after a failed attempt is retried successfully (C2)', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [
        { id: 'pi-1', userId: 'user-1', status: 'failed', creditsAddedCents: 2000n, refundedCents: 0n },
      ],
    })
    const session: any = { metadata: { payment_intent_id: 'pi-1' }, payment_status: 'paid' }
    await handleCheckoutCompleted({ prisma: client, stripe: stripeStub }, session)

    expect(store.users[0].creditsCents).toBe(3000n)
    expect(store.paymentIntents[0].status).toBe('succeeded')
  })

  it('does not credit when payment_status is unpaid (I2)', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [
        { id: 'pi-1', userId: 'user-1', status: 'pending', creditsAddedCents: 2000n, refundedCents: 0n },
      ],
    })
    const session: any = { metadata: { payment_intent_id: 'pi-1' }, payment_status: 'unpaid' }
    await handleCheckoutCompleted({ prisma: client, stripe: stripeStub }, session)

    expect(store.users[0].creditsCents).toBe(1000n)
    expect(store.paymentIntents[0].status).toBe('pending')
  })
})

describe('handleRefund (C3: cumulative amount, decrement the delta)', () => {
  it('two partial refunds decrement 500 then 300, not 500 then 800', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [
        { id: 'pi-1', userId: 'user-1', status: 'succeeded', creditsAddedCents: 2000n, refundedCents: 0n },
      ],
      users: [{ id: 'user-1', creditsCents: 2000n, status: 'active' }],
    })
    const deps = { prisma: client, stripe: stripeStub }

    // First refund $5 → cumulative 500
    await handleRefund(deps, { id: 'ch_1', amount_refunded: 500, payment_intent: 'pi_stripe_1', metadata: { payment_intent_id: 'pi-1' } } as any)
    expect(store.users[0].creditsCents).toBe(1500n)
    expect(store.paymentIntents[0].refundedCents).toBe(500n)

    // Second refund $3 → cumulative 800; delta must be 300
    await handleRefund(deps, { id: 'ch_1', amount_refunded: 800, payment_intent: 'pi_stripe_1', metadata: { payment_intent_id: 'pi-1' } } as any)
    expect(store.users[0].creditsCents).toBe(1200n) // 1500 - 300, NOT 1500 - 800
    expect(store.paymentIntents[0].refundedCents).toBe(800n)
    expect(store.creditTransactions).toHaveLength(2)
  })

  it('is idempotent: redelivered refund event (same cumulative) is a no-op (I1)', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [
        { id: 'pi-1', userId: 'user-1', status: 'succeeded', creditsAddedCents: 2000n, refundedCents: 500n },
      ],
      users: [{ id: 'user-1', creditsCents: 1500n, status: 'active' }],
    })
    await handleRefund({ prisma: client, stripe: stripeStub }, { id: 'ch_1', amount_refunded: 500, metadata: { payment_intent_id: 'pi-1' } } as any)

    expect(store.users[0].creditsCents).toBe(1500n) // unchanged
    expect(store.creditTransactions).toHaveLength(0)
  })
})

describe('handleDispute', () => {
  it('decrements, suspends, audits — once (status-flip idempotency, I1)', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [
        { id: 'pi-1', userId: 'user-1', status: 'succeeded', stripePaymentIntentId: 'pi_stripe_1', creditsAddedCents: 2000n, refundedCents: 0n },
      ],
      users: [{ id: 'user-1', creditsCents: 2000n, status: 'active' }],
    })
    const deps = { prisma: client, stripe: stripeStub }
    const dispute: any = { id: 'dp_1', amount: 2000, charge: 'ch_1', payment_intent: 'pi_stripe_1' }

    await handleDispute(deps, dispute)
    expect(store.users[0].creditsCents).toBe(0n)
    expect(store.users[0].status).toBe('suspended')
    expect(store.auditLogs).toHaveLength(1)
    expect(store.auditLogs[0].kind).toBe('auto_suspend_chargeback')

    // Redelivery: row now 'disputed', flip matches 0 rows → no double decrement
    await handleDispute(deps, dispute)
    expect(store.users[0].creditsCents).toBe(0n)
    expect(store.auditLogs).toHaveLength(1)
  })

  it('writes an unmapped audit row + returns when it cannot resolve the intent (I6)', async () => {
    const { client, store } = makeFakePrisma({ paymentIntents: [] })
    stripeStub.charges.retrieve.mockResolvedValueOnce({ metadata: {} })
    const dispute: any = { id: 'dp_x', amount: 500, charge: 'ch_x' }

    await handleDispute({ prisma: client, stripe: stripeStub }, dispute)
    expect(store.auditLogs).toHaveLength(1)
    expect(store.auditLogs[0].kind).toBe('unmapped_dispute')
  })
})

describe('handleSessionExpired', () => {
  it('marks a pending intent expired', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [{ id: 'pi-1', userId: 'user-1', status: 'pending', creditsAddedCents: 2000n, refundedCents: 0n }],
    })
    await handleSessionExpired({ prisma: client, stripe: stripeStub }, { metadata: { payment_intent_id: 'pi-1' } } as any)
    expect(store.paymentIntents[0].status).toBe('expired')
  })

  it('does not touch an already-succeeded intent', async () => {
    const { client, store } = makeFakePrisma({
      paymentIntents: [{ id: 'pi-1', userId: 'user-1', status: 'succeeded', creditsAddedCents: 2000n, refundedCents: 0n }],
    })
    await handleSessionExpired({ prisma: client, stripe: stripeStub }, { metadata: { payment_intent_id: 'pi-1' } } as any)
    expect(store.paymentIntents[0].status).toBe('succeeded')
  })
})
