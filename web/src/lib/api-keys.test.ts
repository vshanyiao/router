import { describe, it, expect } from 'vitest'
import { generateApiKey, hashApiKey, keyPrefix } from './api-keys'

describe('api-keys', () => {
  it('generates a key with sk-or- prefix and 32 random chars', () => {
    const { plaintext } = generateApiKey()
    expect(plaintext).toMatch(/^sk-or-[A-Za-z0-9]{32}$/)
  })

  it('generates unique keys on consecutive calls', () => {
    const a = generateApiKey().plaintext
    const b = generateApiKey().plaintext
    expect(a).not.toBe(b)
  })

  it('returns prefix matching first 12 chars', () => {
    const { plaintext, prefix } = generateApiKey()
    expect(prefix).toBe(plaintext.slice(0, 12))
    expect(prefix.length).toBe(12)
  })

  it('hashes deterministically with HMAC-SHA256', () => {
    const hash1 = hashApiKey('sk-or-abc')
    const hash2 = hashApiKey('sk-or-abc')
    expect(hash1).toBe(hash2)
    expect(hash1).toMatch(/^[a-f0-9]{64}$/)
  })

  it('produces different hashes for different keys', () => {
    expect(hashApiKey('sk-or-a')).not.toBe(hashApiKey('sk-or-b'))
  })

  it('keyPrefix returns first 12 chars', () => {
    expect(keyPrefix('sk-or-abcdef1234567890')).toBe('sk-or-abcdef')
  })
})
