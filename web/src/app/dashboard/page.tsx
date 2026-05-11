import Link from 'next/link'

export default function DashboardPage() {
  return (
    <div>
      <h1 className="mb-6 text-2xl font-bold">Welcome to MaaS Router</h1>
      <div className="rounded-lg border-2 border-dashed border-indigo-300 bg-indigo-50/50 p-6">
        <div className="mb-2 font-semibold">Get started:</div>
        <ol className="space-y-2 text-sm">
          <li>✅ You've been granted $1.00 trial credit</li>
          <li>
            ⬜ <Link href="/dashboard/keys" className="text-indigo-600 underline">Create your first API key</Link>
          </li>
          <li>⬜ Make your first API call (see <Link href="/docs" className="text-indigo-600 underline">docs</Link>)</li>
        </ol>
      </div>
    </div>
  )
}
