import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type LotteryConfig = {
  enabled: boolean
  daily_participant_limit: number
  daily_winner_limit: number
  reward_quota: number
  entry_fee: number
}
export type LotteryPerson = {
  id: number
  username: string
  draw_date: string
  joined_at: number
  reward_quota: number
  is_self: boolean
}
export type LotteryLists = {
  participants: LotteryPerson[]
  winners: LotteryPerson[]
  participants_total: number
  winners_total: number
}
export type LotteryStatus = LotteryLists & {
  enabled: boolean
  draw_date: string
  participant_limit: number
  winner_limit: number
  reward_quota: number
  entry_fee: number
  participant_count: number
  joined: boolean
  historical_winner: boolean
  settled: boolean
}
export type LotteryAdmin = LotteryLists & {
  config: LotteryConfig
  round: null | {
    draw_date: string
    participant_limit: number
    winner_limit: number
    reward_quota: number
    entry_fee: number
    status: string
    settled_at: number
  }
}
type Envelope<T> = { success: boolean; message: string; data: T }
export async function getLottery(
  participantPage: number,
  winnerPage: number
): Promise<LotteryStatus> {
  const res = await api.get<Envelope<LotteryStatus>>('/api/user/lottery', {
    params: { participant_page: participantPage, winner_page: winnerPage },
  })
  return requireServerSuccess(res.data).data
}
export async function joinLottery(): Promise<void> {
  requireServerSuccess((await api.post('/api/user/lottery/join')).data)
}
export async function getLotteryAdmin(
  date: string,
  participantPage: number,
  winnerPage: number
): Promise<LotteryAdmin> {
  const res = await api.get<Envelope<LotteryAdmin>>('/api/lottery/admin', {
    params: {
      date,
      participant_page: participantPage,
      winner_page: winnerPage,
    },
  })
  return requireServerSuccess(res.data).data
}
export async function saveLottery(
  config: LotteryConfig
): Promise<LotteryConfig> {
  return requireServerSuccess(
    (await api.put<Envelope<LotteryConfig>>('/api/lottery/admin', config)).data
  ).data
}
