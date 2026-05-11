import { randomBytes, createHmac } from 'node:crypto'
import { env } from './env'

const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'

export function generateApiKey(): { plaintext: string; prefix: string; hash: string } {
  const bytes = randomBytes(32)
  let body = ''
  for (let i = 0; i < 32; i++) {
    body += ALPHABET[bytes[i] % ALPHABET.length]
  }
  const plaintext = `sk-or-${body}`
  return {
    plaintext,
    prefix: keyPrefix(plaintext),
    hash: hashApiKey(plaintext),
  }
}

export function hashApiKey(plaintext: string): string {
  return createHmac('sha256', env.HMAC_SERVER_SECRET).update(plaintext).digest('hex')
}

export function keyPrefix(plaintext: string): string {
  return plaintext.slice(0, 12)
}
