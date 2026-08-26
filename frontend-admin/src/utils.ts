import type { HLCVersion } from './types'

export function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date)
}

export function formatRelative(value?: string | null, now = Date.now()): string {
  if (!value) return '暂无记录'
  const elapsed = Math.max(0, now - new Date(value).getTime())
  if (!Number.isFinite(elapsed)) return value
  if (elapsed < 1_000) return '刚刚'
  if (elapsed < 60_000) return `${Math.floor(elapsed / 1_000)} 秒前`
  if (elapsed < 3_600_000) return `${Math.floor(elapsed / 60_000)} 分钟前`
  return `${Math.floor(elapsed / 3_600_000)} 小时前`
}

export function formatVersion(version?: HLCVersion): string {
  if (!version) return '—'
  return `${version.physical_ms}-${version.logical}-${version.origin_node_id}`
}

export function instanceKey(service: string, instanceId: string): string {
  return `${service}/${instanceId}`
}

export function humanReason(reason: string): string {
  const labels: Record<string, string> = {
    REGISTERED: '已注册',
    HEARTBEAT_OK: '心跳正常',
    HEARTBEAT_DELAYED: '心跳延迟',
    TTL_EXPIRED: '租约已过期',
    DEREGISTERED: '实例已注销',
    DEMO_OFFLINE: '演示下线',
  }
  return labels[reason] ?? reason
}
