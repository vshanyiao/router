'use client'
import { useEffect, useRef, useState } from 'react'

type Model = {
  alias: string
  displayName: string
  upstreamProvider: string
  inputCentsPerMillionTokens: number
  outputCentsPerMillionTokens: number
}

type Message = { role: 'user' | 'assistant'; content: string }

const MAX_MESSAGES = 20

function formatPrice(cents: number): string {
  return `$${(cents / 100).toFixed(2)}/M`
}

export default function PlaygroundPage() {
  const [models, setModels] = useState<Model[]>([])
  const [model, setModel] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    fetch('/api/playground/models')
      .then((r) => r.json())
      .then((d) => {
        const list: Model[] = d.models || []
        setModels(list)
        if (list.length > 0) setModel(list[0].alias)
      })
      .catch(() => setError('Failed to load models.'))
  }, [])

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight })
  }, [messages])

  // Group models by provider for the <optgroup> picker.
  const grouped = models.reduce<Record<string, Model[]>>((acc, m) => {
    ;(acc[m.upstreamProvider] ??= []).push(m)
    return acc
  }, {})

  const atCap = messages.length >= MAX_MESSAGES
  const canSend = !streaming && !atCap && !!model && input.trim().length > 0

  async function send() {
    if (!canSend) return
    setError('')

    const userMsg: Message = { role: 'user', content: input.trim() }
    const history = [...messages, userMsg]
    // Add the user message plus an empty assistant placeholder to fill in.
    setMessages([...history, { role: 'assistant', content: '' }])
    setInput('')
    setStreaming(true)

    try {
      const res = await fetch('/api/playground/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model, messages: history }),
      })

      if (!res.ok || !res.body) {
        const d = await res.json().catch(() => ({}))
        throw new Error(d.error || `Request failed (${res.status})`)
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let done = false

      while (!done) {
        const { value, done: rDone } = await reader.read()
        done = rDone
        buffer += decoder.decode(value, { stream: true })

        // SSE events are separated by blank lines; process complete lines.
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          const trimmed = line.trim()
          if (!trimmed.startsWith('data:')) continue
          const data = trimmed.slice(5).trim()
          if (data === '[DONE]') {
            done = true
            break
          }
          try {
            const json = JSON.parse(data)
            const delta = json.choices?.[0]?.delta?.content
            if (typeof delta === 'string' && delta.length > 0) {
              setMessages((prev) => {
                const next = [...prev]
                const last = next[next.length - 1]
                if (last?.role === 'assistant') {
                  next[next.length - 1] = { ...last, content: last.content + delta }
                }
                return next
              })
            }
          } catch {
            // Ignore non-JSON keepalive lines.
          }
        }
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Something went wrong.'
      setError(msg)
      // Drop the empty assistant placeholder on failure.
      setMessages((prev) => {
        const last = prev[prev.length - 1]
        if (last?.role === 'assistant' && last.content === '') return prev.slice(0, -1)
        return prev
      })
    } finally {
      setStreaming(false)
    }
  }

  const selected = models.find((m) => m.alias === model)

  return (
    <div className="flex h-[calc(100vh-4rem)] max-w-3xl flex-col">
      <h1 className="mb-2 text-2xl font-bold">Playground</h1>
      <p className="mb-4 text-sm text-amber-700">
        Playground uses your real credits.
      </p>

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <select
          value={model}
          onChange={(e) => setModel(e.target.value)}
          disabled={streaming}
          className="rounded border px-3 py-2 text-sm"
        >
          {Object.entries(grouped).map(([provider, ms]) => (
            <optgroup key={provider} label={provider}>
              {ms.map((m) => (
                <option key={m.alias} value={m.alias}>
                  {m.displayName} — in {formatPrice(m.inputCentsPerMillionTokens)}, out{' '}
                  {formatPrice(m.outputCentsPerMillionTokens)}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
        {selected && (
          <span className="text-xs text-gray-500">
            in {formatPrice(selected.inputCentsPerMillionTokens)} · out{' '}
            {formatPrice(selected.outputCentsPerMillionTokens)}
          </span>
        )}
      </div>

      <div
        ref={scrollRef}
        className="flex-1 space-y-3 overflow-y-auto rounded border bg-gray-50 p-4"
      >
        {messages.length === 0 && (
          <p className="text-sm text-gray-400">Start a conversation below.</p>
        )}
        {messages.map((m, i) => (
          <div
            key={i}
            className={m.role === 'user' ? 'flex justify-end' : 'flex justify-start'}
          >
            <div
              className={
                m.role === 'user'
                  ? 'max-w-[80%] whitespace-pre-wrap rounded-lg bg-indigo-600 px-3 py-2 text-sm text-white'
                  : 'max-w-[80%] whitespace-pre-wrap rounded-lg border bg-white px-3 py-2 text-sm text-gray-900'
              }
            >
              {m.content || (streaming && i === messages.length - 1 ? '…' : '')}
            </div>
          </div>
        ))}
      </div>

      {error && <p className="mt-2 text-sm text-red-600">{error}</p>}

      <div className="mt-3 flex items-end gap-2">
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              send()
            }
          }}
          placeholder={atCap ? 'Conversation limit reached.' : 'Type a message…'}
          disabled={streaming || atCap}
          rows={2}
          className="flex-1 resize-none rounded border px-3 py-2 text-sm"
        />
        <button
          onClick={send}
          disabled={!canSend}
          className="rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
        >
          {streaming ? 'Sending…' : 'Send'}
        </button>
      </div>
      <p className="mt-1 text-xs text-gray-400">
        {messages.length}/{MAX_MESSAGES} messages
      </p>
    </div>
  )
}
