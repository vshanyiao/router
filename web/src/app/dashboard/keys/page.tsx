'use client'
import { useEffect, useState } from 'react'
import { CreateKeyModal } from '@/components/create-key-modal'
import { KeysTable } from '@/components/keys-table'

export default function KeysPage() {
  const [keys, setKeys] = useState<any[]>([])

  async function load() {
    const r = await fetch('/api/keys')
    const data = await r.json()
    setKeys(data.keys || [])
  }

  useEffect(() => { load() }, [])

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">API Keys</h1>
          <p className="mt-1 text-sm text-gray-600">Up to 5 active keys per account.</p>
        </div>
        <CreateKeyModal onCreated={load} />
      </div>
      <KeysTable keys={keys} onRevoke={load} />
    </div>
  )
}
