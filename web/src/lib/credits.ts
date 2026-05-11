import { prisma } from './db'
import { redis } from './redis'

/**
 * Grant trial credit if the user is eligible.
 *
 * Eligibility rules (checked in order):
 *   1. User must not already have a trial_credit transaction (per-user dedup, checked
 *      first so re-visits don't pollute Redis IP claims)
 *   2. The IP must not have been used to claim trial credit in the last 24h (Redis SETNX)
 *
 * Idempotent — safe to call from dashboard layout on every render.
 * Returns true if credit was granted, false otherwise.
 */
export async function grantTrialCreditIfEligible(
  userId: string,
  ipAddress: string,
): Promise<boolean> {
  // Per-user check FIRST.
  const existing = await prisma.creditTransaction.findFirst({
    where: { userId, kind: 'trial_credit' },
    select: { id: true },
  })
  if (existing) return false

  // Per-IP atomic dedup.
  const ok = await redis.set(`trial:ip:${ipAddress}`, userId, 'EX', 86400, 'NX')
  if (ok !== 'OK') return false

  const cfg = await prisma.appConfig.findUnique({ where: { key: 'trial_credit_cents' } })
  const amount = BigInt(typeof cfg?.value === 'number' ? cfg.value : 100)

  await prisma.$transaction(async (tx) => {
    const user = await tx.user.update({
      where: { id: userId },
      data: { creditsCents: { increment: amount } },
      select: { creditsCents: true },
    })
    await tx.creditTransaction.create({
      data: {
        userId,
        amountCents: amount,
        kind: 'trial_credit',
        balanceAfterCents: user.creditsCents,
      },
    })
  })

  return true
}
