import { NextResponse } from 'next/server'
import { prisma } from '@/lib/db'

// Public catalog — no auth. Exposes only fields safe for the marketing page.
export async function GET() {
  const models = await prisma.modelCatalog.findMany({
    where: { status: 'active' },
    orderBy: [{ upstreamProvider: 'asc' }, { alias: 'asc' }],
    select: {
      alias: true,
      displayName: true,
      upstreamProvider: true,
      contextWindow: true,
      supportsTools: true,
      supportsVision: true,
      supportsStreaming: true,
      inputCentsPerMillionTokens: true,
      outputCentsPerMillionTokens: true,
      descriptionZh: true,
      descriptionEn: true,
    },
  })

  return NextResponse.json({ models })
}
