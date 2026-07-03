'use client'
import Link from 'next/link'
import { Button } from '@/components/ui/button'
import { LocaleToggle } from '@/components/locale-toggle'
import { useT } from '@/lib/i18n/context'

export default function HomePage() {
  const { t } = useT()

  const props = [
    { emoji: '🌐', title: t('landing.prop1Title'), body: t('landing.prop1Body') },
    { emoji: '💰', title: t('landing.prop2Title'), body: t('landing.prop2Body') },
    { emoji: '📊', title: t('landing.prop3Title'), body: t('landing.prop3Body') },
  ]

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

      <main>
        <section className="mx-auto max-w-3xl px-6 py-24 text-center">
          <h1 className="text-4xl font-bold leading-tight sm:text-5xl">
            {t('landing.heroTitle')}<br />
            <span className="text-indigo-600">{t('landing.heroSubtitle')}</span>
          </h1>
          <p className="mx-auto mt-6 max-w-xl text-lg text-gray-600">
            {t('landing.heroBlurb')}
          </p>
          <div className="mt-10">
            <Link href="/signup"><Button size="lg">{t('landing.cta')}</Button></Link>
          </div>
          <p className="mt-4 text-xs text-gray-500">{t('landing.ctaNote')}</p>
        </section>

        <section className="border-y bg-white">
          <div className="mx-auto max-w-6xl px-6 py-6 text-center text-sm text-gray-500">
            OpenAI · Anthropic · Google
            <span className="mx-3 text-gray-300">|</span>
            支付宝 · 微信支付 · Visa/Mastercard
          </div>
        </section>

        <section className="mx-auto max-w-6xl px-6 py-20">
          <div className="grid gap-8 md:grid-cols-3">
            {props.map((p) => (
              <div key={p.title} className="rounded-xl border bg-white p-6">
                <div className="text-3xl">{p.emoji}</div>
                <h3 className="mt-4 text-lg font-semibold">{p.title}</h3>
                <p className="mt-2 text-sm text-gray-600">{p.body}</p>
              </div>
            ))}
          </div>
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
