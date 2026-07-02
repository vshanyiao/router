import { NextRequest, NextResponse } from 'next/server'
import { z } from 'zod'
import { prisma } from '@/lib/db'
import { requireAdmin, logAdminAction, AdminAuthError } from '@/lib/admin'

const updateSchema = z
  .object({
    displayName: z.string().min(1).max(128),
    inputCentsPerMillionTokens: z.number().int().nonnegative(),
    outputCentsPerMillionTokens: z.number().int().nonnegative(),
    markupPct: z.number().int().min(0).max(1000),
    status: z.enum(['active', 'disabled', 'deprecated']),
    supportsTools: z.boolean(),
    supportsVision: z.boolean(),
  })
  .partial()

export async function PATCH(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  let admin: { id: string; email: string }
  try {
    admin = await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) return NextResponse.json({ error: e.message }, { status: e.status })
    throw e
  }

  const { id } = await params
  const body = await req.json().catch(() => null)
  const parsed = updateSchema.safeParse(body)
  if (!parsed.success) return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })

  const changes = parsed.data
  if (Object.keys(changes).length === 0) {
    return NextResponse.json({ error: 'No editable fields provided' }, { status: 400 })
  }

  const model = await prisma.modelCatalog.update({ where: { id }, data: changes })
  await logAdminAction(admin.id, 'model_update', { id, changes })
  return NextResponse.json({ model })
}

export async function DELETE(_req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  let admin: { id: string; email: string }
  try {
    admin = await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) return NextResponse.json({ error: e.message }, { status: e.status })
    throw e
  }

  const { id } = await params
  await prisma.modelCatalog.update({ where: { id }, data: { status: 'disabled' } })
  await logAdminAction(admin.id, 'model_disable', { id })
  return NextResponse.json({ ok: true })
}
