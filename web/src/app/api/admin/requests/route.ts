import { NextResponse } from 'next/server'
import { requireAdmin, AdminAuthError } from '@/lib/admin'
import { prisma } from '@/lib/db'

export async function GET() {
  try {
    await requireAdmin()

    const requests = await prisma.requestLog.findMany({
      orderBy: { createdAt: 'desc' },
      take: 50,
      select: {
        id: true,
        modelAlias: true,
        upstreamProvider: true,
        promptTokens: true,
        completionTokens: true,
        totalChargedCents: true,
        status: true,
        latencyMs: true,
        createdAt: true,
      },
    })

    return NextResponse.json({ requests })
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }
}
