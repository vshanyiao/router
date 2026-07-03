'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import { Button } from '@/components/ui/button'
import { LocaleToggle } from '@/components/locale-toggle'
import { useT } from '@/lib/i18n/context'

interface Model {
  alias: string
  displayName: string
  upstreamProvider: string
  contextWindow: number
  supportsTools: boolean
  supportsVision: boolean
  supportsStreaming: boolean
  inputCentsPerMillionTokens: number
  outputCentsPerMillionTokens: number
  descriptionZh: string | null
  descriptionEn: string | null
}

// cents → dollars per 1M tokens, e.g. 250 cents → "$2.50"
function dollarsPerMillion(cents: number) {
  return `$${(cents / 100).toFixed(2)}`
}

export default function ModelsPage() {
  const { t, locale } = useT()
  const [models, setModels] = useState<Model[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/models')
      .then((r) => r.json())
      .then((data) => setModels(data.models ?? []))
      .catch(() => setModels([]))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="min-h-screen bg-gray-50">
      <header className="border-b bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <Link href="/" className="font-bold">
            ⚡ MaaS Router
          </Link>
          <div className="flex items-center gap-3">
            <LocaleToggle />
            <Link href="/login" className="text-sm text-gray-600 hover:underline">
              {t('nav.login')}
            </Link>
            <Link href="/signup">
              <Button>{t('nav.getStarted')}</Button>
            </Link>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-16">
        <h1 className="text-3xl font-bold">{t('models.title')}</h1>
        <p className="mt-2 text-gray-600">{t('models.subtitle')}</p>

        <div className="mt-10 overflow-hidden rounded-lg border bg-white">
          <table className="w-full text-sm">
            <thead className="border-b bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-4 py-3 font-medium">{t('models.colModel')}</th>
                <th className="px-4 py-3 font-medium">{t('nav.models')}</th>
                <th className="px-4 py-3 font-medium">{t('models.colContext')}</th>
                <th className="px-4 py-3 font-medium">{t('models.colInput')}</th>
                <th className="px-4 py-3 font-medium">{t('models.colOutput')}</th>
                <th className="px-4 py-3 font-medium">{t('models.colCaps')}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {loading ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400">
                    …
                  </td>
                </tr>
              ) : models.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400">
                    —
                  </td>
                </tr>
              ) : (
                models.map((m) => {
                  const description = locale === 'zh-CN' ? m.descriptionZh : m.descriptionEn
                  return (
                    <tr key={m.alias} className="align-top">
                      <td className="px-4 py-3">
                        <div className="font-medium text-gray-900">{m.displayName}</div>
                        <div className="font-mono text-xs text-gray-500">{m.alias}</div>
                        {description && (
                          <div className="mt-1 max-w-xs text-xs text-gray-400">{description}</div>
                        )}
                      </td>
                      <td className="px-4 py-3 text-gray-600">{m.upstreamProvider}</td>
                      <td className="px-4 py-3 text-gray-600">
                        {Math.round(m.contextWindow / 1000)}K
                      </td>
                      <td className="px-4 py-3 text-gray-600">
                        {dollarsPerMillion(m.inputCentsPerMillionTokens)}
                      </td>
                      <td className="px-4 py-3 text-gray-600">
                        {dollarsPerMillion(m.outputCentsPerMillionTokens)}
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap gap-1">
                          {m.supportsStreaming && <Badge>{t('common.streaming')}</Badge>}
                          {m.supportsTools && <Badge>{t('common.tools')}</Badge>}
                          {m.supportsVision && <Badge>{t('common.vision')}</Badge>}
                        </div>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </main>
    </div>
  )
}

function Badge({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex rounded-full bg-indigo-50 px-2 py-0.5 text-xs font-medium text-indigo-700">
      {children}
    </span>
  )
}
