'use client'
import { useEffect, useState } from 'react'
import { useParams, useRouter } from 'next/navigation'

export default function VerifyEmailPage() {
  const { token } = useParams<{ token: string }>()
  const router = useRouter()
  const [status, setStatus] = useState<'pending' | 'ok' | 'error'>('pending')
  const [message, setMessage] = useState('')

  useEffect(() => {
    fetch('/api/auth/verify-email', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    })
      .then(async (r) => {
        const data = await r.json()
        if (r.ok) {
          setStatus('ok')
          setMessage('Email verified — $1 trial credit added.')
          setTimeout(() => router.push('/login'), 2000)
        } else {
          setStatus('error')
          setMessage(data.error || 'Verification failed')
        }
      })
      .catch(() => { setStatus('error'); setMessage('Network error') })
  }, [token, router])

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="rounded border bg-white p-8 shadow-sm">
        {status === 'pending' && <p>Verifying…</p>}
        {status === 'ok' && <p className="text-green-700">{message}</p>}
        {status === 'error' && <p className="text-red-700">{message}</p>}
      </div>
    </div>
  )
}
