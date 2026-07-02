'use client'
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const FALLBACK_PRESETS = [500, 1000, 2000, 5000, 10000]

export function TopUpModal() {
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState<number>(2000)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  // Presets + FX rate come from app_config via /api/config so an admin can
  // change them without a redeploy (M1). Fall back to defaults if unavailable.
  const [presets, setPresets] = useState<number[]>(FALLBACK_PRESETS)
  const [cnyRate, setCnyRate] = useState(7.2)

  useEffect(() => {
    fetch('/api/config')
      .then((r) => r.json())
      .then((d) => {
        if (Array.isArray(d.topupPresetsCents) && d.topupPresetsCents.length) {
          setPresets(d.topupPresetsCents)
          if (!d.topupPresetsCents.includes(2000)) setSelected(d.topupPresetsCents[0])
        }
        if (typeof d.cnyPerUsd === 'number') setCnyRate(d.cnyPerUsd)
      })
      .catch(() => {})
  }, [])

  async function checkout() {
    setLoading(true)
    setError('')
    try {
      const res = await fetch('/api/topup/create-session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ amountCents: selected }),
      })
      const data = await res.json()
      if (!res.ok) {
        setError(data.error || 'Failed to start checkout')
        setLoading(false)
        return
      }
      window.location.href = data.url
    } catch (e) {
      setError('Network error')
      setLoading(false)
    }
  }

  return (
    <>
      <Button onClick={() => setOpen(true)}>Top Up Credits</Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Top up your balance</DialogTitle>
          </DialogHeader>
          <div className="grid grid-cols-3 gap-2 my-4">
            {presets.map((amt) => (
              <button
                key={amt}
                onClick={() => setSelected(amt)}
                className={
                  selected === amt
                    ? 'rounded border-2 border-indigo-500 bg-indigo-50 p-3 text-center'
                    : 'rounded border border-gray-200 p-3 text-center hover:border-gray-300'
                }
              >
                <div className="font-bold">${(amt / 100).toFixed(0)}</div>
                <div className="text-xs text-gray-500">≈¥{((amt / 100) * cnyRate).toFixed(0)}</div>
              </button>
            ))}
          </div>
          <p className="text-xs text-gray-600">
            Payment methods: Card, Alipay, WeChat Pay (chosen on the next page).
          </p>
          {error && <p className="text-sm text-red-600 mt-2">{error}</p>}
          <Button onClick={checkout} disabled={loading} className="w-full mt-4">
            {loading ? 'Redirecting…' : `Continue with $${(selected / 100).toFixed(2)}`}
          </Button>
        </DialogContent>
      </Dialog>
    </>
  )
}
