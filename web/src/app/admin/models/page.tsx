'use client'
import { useEffect, useState } from 'react'

type Model = {
  id: string
  alias: string
  displayName: string
  upstreamProvider: string
  upstreamModelId: string
  contextWindow: number
  supportsStreaming: boolean
  supportsTools: boolean
  supportsVision: boolean
  inputCentsPerMillionTokens: number
  outputCentsPerMillionTokens: number
  markupPct: number
  status: string
  tags: string[]
}

type EditState = {
  inputCentsPerMillionTokens: number
  outputCentsPerMillionTokens: number
  markupPct: number
  status: string
}

const emptyNew = {
  alias: '',
  displayName: '',
  upstreamProvider: '',
  upstreamModelId: '',
  contextWindow: '',
  inputCentsPerMillionTokens: '',
  outputCentsPerMillionTokens: '',
  markupPct: '18',
  supportsTools: false,
  supportsVision: false,
  tags: '',
}

export default function ModelsPage() {
  const [models, setModels] = useState<Model[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<EditState | null>(null)
  const [showNew, setShowNew] = useState(false)
  const [newForm, setNewForm] = useState({ ...emptyNew })
  const [busy, setBusy] = useState(false)

  async function load() {
    setLoading(true)
    const res = await fetch('/api/admin/models')
    const data = await res.json()
    if (res.ok) setModels(data.models || [])
    else setError(data.error || 'Failed to load')
    setLoading(false)
  }

  useEffect(() => {
    load()
  }, [])

  function startEdit(m: Model) {
    setEditing(m.id)
    setEditForm({
      inputCentsPerMillionTokens: m.inputCentsPerMillionTokens,
      outputCentsPerMillionTokens: m.outputCentsPerMillionTokens,
      markupPct: m.markupPct,
      status: m.status,
    })
  }

  async function saveEdit(id: string) {
    if (!editForm) return
    setBusy(true)
    setError(null)
    const res = await fetch(`/api/admin/models/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(editForm),
    })
    const data = await res.json()
    setBusy(false)
    if (!res.ok) {
      setError(data.error || 'Update failed')
      return
    }
    setEditing(null)
    setEditForm(null)
    await load()
  }

  async function disableModel(id: string) {
    if (!confirm('Disable this model? It will be hidden from users.')) return
    setBusy(true)
    setError(null)
    const res = await fetch(`/api/admin/models/${id}`, { method: 'DELETE' })
    const data = await res.json()
    setBusy(false)
    if (!res.ok) {
      setError(data.error || 'Disable failed')
      return
    }
    await load()
  }

  async function createModel(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const payload = {
      alias: newForm.alias.trim(),
      displayName: newForm.displayName.trim(),
      upstreamProvider: newForm.upstreamProvider.trim(),
      upstreamModelId: newForm.upstreamModelId.trim(),
      contextWindow: Number(newForm.contextWindow),
      inputCentsPerMillionTokens: Number(newForm.inputCentsPerMillionTokens),
      outputCentsPerMillionTokens: Number(newForm.outputCentsPerMillionTokens),
      markupPct: Number(newForm.markupPct),
      supportsTools: newForm.supportsTools,
      supportsVision: newForm.supportsVision,
      tags: newForm.tags
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean),
    }
    const res = await fetch('/api/admin/models', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const data = await res.json()
    setBusy(false)
    if (!res.ok) {
      setError(data.error || 'Create failed')
      return
    }
    setShowNew(false)
    setNewForm({ ...emptyNew })
    await load()
  }

  const inputCls = 'w-full rounded border px-2 py-1 text-sm'

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Model Catalog</h1>
        <button
          onClick={() => setShowNew((s) => !s)}
          className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-indigo-700"
        >
          {showNew ? 'Cancel' : '+ New Model'}
        </button>
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800">{error}</div>
      )}

      {showNew && (
        <form onSubmit={createModel} className="mb-6 grid grid-cols-2 gap-3 rounded-lg border bg-white p-4 shadow-sm md:grid-cols-4">
          <label className="col-span-2 md:col-span-1 text-xs text-gray-600">
            Alias
            <input required className={inputCls} value={newForm.alias} onChange={(e) => setNewForm({ ...newForm, alias: e.target.value })} />
          </label>
          <label className="col-span-2 md:col-span-1 text-xs text-gray-600">
            Display Name
            <input required className={inputCls} value={newForm.displayName} onChange={(e) => setNewForm({ ...newForm, displayName: e.target.value })} />
          </label>
          <label className="col-span-2 md:col-span-1 text-xs text-gray-600">
            Provider
            <input required className={inputCls} value={newForm.upstreamProvider} onChange={(e) => setNewForm({ ...newForm, upstreamProvider: e.target.value })} />
          </label>
          <label className="col-span-2 md:col-span-1 text-xs text-gray-600">
            Upstream Model ID
            <input required className={inputCls} value={newForm.upstreamModelId} onChange={(e) => setNewForm({ ...newForm, upstreamModelId: e.target.value })} />
          </label>
          <label className="text-xs text-gray-600">
            Context Window
            <input required type="number" className={inputCls} value={newForm.contextWindow} onChange={(e) => setNewForm({ ...newForm, contextWindow: e.target.value })} />
          </label>
          <label className="text-xs text-gray-600">
            Input ¢/1M
            <input required type="number" className={inputCls} value={newForm.inputCentsPerMillionTokens} onChange={(e) => setNewForm({ ...newForm, inputCentsPerMillionTokens: e.target.value })} />
          </label>
          <label className="text-xs text-gray-600">
            Output ¢/1M
            <input required type="number" className={inputCls} value={newForm.outputCentsPerMillionTokens} onChange={(e) => setNewForm({ ...newForm, outputCentsPerMillionTokens: e.target.value })} />
          </label>
          <label className="text-xs text-gray-600">
            Markup %
            <input required type="number" className={inputCls} value={newForm.markupPct} onChange={(e) => setNewForm({ ...newForm, markupPct: e.target.value })} />
          </label>
          <label className="col-span-2 md:col-span-2 text-xs text-gray-600">
            Tags (comma-separated)
            <input className={inputCls} value={newForm.tags} onChange={(e) => setNewForm({ ...newForm, tags: e.target.value })} />
          </label>
          <label className="flex items-center gap-2 text-xs text-gray-600">
            <input type="checkbox" checked={newForm.supportsTools} onChange={(e) => setNewForm({ ...newForm, supportsTools: e.target.checked })} />
            Supports Tools
          </label>
          <label className="flex items-center gap-2 text-xs text-gray-600">
            <input type="checkbox" checked={newForm.supportsVision} onChange={(e) => setNewForm({ ...newForm, supportsVision: e.target.checked })} />
            Supports Vision
          </label>
          <div className="col-span-2 md:col-span-4">
            <button disabled={busy} type="submit" className="rounded bg-green-600 px-4 py-1.5 text-sm font-semibold text-white hover:bg-green-700 disabled:opacity-50">
              {busy ? 'Creating…' : 'Create Model'}
            </button>
          </div>
        </form>
      )}

      {loading ? (
        <p className="text-sm text-gray-500">Loading…</p>
      ) : models.length === 0 ? (
        <div className="rounded border bg-white p-8 text-center text-sm text-gray-500">No models yet.</div>
      ) : (
        <div className="overflow-x-auto rounded-lg border bg-white shadow-sm">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase text-gray-500">
              <tr>
                <th className="px-3 py-2">Alias</th>
                <th className="px-3 py-2">Provider</th>
                <th className="px-3 py-2">In $/1M</th>
                <th className="px-3 py-2">Out $/1M</th>
                <th className="px-3 py-2">Markup</th>
                <th className="px-3 py-2">Caps</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {models.map((m) => {
                const isEditing = editing === m.id
                return (
                  <tr key={m.id} className={m.status === 'disabled' ? 'bg-gray-50 text-gray-400' : ''}>
                    <td className="px-3 py-2">
                      <div className="font-mono font-semibold">{m.alias}</div>
                      <div className="text-xs text-gray-500">{m.displayName}</div>
                    </td>
                    <td className="px-3 py-2">{m.upstreamProvider}</td>
                    {isEditing && editForm ? (
                      <>
                        <td className="px-3 py-2">
                          <input type="number" className="w-24 rounded border px-1 py-0.5" value={editForm.inputCentsPerMillionTokens} onChange={(e) => setEditForm({ ...editForm, inputCentsPerMillionTokens: Number(e.target.value) })} />
                          <span className="ml-1 text-xs text-gray-400">¢</span>
                        </td>
                        <td className="px-3 py-2">
                          <input type="number" className="w-24 rounded border px-1 py-0.5" value={editForm.outputCentsPerMillionTokens} onChange={(e) => setEditForm({ ...editForm, outputCentsPerMillionTokens: Number(e.target.value) })} />
                          <span className="ml-1 text-xs text-gray-400">¢</span>
                        </td>
                        <td className="px-3 py-2">
                          <input type="number" className="w-16 rounded border px-1 py-0.5" value={editForm.markupPct} onChange={(e) => setEditForm({ ...editForm, markupPct: Number(e.target.value) })} />
                        </td>
                        <td className="px-3 py-2 text-xs">
                          {m.supportsTools && <span className="mr-1 rounded bg-blue-100 px-1 text-blue-700">tools</span>}
                          {m.supportsVision && <span className="rounded bg-purple-100 px-1 text-purple-700">vision</span>}
                        </td>
                        <td className="px-3 py-2">
                          <select className="rounded border px-1 py-0.5 text-xs" value={editForm.status} onChange={(e) => setEditForm({ ...editForm, status: e.target.value })}>
                            <option value="active">active</option>
                            <option value="disabled">disabled</option>
                            <option value="deprecated">deprecated</option>
                          </select>
                        </td>
                        <td className="px-3 py-2 whitespace-nowrap">
                          <button disabled={busy} onClick={() => saveEdit(m.id)} className="mr-2 rounded bg-green-600 px-2 py-1 text-xs font-semibold text-white hover:bg-green-700 disabled:opacity-50">
                            Save
                          </button>
                          <button onClick={() => { setEditing(null); setEditForm(null) }} className="rounded bg-gray-200 px-2 py-1 text-xs hover:bg-gray-300">
                            Cancel
                          </button>
                        </td>
                      </>
                    ) : (
                      <>
                        <td className="px-3 py-2">${(m.inputCentsPerMillionTokens / 100).toFixed(2)}</td>
                        <td className="px-3 py-2">${(m.outputCentsPerMillionTokens / 100).toFixed(2)}</td>
                        <td className="px-3 py-2">{m.markupPct}%</td>
                        <td className="px-3 py-2 text-xs">
                          {m.supportsTools && <span className="mr-1 rounded bg-blue-100 px-1 text-blue-700">tools</span>}
                          {m.supportsVision && <span className="rounded bg-purple-100 px-1 text-purple-700">vision</span>}
                        </td>
                        <td className="px-3 py-2">
                          <span className={`rounded px-2 py-0.5 text-xs font-semibold ${m.status === 'active' ? 'bg-green-100 text-green-700' : m.status === 'deprecated' ? 'bg-yellow-100 text-yellow-700' : 'bg-gray-200 text-gray-600'}`}>
                            {m.status}
                          </span>
                        </td>
                        <td className="px-3 py-2 whitespace-nowrap">
                          <button onClick={() => startEdit(m)} className="mr-2 rounded bg-indigo-50 px-2 py-1 text-xs font-semibold text-indigo-700 hover:bg-indigo-100">
                            Edit
                          </button>
                          {m.status !== 'disabled' && (
                            <button disabled={busy} onClick={() => disableModel(m.id)} className="rounded bg-red-50 px-2 py-1 text-xs font-semibold text-red-700 hover:bg-red-100 disabled:opacity-50">
                              Disable
                            </button>
                          )}
                        </td>
                      </>
                    )}
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
