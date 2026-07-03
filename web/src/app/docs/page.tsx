'use client'

import Link from 'next/link'
import { Button } from '@/components/ui/button'
import { LocaleToggle } from '@/components/locale-toggle'
import { useT } from '@/lib/i18n/context'

const curlExample = `curl https://api.<yourdomain>/v1/chat/completions \\
  -H "Authorization: Bearer sk-or-..." \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "openai/gpt-4o",
    "messages": [
      { "role": "user", "content": "Hello from MaaS Router!" }
    ]
  }'`

const pythonExample = `from openai import OpenAI

client = OpenAI(
    base_url="https://api.<yourdomain>/v1",
    api_key="sk-or-...",
)

resp = client.chat.completions.create(
    model="anthropic/claude-haiku-4-5",
    messages=[
        {"role": "user", "content": "Hello from MaaS Router!"},
    ],
)

print(resp.choices[0].message.content)`

const typescriptExample = `import OpenAI from "openai"

const client = new OpenAI({
  baseURL: "https://api.<yourdomain>/v1",
  apiKey: "sk-or-...",
})

const resp = await client.chat.completions.create({
  model: "anthropic/claude-haiku-4-5",
  messages: [
    { role: "user", content: "Hello from MaaS Router!" },
  ],
})

console.log(resp.choices[0].message.content)`

const anthropicExample = `from anthropic import Anthropic

client = Anthropic(
    base_url="https://api.<yourdomain>",
    api_key="sk-or-...",  # sent as x-api-key
)

msg = client.messages.create(
    model="anthropic/claude-haiku-4-5",
    max_tokens=1024,
    messages=[
        {"role": "user", "content": "Hello from MaaS Router!"},
    ],
)

print(msg.content[0].text)`

const streamingExample = `resp = client.chat.completions.create(
    model="openai/gpt-4o",
    messages=[{"role": "user", "content": "Stream this, please."}],
    stream=True,
)

for chunk in resp:
    delta = chunk.choices[0].delta.content
    if delta:
        print(delta, end="", flush=True)`

const errorRows: [string, string, string][] = [
  ['401', 'unauthorized', 'API key missing or invalid.'],
  ['402', 'insufficient_credits', 'Account balance too low — top up in the dashboard.'],
  ['429', 'rate_limit_exceeded', 'Too many requests — back off and retry.'],
  ['502', 'upstream_error', 'The upstream model provider returned an error.'],
]

function CodeBlock({ children }: { children: string }) {
  return (
    <pre className="bg-gray-900 text-gray-100 rounded p-4 overflow-x-auto text-sm">
      <code>{children}</code>
    </pre>
  )
}

