import Link from 'next/link'

export default function TopupCancelPage() {
  return (
    <div className="mx-auto max-w-md p-8 text-center">
      <h1 className="mb-2 text-2xl font-bold">Payment cancelled</h1>
      <p className="mb-6 text-gray-600">
        No charges were made. You can try again whenever you're ready.
      </p>
      <Link href="/dashboard/billing" className="text-indigo-600 hover:underline">
        Back to billing
      </Link>
    </div>
  )
}
