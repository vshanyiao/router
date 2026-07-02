import Stripe from 'stripe'
import { env } from './env'

// Lazy: only instantiate when STRIPE_SECRET_KEY is set. Routes that require
// Stripe should check `stripe !== null` and return 503 if missing — this lets
// dev environments boot without Stripe configured.
//
// apiVersion intentionally omitted: lets the SDK use the version pinned by the
// account dashboard. Set explicitly here only when reproducing across SDK upgrades.
export const stripe: Stripe | null = env.STRIPE_SECRET_KEY
  ? new Stripe(env.STRIPE_SECRET_KEY)
  : null
