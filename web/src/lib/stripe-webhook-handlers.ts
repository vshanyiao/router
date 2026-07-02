import type Stripe from 'stripe'
import type { PrismaClient } from '@prisma/client'

/**
 * Stripe webhook event handlers, extracted from the route so they can be unit
 * tested with a mocked Prisma client and Stripe object. Each handler is
 * idempotent on its own (does not rely solely on the Redis SETNX layer, which
 * only has a 24h TTL while Stripe retries for up to 72h and supports manual
 * "resend event" indefinitely).
 *
 * The `deps` shape lets tests inject mocks without importing the real clients.
 */
export interface WebhookDeps {
  prisma: PrismaClient
  stripe: Stripe
}

/**
 * checkout.session.completed — the paid-and-fulfilled event.
 *
 * Idempotency: the conditional updateMany WHERE status IN (pending, failed)
 * flips exactly once; a redelivered event finds 0 rows and skips crediting.
 * We accept both 'pending' and 'failed' because a customer can fail one card
 * attempt (which fires payment_intent.payment_failed → we DON'T touch status
 * anymore, see below) and succeed on a retry within the same Checkout session.
 *
 * Guards against delayed-notification methods: if payment_status is 'unpaid'
 * the money hasn't actually arrived yet — skip until async_payment_succeeded.
 */
export async function handleCheckoutCompleted(
  deps: WebhookDeps,
  session: Stripe.Checkout.Session,
): Promise<void> {
  const piId = session.metadata?.payment_intent_id
  if (!piId) {
    console.error('checkout.session.completed missing payment_intent_id metadata')
    return
  }

  // Delayed-notification methods (future: some Alipay/WeChat flows) fire
  // completed with payment_status 'unpaid'. Only fulfill on actual payment.
  if (session.payment_status === 'unpaid') return

  await creditTopup(deps, piId, session)
}

/**
 * async_payment_succeeded — for delayed-notification methods, this is the real
 * "money arrived" signal. Same crediting path as completed.
 */
export async function handleAsyncPaymentSucceeded(
  deps: WebhookDeps,
  session: Stripe.Checkout.Session,
): Promise<void> {
  const piId = session.metadata?.payment_intent_id
  if (!piId) return
  await creditTopup(deps, piId, session)
}

