'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import { Button } from '@/components/ui/button'
import { LocaleToggle } from '@/components/locale-toggle'
import { useT } from '@/lib/i18n/context'

interface Config {
  topupPresetsCents: number[]
  cnyPerUsd: number
}

interface Model {
  displayName: string
  inputCentsPerMillionTokens: number
  outputCentsPerMillionTokens: number
}

// cents → "$X.XX per 1M tokens"
function perMillion(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`
}

export default function PricingPage() {
  const { t } = useT()
  const [config, setConfig] = useState<Config | null>(null)
  const [models, setModels] = useState<Model[]>([])

  useEffect(() => {
    fetch('/api/config')
      .then((r) => r.json())
      .then(setConfig)
      .catch(() => {})
    fetch('/api/models')
      .then((r) => r.json())
      .then((d) => setModels(d.models ?? []))
      .catch(() => {})
  }, [])

  return (
    <div className="min-h-screen bg-gray-50">
      <header className="sticky top-0 z-10 border-b bg-white/80 backdrop-blur">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <Link href="/" className="font-bold">⚡ MaaS Router</Link>
          <nav className="hidden items-center gap-6 text-sm text-gray-600 md:flex">
            <Link href="/models" className="hover:text-gray-900">{t('nav.models')}</Link>
            <Link href="/pricing" className="hover:text-gray-900">{t('nav.pricing')}</Link>
            <Link href="/docs" className="hover:text-gray-900">{t('nav.docs')}</Link>
          </nav>
          <div className="flex items-center gap-3">
            <LocaleToggle />
            <Link href="/login" className="text-sm text-gray-600 hover:text-gray-900">{t('nav.login')}</Link>
            <Link href="/signup"><Button>{t('nav.getStarted')}</Button></Link>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-16">
        <div className="text-center">
          <h1 className="text-4xl font-bold">{t('pricing.title')}</h1>
          <p className="mx-auto mt-4 max-w-xl text-lg text-gray-600">{t('pricing.subtitle')}</p>
        </div>

        {/* Top-up presets */}
        <section className="mt-16">
          <h2 className="text-xl font-semibold">{t('pricing.presetsTitle')}</h2>
          <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-5">
            {config?.topupPresetsCents.map((cents) => {
              const usd = cents / 100
              const cny = usd * config.cnyPerUsd
              return (
                <div key={cents} className="rounded-xl border bg-white p-5 text-center">
                  <div className="text-2xl font-bold">${usd.toFixed(0)}</div>
                  <div className="mt-1 text-sm text-gray-500">≈ ¥{cny.toFixed(0)}</div>
                </div>
              )
            })}
          </div>
        </section>

        {/* Per-model rates */}
        <section className="mt-16">
          <h2 className="text-xl font-semibold">{t('pricing.perModelTitle')}</h2>
          <p className="mt-2 text-sm text-gray-500">+18% markup at billing</p>
          <div className="mt-6 overflow-x-auto rounded-xl border bg-white">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-gray-50 text-left text-gray-500">
                  <th className="px-5 py-3 font-medium">{t('pricing.colModel')}</th>
                  <th className="px-5 py-3 text-right font-medium">{t('pricing.colInput')}</th>
                  <th className="px-5 py-3 text-right font-medium">{t('pricing.colOutput')}</th>
                </tr>
              </thead>
              <tbody>
                {models.map((m) => (
                  <tr key={m.displayName} className="border-b last:border-0">
                    <td className="px-5 py-3 font-medium">{m.displayName}</td>
                    <td className="px-5 py-3 text-right tabular-nums">{perMillion(m.inputCentsPerMillionTokens)}</td>
                    <td className="px-5 py-3 text-right tabular-nums">{perMillion(m.outputCentsPerMillionTokens)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        {/* FAQ */}
        <section className="mt-16 max-w-2xl">
          <h2 className="text-xl font-semibold">{t('pricing.faqTitle')}</h2>
          <dl className="mt-6 space-y-6">
            <div>
              <dt className="font-medium">How does billing work?</dt>
              <dd className="mt-1 text-sm text-gray-600">
                Pre-pay with Alipay or WeChat Pay to add credits. Each request deducts the provider
                cost plus a flat 18% markup — no subscriptions, no minimums.
              </dd>
            </div>
            <div>
              <dt className="font-medium">Do credits expire?</dt>
              <dd className="mt-1 text-sm text-gray-600">
                No. Your balance stays until you spend it.
              </dd>
            </div>
            <div>
              <dt className="font-medium">What does the ¥ estimate mean?</dt>
              <dd className="mt-1 text-sm text-gray-600">
                Prices are set in USD. The CNY figure is an approximate conversion at the current
                display rate; the exact amount charged is shown at checkout.
              </dd>
            </div>
          </dl>
        </section>
      </main>

      <footer className="border-t bg-white">
        <div className="mx-auto max-w-6xl px-6 py-8 text-center text-xs text-gray-400">
          © {new Date().getFullYear()} MaaS Router
        </div>
      </footer>
    </div>
  )
}
