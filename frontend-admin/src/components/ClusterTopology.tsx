import { useEffect, useMemo, useState } from 'react'
import { Network } from 'lucide-react'
import type { ClusterNode, EventEnvelope, TopologyEdge } from '../types'
import { formatRelative } from '../utils'
import { StatusBadge } from './StatusBadge'

interface Point { x: number; y: number }

function nodePositions(nodes: ClusterNode[]): Record<string, Point> {
  const sorted = [...nodes].sort((a, b) => a.node_id.localeCompare(b.node_id))
  const positions: Record<string, Point> = {}
  const count = Math.max(sorted.length, 1)
  sorted.forEach((node, index) => {
    const angle = -Math.PI / 2 + (index * Math.PI * 2) / count
    positions[node.node_id] = {
      x: 320 + Math.cos(angle) * (count === 1 ? 0 : 205),
      y: 170 + Math.sin(angle) * (count === 1 ? 0 : 105),
    }
  })
  return positions
}

function curve(source: Point, target: Point): string {
  const mx = (source.x + target.x) / 2
  const my = (source.y + target.y) / 2 - 18
  return `M ${source.x} ${source.y} Q ${mx} ${my} ${target.x} ${target.y}`
}

export function ClusterTopology({ nodes, edges, events, onNodeFilter }: {
  nodes: ClusterNode[]
  edges: TopologyEdge[]
  events: EventEnvelope[]
  onNodeFilter: (nodeId: string) => void
}) {
  const [visible, setVisible] = useState(document.visibilityState === 'visible')
  useEffect(() => {
    const update = () => setVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', update)
    return () => document.removeEventListener('visibilitychange', update)
  }, [])
  const positions = useMemo(() => nodePositions(nodes), [nodes])
  const deliveries = useMemo(() => events.filter((event) => {
    const delivery = event.delivery
    return delivery && positions[delivery.source_node_id] && positions[delivery.target_node_id]
  }).slice(0, 100), [events, positions])

  return (
    <section className="topology-panel panel" aria-labelledby="topology-title">
      <div className="panel-heading">
        <div><p className="eyebrow">CLUSTER MESH</p><h2 id="topology-title">注册中心拓扑</h2></div>
        <span className="panel-count">{nodes.length} 节点</span>
      </div>
      {nodes.length === 0 ? (
        <div className="compact-empty"><Network aria-hidden="true" size={25} /><p>等待成员发现</p></div>
      ) : (
        <>
          <svg className="topology-svg" viewBox="0 0 640 340" role="img" aria-labelledby="topology-svg-title topology-svg-desc">
            <title id="topology-svg-title">注册中心节点关系图</title>
            <desc id="topology-svg-desc">展示真实 peer 连接和最近 Gossip 传播路径，详细状态见图下节点列表。</desc>
            <defs>
              <marker id="edge-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5" markerHeight="5" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" className="edge-arrow" />
              </marker>
            </defs>
            {edges.map((edge) => {
              const source = positions[edge.source_node_id]
              const target = positions[edge.target_node_id]
              if (!source || !target) return null
              return (
                <path
                  key={`${edge.source_node_id}-${edge.target_node_id}`}
                  d={curve(source, target)}
                  className={`topology-edge edge-${edge.state.toLowerCase()}`}
                  markerEnd="url(#edge-arrow)"
                />
              )
            })}
            {visible && deliveries.map((event) => {
              const delivery = event.delivery!
              const source = positions[delivery.source_node_id]
              const target = positions[delivery.target_node_id]
              return (
                <path
                  key={`${event.stream_boot_id}-${event.seq}-${delivery.attempt_id}`}
                  d={curve(source, target)}
                  className={`flight-path flight-${delivery.result.toLowerCase()}`}
                  data-event-id={event.event_id}
                  data-hop={delivery.hop}
                />
              )
            })}
            {nodes.map((node) => {
              const point = positions[node.node_id]
              return (
                <g className={`topology-node node-${node.status.toLowerCase()}`} transform={`translate(${point.x} ${point.y})`} data-node-id={node.node_id} key={node.node_id}>
                  <circle r="39" className="node-ring" />
                  <circle r="31" className="node-core" />
                  <text className="node-label" textAnchor="middle" y="4">{node.node_id}</text>
                  <text className="node-state" textAnchor="middle" y="58">{node.status}</text>
                </g>
              )
            })}
          </svg>
          <ul className="node-list" aria-label="注册中心节点状态列表">
            {nodes.map((node) => (
              <li key={node.node_id}>
                <button onClick={() => onNodeFilter(node.node_id)} aria-label={`筛选来源节点 ${node.node_id}`}>
                  <span><strong>{node.node_id}</strong><small className="mono">{node.gossip_address}</small></span>
                  <span><StatusBadge status={node.status} compact /><small>{formatRelative(node.last_seen_at)}</small></span>
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}
