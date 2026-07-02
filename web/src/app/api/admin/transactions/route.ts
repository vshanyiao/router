import { NextResponse } from 'next/server'
import { requireAdmin, AdminAuthError } from '@/lib/admin'
import { prisma } from '@/lib/db'

export async function GET() {
  try {
    await requireAdmin()

    const rows = await prisma.creditTransaction.findMany({
      orderBy: { createdAt: 'desc' },
      take: 50,
      select: {
        id: true,
        amountCents: true,
        kind: true,
        description: true,
        createdAt: true,
        user: { select: { email: true } },
      },
    })

    // BigInt → Number for JSON serialization. Cents fit within
    // Number.MAX_SAFE_INTEGER (~$90 trillion).
    return NextResponse.json({
      transactions: rows.map((r) => ({
        id: r.id,
        userEmail: r.user.email,
        amountCents: Number(r.amountCents),
        kind: r.kind,
        description: r.description,
        createdAt: r.createdAt,
      })),
    })
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }
}
