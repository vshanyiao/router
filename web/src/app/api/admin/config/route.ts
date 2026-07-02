import { NextRequest, NextResponse } from 'next/server'
import { requireAdmin, logAdminAction, AdminAuthError } from '@/lib/admin'
import { prisma } from '@/lib/db'

// Whitelist of editable config keys. PUT rejects anything not in this list so
// admins can't create arbitrary rows via the editor.
const KNOWN_KEYS = [
  'default_markup_pct',
  'topup_presets_cents',
  'trial_credit_cents',
  'cny_per_usd_rate',
  'rate_limit_per_user_per_minute',
] as const

export async function GET() {
  try {
    await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }

  const rows = await prisma.appConfig.findMany()
  const config = rows.reduce<Record<string, unknown>>((acc, row) => {
    acc[row.key] = row.value
    return acc
  }, {})

  return NextResponse.json({ config })
}

export async function PUT(req: NextRequest) {
  let admin: { id: string; email: string }
  try {
    admin = await requireAdmin()
  } catch (e) {
    if (e instanceof AdminAuthError) {
      return NextResponse.json({ error: e.message }, { status: e.status })
    }
    throw e
  }

  let body: { key?: unknown; value?: unknown }
  try {
    body = await req.json()
  } catch {
    return NextResponse.json({ error: 'Invalid JSON body' }, { status: 400 })
  }

  const { key, value } = body
  if (typeof key !== 'string' || !(KNOWN_KEYS as readonly string[]).includes(key)) {
    return NextResponse.json({ error: 'Unknown config key' }, { status: 400 })
  }

  const existing = await prisma.appConfig.findUnique({ where: { key } })
  const oldValue = existing?.value ?? null

  await prisma.appConfig.upsert({
    where: { key },
    update: { value: value as object },
    create: { key, value: value as object },
  })

  await logAdminAction(admin.id, 'config_change', { key, from: oldValue, to: value })

  return NextResponse.json({ ok: true })
}
