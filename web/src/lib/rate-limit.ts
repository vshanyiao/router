import { redis } from './redis'

/**
 * Fixed-window rate limit for web endpoints (signup, top-up session creation).
 * Returns true if the request is allowed. Fails open on Redis errors — an abuse
 * control should not take down signups if Redis blips.
 *
 * key example: `rl:signup:ip:1.2.3.4`, limit 5, windowSec 3600 → 5 signups/IP/hour.
 */
export async function rateLimit(key: string, limit: number, windowSec: number): Promise<boolean> {
  try {
    const n = await redis.incr(key)
    if (n === 1) await redis.expire(key, windowSec)
    return n <= limit
  } catch {
    return true // fail open
  }
}

/** Extract the client IP from a Next.js request's forwarded headers. */
export function clientIp(req: Request): string {
  const fwd = req.headers.get('x-forwarded-for')
  if (fwd) return fwd.split(',')[0].trim()
  return req.headers.get('x-real-ip') || '0.0.0.0'
}
