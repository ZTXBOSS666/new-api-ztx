import { describe, expect, it } from 'vitest'

import { lotteryConfigSchema } from '../lib/schema'

describe('lotteryConfigSchema', () => {
  it('rejects enabled lottery without positive limits and reward', () => {
    const result = lotteryConfigSchema.safeParse({
      enabled: true,
      daily_participant_limit: 0,
      daily_winner_limit: 0,
      reward_quota: 0,
      entry_fee: 0,
    })
    expect(result.success).toBe(false)
  })

  it('accepts the explicitly disabled empty initial configuration', () => {
    const result = lotteryConfigSchema.safeParse({
      enabled: false,
      daily_participant_limit: 0,
      daily_winner_limit: 0,
      reward_quota: 0,
      entry_fee: 0,
    })
    expect(result.success).toBe(true)
  })

  it('rejects more winners than participants', () => {
    const result = lotteryConfigSchema.safeParse({
      enabled: true,
      daily_participant_limit: 1,
      daily_winner_limit: 2,
      reward_quota: 1,
      entry_fee: 1,
    })
    expect(result.success).toBe(false)
  })

  it('rejects a negative entry fee and accepts a free round', () => {
    expect(
      lotteryConfigSchema.safeParse({
        enabled: true,
        daily_participant_limit: 5,
        daily_winner_limit: 1,
        reward_quota: 10,
        entry_fee: -1,
      }).success
    ).toBe(false)
    expect(
      lotteryConfigSchema.safeParse({
        enabled: true,
        daily_participant_limit: 5,
        daily_winner_limit: 1,
        reward_quota: 10,
        entry_fee: 0,
      }).success
    ).toBe(true)
  })
})
