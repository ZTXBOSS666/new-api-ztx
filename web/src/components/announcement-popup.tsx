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
import { Megaphone } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useAnnouncements } from '@/features/dashboard/hooks/use-status-data'
import type { AnnouncementItem } from '@/features/dashboard/types'
import { getAnnouncementKey } from '@/lib/announcement-key'
import { getAnnouncementColorClass } from '@/lib/colors'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

// 「不再显示」是永久记忆（localStorage），刷新/重开浏览器都不再弹。
const DISMISSED_KEY = 'announcement-popup-dismissed:v1'
// 「本次会话已展示」是临时记忆（sessionStorage），刷新页面不重复打扰，下次登录再弹。
const SHOWN_KEY = 'announcement-popup-shown:v1'
// 后端已把「登录后弹窗」的公告限制在 5 条（见 validateAnnouncements），
// 这里再兜一层：万一有人绕过管理端直接写配置，也不会把用户炸死。
const MAX_POPUPS_PER_SESSION = 5

function userScopedKey(prefix: string, userId: number): string {
  return `${prefix}:${userId}`
}

function getStorage(kind: 'local' | 'session'): Storage | null {
  if (typeof window === 'undefined') return null
  try {
    return kind === 'local' ? window.localStorage : window.sessionStorage
  } catch {
    return null
  }
}

function readKeySet(storage: Storage | null, key: string): Set<string> {
  if (!storage) return new Set()
  try {
    const raw = storage.getItem(key)
    if (!raw) return new Set()
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((value): value is string => typeof value === 'string'))
  } catch {
    return new Set()
  }
}

function writeKeySet(storage: Storage | null, key: string, values: Set<string>): void {
  if (!storage) return
  try {
    storage.setItem(key, JSON.stringify([...values]))
  } catch {
    // 隐私模式或配额满时写入会抛错：静默降级，弹窗本身不受影响。
  }
}

// 公告的稳定键与通知面板共用（见 lib/announcement-key.ts），避免同一个公告
// 在两处被当成两条不同记录。

/**
 * 登录后公告弹窗：把管理端标记了 popup 的公告按队列依次弹出。
 * 每个弹窗都可以勾选「不再显示」，勾选后对该用户永久生效。
 */
export function AnnouncementPopup() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { items, loading } = useAnnouncements()
  const [queue, setQueue] = useState<AnnouncementItem[]>([])
  const [total, setTotal] = useState(0)
  const [dontShowAgain, setDontShowAgain] = useState(false)

  // 换账号时清空队列，避免上一个用户的面板残留。
  useEffect(() => {
    setQueue([])
    setTotal(0)
    setDontShowAgain(false)
  }, [userId])

  // 由队列状态驱动初始化，不能用「只跑一次」的 ref 守卫：
  // useStatus 命中本地缓存时 loading 可能是 false，而首次访问又没有缓存，
  // 过早打标记会让第一次登录永远弹不出来。队列空了才重新计算，天然幂等。
  useEffect(() => {
    if (!userId || loading || queue.length > 0) return
    // items 为空时直接退出：useStatusData 在公告未加载/未启用时会返回新数组，
    // 不短路的话每次渲染都会去 JSON.parse 两遍 localStorage。
    if (items.length === 0) return

    const dismissed = readKeySet(
      getStorage('local'),
      userScopedKey(DISMISSED_KEY, userId)
    )
    const shown = readKeySet(
      getStorage('session'),
      userScopedKey(SHOWN_KEY, userId)
    )
    const pending = items
      .filter((item) => {
        if (item.popup !== true) return false
        const key = getAnnouncementKey(item)
        return !dismissed.has(key) && !shown.has(key)
      })
      .slice(0, MAX_POPUPS_PER_SESSION)
    if (pending.length === 0) return
    setTotal(pending.length)
    setQueue(pending)
  }, [userId, loading, items, queue.length])

  const current = queue[0]

  const finishCurrent = (persistDismiss: boolean) => {
    if (!userId || !current) return
    const key = getAnnouncementKey(current)

    const shownKey = userScopedKey(SHOWN_KEY, userId)
    const shown = readKeySet(getStorage('session'), shownKey)
    shown.add(key)
    writeKeySet(getStorage('session'), shownKey, shown)

    if (persistDismiss) {
      const dismissedKey = userScopedKey(DISMISSED_KEY, userId)
      const dismissed = readKeySet(getStorage('local'), dismissedKey)
      dismissed.add(key)
      writeKeySet(getStorage('local'), dismissedKey, dismissed)
    }

    setDontShowAgain(false)
    // 只移除「当前这条」：连点两下按钮时第二次回调可能还持有旧的 current 闭包，
    // 直接 slice(1) 会把下一条公告一起吞掉。
    setQueue((prev) =>
      prev[0] && getAnnouncementKey(prev[0]) === key ? prev.slice(1) : prev
    )
  }

  if (!current) return null

  const currentIndex = total - queue.length + 1

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) finishCurrent(false)
      }}
      title={
        <span className='flex items-center gap-2'>
          <Megaphone className='size-4' />
          {t('Announcements')}
        </span>
      }
      description={
        current.publishDate
          ? `${t('Published:')} ${formatDateTimeObject(new Date(current.publishDate))}`
          : undefined
      }
      contentClassName='sm:max-w-lg'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footerClassName='flex-row flex-wrap items-center justify-between gap-3'
      footer={
        <>
          <div className='flex items-center gap-3'>
            {total > 1 && (
              <span className='text-muted-foreground text-xs'>
                {currentIndex}/{total}
              </span>
            )}
            <label className='flex cursor-pointer items-center gap-2 text-sm'>
              <Checkbox
                checked={dontShowAgain}
                onCheckedChange={(checked) => setDontShowAgain(checked === true)}
              />
              {t("Don't show this announcement again")}
            </label>
          </div>
          <Button onClick={() => finishCurrent(dontShowAgain)}>
            {t('Got it')}
          </Button>
        </>
      }
    >
      <ScrollArea className='max-h-[min(58vh,520px)] pr-4'>
        <div className='space-y-4'>
          <div className='flex items-center gap-2'>
            <span
              className={cn(
                'inline-block size-2 shrink-0 rounded-full',
                getAnnouncementColorClass(current.type)
              )}
            />
            <span className='text-muted-foreground text-xs'>
              {t('Announcement')}
            </span>
          </div>
          <RichContent breaks content={current.content} />
          {current.extra && (
            <RichContent
              breaks
              content={current.extra}
              className='text-muted-foreground'
            />
          )}
        </div>
      </ScrollArea>
    </Dialog>
  )
}
