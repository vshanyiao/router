import { NextResponse } from 'next/server'
import { z } from 'zod'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { stripe } from '@/lib/stripe'
import { env } from '@/lib/env'
import { rateLimit, clientIp } from '@/lib/rate-limit'

const schema = z.object({ amountCents: z.number().int().positive() })

const DEFAULT_PRESETS = [500, 1000, 2000, 5000, 10000]

export async function POST(req: Request) {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  if (!stripe) return NextResponse.json({ error: 'Billing not configured' }, { status: 503 })

  // M2: cap session creation to 10/hour per user — prevents a user from minting
  // unbounded pending payment_intents / Stripe sessions.
  if (!(await rateLimit(`rl:topup:${session.user.id}`, 10, 3600))) {
    return NextResponse.json({ error: 'Too many top-up attempts. Try again later.' }, { status: 429 })
  }

  const body = await req.json().catch(() => null)
  const parsed = schema.safeParse(body)
  if (!parsed.success) {
    return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })
  }

  // Validate against allowed presets from app_config (with fallback default)
  const cfg = await prisma.appConfig.findUnique({ where: { key: 'topup_presets_cents' } })
  const presets = Array.isArray(cfg?.value) ? (cfg.value as number[]) : DEFAULT_PRESETS
  if (!presets.includes(parsed.data.amountCents)) {
    return NextResponse.json({ error: 'amount must be one of the allowed presets' }, { status: 400 })
  }

  // Create the Stripe session FIRST, then persist the payment_intent. This
  // avoids the M2 orphan: if session creation throws, no eternal 'pending' row
  // is left behind. We backfill the session id right after creation.
  const pi = await prisma.paymentIntent.create({
    data: {
      userId: session.user.id,
      amountCents: BigInt(parsed.data.amountCents),
      creditsAddedCents: BigInt(parsed.data.amountCents),
      status: 'pending',
    },
  })

  let checkoutSession
  try {
    checkoutSession = await stripe.checkout.sessions.create({
      mode: 'payment',
      payment_method_types: ['card', 'alipay', 'wechat_pay'],
      line_items: [{
        price_data: {
          currency: 'usd',
          unit_amount: parsed.data.amountCents,
          product_data: {
            name: `MaaS Router credit top-up: $${(parsed.data.amountCents / 100).toFixed(2)}`,
          },
        },
        quantity: 1,
      }],
      payment_method_options: {
        wechat_pay: { client: 'web' },
      },
      metadata: { payment_intent_id: pi.id },
      payment_intent_data: { metadata: { payment_intent_id: pi.id } },
      success_url: `${env.NEXTAUTH_URL}/dashboard/billing/topup/success?pi=${pi.id}`,
      cancel_url: `${env.NEXTAUTH_URL}/dashboard/billing/topup/cancel?pi=${pi.id}`,
    })
  } catch (err) {
    // Stripe failed — mark the row failed instead of leaving it eternally pending.
    await prisma.paymentIntent.update({ where: { id: pi.id }, data: { status: 'failed' } }).catch(() => {})
    console.error('stripe session create failed:', err)
    return NextResponse.json({ error: 'Could not start checkout' }, { status: 502 })
  }

  await prisma.paymentIntent.update({
    where: { id: pi.id },
    data: { stripeCheckoutSessionId: checkoutSession.id },
  })

  return NextResponse.json({ url: checkoutSession.url })
}
