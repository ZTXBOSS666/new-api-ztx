/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getMemberQuotaSummary } from '../api'

type SummaryStatProps = {
  label: string
  value: string
  tone?: 'default' | 'active' | 'inactive'
  title?: string
}

function SummaryStat({ label, value, tone = 'default', title }: SummaryStatProps) {
  const toneClass =
    tone === 'active'
      ? 'text-emerald-600 dark:text-emerald-400'
      : tone === 'inactive'
        ? 'text-muted-foreground'
        : ''
  return (
    <div className='flex items-baseline gap-1.5 whitespace-nowrap' title={title}>
      <span className='text-muted-foreground text-xs'>{label}</span>
      <span className={`text-sm font-medium tabular-nums ${toneClass}`}>
        {value}
      </span>
    </div>
  )
}

/**
 * 用户列表页顶部的会员汇总：总数、总余额、正常使用用户、无效客户。
 * 口径由后端统一给出（只统计普通会员），前端不做二次计算，避免两处数字打架。
 */
export function MemberQuotaSummary() {
  const { t } = useTranslation()
  const { data, isLoading, isError } = useQuery({
    queryKey: ['member-quota-summary'],
    queryFn: async () => requireServerSuccess(await getMemberQuotaSummary()),
    staleTime: 60_000,
  })

  if (isLoading) {
    return <Skeleton className='h-9 w-full max-w-xl' />
  }
  if (isError || !data) {
    return null
  }

  return (
    <div className='bg-muted/40 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-md border px-3 py-1.5'>
      <SummaryStat label={t('Members')} value={String(data.member_count)} />
      <SummaryStat
        label={t('Total balance')}
        value={formatQuotaWithCurrency(data.member_quota)}
      />
      <SummaryStat
        label={t('Active')}
        value={String(data.active_count)}
        tone='active'
        title={t('Members with a positive balance')}
      />
      <SummaryStat
        label={t('Inactive')}
        value={String(data.inactive_count)}
        tone='inactive'
        title={t('Members whose balance is zero or negative')}
      />
    </div>
  )
}