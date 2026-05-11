import Link from 'next/link'
import { Button } from '@/components/ui/button'

export default function HomePage() {
  return (
    <div className="min-h-screen bg-gray-50">
      <header className="border-b bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <div className="font-bold">⚡ MaaS Router</div>
          <div className="flex items-center gap-3">
            <Link href="/login" className="text-sm text-gray-600 hover:underline">Log in</Link>
            <Link href="/signup"><Button>Get Started</Button></Link>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-3xl px-6 py-24 text-center">
        <h1 className="text-4xl font-bold leading-tight">
          Call frontier LLMs through one API.<br />
          <span className="text-indigo-600">Pay with Alipay or WeChat.</span>
        </h1>
        <p className="mx-auto mt-6 max-w-xl text-lg text-gray-600">
          GPT-4o, Claude, Gemini — one OpenAI-compatible endpoint, transparent 18% markup,
          $1 free trial to get started.
        </p>
        <div className="mt-10">
          <Link href="/signup"><Button size="lg">Start with $1 free →</Button></Link>
        </div>
        <p className="mt-4 text-xs text-gray-500">No credit card required for signup.</p>
      </main>
    </div>
  )
}
