import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useIsAdmin } from '@/hooks/use-admin'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getLogRefundAdmin,
  runLogRefund,
  saveLogRefundEnabled,
  type LogRefundRun,
} from './api'

const QUERY_KEY = ['log-refund', 'admin']

function localDateString(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${date.getFullYear()}-${month}-${day}`
}

function RunSummary(props: { run: LogRefundRun | null }) {
  const { t } = useTranslation()
  if (!props.run) {
    return (
      <p className='text-muted-foreground text-sm'>{t('No refund run yet')}</p>
    )
  }
  const run = props.run
  return (
    <dl className='grid gap-3 text-sm sm:grid-cols-3'>
      <div>
        <dt className='text-muted-foreground'>{t('Target date')}</dt>
        <dd className='font-medium tabular-nums'>{run.target_date}</dd>
      </div>
      <div>
        <dt className='text-muted-foreground'>{t('Trigger')}</dt>
        <dd className='font-medium'>
          {run.trigger === 'manual' ? t('Manual') : t('Auto')}
        </dd>
      </div>
      <div>
        <dt className='text-muted-foreground'>{t('Run time')}</dt>
        <dd className='font-medium tabular-nums'>
          {formatTimestampToDate(run.created_at)}
        </dd>
      </div>
      <div>
        <dt className='text-muted-foreground'>{t('Refunded logs')}</dt>
        <dd className='font-medium tabular-nums'>{run.log_count}</dd>
      </div>
      <div>
        <dt className='text-muted-foreground'>{t('Refunded users')}</dt>
        <dd className='font-medium tabular-nums'>{run.user_count}</dd>
      </div>
      <div>
        <dt className='text-muted-foreground'>{t('Refunded amount')}</dt>
        <dd className='font-medium tabular-nums'>
          {formatQuotaWithCurrency(run.refunded_quota)}
        </dd>
      </div>
      {run.failed_count > 0 && (
        <div className='sm:col-span-3'>
          <dt className='text-destructive'>{t('Failed users')}</dt>
          <dd className='text-destructive font-medium tabular-nums'>
            {run.failed_count}
          </dd>
        </div>
      )}
    </dl>
  )
}

/**
 * 异常日志返费管理面板（仅管理员可见）。
 *
 * 规则：消费日志里「补全 token 为 0 的空返回」，或「流以错误结束且输入/输出 token
 * 不完整」的记录，只要确实产生了扣费（quota > 0），就会在每天北京时间 0 点后按用户
 * 汇总退还，并在用户日志里写一条「今日异常返费 XX」。同一条消费日志只会退还一次。
 *
 * 手动「一键返费」：某一天只要已经完整执行过一次返费（手动或凌晨自动），该日期的
 * 按钮就会被禁用，不能重复执行。
 */
export function LogRefundAdminPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isAdmin = useIsAdmin()
  const [date, setDate] = useState(() => localDateString(new Date()))

  const query = useQuery({
    queryKey: [...QUERY_KEY, date],
    queryFn: () => getLogRefundAdmin(date),
    enabled: isAdmin && date !== '',
  })

  const saveEnabled = useMutation({
    mutationFn: saveLogRefundEnabled,
    onSuccess: () => {
      toast.success(t('Abnormal refund settings saved'))
      void queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => handleServerError(error),
  })

  const runNow = useMutation({
    mutationFn: runLogRefund,
    onSuccess: () => {
      toast.success(t('Refund task submitted, running now'))
      // 任务由系统任务运行器异步执行，稍后刷新即可看到该日期的运行结果与按钮状态。
      window.setTimeout(() => {
        void queryClient.invalidateQueries({ queryKey: QUERY_KEY })
      }, 2500)
      window.setTimeout(() => {
        void queryClient.invalidateQueries({ queryKey: QUERY_KEY })
      }, 6000)
    },
    onError: (error) => handleServerError(error),
  })

  if (!isAdmin) {
    return <ErrorState title={t('Admin access required')} />
  }
  if (query.isPending) {
    return <LoadingState message={t('Loading abnormal refund settings...')} />
  }
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load abnormal refund settings')}
        onRetry={() => void query.refetch()}
      />
    )
  }

  const data = query.data
  const processed = data.date_processed
  const runDisabled = processed || runNow.isPending || date === ''

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Abnormal log refund')}</CardTitle>
        </CardHeader>
        <CardContent className='space-y-4'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Consume logs whose stream status is error, or whose completion tokens are 0 while quota was still charged, are refunded to users automatically every day at midnight (Beijing). Each user gets one summary log entry.'
            )}
          </p>
          <div className='flex items-center gap-3'>
            <Label htmlFor='log-refund-enabled'>
              {t('Enable abnormal log refund')}
            </Label>
            <Switch
              id='log-refund-enabled'
              checked={data.enabled}
              disabled={saveEnabled.isPending}
              onCheckedChange={(checked) => saveEnabled.mutate(checked)}
            />
          </div>
          <div className='flex flex-wrap items-end gap-3'>
            <div className='grid gap-2'>
              <Label htmlFor='log-refund-date'>{t('Target date')}</Label>
              <Input
                className='w-auto'
                id='log-refund-date'
                type='date'
                value={date}
                onChange={(event) => setDate(event.target.value)}
              />
            </div>
            <Button
              disabled={runDisabled}
              onClick={() => runNow.mutate(date)}
              title={
                processed ? t('This date has already been refunded') : undefined
              }
            >
              {processed ? t('Already refunded') : t('Run refund now')}
            </Button>
          </div>
          {processed && (
            <p className='text-destructive text-sm'>
              {t(
                'This date has already been refunded (manually or by the daily job), so it cannot be run again.'
              )}
            </p>
          )}
          {data.date_last_run && (
            <p className='text-muted-foreground text-sm'>
              {t('Last run for this date')}:{' '}
              {data.date_last_run.trigger === 'manual'
                ? t('Manual')
                : t('Auto')}
              {' · '}
              {formatTimestampToDate(data.date_last_run.created_at)}
              {' · '}
              {data.date_last_run.log_count} {t('Refunded logs')}
              {' · '}
              {formatQuotaWithCurrency(data.date_last_run.refunded_quota)}
            </p>
          )}
          <p className='text-muted-foreground text-sm'>
            {t(
              'Manual runs default to today and never double-refund: a consume log is refunded at most once. Already refunded logs are skipped.'
            )}
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('Last refund run')}</CardTitle>
        </CardHeader>
        <CardContent>
          <RunSummary run={data.last_run} />
        </CardContent>
      </Card>
    </div>
  )
}
