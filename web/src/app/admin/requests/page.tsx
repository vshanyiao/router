'use client'
import { useEffect, useState } from 'react'

type RequestRow = {
  id: string
  modelAlias: string
  upstreamProvider: string
  promptTokens: number | null
  completionTokens: number | null
  totalChargedCents: number
  status: string
  latencyMs: number | null
  createdAt: string
}

const statusStyles: Record<string, string> = {
  success: 'bg-green-100 text-green-700',
  ok: 'bg-green-100 text-green-700',
  provider_error: 'bg-red-100 text-red-700',
  insufficient_credits: 'bg-yellow-100 text-yellow-700',
  rate_limited: 'bg-orange-100 text-orange-700',
}

export default function AdminRequestsPage() {
  const [requests, setRequests] = useState<RequestRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/admin/requests')
      .then(async (r) => {
        if (!r.ok) {
          const body = await r.json().catch(() => ({}))
          throw new Error(body.error || `Request failed (${r.status})`)
        }
        return r.json()
      })
      .then((d: { requests: RequestRow[] }) => setRequests(d.requests))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Requests</h1>
      {loading ? (
        <p className="text-sm text-gray-500">Loading…</p>
      ) : error ? (
        <p className="text-sm text-red-600">{error}</p>
      ) : requests.length === 0 ? (
        <p className="text-sm text-gray-500">No requests yet.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border bg-white">
          <table className="w-full text-sm">
            <thead className="border-b text-left text-xs uppercase text-gray-500">
              <tr>
                <th className="px-4 py-3">Date</th>
                <th className="px-4 py-3">Model</th>
                <th className="px-4 py-3">Provider</th>
                <th className="px-4 py-3 text-right">Prompt</th>
                <th className="px-4 py-3 text-right">Completion</th>
                <th className="px-4 py-3 text-right">Charged</th>
                <th className="px-4 py-3 text-right">Latency</th>
                <th className="px-4 py-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {requests.map((r) => (
                <tr key={r.id} className="border-t">
                  <td className="whitespace-nowrap px-4 py-2 text-gray-600">
                    {new Date(r.createdAt).toLocaleString()}
                  </td>
                  <td className="px-4 py-2 font-medium">{r.modelAlias}</td>
                  <td className="px-4 py-2 text-gray-600">{r.upstreamProvider}</td>
                  <td className="px-4 py-2 text-right text-gray-600">{r.promptTokens ?? '—'}</td>
                  <td className="px-4 py-2 text-right text-gray-600">
                    {r.completionTokens ?? '—'}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right">
                    ${(r.totalChargedCents / 100).toFixed(4)}
                  </td>
                  <td className="px-4 py-2 text-right text-gray-600">
                    {r.latencyMs != null ? `${r.latencyMs} ms` : '—'}
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={`rounded px-2 py-1 text-xs ${
                        statusStyles[r.status] || 'bg-gray-100 text-gray-700'
                      }`}
                    >
                      {r.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
