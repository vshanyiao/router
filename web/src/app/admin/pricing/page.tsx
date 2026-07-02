'use client'

import { useEffect, useState } from 'react'

type Config = Record<string, unknown>

type RowStatus = { ok: boolean; message: string } | null

// Coerce a raw config value to the string shown in its input.
function toInputValue(key: string, value: unknown): string {
  if (key === 'topup_presets_cents') {
    return Array.isArray(value) ? value.join(', ') : ''
  }
  return value === undefined || value === null ? '' : String(value)
}

// Parse an input string back into the value we PUT for a given key.
// Returns { value } on success or { error } if it can't be parsed.
function parseValue(key: string, raw: string): { value: unknown } | { error: string } {
  if (key === 'topup_presets_cents') {
    const parts = raw
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
    const nums = parts.map((s) => Number(s))
    if (nums.some((n) => !Number.isFinite(n))) {
      return { error: 'All presets must be numbers' }
    }
    return { value: nums }
  }
  const n = Number(raw)
  if (raw.trim() === '' || !Number.isFinite(n)) {
    return { error: 'Must be a number' }
  }
  return { value: n }
}

const FIELDS: {
  key: string
  label: string
  type: 'number' | 'text'
  step?: string
}[] = [
  { key: 'default_markup_pct', label: 'Default markup %', type: 'number' },
  { key: 'trial_credit_cents', label: 'Trial credit (cents)', type: 'number' },
  { key: 'cny_per_usd_rate', label: 'CNY per USD', type: 'number', step: '0.01' },
  { key: 'rate_limit_per_user_per_minute', label: 'Rate limit / user / min', type: 'number' },
  { key: 'topup_presets_cents', label: 'Top-up presets (cents, comma-sep)', type: 'text' },
]

export default function PricingPage() {
  const [values, setValues] = useState<Record<string, string>>({})
  const [status, setStatus] = useState<Record<string, RowStatus>>({})
  const [saving, setSaving] = useState<Record<string, boolean>>({})
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const res = await fetch('/api/admin/config')
        if (!res.ok) throw new Error(`Failed to load config (${res.status})`)
        const data: { config: Config } = await res.json()
        if (cancelled) return
        const next: Record<string, string> = {}
        for (const f of FIELDS) {
          next[f.key] = toInputValue(f.key, data.config[f.key])
        }
        setValues(next)
      } catch (e) {
        if (!cancelled) setLoadError(e instanceof Error ? e.message : 'Failed to load config')
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  async function save(key: string) {
    const parsed = parseValue(key, values[key] ?? '')
    if ('error' in parsed) {
      setStatus((s) => ({ ...s, [key]: { ok: false, message: parsed.error } }))
      return
    }
    setSaving((s) => ({ ...s, [key]: true }))
    setStatus((s) => ({ ...s, [key]: null }))
    try {
      const res = await fetch('/api/admin/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key, value: parsed.value }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        throw new Error(data?.error ?? `Save failed (${res.status})`)
      }
      setStatus((s) => ({ ...s, [key]: { ok: true, message: 'Saved' } }))
    } catch (e) {
      setStatus((s) => ({
        ...s,
        [key]: { ok: false, message: e instanceof Error ? e.message : 'Save failed' },
      }))
    } finally {
      setSaving((s) => ({ ...s, [key]: false }))
    }
  }

  if (loading) {
    return <p className="text-sm text-gray-500">Loading config…</p>
  }
  if (loadError) {
    return <p className="text-sm text-red-600">{loadError}</p>
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Pricing &amp; Config</h1>
        <p className="mt-1 text-sm text-gray-500">
          Changes take effect immediately and are recorded to the audit log.
        </p>
      </div>

      <div className="divide-y rounded-lg border bg-white">
        {FIELDS.map((f) => {
          const rowStatus = status[f.key]
          return (
            <div key={f.key} className="flex flex-col gap-2 p-4 sm:flex-row sm:items-center">
              <label htmlFor={f.key} className="w-64 text-sm font-medium text-gray-700">
                {f.label}
              </label>
              <div className="flex flex-1 items-center gap-2">
                <input
                  id={f.key}
                  type={f.type}
                  step={f.step}
                  value={values[f.key] ?? ''}
                  onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
                  className="flex-1 rounded border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
                />
                <button
                  type="button"
                  onClick={() => save(f.key)}
                  disabled={saving[f.key]}
                  className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                >
                  {saving[f.key] ? 'Saving…' : 'Save'}
                </button>
              </div>
              {rowStatus && (
                <span
                  className={`text-sm sm:w-24 ${rowStatus.ok ? 'text-green-600' : 'text-red-600'}`}
                >
                  {rowStatus.message}
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
