import Link from 'next/link'
import { headers } from 'next/headers'
import { BalanceWidget } from '@/components/balance-widget'
import { auth } from '@/lib/auth'
import { grantTrialCreditIfEligible } from '@/lib/credits'

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  // Idempotent trial credit grant: covers both GitHub OAuth users (who skip the
  // email-verification path) and as a safety net for email users.
  const session = await auth()
  if (session?.user?.id) {
    const ip = (await headers()).get('x-forwarded-for')?.split(',')[0].trim() || '0.0.0.0'
    await grantTrialCreditIfEligible(session.user.id, ip)
  }

  return (
    <div className="flex min-h-screen bg-gray-50">
      <aside className="w-56 border-r bg-white p-4">
        <div className="mb-6 font-bold">⚡ MaaS Router</div>
        <BalanceWidget />
        <nav className="mt-6 space-y-1 text-sm">
          <Link href="/dashboard" className="block rounded px-3 py-2 hover:bg-gray-100">Overview</Link>
          <Link href="/dashboard/keys" className="block rounded px-3 py-2 hover:bg-gray-100">API Keys</Link>
        </nav>
      </aside>
      <main className="flex-1 p-8">{children}</main>
    </div>
  )
}
