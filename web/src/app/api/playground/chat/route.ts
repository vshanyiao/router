import { NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'
import { generateApiKey } from '@/lib/api-keys'

export const runtime = 'nodejs'
export const dynamic = 'force-dynamic'

const PROXY_URL = process.env.PROXY_URL || 'http://localhost:8080'

export async function POST(req: Request) {
  const session = await auth()
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }
  const userId = session.user.id

  const body = await req.json().catch(() => null)
  const model = body?.model
  const messages = body?.messages
  if (typeof model !== 'string' || !Array.isArray(messages)) {
    return NextResponse.json({ error: 'Invalid request' }, { status: 400 })
  }

  // Lazy cleanup: revoke any prior non-revoked playground keys for this user so
  // that at most one ephemeral playground key is ever active. This avoids the
  // need for post-stream cleanup once the piped SSE body has drained.
  await prisma.apiKey.updateMany({
    where: { userId, name: 'playground', revokedAt: null },
    data: { revokedAt: new Date() },
  })

  // Create a fresh ephemeral key valid for the proxy's HMAC lookup.
  const k = generateApiKey()
  await prisma.apiKey.create({
    data: { userId, name: 'playground', keyPrefix: k.prefix, keyHash: k.hash },
  })

  const upstream = await fetch(`${PROXY_URL}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${k.plaintext}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ model, messages, stream: true, max_tokens: 1024 }),
  })

  if (!upstream.ok || !upstream.body) {
    const text = await upstream.text().catch(() => '')
    return NextResponse.json(
      { error: text || 'Upstream error' },
      { status: upstream.status || 502 },
    )
  }

  // Pipe the SSE stream straight through to the browser.
  return new Response(upstream.body, {
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
    },
  })
}
