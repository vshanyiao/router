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
