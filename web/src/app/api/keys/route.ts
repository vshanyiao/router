import { NextRequest, NextResponse } from 'next/server'
import { z } from 'zod'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { generateApiKey } from '@/lib/api-keys'

const createSchema = z.object({ name: z.string().min(1).max(64) })

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const keys = await prisma.apiKey.findMany({
    where: { userId: session.user.id, revokedAt: null },
    orderBy: { createdAt: 'desc' },
    select: { id: true, name: true, keyPrefix: true, lastUsedAt: true, createdAt: true },
  })
  return NextResponse.json({ keys })
}

export async function POST(req: NextRequest) {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const body = await req.json().catch(() => null)
  const parsed = createSchema.safeParse(body)
  if (!parsed.success) return NextResponse.json({ error: parsed.error.issues[0].message }, { status: 400 })

  const count = await prisma.apiKey.count({ where: { userId: session.user.id, revokedAt: null } })
  if (count >= 5) {
    return NextResponse.json({ error: 'Maximum 5 active keys. Revoke an existing key first.' }, { status: 400 })
  }

  const { plaintext, prefix, hash } = generateApiKey()
  const key = await prisma.apiKey.create({
    data: {
      userId: session.user.id,
      name: parsed.data.name,
      keyPrefix: prefix,
      keyHash: hash,
    },
    select: { id: true, name: true, keyPrefix: true, createdAt: true },
  })

  return NextResponse.json({ key: { ...key, plaintext } })
}
