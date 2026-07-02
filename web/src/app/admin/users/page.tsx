'use client'
import { useCallback, useEffect, useState } from 'react'

type AdminUser = {
  id: string
  email: string
  status: string
  creditsCents: number
  spend30dCents: number
  method: 'github' | 'email'
  createdAt: string
}

const FRAUD_THRESHOLD_CENTS = 5000 // $50

function usd(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`
}

function StatusBadge({ status }: { status: string }) {
  const styles: Record<string, string> = {
    active: 'bg-green-100 text-green-800',
    suspended: 'bg-yellow-100 text-yellow-800',
    banned: 'bg-red-100 text-red-800',
  }
  return (
    <span
      className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${
        styles[status] ?? 'bg-gray-100 text-gray-800'
      }`}
    >
      {status}
    </span>
  )
}

function UserRow({ user, onChanged }: { user: AdminUser; onChanged: () => void }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')

  const flagged = user.spend30dCents > FRAUD_THRESHOLD_CENTS

  async function doAction(action: 'suspend' | 'ban' | 'activate') {
    setBusy(true)
    setError(null)
    try {
      const r = await fetch(`/api/admin/users/${user.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      })
      if (!r.ok) {
        const body = await r.json().catch(() => ({}))
        throw new Error(body.error || `Request failed (${r.status})`)
      }
      onChanged()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function adjustCredit(e: React.FormEvent) {
    e.preventDefault()
    const dollars = Number(amount)
    if (!Number.isFinite(dollars) || dollars === 0) {
      setError('Enter a non-zero dollar amount')
      return
    }
    if (!reason.trim()) {
      setError('Reason is required')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const r = await fetch(`/api/admin/users/${user.id}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ amountCents: Math.round(dollars * 100), reason: reason.trim() }),
      })
      if (!r.ok) {
        const body = await r.json().catch(() => ({}))
        throw new Error(body.error || `Request failed (${r.status})`)
      }
      setAmount('')
      setReason('')
      onChanged()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <tr className={flagged ? 'bg-yellow-50' : undefined}>
        <td className="px-3 py-2 font-medium">{user.email}</td>
        <td className="px-3 py-2 text-gray-500">
          {new Date(user.createdAt).toLocaleDateString()}
        </td>
        <td className="px-3 py-2 text-gray-500">{user.method}</td>
        <td className="px-3 py-2 tabular-nums">{usd(user.creditsCents)}</td>
        <td className={`px-3 py-2 tabular-nums ${flagged ? 'font-semibold text-yellow-800' : ''}`}>
          {usd(user.spend30dCents)}
        </td>
        <td className="px-3 py-2">
          <StatusBadge status={user.status} />
        </td>
        <td className="px-3 py-2">
          <button
            onClick={() => setOpen((v) => !v)}
            className="text-indigo-600 hover:underline"
          >
            {open ? 'Close' : 'Manage'}
          </button>
        </td>
      </tr>
      {open && (
        <tr className={flagged ? 'bg-yellow-50' : 'bg-gray-50'}>
          <td colSpan={7} className="px-3 py-3">
            <div className="flex flex-wrap items-end gap-6">
              <div className="flex gap-2">
                <button
                  onClick={() => doAction('suspend')}
                  disabled={busy || user.status === 'suspended'}
                  className="rounded bg-yellow-500 px-3 py-1 text-sm font-medium text-white disabled:opacity-40"
                >
                  Suspend
                </button>
                <button
                  onClick={() => doAction('ban')}
                  disabled={busy || user.status === 'banned'}
                  className="rounded bg-red-600 px-3 py-1 text-sm font-medium text-white disabled:opacity-40"
                >
                  Ban
                </button>
                <button
                  onClick={() => doAction('activate')}
                  disabled={busy || user.status === 'active'}
                  className="rounded bg-green-600 px-3 py-1 text-sm font-medium text-white disabled:opacity-40"
                >
                  Activate
                </button>
              </div>
              <form onSubmit={adjustCredit} className="flex items-end gap-2">
                <label className="flex flex-col text-xs text-gray-600">
                  Amount ($)
                  <input
                    type="number"
                    step="0.01"
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                    placeholder="e.g. 5.00 or -2.50"
                    className="mt-1 w-32 rounded border px-2 py-1 text-sm"
                  />
                </label>
                <label className="flex flex-col text-xs text-gray-600">
                  Reason
                  <input
                    type="text"
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder="Reason for adjustment"
                    className="mt-1 w-56 rounded border px-2 py-1 text-sm"
                  />
                </label>
                <button
                  type="submit"
                  disabled={busy}
                  className="rounded bg-indigo-600 px-3 py-1 text-sm font-medium text-white disabled:opacity-40"
                >
                  Adjust
                </button>
              </form>
            </div>
            {error && <p className="mt-2 text-sm text-red-600">{error}</p>}
          </td>
        </tr>
      )}
    </>
  )
}

export default function AdminUsersPage() {
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [users, setUsers] = useState<AdminUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const params = new URLSearchParams()
      if (q.trim()) params.set('q', q.trim())
      if (status) params.set('status', status)
      const r = await fetch(`/api/admin/users?${params.toString()}`)
      if (!r.ok) {
        const body = await r.json().catch(() => ({}))
        throw new Error(body.error || `Request failed (${r.status})`)
      }
      const d: { users: AdminUser[] } = await r.json()
      setUsers(d.users)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [q, status])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Users</h1>

      <form
        onSubmit={(e) => {
          e.preventDefault()
          load()
        }}
        className="flex flex-wrap items-center gap-3"
      >
        <input
          type="text"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search by email…"
          className="w-64 rounded border px-3 py-2 text-sm"
        />
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="rounded border px-3 py-2 text-sm"
        >
          <option value="">All statuses</option>
          <option value="active">Active</option>
          <option value="suspended">Suspended</option>
          <option value="banned">Banned</option>
        </select>
        <button
          type="submit"
          className="rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white"
        >
          Search
        </button>
      </form>

      {loading ? (
        <p className="text-sm text-gray-500">Loading…</p>
      ) : error ? (
        <p className="text-sm text-red-600">{error}</p>
      ) : users.length === 0 ? (
        <p className="text-sm text-gray-500">No users found.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border bg-white">
          <table className="min-w-full text-sm">
            <thead>
              <tr className="border-b bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
                <th className="px-3 py-2 font-medium">Email</th>
                <th className="px-3 py-2 font-medium">Signup</th>
                <th className="px-3 py-2 font-medium">Method</th>
                <th className="px-3 py-2 font-medium">Balance</th>
                <th className="px-3 py-2 font-medium">Spend 30d</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Manage</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {users.map((u) => (
                <UserRow key={u.id} user={u} onChanged={load} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
