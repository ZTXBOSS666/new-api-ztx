import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { DataTablePagination, StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

import type { LotteryPerson } from './api'

export function LotteryPeople(props: {
  title: string
  rows: LotteryPerson[]
  total: number
  page: number
  onPage: (page: number) => void
  winners?: boolean
}) {
  const { t } = useTranslation()
  const table = useReactTable({
    data: props.rows,
    columns: [],
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: props.total,
    state: { pagination: { pageIndex: props.page - 1, pageSize: 20 } },
    onPaginationChange: (updater) => {
      const current = { pageIndex: props.page - 1, pageSize: 20 }
      props.onPage(
        (typeof updater === 'function' ? updater(current) : updater).pageIndex +
          1
      )
    },
  })
  const columns = [
    {
      id: 'username',
      header: t('Username'),
      cell: (row: LotteryPerson) => (
        <span className='break-all'>
          {row.username}
          {row.is_self ? ` (${t('You')})` : ''}
        </span>
      ),
    },
    {
      id: 'date',
      header: t('Lottery date (Beijing)'),
      cell: (row: LotteryPerson) => row.draw_date,
    },
  ]
  if (props.winners) {
    columns.push({
      id: 'reward',
      header: t('Reward (balance)'),
      cell: (row: LotteryPerson) => `$${row.reward_balance.toFixed(2)}`,
    })
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        <StaticDataTable
          data={props.rows}
          columns={columns}
          getRowKey={(row) => row.id}
          emptyContent={
            <EmptyState title={t('No lottery records')} className='min-h-24' />
          }
        />
        <DataTablePagination table={table} compact />
      </CardContent>
    </Card>
  )
}

export function LotteryRules() {
  const { t } = useTranslation()
  return (
    <div className='text-muted-foreground space-y-2 text-sm'>
      <p>
        {t(
          'Each entry costs the configured balance amount, deducted from your balance on the spot. Each user may join once per Beijing day. Winners can never join again; non-winners must register again the next day.'
        )}
      </p>
      <p>
        {t(
          'The previous day is drawn automatically at 00:00 Beijing time (UTC+8). Rewards are fixed balance amounts per winner, not quota points. Fewer eligible entrants means fewer winners; empty rounds have no winners.'
        )}
      </p>
      <p>
        {t(
          'The first entry locks that round’s limits, balance entry fee and balance reward. Configuration changes apply only to rounds not yet started. Disabling entries does not cancel promised rewards.'
        )}
      </p>
    </div>
  )
}
