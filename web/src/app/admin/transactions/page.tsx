'use client'
import { useEffect, useState } from 'react'

type Transaction = {
  id: string
  userEmail: string
  amountCents: number
  kind: string
  description: string | null
  createdAt: string
}

const kindStyles: Record<string, string> = {
  topup: 'bg-green-100 text-green-700',
  usage: 'bg-blue-100 text-blue-700',
  trial: 'bg-indigo-100 text-indigo-700',
  refund: 'bg-yellow-100 text-yellow-700',
  adjustment: 'bg-purple-100 text-purple-700',
}

export default function AdminTransactionsPage() {
  const [transactions, setTransactions] = useState<Transaction[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/admin/transactions')
      .then(async (r) => {
        if (!r.ok) {
          const body = await r.json().catch(() => ({}))
          throw new Error(body.error || `Request failed (${r.status})`)
        }
        return r.json()
      })
      .then((d: { transactions: Transaction[] }) => setTransactions(d.transactions))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Transactions</h1>
      {loading ? (
        <p className="text-sm text-gray-500">Loading…</p>
      ) : error ? (
        <p className="text-sm text-red-600">{error}</p>
      ) : transactions.length === 0 ? (
        <p className="text-sm text-gray-500">No transactions yet.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border bg-white">
          <table className="w-full text-sm">
            <thead className="border-b text-left text-xs uppercase text-gray-500">
              <tr>
                <th className="px-4 py-3">Date</th>
                <th className="px-4 py-3">User</th>
                <th className="px-4 py-3">Kind</th>
                <th className="px-4 py-3 text-right">Amount</th>
                <th className="px-4 py-3">Description</th>
              </tr>
            </thead>
            <tbody>
              {transactions.map((t) => (
                <tr key={t.id} className="border-t">
                  <td className="whitespace-nowrap px-4 py-2 text-gray-600">
                    {new Date(t.createdAt).toLocaleString()}
                  </td>
                  <td className="px-4 py-2">{t.userEmail}</td>
                  <td className="px-4 py-2">
                    <span
                      className={`rounded px-2 py-1 text-xs ${
                        kindStyles[t.kind] || 'bg-gray-100 text-gray-700'
                      }`}
                    >
                      {t.kind}
                    </span>
                  </td>
                  <td
                    className={`whitespace-nowrap px-4 py-2 text-right font-medium ${
                      t.amountCents < 0 ? 'text-red-600' : 'text-green-700'
                    }`}
                  >
                    {t.amountCents < 0 ? '-' : '+'}${(Math.abs(t.amountCents) / 100).toFixed(2)}
                  </td>
                  <td className="px-4 py-2 text-gray-600">{t.description ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
