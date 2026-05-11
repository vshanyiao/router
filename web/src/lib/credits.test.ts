import { describe, it, expect, beforeEach, vi } from 'vitest'

const { mockSetNx, mockFindFirst, mockAppConfigFindUnique, mockTransaction } = vi.hoisted(() => ({
  mockSetNx: vi.fn(),
  mockFindFirst: vi.fn(),
  mockAppConfigFindUnique: vi.fn(),
  mockTransaction: vi.fn(),
}))

vi.mock('./redis', () => ({
  redis: { set: mockSetNx },
}))

vi.mock('./db', () => ({
  prisma: {
    creditTransaction: { findFirst: mockFindFirst },
    appConfig: { findUnique: mockAppConfigFindUnique },
    $transaction: mockTransaction,
  },
}))

import { grantTrialCreditIfEligible } from './credits'

describe('grantTrialCreditIfEligible', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockAppConfigFindUnique.mockResolvedValue({ value: 100 })
  })

  it('grants 100 cents to a new user from a fresh IP', async () => {
    mockFindFirst.mockResolvedValue(null)
    mockSetNx.mockResolvedValue('OK')
    mockTransaction.mockImplementation(async (fn: any) => fn({
      user: { update: vi.fn().mockResolvedValue({ creditsCents: 100n }) },
      creditTransaction: { create: vi.fn().mockResolvedValue({}) },
    }))

    const result = await grantTrialCreditIfEligible('user-1', '1.2.3.4')
    expect(result).toBe(true)
    expect(mockFindFirst).toHaveBeenCalledBefore(mockSetNx as any)
    expect(mockSetNx).toHaveBeenCalledWith('trial:ip:1.2.3.4', 'user-1', 'EX', 86400, 'NX')
  })

  it('returns false (no Redis call) if user already has trial credit', async () => {
    mockFindFirst.mockResolvedValue({ id: 'existing-tx' })

    const result = await grantTrialCreditIfEligible('user-1', '1.2.3.4')
    expect(result).toBe(false)
    expect(mockSetNx).not.toHaveBeenCalled()
  })

  it('returns false if IP already claimed in last 24h', async () => {
    mockFindFirst.mockResolvedValue(null)
    mockSetNx.mockResolvedValue(null)

    const result = await grantTrialCreditIfEligible('user-1', '1.2.3.4')
    expect(result).toBe(false)
    expect(mockTransaction).not.toHaveBeenCalled()
  })
})
