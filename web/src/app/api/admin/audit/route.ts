import { NextResponse } from 'next/server'
import { requireAdmin, AdminAuthError } from '@/lib/admin'
import { prisma } from '@/lib/db'

export async function GET() {
  try {
    await requireAdmin()

    const rows = await prisma.auditLog.findMany({
      orderBy: { createdAt: 'desc' },
      take: 50,
      select: {
        id: true,
        kind: true,
        targetUserId: true,
        payload: true,
        createdAt: true,
        actor: { select: { email: true } },
      },
    })

    return NextResponse.json({
      logs: rows.map((r) => ({
        id: r.id,
        actorEmail: r.actor?.email ?? null,
        kind: r.kind,
        targetUserId: r.targetUserId,
        payload: r.payload,
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
