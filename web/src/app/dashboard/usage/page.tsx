'use client'
import { useEffect, useState } from 'react'

type Log = {
  id: string
  modelAlias: string
  promptTokens: number | null
  completionTokens: number | null
  totalChargedCents: number
  status: string
  latencyMs: number | null
  createdAt: string
}

export default function UsagePage() {
  const [logs, setLogs] = useState<Log[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/usage')
      .then((r) => r.json())
      .then((d) => setLogs(d.logs || []))
      .finally(() => setLoading(false))
  }, [])

  const total = logs.reduce((sum, l) => sum + l.totalChargedCents, 0)

  return (
    <div>
      <h1 className="mb-6 text-2xl font-bold">Usage</h1>
      <div className="mb-6 rounded-lg bg-white p-4 shadow-sm">
        <div className="text-xs uppercase text-gray-500">Last 50 requests · total spent</div>
        <div className="text-2xl font-bold">${(total / 100).toFixed(2)}</div>
      </div>
      {loading ? (
        <p className="text-sm text-gray-500">Loading…</p>
      ) : logs.length === 0 ? (
        <div className="rounded border bg-white p-8 text-center text-sm text-gray-500">
          No requests yet. Make your first API call to see usage here.
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border bg-white">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 text-xs uppercase text-gray-600">
              <tr>
                <th className="px-4 py-3 text-left">Time</th>
                <th className="px-4 py-3 text-left">Model</th>
                <th className="px-4 py-3 text-right">Prompt / Completion</th>
                <th className="px-4 py-3 text-right">Cost</th>
                <th className="px-4 py-3 text-right">Latency</th>
                <th className="px-4 py-3 text-left">Status</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l) => (
                <tr key={l.id} className="border-t">
                  <td className="px-4 py-3 text-gray-600">{new Date(l.createdAt).toLocaleString()}</td>
                  <td className="px-4 py-3 font-mono text-xs">{l.modelAlias}</td>
                  <td className="px-4 py-3 text-right text-gray-600">{l.promptTokens ?? '–'} / {l.completionTokens ?? '–'}</td>
                  <td className="px-4 py-3 text-right">${(l.totalChargedCents / 100).toFixed(4)}</td>
                  <td className="px-4 py-3 text-right text-gray-600">{l.latencyMs ?? '–'}ms</td>
                  <td className="px-4 py-3"><Badge status={l.status} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function Badge({ status }: { status: string }) {
  const map: Record<string, string> = {
    success: 'bg-green-100 text-green-800',
    provider_error: 'bg-red-100 text-red-800',
    insufficient_credits: 'bg-yellow-100 text-yellow-800',
    rate_limited: 'bg-yellow-100 text-yellow-800',
    cancelled: 'bg-gray-100 text-gray-800',
  }
  const cls = map[status] || 'bg-gray-100 text-gray-800'
  return <span className={`rounded px-2 py-1 text-xs ${cls}`}>{status}</span>
}
