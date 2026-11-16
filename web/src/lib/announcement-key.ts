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
/**
 * 公告的稳定标识。通知面板的「已读」和登录弹窗的「不再显示」共用这一套，
 * 否则同一个公告在两处会得到不同的键，用户读了通知还会被弹窗再打扰一次。
 */

export type AnnouncementKeyInput = {
  id?: unknown
  publishDate?: unknown
  content?: unknown
  extra?: unknown
  type?: unknown
  title?: unknown
  link?: unknown
}

function hashString(input: string): string {
  let hash = 0
  if (!input) return '0'

  for (let i = 0; i < input.length; i += 1) {
    const chr = input.charCodeAt(i)
    hash = (hash << 5) - hash + chr
    hash |= 0
  }

  return hash.toString(36)
}

function contentFingerprint(item: AnnouncementKeyInput): string {
  return JSON.stringify({
    publishDate: (item.publishDate as string) || '',
    content: ((item.content as string) || '').trim(),
    extra: ((item.extra as string) || '').trim(),
    type: (item.type as string) || '',
    title: ((item.title as string) || '').trim(),
    link: ((item.link as string) || '').trim(),
  })
}

/**
 * 生成公告键：`id:<id>:hash:<内容指纹>`，没有 id 时退化为纯内容指纹。
 *
 * id 必须和内容指纹一起用：管理端的 id 是 `当前最大 id + 1`，删掉最大那条
 * 再新增会复用同一个 id。只认 id 的话，用户对旧公告的「已读 / 不再显示」
 * 会被一条全新的公告继承，直接吞掉一次提醒。
 */
export function getAnnouncementKey(
  item: AnnouncementKeyInput | null | undefined
): string {
  if (!item) return ''
  const contentKey = `hash:${hashString(contentFingerprint(item))}`
  if (item.id !== undefined && item.id !== null) {
    return `id:${item.id}:${contentKey}`
  }
  return contentKey
}

/**
 * 旧版键（只认 id），仅用于迁移期兼容。
 * 升级前用户标记的「已读」用的都是这种键；新键带上内容指纹后老记录会失配，
 * 不兼容的话整个站点升级后通知红点会全部重新亮起。
 */
export function getLegacyAnnouncementKey(
  item: AnnouncementKeyInput | null | undefined
): string {
  if (!item || item.id === undefined || item.id === null) return ''
  return `id:${item.id}`
}
