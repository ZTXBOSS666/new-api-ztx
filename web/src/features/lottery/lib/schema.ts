import { z } from 'zod'

export const lotteryConfigSchema = z
  .object({
    enabled: z.boolean(),
    daily_participant_limit: z.number().int().min(0).max(1000000),
    daily_winner_limit: z.number().int().min(0).max(100000),
    reward_balance: z.number().finite().min(0).max(Number.MAX_SAFE_INTEGER),
    entry_fee_balance: z.number().finite().min(0).max(Number.MAX_SAFE_INTEGER),
  })
  .superRefine((value, context) => {
    if (
      !value.enabled &&
      value.daily_participant_limit === 0 &&
      value.daily_winner_limit === 0 &&
      value.reward_balance === 0 &&
      value.entry_fee_balance === 0
    ) {
      return
    }
    for (const field of [
      'daily_participant_limit',
      'daily_winner_limit',
      'reward_balance',
    ] as const) {
      if (value[field] <= 0) {
        context.addIssue({
          code: 'custom',
          path: [field],
          message: 'Enter positive whole numbers before enabling the lottery',
        })
      }
    }
    if (value.daily_winner_limit > value.daily_participant_limit) {
      context.addIssue({
        code: 'custom',
        path: ['daily_winner_limit'],
        message: 'Winner limit cannot exceed participant limit',
      })
    }
  })
