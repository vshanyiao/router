'use client'
import { useEffect, useState } from 'react'

type Intent = {
  id: string
  amountCents: number
  creditsAddedCents: number
  currency: string
  status: 'pending' | 'succeeded' | 'failed' | 'expired' | string
  createdAt: string
  completedAt: string | null
}

const statusStyles: Record<string, string> = {
  succeeded: 'bg-green-100 text-green-700',
  pending: 'bg-yellow-100 text-yellow-700',
  failed: 'bg-red-100 text-red-700',
  expired: 'bg-gray-100 text-gray-700',
}

export function TopUpHistoryTable() {
  const [intents, setIntents] = useState<Intent[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/topup/history')
      .then((r) => r.json())
      .then((d) => setIntents(d.intents || []))
      .catch(() => setIntents([]))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <p className="text-sm text-gray-500">Loading…</p>
  if (intents.length === 0) {
    return <p className="text-sm text-gray-500">No top-ups yet.</p>
  }

  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs uppercase text-gray-500">
        <tr>
          <th className="pb-2">Date</th>
          <th>Amount</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {intents.map((i) => (
          <tr key={i.id} className="border-t">
            <td className="py-2">{new Date(i.createdAt).toLocaleString()}</td>
            <td>${(i.amountCents / 100).toFixed(2)}</td>
            <td>
              <span
                className={`text-xs px-2 py-1 rounded ${
                  statusStyles[i.status] || 'bg-gray-100 text-gray-700'
                }`}
              >
                {i.status}
              </span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
