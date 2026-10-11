import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type LogRefundRun = {
  id: number
  target_date: string
  trigger: string
  log_count: number
  user_count: number
  refunded_quota: number
  failed_count: number
  truncated: boolean
  created_at: number
}

export type LogRefundAdmin = {
  enabled: boolean
  updated_at: number
  /** 后端回显的查询日期（北京时间自然日） */
  date: string
  /** 该日期是否已完整执行过返费（手动或凌晨自动）——已执行则禁止再次手动执行 */
  date_processed: boolean
  /** 该日期最近一次运行记录 */
  date_last_run: LogRefundRun | null
  /** 全局最近一次运行记录 */
  last_run: LogRefundRun | null
}

type Envelope<T> = { success: boolean; message: string; data: T }

export async function getLogRefundAdmin(date: string): Promise<LogRefundAdmin> {
  const res = await api.get<Envelope<LogRefundAdmin>>(
    '/api/abnormal-refund/admin',
    {
      params: { date },
    }
  )
  return requireServerSuccess(res.data).data
}

export async function saveLogRefundEnabled(enabled: boolean): Promise<boolean> {
  const res = await api.put<Envelope<{ enabled: boolean }>>(
    '/api/abnormal-refund/admin',
    { enabled }
  )
  return requireServerSuccess(res.data).data.enabled
}

export async function runLogRefund(date: string): Promise<void> {
  const res = await api.post<Envelope<unknown>>(
    '/api/abnormal-refund/admin/run',
    undefined,
    { params: { date } }
  )
  requireServerSuccess(res.data)
}
