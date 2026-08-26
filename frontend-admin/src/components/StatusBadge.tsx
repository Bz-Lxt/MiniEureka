import { CircleCheck, SquareX, TriangleAlert } from 'lucide-react'
import type { InstanceStatus, MemberStatus } from '../types'

const labels: Record<InstanceStatus | MemberStatus, string> = {
  ACTIVE: '活跃',
  DELAYED: '延迟',
  EVICTED: '已摘除',
  ALIVE: '在线',
  SUSPECT: '可疑',
  DEAD: '离线',
}

function tone(status: InstanceStatus | MemberStatus): 'active' | 'delayed' | 'evicted' {
  if (status === 'ACTIVE' || status === 'ALIVE') return 'active'
  if (status === 'DELAYED' || status === 'SUSPECT') return 'delayed'
  return 'evicted'
}

export function StatusBadge({ status, compact = false }: {
  status: InstanceStatus | MemberStatus
  compact?: boolean
}) {
  const kind = tone(status)
  const Icon = kind === 'active' ? CircleCheck : kind === 'delayed' ? TriangleAlert : SquareX
  return (
    <span className={`status-badge status-${kind}`} data-status={status}>
      <Icon aria-hidden="true" size={compact ? 13 : 15} strokeWidth={2.4} />
      <span>{labels[status]}</span>
    </span>
  )
}
