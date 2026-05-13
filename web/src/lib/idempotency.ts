import { redis } from './redis'

/**
 * Atomically claim a webhook event for processing. Returns true if this is
 * the first time we've seen the event id (within 24h); subsequent calls for
 * the same id return false so callers can ack-and-skip.
 *
 * This is the first of three defense layers against double-credit (per spec
 * §7.3). The other two are the unique constraint on
 * payment_intents.stripe_payment_intent_id and the conditional UPDATE WHERE
 * status='pending' inside the webhook handler's transaction.
 */
export async function claimWebhookEvent(eventId: string): Promise<boolean> {
  const ok = await redis.set(`idem:${eventId}`, '1', 'EX', 86400, 'NX')
  return ok === 'OK'
}
