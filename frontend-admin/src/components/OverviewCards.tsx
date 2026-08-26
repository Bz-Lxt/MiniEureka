import { Activity, Boxes, Clock3, Network, Server, SquareX } from 'lucide-react'
import type { DashboardSummary, InstanceStatus } from '../types'

interface OverviewCardsProps {
  summary: DashboardSummary
  activeStatus: InstanceStatus | null
  onStatusSelect: (status: InstanceStatus | null) => void
}

export function OverviewCards({ summary, activeStatus, onStatusSelect }: OverviewCardsProps) {
  const cards = [
    { label: '服务', value: summary.services, icon: Server },
    { label: '总实例', value: summary.instances, icon: Boxes },
    { label: '活跃', value: summary.active, icon: Activity, status: 'ACTIVE' as const },
    { label: '延迟', value: summary.delayed, icon: Clock3, status: 'DELAYED' as const },
    { label: '已摘除', value: summary.evicted, icon: SquareX, status: 'EVICTED' as const },
  ]
  return (
    <section className="overview-grid" aria-label="集群概览">
      {cards.map(({ label, value, icon: Icon, status }) => {
        const content = (
          <>
            <span className="overview-icon"><Icon aria-hidden="true" size={18} /></span>
            <span className="overview-label">{label}</span>
            <strong className="overview-value">{value.toLocaleString('zh-CN')}</strong>
          </>
        )
        return status ? (
          <button
            className="overview-card overview-action"
            aria-pressed={activeStatus === status}
            key={label}
            onClick={() => onStatusSelect(activeStatus === status ? null : status)}
          >
            {content}
          </button>
        ) : <article className="overview-card" key={label}>{content}</article>
      })}
      <article className="overview-card overview-cluster">
        <span className="overview-icon"><Network aria-hidden="true" size={18} /></span>
        <span className="overview-label">注册中心 / Gossip</span>
        <strong className="overview-value">
          {summary.nodes} <small>节点</small>
          <span className="metric-divider" aria-hidden="true">/</span>
          {summary.gossip_rate.toFixed(1)} <small>次/秒</small>
        </strong>
      </article>
    </section>
  )
}
