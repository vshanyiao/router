'use client'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'

export function CreateKeyModal({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [plaintext, setPlaintext] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    const res = await fetch('/api/keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
    const data = await res.json()
    setLoading(false)
    if (res.ok) {
      setPlaintext(data.key.plaintext)
    } else {
      setError(data.error || 'Failed to create key')
    }
  }

  function close() {
    setOpen(false)
    setName('')
    setPlaintext(null)
    setError('')
    onCreated()
  }

  return (
    <>
      <Button onClick={() => setOpen(true)}>+ New Key</Button>
      <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{plaintext ? 'Save this key — it will not be shown again' : 'Create API key'}</DialogTitle>
        </DialogHeader>
        {!plaintext ? (
          <form onSubmit={onSubmit} className="space-y-4">
            <div>
              <Label htmlFor="key-name">Name</Label>
              <Input id="key-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. production-app" required maxLength={64} />
            </div>
            {error && <p className="text-sm text-red-700">{error}</p>}
            <Button type="submit" disabled={loading} className="w-full">{loading ? 'Creating…' : 'Create key'}</Button>
          </form>
        ) : (
          <div className="space-y-4">
            <div className="rounded border bg-yellow-50 p-3 text-sm text-yellow-900">
              ⚠️ Copy this key now. We will not show it again.
            </div>
            <code className="block break-all rounded bg-gray-100 p-3 text-sm">{plaintext}</code>
            <Button onClick={() => navigator.clipboard.writeText(plaintext)} className="w-full">Copy to clipboard</Button>
            <Button onClick={close} variant="outline" className="w-full">Done</Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
    </>
  )
}
