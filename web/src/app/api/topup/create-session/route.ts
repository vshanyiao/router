import { NextResponse } from 'next/server'
import { z } from 'zod'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { stripe } from '@/lib/stripe'
import { env } from '@/lib/env'

const schema = z.object({ amountCents: z.number().int().positive() })

const DEFAULT_PRESETS = [500, 1000, 2000, 5000, 10000]

export async function POST(req: Request) {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  if (!stripe) return NextResponse.json({ error: 'Billing not configured' }, { status: 503 })

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

  // Create payment_intent in pending state. This is the row whose unique
  // constraint on stripe_payment_intent_id provides defense layer 2 against
  // double-credit (per spec §7.3).
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

  await prisma.paymentIntent.update({
    where: { id: pi.id },
    data: { stripeCheckoutSessionId: checkoutSession.id },
  })

  return NextResponse.json({ url: checkoutSession.url })
}
