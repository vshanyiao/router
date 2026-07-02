import { NextRequest, NextResponse } from 'next/server'
import { z } from 'zod'
import bcrypt from 'bcryptjs'
import { randomBytes } from 'node:crypto'
import { prisma } from '@/lib/db'
import { sendVerificationEmail } from '@/lib/email'
import { env } from '@/lib/env'
import { verifyTurnstile } from '@/lib/turnstile'
import { rateLimit, clientIp } from '@/lib/rate-limit'

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(10, 'Password must be at least 10 characters'),
  turnstileToken: z.string().optional(),
})

export async function POST(req: NextRequest) {
  const ip = clientIp(req)

  // Per-IP signup velocity cap: 5/hour. Blocks bulk trial-credit farming.
  if (!(await rateLimit(`rl:signup:ip:${ip}`, 5, 3600))) {
    return NextResponse.json({ error: 'Too many signup attempts. Try again later.' }, { status: 429 })
  }

  const body = await req.json().catch(() => null)
  const parsed = schema.safeParse(body)
  if (!parsed.success) {
    return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })
  }
  const { email, password } = parsed.data

  // Bot check (skipped if Turnstile not configured, e.g. dev).
  if (!(await verifyTurnstile(parsed.data.turnstileToken, ip))) {
    return NextResponse.json({ error: 'CAPTCHA verification failed' }, { status: 400 })
  }

  const existing = await prisma.user.findUnique({ where: { email } })
  if (existing) {
    // Don't disclose account existence. Return the same response shape as a
    // successful signup. The attacker can't tell whether an account exists,
    // and the legitimate user (who forgot they signed up) will retry login.
    return NextResponse.json({ ok: true, message: 'Check your email for the verification link.' })
  }

  const passwordHash = await bcrypt.hash(password, 12)
  await prisma.user.create({ data: { email, passwordHash } })

  const token = randomBytes(32).toString('hex')
  const expires = new Date(Date.now() + 24 * 60 * 60 * 1000)
  await prisma.verificationToken.create({
    data: { identifier: email, token, expires },
  })

  const verifyUrl = `${env.NEXTAUTH_URL}/auth/verify-email/${token}`
  await sendVerificationEmail(email, verifyUrl)

  return NextResponse.json({ ok: true, message: 'Check your email for the verification link.' })
}
