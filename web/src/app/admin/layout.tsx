import Link from 'next/link'
import { redirect } from 'next/navigation'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

// Server-side gate: only is_admin users reach any /admin/* page. Non-admins
// (and unauthenticated visitors) are redirected to the dashboard.
export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  const session = await auth()
  if (!session?.user?.id) redirect('/login')
  const user = await prisma.user.findUnique({
    where: { id: session.user.id },
    select: { isAdmin: true, email: true },
  })
  if (!user?.isAdmin) redirect('/dashboard')

  const nav = [
    ['/admin', 'Overview'],
    ['/admin/pricing', 'Pricing'],
    ['/admin/models', 'Models'],
    ['/admin/users', 'Users'],
    ['/admin/transactions', 'Transactions'],
    ['/admin/requests', 'Requests'],
    ['/admin/audit', 'Audit Log'],
  ]

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="bg-red-50 py-1.5 text-center text-xs font-semibold text-red-800">
        ⚠️ ADMIN MODE — all actions are recorded to the audit log
      </div>
      <div className="flex min-h-screen">
        <aside className="w-52 bg-gray-800 p-4 text-gray-300">
          <div className="mb-4 font-bold text-white">⚙️ Admin Panel</div>
          <nav className="space-y-1 text-sm">
            {nav.map(([href, label]) => (
              <Link key={href} href={href} className="block rounded px-3 py-2 hover:bg-gray-700 hover:text-white">
                {label}
              </Link>
            ))}
          </nav>
          <div className="mt-8 border-t border-gray-700 pt-4 text-xs">
            <div className="text-gray-400">Signed in as</div>
            <div className="font-semibold text-white">{user.email}</div>
            <Link href="/dashboard" className="mt-2 block text-indigo-400 hover:underline">
              ← Back to dashboard
            </Link>
          </div>
        </aside>
        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  )
}
