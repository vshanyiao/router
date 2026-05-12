import { NextRequest, NextResponse } from 'next/server'
import { Prisma } from '@prisma/client'
import { prisma } from '@/lib/db'
import { grantTrialCreditIfEligible } from '@/lib/credits'

export async function POST(req: NextRequest) {
  const { token } = await req.json().catch(() => ({}))
  if (!token || typeof token !== 'string') {
    return NextResponse.json({ error: 'Missing token' }, { status: 400 })
  }

  const ip = req.headers.get('x-forwarded-for')?.split(',')[0].trim() || '0.0.0.0'

  let userId: string | null = null
  try {
    userId = await prisma.$transaction(async (tx) => {
      // Delete-first serializes concurrent verify attempts: the second request
      // hits "record not found" because the first transaction already deleted it.
      let deleted
      try {
        deleted = await tx.verificationToken.delete({ where: { token } })
      } catch (e) {
        if (e instanceof Prisma.PrismaClientKnownRequestError && e.code === 'P2025') {
          throw new Error('TOKEN_NOT_FOUND')
        }
        throw e
      }
      if (deleted.expires < new Date()) throw new Error('TOKEN_EXPIRED')

      const user = await tx.user.findUnique({ where: { email: deleted.identifier } })
      if (!user) throw new Error('USER_NOT_FOUND')

      await tx.user.update({
        where: { id: user.id },
        data: { emailVerifiedAt: new Date() },
      })
      return user.id
    })
  } catch (e) {
    const msg = (e as Error).message
    if (msg === 'TOKEN_EXPIRED') {
      return NextResponse.json({ error: 'Token expired' }, { status: 400 })
    }
    return NextResponse.json({ error: 'Invalid token' }, { status: 400 })
  }

  // Trial credit grant runs outside the verify transaction. It has its own
  // per-user DB check and per-IP Redis SETNX so concurrent calls are safe.
  await grantTrialCreditIfEligible(userId!, ip)

  return NextResponse.json({ ok: true })
}
