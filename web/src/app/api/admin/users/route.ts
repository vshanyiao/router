import { NextRequest, NextResponse } from 'next/server'
import { requireAdmin, AdminAuthError } from '@/lib/admin'
import { prisma } from '@/lib/db'

const VALID_STATUSES = ['active', 'suspended', 'banned']

export async function GET(req: NextRequest) {
  try {
    await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }

  const { searchParams } = req.nextUrl
  const q = searchParams.get('q')?.trim() || undefined
  const statusParam = searchParams.get('status')?.trim() || undefined
  const status = statusParam && VALID_STATUSES.includes(statusParam) ? statusParam : undefined

  const users = await prisma.user.findMany({
    where: {
      ...(q ? { email: { contains: q, mode: 'insensitive' } } : {}),
      ...(status ? { status } : {}),
    },
    orderBy: { createdAt: 'desc' },
    take: 50,
    select: {
      id: true,
      email: true,
      status: true,
      creditsCents: true,
      githubId: true,
      createdAt: true,
    },
  })

  // 30-day spend per user: one groupBy over request_logs scoped to the page ids.
  const thirtyDaysAgo = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000)
  const spendByUser =
    users.length === 0
      ? []
      : await prisma.requestLog.groupBy({
          by: ['userId'],
          _sum: { totalChargedCents: true },
          where: {
            userId: { in: users.map((u) => u.id) },
            createdAt: { gte: thirtyDaysAgo },
          },
        })

  const spendMap = new Map(
    spendByUser.map((row) => [row.userId, row._sum.totalChargedCents ?? 0]),
  )

  return NextResponse.json({
    users: users.map((u) => ({
      id: u.id,
      email: u.email,
      status: u.status,
      creditsCents: Number(u.creditsCents),
      spend30dCents: spendMap.get(u.id) ?? 0,
      method: u.githubId ? 'github' : 'email',
      createdAt: u.createdAt,
    })),
  })
}
