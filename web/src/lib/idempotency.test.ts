import { describe, it, expect, beforeEach, vi } from 'vitest'

const { mockSet } = vi.hoisted(() => ({ mockSet: vi.fn() }))

vi.mock('./redis', () => ({ redis: { set: mockSet } }))

import { claimWebhookEvent } from './idempotency'

describe('claimWebhookEvent', () => {
  beforeEach(() => vi.clearAllMocks())

  it('returns true and sets the key on first call', async () => {
    mockSet.mockResolvedValueOnce('OK')
    const result = await claimWebhookEvent('evt_1')
    expect(result).toBe(true)
    expect(mockSet).toHaveBeenCalledWith('idem:evt_1', '1', 'EX', 86400, 'NX')
  })

  it('returns false when the key already exists (NX guard)', async () => {
    mockSet.mockResolvedValueOnce(null)
    expect(await claimWebhookEvent('evt_1')).toBe(false)
  })

  it('returns false on any non-OK reply', async () => {
    mockSet.mockResolvedValueOnce('NOT_OK')
    expect(await claimWebhookEvent('evt_unknown')).toBe(false)
  })

  it('uses the same key prefix for every call', async () => {
    mockSet.mockResolvedValue('OK')
    await claimWebhookEvent('a')
    await claimWebhookEvent('b')
    const keys = mockSet.mock.calls.map((c) => c[0])
    expect(keys).toEqual(['idem:a', 'idem:b'])
  })
})
