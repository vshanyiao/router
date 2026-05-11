'use client'
import { useEffect, useState } from 'react'

export function BalanceWidget() {
  const [balance, setBalance] = useState<number | null>(null)

  useEffect(() => {
    fetch('/api/credits/balance').then(async (r) => {
      if (r.ok) {
        const data = await r.json()
        setBalance(data.creditsCents)
      }
    })
  }, [])

  if (balance === null) return <div className="text-sm text-gray-500">…</div>
  const dollars = (balance / 100).toFixed(2)
  return (
    <div className="rounded-lg bg-indigo-50 p-4">
      <div className="text-xs uppercase text-gray-600">Balance</div>
      <div className="text-2xl font-bold">${dollars}</div>
    </div>
  )
}
