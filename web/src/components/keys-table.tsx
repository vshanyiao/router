'use client'

type Key = { id: string; name: string; keyPrefix: string; createdAt: string; lastUsedAt: string | null }

export function KeysTable({ keys, onRevoke }: { keys: Key[]; onRevoke: () => void }) {
  async function revoke(id: string) {
    if (!confirm('Revoke this key? Existing API calls using it will start failing.')) return
    await fetch(`/api/keys/${id}`, { method: 'DELETE' })
    onRevoke()
  }

  if (keys.length === 0) {
    return <div className="rounded border bg-white p-8 text-center text-sm text-gray-500">No API keys yet. Click "New Key" to create one.</div>
  }

  return (
    <div className="overflow-hidden rounded-lg border bg-white">
      <table className="w-full text-sm">
        <thead className="bg-gray-50 text-xs uppercase text-gray-600">
          <tr>
            <th className="px-4 py-3 text-left">Name</th>
            <th className="px-4 py-3 text-left">Key</th>
            <th className="px-4 py-3 text-left">Created</th>
            <th className="px-4 py-3 text-left">Last used</th>
            <th className="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          {keys.map((k) => (
            <tr key={k.id} className="border-t">
              <td className="px-4 py-3">{k.name}</td>
              <td className="px-4 py-3 font-mono text-gray-600">{k.keyPrefix}...</td>
              <td className="px-4 py-3 text-gray-600">{new Date(k.createdAt).toLocaleDateString()}</td>
              <td className="px-4 py-3 text-gray-600">{k.lastUsedAt ? new Date(k.lastUsedAt).toLocaleString() : 'Never'}</td>
              <td className="px-4 py-3 text-right">
                <button onClick={() => revoke(k.id)} className="text-sm text-red-600 hover:underline">Revoke</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
