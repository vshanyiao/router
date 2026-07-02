'use client'
import { createContext, useContext, useEffect, useState, useCallback } from 'react'
import { messages, type Locale, type MessageKey } from './messages'

interface LocaleContextValue {
  locale: Locale
  setLocale: (l: Locale) => void
  t: (key: MessageKey) => string
}

const LocaleContext = createContext<LocaleContextValue | null>(null)

const STORAGE_KEY = 'maas-locale'

export function LocaleProvider({ children }: { children: React.ReactNode }) {
  // Default zh-CN (target audience). Hydrate from localStorage on mount, and
  // detect Accept-Language leaning English only if no stored preference.
  const [locale, setLocaleState] = useState<Locale>('zh-CN')

  useEffect(() => {
    const stored = typeof window !== 'undefined' ? (localStorage.getItem(STORAGE_KEY) as Locale | null) : null
    if (stored === 'zh-CN' || stored === 'en') {
      setLocaleState(stored)
    } else if (typeof navigator !== 'undefined' && navigator.language.startsWith('en')) {
      setLocaleState('en')
    }
  }, [])

  const setLocale = useCallback((l: Locale) => {
    setLocaleState(l)
    if (typeof window !== 'undefined') localStorage.setItem(STORAGE_KEY, l)
    // Best-effort persist to the user's profile if logged in.
    fetch('/api/locale', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ locale: l }),
    }).catch(() => {})
  }, [])

  const t = useCallback((key: MessageKey) => messages[locale][key] ?? key, [locale])

  return <LocaleContext.Provider value={{ locale, setLocale, t }}>{children}</LocaleContext.Provider>
}

export function useT() {
  const ctx = useContext(LocaleContext)
  if (!ctx) throw new Error('useT must be used within LocaleProvider')
  return ctx
}
