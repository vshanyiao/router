import { NextResponse } from 'next/server'
import { prisma } from '@/lib/db'

// Public, read-only config the frontend needs (top-up presets, display FX rate).
// No auth: these values are non-sensitive and drive the top-up UI. Admin edits
// them via /api/admin/config.
export async function GET() {
  const rows = await prisma.appConfig.findMany({
    where: { key: { in: ['topup_presets_cents', 'cny_per_usd_rate'] } },
  })
  const map = new Map(rows.map((r) => [r.key, r.value]))
  const presets = map.get('topup_presets_cents')
  const fx = map.get('cny_per_usd_rate')
  return NextResponse.json({
    topupPresetsCents: Array.isArray(presets) ? (presets as number[]) : [500, 1000, 2000, 5000, 10000],
    cnyPerUsd: typeof fx === 'number' ? fx : 7.2,
  })
}
