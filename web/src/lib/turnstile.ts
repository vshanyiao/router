import { env } from './env'

/**
 * Verify a Cloudflare Turnstile token server-side. If TURNSTILE_SECRET_KEY is
 * unset (dev), verification is skipped and returns true — so local signups
 * work without configuring Turnstile. In production the key must be set.
 */
export async function verifyTurnstile(token: string | undefined, ip?: string): Promise<boolean> {
  if (!env.TURNSTILE_SECRET_KEY) return true // not configured — skip (dev)
  if (!token) return false

  const form = new URLSearchParams()
  form.append('secret', env.TURNSTILE_SECRET_KEY)
  form.append('response', token)
  if (ip) form.append('remoteip', ip)

  try {
    const res = await fetch('https://challenges.cloudflare.com/turnstile/v0/siteverify', {
      method: 'POST',
      body: form,
    })
    const data = (await res.json()) as { success?: boolean }
    return data.success === true
  } catch {
    return false
  }
}
