import Link from 'next/link'
import { headers } from 'next/headers'
import { BalanceWidget } from '@/components/balance-widget'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { grantTrialCreditIfEligible } from '@/lib/credits'

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  // Idempotent trial credit grant: covers both GitHub OAuth users (who skip the
  // email-verification path) and as a safety net for email users.
  const session = await auth()
  let isAdmin = false
  if (session?.user?.id) {
    const ip = (await headers()).get('x-forwarded-for')?.split(',')[0].trim() || '0.0.0.0'
    await grantTrialCreditIfEligible(session.user.id, ip)
    const u = await prisma.user.findUnique({
      where: { id: session.user.id },
      select: { isAdmin: true },
    })
    isAdmin = u?.isAdmin ?? false
  }

  return (
    <div className="flex min-h-screen bg-gray-50">
      <aside className="w-56 border-r bg-white p-4">
        <div className="mb-6 font-bold">⚡ MaaS Router</div>
        <BalanceWidget />
        <nav className="mt-6 space-y-1 text-sm">
          <Link href="/dashboard" className="block rounded px-3 py-2 hover:bg-gray-100">Overview</Link>
          <Link href="/dashboard/keys" className="block rounded px-3 py-2 hover:bg-gray-100">API Keys</Link>
          <Link href="/dashboard/billing" className="block rounded px-3 py-2 hover:bg-gray-100">Billing</Link>
          <Link href="/dashboard/usage" className="block rounded px-3 py-2 hover:bg-gray-100">Usage</Link>
          <Link href="/dashboard/playground" className="block rounded px-3 py-2 hover:bg-gray-100">Playground</Link>
          {isAdmin && (
            <Link href="/admin" className="block rounded px-3 py-2 font-medium text-red-700 hover:bg-red-50">
              ⚙️ Admin
            </Link>
          )}
        </nav>
      </aside>
      <main className="flex-1 p-8">{children}</main>
    </div>
  )
}
