import { NextRequest, NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

// Persist a logged-in user's locale preference. No-op 200 for anonymous callers
// (their preference lives in localStorage only).
export async function PATCH(req: NextRequest) {
  const { locale } = await req.json().catch(() => ({}))
  if (locale !== 'zh-CN' && locale !== 'en') {
    return NextResponse.json({ error: 'invalid locale' }, { status: 400 })
  }
  const session = await auth()
  if (session?.user?.id) {
    await prisma.user.update({ where: { id: session.user.id }, data: { locale } }).catch(() => {})
  }
  return NextResponse.json({ ok: true })
}
