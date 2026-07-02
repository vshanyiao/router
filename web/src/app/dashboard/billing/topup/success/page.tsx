'use client'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'next/navigation'
import Link from 'next/link'

type Phase = 'polling' | 'succeeded' | 'timeout'

export default function TopupSuccessPage() {
  const params = useSearchParams()
  const piId = params.get('pi')
  const [phase, setPhase] = useState<Phase>('polling')
  const [creditsCents, setCreditsCents] = useState<number | null>(null)

  useEffect(() => {
    if (!piId) {
      setPhase('timeout')
      return
    }

    let cancelled = false
    let attempts = 0
    const maxAttempts = 15 // ~30s at 2s intervals

    async function poll() {
      if (cancelled) return
      attempts++
      try {
        const r = await fetch(`/api/topup/status?pi=${encodeURIComponent(piId!)}`)
        if (r.ok) {
          const data = await r.json()
          if (data.status === 'succeeded') {
            if (!cancelled) {
              setCreditsCents(data.creditsAddedCents)
              setPhase('succeeded')
            }
            return // stop polling
          }
        }
      } catch {
        // transient — keep polling
      }
      if (attempts >= maxAttempts) {
        if (!cancelled) setPhase('timeout')
        return
      }
      timer = setTimeout(poll, 2000)
    }

    let timer = setTimeout(poll, 0)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [piId])

  return (
    <div className="mx-auto max-w-md p-8 text-center">
      <h1 className="mb-2 text-2xl font-bold">
        {phase === 'succeeded' ? '✓ Credits added' : 'Payment received'}
      </h1>
      <p className="mb-6 text-gray-600">
        {phase === 'succeeded'
          ? 'Your balance has been updated.'
          : phase === 'timeout'
          ? 'Still processing — your balance will update shortly. Refresh billing to check.'
          : 'Confirming your payment…'}
      </p>
      {phase === 'succeeded' && creditsCents !== null && (
        <div className="mb-6 rounded-lg border p-4">
          <div className="text-xs uppercase text-gray-500">Credits added</div>
          <div className="text-3xl font-bold">${(creditsCents / 100).toFixed(2)}</div>
        </div>
      )}
      <Link href="/dashboard/billing" className="text-indigo-600 hover:underline">
        Back to billing
      </Link>
    </div>
  )
}
