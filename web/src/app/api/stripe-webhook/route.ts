import { NextRequest, NextResponse } from 'next/server'
import type Stripe from 'stripe'
import { stripe } from '@/lib/stripe'
import { env } from '@/lib/env'
import { prisma } from '@/lib/db'
import { redis } from '@/lib/redis'
import { claimWebhookEvent } from '@/lib/idempotency'
import {
  handleCheckoutCompleted,
  handleAsyncPaymentSucceeded,
  handleSessionExpired,
  handleRefund,
  handleDispute,
} from '@/lib/stripe-webhook-handlers'

// Force Node.js runtime (not Edge): Stripe webhook signature verification
// requires the raw body, and we need pg + redis which aren't on Edge.
export const runtime = 'nodejs'

export async function POST(req: NextRequest) {
  if (!stripe || !env.STRIPE_WEBHOOK_SECRET) {
    return NextResponse.json({ error: 'Webhook not configured' }, { status: 503 })
  }

  const sig = req.headers.get('stripe-signature')
  if (!sig) {
    return NextResponse.json({ error: 'Missing stripe-signature header' }, { status: 400 })
  }

  const rawBody = await req.text()

  let event: Stripe.Event
  try {
    event = stripe.webhooks.constructEvent(rawBody, sig, env.STRIPE_WEBHOOK_SECRET)
  } catch {
    // Static message — don't echo library internals on a public endpoint.
    return NextResponse.json({ error: 'Invalid signature' }, { status: 400 })
  }

  // Layer 1 of the idempotency defense: Redis SETNX with 24h TTL. Layers 2/3
  // (per-handler conditional updates) live in the handlers and cover the
  // window beyond the Redis TTL and manual event resends.
  const fresh = await claimWebhookEvent(event.id)
  if (!fresh) {
    return NextResponse.json({ ok: true, idempotent: true })
  }

  const deps = { prisma, stripe }
  try {
    switch (event.type) {
      case 'checkout.session.completed':
        await handleCheckoutCompleted(deps, event.data.object as Stripe.Checkout.Session)
        break
      case 'checkout.session.async_payment_succeeded':
        await handleAsyncPaymentSucceeded(deps, event.data.object as Stripe.Checkout.Session)
        break
      case 'checkout.session.async_payment_failed':
      case 'checkout.session.expired':
        await handleSessionExpired(deps, event.data.object as Stripe.Checkout.Session)
        break
      case 'charge.refunded':
        await handleRefund(deps, event.data.object as Stripe.Charge)
        break
      case 'charge.dispute.created':
        await handleDispute(deps, event.data.object as Stripe.Dispute)
        break
      default:
        break
    }
  } catch (err) {
    console.error('stripe webhook handler failed:', event.type, err)
    // Release the idempotency claim so Stripe's retry actually reprocesses the
    // event. Without this, the claim outlives the failure and the retry is
    // treated as a duplicate — permanently dropping a paid event. The handlers
    // are individually idempotent, so reprocessing is safe.
    await redis.del(`idem:${event.id}`).catch(() => {})
    return NextResponse.json({ error: 'Handler error' }, { status: 500 })
  }

  return NextResponse.json({ ok: true })
}