export default function DocsPage() {
  const { t } = useT()

  return (
    <div className="min-h-screen bg-gray-50">
      <header className="border-b bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <Link href="/" className="font-bold">
            ⚡ MaaS Router
          </Link>
          <nav className="flex items-center gap-4">
            <Link href="/models" className="text-sm text-gray-600 hover:text-gray-900">
              {t('nav.models')}
            </Link>
            <Link href="/pricing" className="text-sm text-gray-600 hover:text-gray-900">
              {t('nav.pricing')}
            </Link>
            <Link href="/docs" className="text-sm text-gray-900 font-medium">
              {t('nav.docs')}
            </Link>
            <LocaleToggle />
            <Link href="/login" className="text-sm text-gray-600 hover:text-gray-900">
              {t('nav.login')}
            </Link>
            <Link href="/signup">
              <Button>{t('nav.getStarted')}</Button>
            </Link>
          </nav>
        </div>
      </header>

      <main className="mx-auto max-w-3xl px-6 py-16">
        <h1 className="text-3xl font-bold text-gray-900">{t('docs.title')}</h1>

        <section className="mt-10">
          <h2 className="text-xl font-semibold text-gray-900">{t('docs.quickstart')}</h2>
          <p className="mt-3 text-gray-700">
            MaaS Router exposes an <strong>OpenAI-compatible</strong> API. Point any OpenAI SDK at
            our base URL, swap in your API key, and prefix the model name with its provider
            (e.g. <code className="rounded bg-gray-200 px-1">openai/gpt-4o</code>).
          </p>
          <ul className="mt-4 list-disc space-y-2 pl-6 text-gray-700">
            <li>
              Base URL: <code className="rounded bg-gray-200 px-1">https://api.&lt;yourdomain&gt;/v1</code>
              {' '}(dev: <code className="rounded bg-gray-200 px-1">http://localhost:8080/v1</code>)
            </li>
            <li>
              Get an API key from your{' '}
              <Link href="/dashboard" className="text-indigo-600 hover:underline">
                dashboard
              </Link>
              . Keys look like <code className="rounded bg-gray-200 px-1">sk-or-...</code>.
            </li>
            <li>Send the key as a bearer token in the <code className="rounded bg-gray-200 px-1">Authorization</code> header.</li>
          </ul>
        </section>

        <section className="mt-10">
          <h2 className="text-xl font-semibold text-gray-900">cURL</h2>
          <p className="mt-3 text-gray-700">
            A minimal chat completion request against <code className="rounded bg-gray-200 px-1">/v1/chat/completions</code>:
          </p>
          <div className="mt-4">
            <CodeBlock>{curlExample}</CodeBlock>
          </div>
        </section>

        <section className="mt-10">
          <h2 className="text-xl font-semibold text-gray-900">Python (openai SDK)</h2>
          <p className="mt-3 text-gray-700">
            Install with <code className="rounded bg-gray-200 px-1">pip install openai</code>, then
            override the base URL:
          </p>
          <div className="mt-4">
            <CodeBlock>{pythonExample}</CodeBlock>
          </div>
        </section>

        <section className="mt-10">
          <h2 className="text-xl font-semibold text-gray-900">TypeScript (openai SDK)</h2>
          <p className="mt-3 text-gray-700">
            Install with <code className="rounded bg-gray-200 px-1">npm install openai</code>:
          </p>
          <div className="mt-4">
            <CodeBlock>{typescriptExample}</CodeBlock>
          </div>
        </section>

        <section className="mt-10">
          <h2 className="text-xl font-semibold text-gray-900">Anthropic-compatible surface</h2>
          <p className="mt-3 text-gray-700">
            Prefer the Anthropic SDK? <code className="rounded bg-gray-200 px-1">POST /v1/messages</code>{' '}
            is supported. Override the SDK&apos;s base URL and pass your key as{' '}
            <code className="rounded bg-gray-200 px-1">x-api-key</code> (the SDK does this for you when
            you set <code className="rounded bg-gray-200 px-1">api_key</code>):
          </p>
          <div className="mt-4">
            <CodeBlock>{anthropicExample}</CodeBlock>
          </div>
        </section>

        <section className="mt-10">
          <h2 className="text-xl font-semibold text-gray-900">Streaming</h2>
          <p className="mt-3 text-gray-700">
            Add <code className="rounded bg-gray-200 px-1">stream: true</code> to receive
            server-sent tokens as they are generated:
          </p>
          <div className="mt-4">
            <CodeBlock>{streamingExample}</CodeBlock>
          </div>
        </section>

        <section className="mt-10 mb-16">
          <h2 className="text-xl font-semibold text-gray-900">Errors</h2>
          <p className="mt-3 text-gray-700">
            Errors are returned as JSON with an HTTP status and an{' '}
            <code className="rounded bg-gray-200 px-1">error.code</code>:
          </p>
          <div className="mt-4 overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <thead>
                <tr className="border-b border-gray-300 text-left text-gray-600">
                  <th className="py-2 pr-4 font-medium">Status</th>
                  <th className="py-2 pr-4 font-medium">Code</th>
                  <th className="py-2 font-medium">Meaning</th>
                </tr>
              </thead>
              <tbody>
                {errorRows.map(([status, code, meaning]) => (
                  <tr key={status} className="border-b border-gray-200">
                    <td className="py-2 pr-4 font-mono text-gray-900">{status}</td>
                    <td className="py-2 pr-4 font-mono text-gray-700">{code}</td>
                    <td className="py-2 text-gray-700">{meaning}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </main>
    </div>
  )
}
