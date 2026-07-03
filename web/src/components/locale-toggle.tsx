'use client'
import { useT } from '@/lib/i18n/context'

/** Header 中/EN toggle. */
export function LocaleToggle() {
  const { locale, setLocale } = useT()
  return (
    <button
      onClick={() => setLocale(locale === 'zh-CN' ? 'en' : 'zh-CN')}
      className="text-xs text-gray-600 hover:text-gray-900"
      title="Switch language / 切换语言"
    >
      {locale === 'zh-CN' ? '中 / EN' : 'EN / 中'}
    </button>
  )
}
