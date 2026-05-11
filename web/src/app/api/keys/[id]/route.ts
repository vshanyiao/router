import { NextRequest, NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { redis } from '@/lib/redis'

export async function DELETE(_req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const { id } = await params
  const key = await prisma.apiKey.findUnique({ where: { id } })
  if (!key || key.userId !== session.user.id) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 })
  }
  if (key.revokedAt) {
    return NextResponse.json({ error: 'Already revoked' }, { status: 400 })
  }

  await prisma.apiKey.update({
    where: { id },
    data: { revokedAt: new Date() },
  })
  await redis.del(`key:${key.keyHash}`)

  return NextResponse.json({ ok: true })
}
