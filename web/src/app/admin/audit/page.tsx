'use client'
import { useEffect, useState } from 'react'

type AuditLog = {
  id: string
  actorEmail: string | null
  kind: string
  targetUserId: string | null
  payload: unknown
  createdAt: string
}

export default function AdminAuditPage() {
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/admin/audit')
      .then(async (r) => {
        if (!r.ok) {
          const body = await r.json().catch(() => ({}))
          throw new Error(body.error || `Request failed (${r.status})`)
        }
        return r.json()
      })
      .then((d: { logs: AuditLog[] }) => setLogs(d.logs))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Audit Log</h1>
      {loading ? (
        <p className="text-sm text-gray-500">Loading…</p>
      ) : error ? (
        <p className="text-sm text-red-600">{error}</p>
      ) : logs.length === 0 ? (
        <p className="text-sm text-gray-500">No audit entries yet.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border bg-white">
          <table className="w-full text-sm">
            <thead className="border-b text-left text-xs uppercase text-gray-500">
              <tr>
                <th className="px-4 py-3">Date</th>
                <th className="px-4 py-3">Actor</th>
                <th className="px-4 py-3">Action</th>
                <th className="px-4 py-3">Target user</th>
                <th className="px-4 py-3">Payload</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l) => (
                <tr key={l.id} className="border-t align-top">
                  <td className="whitespace-nowrap px-4 py-2 text-gray-600">
                    {new Date(l.createdAt).toLocaleString()}
                  </td>
                  <td className="px-4 py-2">{l.actorEmail ?? '—'}</td>
                  <td className="px-4 py-2">
                    <span className="rounded bg-gray-100 px-2 py-1 text-xs text-gray-700">
                      {l.kind}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 font-mono text-xs text-gray-500">
                    {l.targetUserId ?? '—'}
                  </td>
                  <td className="px-4 py-2">
                    <pre className="max-w-md overflow-x-auto whitespace-pre-wrap break-words text-xs text-gray-600">
                      {l.payload == null ? '—' : JSON.stringify(l.payload)}
                    </pre>
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
