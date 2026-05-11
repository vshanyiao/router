import { NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const logs = await prisma.requestLog.findMany({
    where: { userId: session.user.id },
    orderBy: { createdAt: 'desc' },
    take: 50,
    select: {
      id: true,
      modelAlias: true,
      promptTokens: true,
      completionTokens: true,
      totalChargedCents: true,
      status: true,
      latencyMs: true,
      createdAt: true,
    },
  })
  return NextResponse.json({ logs })
}
