import { TopUpModal } from '@/components/topup-modal'
import { TopUpHistoryTable } from '@/components/topup-history-table'
import { BalanceWidget } from '@/components/balance-widget'

export default function BillingPage() {
  return (
    <div className="max-w-3xl space-y-6">
      <h1 className="text-2xl font-bold">Billing</h1>
      <div className="rounded-lg border bg-white p-6">
        <BalanceWidget />
        <div className="mt-4">
          <TopUpModal />
        </div>
      </div>
      <div className="rounded-lg border bg-white p-6">
        <h2 className="mb-4 font-semibold">Top-up history</h2>
        <TopUpHistoryTable />
      </div>
    </div>
  )
}
