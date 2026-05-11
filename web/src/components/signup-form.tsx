'use client'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { signIn } from 'next-auth/react'

export function SignupForm() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [status, setStatus] = useState<'idle' | 'submitting' | 'sent' | 'error'>('idle')
  const [message, setMessage] = useState('')

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setStatus('submitting')
    const res = await fetch('/api/signup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    })
    const data = await res.json()
    if (res.ok) {
      setStatus('sent')
      setMessage(data.message)
    } else {
      setStatus('error')
      setMessage(data.error || 'Signup failed')
    }
  }

  if (status === 'sent') {
    return <p className="text-green-700">{message}</p>
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <div>
        <Label htmlFor="email">Email</Label>
        <Input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
      </div>
      <div>
        <Label htmlFor="password">Password (10+ characters)</Label>
        <Input id="password" type="password" minLength={10} value={password} onChange={(e) => setPassword(e.target.value)} required />
      </div>
      {status === 'error' && <p className="text-sm text-red-700">{message}</p>}
      <Button type="submit" disabled={status === 'submitting'} className="w-full">
        {status === 'submitting' ? 'Creating account…' : 'Sign up'}
      </Button>
      <div className="text-center text-sm text-gray-500">or</div>
      <Button type="button" variant="outline" onClick={() => signIn('github', { callbackUrl: '/dashboard' })} className="w-full">
        Continue with GitHub
      </Button>
    </form>
  )
}
