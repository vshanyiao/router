'use client'
import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'

export default function TopupSuccessPage() {
  const [balance, setBalance] = useState<number | null>(null)
  const [polled, setPolled] = useState(0)
  const initialBalanceRef = useRef<number | null>(null)
  const settledRef = useRef(false)
  const maxPolls = 15 // ~30s at 2s intervals

  useEffect(() => {
    let cancelled = false

    async function tick() {
      if (cancelled || settledRef.current) return
      try {
        const r = await fetch('/api/credits/balance')
        if (!r.ok) return
        const data = await r.json()
        if (initialBalanceRef.current === null) {
          initialBalanceRef.current = data.creditsCents
        }
        setBalance(data.creditsCents)
        if (data.creditsCents > (initialBalanceRef.current ?? 0)) {
          settledRef.current = true
        }
      } catch {
        // swallow — next tick will retry
      }
    }

    tick()
    const interval = setInterval(() => {
      setPolled((p) => {
        if (p >= maxPolls || settledRef.current) {
          clearInterval(interval)
          return p
        }
        tick()
        return p + 1
      })
    }, 2000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [])

  const settled = settledRef.current
  const giveUp = polled >= maxPolls && !settled

  return (
    <div className="mx-auto max-w-md p-8 text-center">
      <h1 className="mb-2 text-2xl font-bold">
        {settled ? '✓ Credits added' : 'Payment received'}
      </h1>
      <p className="mb-6 text-gray-600">
        {settled
          ? 'Your balance has been updated.'
          : giveUp
          ? 'Webhook is still processing — your balance will update shortly. Refresh to check.'
          : 'Waiting for credits to land in your account…'}
      </p>
      {balance !== null && (
        <div className="mb-6 rounded-lg border p-4">
          <div className="text-xs uppercase text-gray-500">Current balance</div>
          <div className="text-3xl font-bold">${(balance / 100).toFixed(2)}</div>
        </div>
      )}
      <Link href="/dashboard/billing" className="text-indigo-600 hover:underline">
        Back to billing
      </Link>
    </div>
  )
}
