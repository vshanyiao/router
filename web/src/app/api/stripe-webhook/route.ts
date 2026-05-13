import { NextRequest, NextResponse } from 'next/server'
import type Stripe from 'stripe'
import { stripe } from '@/lib/stripe'
import { env } from '@/lib/env'
import { prisma } from '@/lib/db'
import { claimWebhookEvent } from '@/lib/idempotency'

// Force Node.js runtime (not Edge): Stripe webhook signature verification
// requires the raw body, which Edge runtime makes awkward. Also we need
// pg + redis which aren't available on Edge.
export const runtime = 'nodejs'

export async function POST(req: NextRequest) {
  if (!stripe || !env.STRIPE_WEBHOOK_SECRET) {
    return NextResponse.json({ error: 'Webhook not configured' }, { status: 503 })
  }

  const sig = req.headers.get('stripe-signature')
  if (!sig) {
    return NextResponse.json({ error: 'Missing stripe-signature header' }, { status: 400 })
  }

  // Stripe requires the raw body bytes for signature verification.
  const rawBody = await req.text()

  let event: Stripe.Event
  try {
    event = stripe.webhooks.constructEvent(rawBody, sig, env.STRIPE_WEBHOOK_SECRET)
  } catch (err) {
    return NextResponse.json(
      { error: 'Invalid signature: ' + (err as Error).message },
      { status: 400 },
    )
  }

  // Layer 1 of 3 against double-processing: Redis SETNX with 24h TTL.
  // Layer 2 is the unique constraint on payment_intents.stripe_payment_intent_id.
  // Layer 3 is the conditional UPDATE WHERE status='pending' inside each handler.
  const fresh = await claimWebhookEvent(event.id)
  if (!fresh) {
    return NextResponse.json({ ok: true, idempotent: true })
  }

  try {
    switch (event.type) {
      case 'checkout.session.completed':
        await handleCheckoutCompleted(event.data.object as Stripe.Checkout.Session)
        break
      case 'checkout.session.expired':
      case 'payment_intent.payment_failed':
        await markFailed(event.data.object as { metadata?: Record<string, string> })
        break
      case 'charge.refunded':
        await handleRefund(event.data.object as Stripe.Charge)
        break
      case 'charge.dispute.created':
        await handleDispute(event.data.object as Stripe.Dispute)
        break
      default:
        // Other event types ignored. Stripe sends a lot — we ack only the
        // ones we care about and 200 the rest.
        break
    }
  } catch (err) {
    console.error('stripe webhook handler failed:', event.type, err)
    return NextResponse.json({ error: 'Handler error' }, { status: 500 })
  }

  return NextResponse.json({ ok: true })
}

async function handleCheckoutCompleted(session: Stripe.Checkout.Session) {
  const piId = session.metadata?.payment_intent_id
  if (!piId) {
    console.error('checkout.session.completed missing payment_intent_id metadata')
    return
  }

  await prisma.$transaction(async (tx) => {
    // Layer 3: conditional UPDATE only flips pending → succeeded. A duplicate
    // webhook delivery that slipped past Redis SETNX (e.g., Redis down for a
    // moment) would have updated.count === 0 here and skip the credit grant.
    const updated = await tx.paymentIntent.updateMany({
      where: { id: piId, status: 'pending' },
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

async function markFailed(obj: { metadata?: Record<string, string> }) {
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
  if (refunded <= 0n) return

  await prisma.$transaction(async (tx) => {
    const pi = await tx.paymentIntent.findUnique({ where: { id: piId } })
    if (!pi) return

    const user = await tx.user.update({
      where: { id: pi.userId },
      // Balance is allowed to go negative on refund (user already spent the credits).
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
        payload: {
          disputeId: dispute.id,
          chargeId,
          amount: dispute.amount,
        },
      },
    })
  })
}
