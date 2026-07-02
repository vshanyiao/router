import { NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const models = await prisma.modelCatalog.findMany({
    where: { status: 'active', supportsStreaming: true },
    orderBy: { alias: 'asc' },
    select: {
      alias: true,
      displayName: true,
      upstreamProvider: true,
      inputCentsPerMillionTokens: true,
      outputCentsPerMillionTokens: true,
    },
  })

  return NextResponse.json({ models })
}