async function creditTopup(
  deps: WebhookDeps,
  piId: string,
  session: Stripe.Checkout.Session,
): Promise<void> {
  await deps.prisma.$transaction(async (tx) => {
    // Conditional flip: pending|failed → succeeded. Zero rows means already
    // credited (or in a terminal non-fulfillable state) — skip the grant.
    const updated = await tx.paymentIntent.updateMany({
      where: { id: piId, status: { in: ['pending', 'failed'] } },
      data: {
        status: 'succeeded',
        stripePaymentIntentId:
          typeof session.payment_intent === 'string' ? session.payment_intent : null,
        completedAt: new Date(),
      },
    })
    if (updated.count === 0) return

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

/**
 * checkout.session.expired — the session timed out unpaid. Mark the row so it
 * shows as 'expired' in history. Only touches still-pending rows.
 *
 * NOTE: we deliberately do NOT handle payment_intent.payment_failed. A failed
 * card attempt inside Checkout is not terminal — the customer can retry with
 * another card in the same session. If we flipped the row to 'failed' on the
 * first decline, a successful retry's checkout.session.completed would still
 * credit correctly (we accept 'failed' in the conditional above), but marking
 * it failed prematurely muddies the history badge. Expiry is the true terminal.
 */
export async function handleSessionExpired(
  deps: WebhookDeps,
  session: Stripe.Checkout.Session,
): Promise<void> {
  const piId = session.metadata?.payment_intent_id
  if (!piId) return
  await deps.prisma.paymentIntent.updateMany({
    where: { id: piId, status: 'pending' },
    data: { status: 'expired' },
  })
}

/**
 * charge.refunded — a refund was issued. Stripe's charge.amount_refunded is
 * CUMULATIVE across all refunds on the charge, and this event fires once per
 * refund. We must decrement only the delta since our last-seen cumulative
 * figure, tracked in payment_intents.refunded_cents. This also gives the
 * refund path its own idempotency: a redelivered event has a delta of 0.
 */
export async function handleRefund(
  deps: WebhookDeps,
  charge: Stripe.Charge,
): Promise<void> {
  const piId = await resolvePaymentIntentId(deps, charge)
  if (!piId) {
    console.error('charge.refunded could not resolve payment_intent_id', { chargeId: charge.id })
    return
  }
  const cumulative = BigInt(charge.amount_refunded)

  await deps.prisma.$transaction(async (tx) => {
    const pi = await tx.paymentIntent.findUnique({ where: { id: piId } })
    if (!pi) return

    const delta = cumulative - pi.refundedCents
    if (delta <= 0n) return // redelivery or out-of-order — nothing new to refund

    await tx.paymentIntent.update({
      where: { id: pi.id },
      data: { refundedCents: cumulative },
    })

    const user = await tx.user.update({
      where: { id: pi.userId },
      // Balance may go negative — user already spent the credits.
      data: { creditsCents: { decrement: delta } },
      select: { creditsCents: true },
    })

    await tx.creditTransaction.create({
      data: {
        userId: pi.userId,
        amountCents: -delta,
        kind: 'refund',
        paymentIntentId: pi.id,
        balanceAfterCents: user.creditsCents,
        description: 'stripe refund',
      },
    })
  })
}

/**
 * charge.dispute.created — a chargeback. Decrement, suspend, audit. Idempotent
 * via a status flip guard: only the first delivery (row not yet 'disputed')
 * does the work.
 */
export async function handleDispute(
  deps: WebhookDeps,
  dispute: Stripe.Dispute,
): Promise<void> {
  const chargeId = typeof dispute.charge === 'string' ? dispute.charge : dispute.charge.id
  let piId: string | null = null

  // Prefer the payment_intent on the dispute directly, then DB lookup by the
  // stored stripePaymentIntentId, then fall back to charge metadata.
  if (dispute.payment_intent) {
    const stripePiId =
      typeof dispute.payment_intent === 'string' ? dispute.payment_intent : dispute.payment_intent.id
    const row = await deps.prisma.paymentIntent.findUnique({
      where: { stripePaymentIntentId: stripePiId },
    })
    piId = row?.id ?? null
  }
  if (!piId) {
    const charge = await deps.stripe.charges.retrieve(chargeId)
    piId = charge.metadata?.payment_intent_id ?? null
  }
  if (!piId) {
    // A chargeback is the highest fraud signal we get — never let it vanish
    // silently. Write an unmapped audit row and log loudly.
    console.error('charge.dispute.created could not be mapped to a payment intent', {
      disputeId: dispute.id,
      chargeId,
    })
    await deps.prisma.auditLog.create({
      data: {
        kind: 'unmapped_dispute',
        payload: { disputeId: dispute.id, chargeId, amount: dispute.amount },
      },
    })
    return
  }

  const disputed = BigInt(dispute.amount)

  await deps.prisma.$transaction(async (tx) => {
    // Status-flip idempotency: only suspend/decrement on the first delivery.
    const flipped = await tx.paymentIntent.updateMany({
      where: { id: piId!, status: { not: 'disputed' } },
      data: { status: 'disputed' },
    })
    if (flipped.count === 0) return

    const pi = await tx.paymentIntent.findUnique({ where: { id: piId! } })
    if (!pi) return

    const user = await tx.user.update({
      where: { id: pi.userId },
      data: {
        creditsCents: { decrement: disputed },
        status: 'suspended',
      },
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

/**
 * Resolve our internal payment_intent id from a Charge: prefer a DB lookup by
 * the stored Stripe PaymentIntent id (sturdier — no metadata dependency), fall
 * back to the metadata snapshot Stripe copies onto the charge.
 */
async function resolvePaymentIntentId(
  deps: WebhookDeps,
  charge: Stripe.Charge,
): Promise<string | null> {
  if (charge.payment_intent) {
    const stripePiId =
      typeof charge.payment_intent === 'string' ? charge.payment_intent : charge.payment_intent.id
    const row = await deps.prisma.paymentIntent.findUnique({
      where: { stripePaymentIntentId: stripePiId },
    })
    if (row) return row.id
  }
  return charge.metadata?.payment_intent_id ?? null
}
