import { NextResponse } from 'next/server'
import { requireAdmin, AdminAuthError } from '@/lib/admin'
import { prisma } from '@/lib/db'

/** Start of the current UTC day. */
function startOfTodayUtc(): Date {
  const now = new Date()
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()))
}

const ERROR_STATUSES = ['provider_error', 'insufficient_credits', 'rate_limited']

export async function GET() {
  try {
    await requireAdmin()

    const startOfToday = startOfTodayUtc()

    const [
      usersTotal,
      usersToday,
      revenueAgg,
      requestsToday,
      errorsToday,
      requestAgg,
    ] = await Promise.all([
      prisma.user.count(),
      prisma.user.count({ where: { createdAt: { gte: startOfToday } } }),
      prisma.paymentIntent.aggregate({
        _sum: { amountCents: true },
        where: { status: 'succeeded', completedAt: { gte: startOfToday } },
      }),
      prisma.requestLog.count({ where: { createdAt: { gte: startOfToday } } }),
      prisma.requestLog.count({
        where: { createdAt: { gte: startOfToday }, status: { in: ERROR_STATUSES } },
      }),
      prisma.requestLog.aggregate({
        _sum: {
          totalChargedCents: true,
          inputCostCents: true,
          outputCostCents: true,
        },
        where: { createdAt: { gte: startOfToday } },
      }),
    ])

    const revenueTodayCents = Number(revenueAgg._sum.amountCents ?? 0n)
    const totalChargedCents = requestAgg._sum.totalChargedCents ?? 0
    const inputCostCents = requestAgg._sum.inputCostCents ?? 0
    const outputCostCents = requestAgg._sum.outputCostCents ?? 0
    const cogsCents = inputCostCents + outputCostCents
    const marginCents = totalChargedCents - cogsCents
    const errorRate =
      requestsToday === 0 ? 0 : Math.round((errorsToday / requestsToday) * 1000) / 1000

    return NextResponse.json({
      usersTotal,
      usersToday,
      revenueTodayCents,
      requestsToday,
      errorsToday,
      errorRate,
      cogsCents,
      marginCents,
    })
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }
}
