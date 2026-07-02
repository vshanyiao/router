'use client'
import { useEffect, useState } from 'react'

type Overview = {
  usersTotal: number
  usersToday: number
  revenueTodayCents: number
  requestsToday: number
  errorsToday: number
  errorRate: number
  cogsCents: number
  marginCents: number
}

function KpiCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border bg-white p-6">
      <div className="text-xs uppercase tracking-wide text-gray-500">{label}</div>
      <div className="mt-2 text-2xl font-bold">{value}</div>
    </div>
  )
}

export default function AdminOverviewPage() {
  const [data, setData] = useState<Overview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/admin/overview')
      .then(async (r) => {
        if (!r.ok) {
          const body = await r.json().catch(() => ({}))
          throw new Error(body.error || `Request failed (${r.status})`)
        }
        return r.json()
      })
      .then((d: Overview) => setData(d))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <p className="text-sm text-gray-500">Loading…</p>
  }
  if (error) {
    return <p className="text-sm text-red-600">{error}</p>
  }
  if (!data) return null

  const cards = [
    { label: 'Users today', value: String(data.usersToday) },
    { label: 'Revenue today', value: `$${(data.revenueTodayCents / 100).toFixed(2)}` },
    { label: 'Requests today', value: String(data.requestsToday) },
    { label: 'Error rate', value: `${(data.errorRate * 100).toFixed(1)}%` },
    { label: 'Margin today', value: `$${(data.marginCents / 100).toFixed(2)}` },
  ]

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Overview</h1>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {cards.map((c) => (
          <KpiCard key={c.label} label={c.label} value={c.value} />
        ))}
      </div>
    </div>
  )
}
