import { NextRequest, NextResponse } from 'next/server'
import { z } from 'zod'
import { prisma } from '@/lib/db'
import { requireAdmin, logAdminAction, AdminAuthError } from '@/lib/admin'

const createSchema = z.object({
  alias: z.string().min(1).max(128),
  displayName: z.string().min(1).max(128),
  upstreamProvider: z.string().min(1).max(64),
  upstreamModelId: z.string().min(1).max(128),
  contextWindow: z.number().int().positive(),
  inputCentsPerMillionTokens: z.number().int().nonnegative(),
  outputCentsPerMillionTokens: z.number().int().nonnegative(),
  markupPct: z.number().int().min(0).max(1000),
  supportsTools: z.boolean().optional(),
  supportsVision: z.boolean().optional(),
  tags: z.array(z.string()).optional(),
})

export async function GET() {
  try {
    await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) return NextResponse.json({ error: e.message }, { status: e.status })
    throw e
  }

  const models = await prisma.modelCatalog.findMany({ orderBy: { alias: 'asc' } })
  return NextResponse.json({ models })
}

export async function POST(req: NextRequest) {
  let admin: { id: string; email: string }
  try {
    admin = await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) return NextResponse.json({ error: e.message }, { status: e.status })
    throw e
  }

  const body = await req.json().catch(() => null)
  const parsed = createSchema.safeParse(body)
  if (!parsed.success) return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })

  const data = parsed.data
  const model = await prisma.modelCatalog.create({
    data: {
      alias: data.alias,
      displayName: data.displayName,
      upstreamProvider: data.upstreamProvider,
      upstreamModelId: data.upstreamModelId,
      contextWindow: data.contextWindow,
      inputCentsPerMillionTokens: data.inputCentsPerMillionTokens,
      outputCentsPerMillionTokens: data.outputCentsPerMillionTokens,
      markupPct: data.markupPct,
      supportsStreaming: true,
      supportsTools: data.supportsTools ?? false,
      supportsVision: data.supportsVision ?? false,
      tags: data.tags ?? [],
      status: 'active',
    },
  })

  await logAdminAction(admin.id, 'model_create', { alias: model.alias })
  return NextResponse.json({ model }, { status: 201 })
}
