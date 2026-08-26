import { ArrowRight, Radio, Search, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import type { EventEnvelope } from '../types'
import { formatDateTime } from '../utils'

function resultLabel(result: string): string {
  const labels: Record<string, string> = { APPLIED: '已应用', DUPLICATE: '重复忽略', REJECTED: '已拒绝', SENT: '已发送' }
  return labels[result] ?? result
}

export function GossipTimeline({ events }: { events: EventEnvelope[] }) {
  const [query, setQuery] = useState('')
  const deliveries = useMemo(() => events
    .filter((event) => event.delivery && (!query || event.event_id.toLocaleLowerCase().includes(query.toLocaleLowerCase())))
    .slice(0, 100), [events, query])

  return (
    <section className="timeline-panel panel" aria-labelledby="timeline-title">
      <div className="panel-heading timeline-heading">
        <div><p className="eyebrow">REAL DELIVERY</p><h2 id="timeline-title">Gossip 传播事件</h2></div>
        <span className="live-indicator"><span aria-hidden="true" />实时</span>
      </div>
      <label className="event-search">
        <span className="sr-only">按 event ID 筛选传播事件</span>
        <Search aria-hidden="true" size={15} />
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="按 event ID 追踪" />
        {query && <button aria-label="清除 event ID 筛选" onClick={() => setQuery('')}><X aria-hidden="true" size={14} /></button>}
      </label>
      {deliveries.length === 0 ? (
        <div className="compact-empty"><Radio aria-hidden="true" size={24} /><p>{query ? '没有匹配的传播事件' : '等待真实传播事件'}</p></div>
      ) : (
        <ol className="event-list" aria-label="近期 Gossip 传播事件">
          {deliveries.map((event) => {
            const delivery = event.delivery!
            return (
              <li key={`${event.stream_boot_id}-${event.seq}-${delivery.attempt_id}`} data-event-id={event.event_id}>
                <div className="event-rail" aria-hidden="true"><span /></div>
                <div className="event-content">
                  <div className="event-title">
                    <code title={event.event_id}>{event.event_id}</code>
                    <span className={`result result-${delivery.result.toLowerCase()}`}>{resultLabel(delivery.result)}</span>
                  </div>
                  <div className="event-route">
                    <span>{delivery.source_node_id}</span><ArrowRight aria-hidden="true" size={13} /><span>{delivery.target_node_id}</span>
                    <small>hop {delivery.hop}</small>
                  </div>
                  <div className="event-time"><time dateTime={event.occurred_at}>{formatDateTime(event.occurred_at)}</time>{delivery.latency_ms !== undefined && <span>{delivery.latency_ms} ms</span>}</div>
                </div>
              </li>
            )
          })}
        </ol>
      )}
    </section>
  )
}
