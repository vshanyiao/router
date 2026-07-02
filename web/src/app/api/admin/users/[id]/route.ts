import { NextRequest, NextResponse } from 'next/server'
import { requireAdmin, logAdminAction, AdminAuthError } from '@/lib/admin'
import { prisma } from '@/lib/db'

const ACTION_TO_STATUS: Record<string, string> = {
  suspend: 'suspended',
  ban: 'banned',
  activate: 'active',
}

// PATCH: change account status (suspend / ban / activate).
export async function PATCH(
  req: NextRequest,
  { params }: { params: Promise<{ id: string }> },
) {
  let admin: { id: string; email: string }
  try {
    admin = await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }

  const { id } = await params

  let body: { action?: unknown }
  try {
    body = await req.json()
  } catch {
    return NextResponse.json({ error: 'Invalid JSON body' }, { status: 400 })
  }

  const action = typeof body.action === 'string' ? body.action : ''
  const status = ACTION_TO_STATUS[action]
  if (!status) {
    return NextResponse.json({ error: 'Invalid action' }, { status: 400 })
  }

  const user = await prisma.user.findUnique({ where: { id } })
  if (!user) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 })
  }

  const updated = await prisma.user.update({
    where: { id },
    data: { status },
    select: {
      id: true,
      email: true,
      status: true,
      creditsCents: true,
      githubId: true,
      createdAt: true,
    },
  })

  await logAdminAction(admin.id, 'user_' + action, {}, id)

  return NextResponse.json({
    user: {
      ...updated,
      creditsCents: Number(updated.creditsCents),
      method: updated.githubId ? 'github' : 'email',
    },
  })
}

// POST: manual credit adjustment (amount may be negative).
export async function POST(
  req: NextRequest,
  { params }: { params: Promise<{ id: string }> },
) {
  let admin: { id: string; email: string }
  try {
    admin = await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }

  const { id } = await params

  let body: { amountCents?: unknown; reason?: unknown }
  try {
    body = await req.json()
  } catch {
    return NextResponse.json({ error: 'Invalid JSON body' }, { status: 400 })
  }

  const amountCents = body.amountCents
  const reason = typeof body.reason === 'string' ? body.reason.trim() : ''
  if (typeof amountCents !== 'number' || !Number.isFinite(amountCents) || !Number.isInteger(amountCents)) {
    return NextResponse.json({ error: 'Invalid amountCents' }, { status: 400 })
  }
  if (!reason) {
    return NextResponse.json({ error: 'Reason is required' }, { status: 400 })
  }

  const user = await prisma.user.findUnique({ where: { id } })
  if (!user) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 })
  }

  const delta = BigInt(amountCents)

  const newBalanceCents = await prisma.$transaction(async (tx) => {
    const updated = await tx.user.update({
      where: { id },
      data: { creditsCents: { increment: delta } },
      select: { creditsCents: true },
    })
    await tx.creditTransaction.create({
      data: {
        userId: id,
        amountCents: delta,
        kind: 'adjustment',
        balanceAfterCents: updated.creditsCents,
        description: reason,
      },
    })
    return updated.creditsCents
  })

  await logAdminAction(admin.id, 'credit_adjust', { amountCents, reason }, id)

  return NextResponse.json({ ok: true, newBalanceCents: Number(newBalanceCents) })
}
