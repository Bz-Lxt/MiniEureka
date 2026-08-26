import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowUpRight, Ban, ServerOff } from 'lucide-react'
import type { Filters, InstanceRecord } from '../types'
import { formatRelative, humanReason, instanceKey } from '../utils'
import { StatusBadge } from './StatusBadge'

const MIN_TILE_WIDTH = 176
const TILE_GAP = 8
const TILE_ROW_HEIGHT = 126
const HEADER_ROW_HEIGHT = 38
const OVERSCAN = 3

type WallRow =
  | { type: 'header'; service: string; count: number; height: number }
  | { type: 'tiles'; service: string; items: InstanceRecord[]; height: number }

function useElementSize(ref: React.RefObject<HTMLElement | null>) {
  const [size, setSize] = useState({ width: 1000, height: 560 })
  useEffect(() => {
    const element = ref.current
    if (!element || typeof ResizeObserver === 'undefined') return
    const update = () => setSize({
      width: element.clientWidth || 1000,
      height: element.clientHeight || 560,
    })
    update()
    const observer = new ResizeObserver(update)
    observer.observe(element)
    return () => observer.disconnect()
  }, [ref])
  return size
}

function firstVisible(offsets: number[], scrollTop: number): number {
  let low = 0
  let high = offsets.length - 1
  while (low < high) {
    const middle = Math.floor((low + high + 1) / 2)
    if (offsets[middle] <= scrollTop) low = middle
    else high = middle - 1
  }
  return low
}

interface InstanceWallProps {
  instances: InstanceRecord[]
  filters: Filters
  canSimulateOffline: boolean
  onSelect: (instance: InstanceRecord) => void
  onRequestOffline: (instance: InstanceRecord) => void
  onFilteredCount: (count: number) => void
  onClearFilters: () => void
}

export function InstanceWall({
  instances,
  filters,
  canSimulateOffline,
  onSelect,
  onRequestOffline,
  onFilteredCount,
  onClearFilters,
}: InstanceWallProps) {
  const viewportRef = useRef<HTMLDivElement>(null)
  const { width, height } = useElementSize(viewportRef)
  const [scrollTop, setScrollTop] = useState(0)
  const deferredQuery = useDeferredValue(filters.query.trim().toLocaleLowerCase('zh-CN'))
  const columns = Math.max(1, Math.floor((width + TILE_GAP) / (MIN_TILE_WIDTH + TILE_GAP)))

  const filtered = useMemo(() => instances.filter((instance) => {
    if (filters.service && instance.service !== filters.service) return false
    if (filters.node && instance.origin_node_id !== filters.node) return false
    if (!filters.statuses.has(instance.status)) return false
    if (!deferredQuery) return true
    const haystack = `${instance.service} ${instance.instance_id} ${instance.host}:${instance.port} ${instance.origin_node_id}`.toLocaleLowerCase('zh-CN')
    return haystack.includes(deferredQuery)
  }), [deferredQuery, filters.node, filters.service, filters.statuses, instances])

  const rows = useMemo(() => {
    const groups = new Map<string, InstanceRecord[]>()
    for (const instance of filtered) {
      const current = groups.get(instance.service) ?? []
      current.push(instance)
      groups.set(instance.service, current)
    }
    const nextRows: WallRow[] = []
    for (const service of [...groups.keys()].sort((a, b) => a.localeCompare(b))) {
      const items = groups.get(service)!.sort((a, b) => a.instance_id.localeCompare(b.instance_id))
      nextRows.push({ type: 'header', service, count: items.length, height: HEADER_ROW_HEIGHT })
      for (let index = 0; index < items.length; index += columns) {
        nextRows.push({ type: 'tiles', service, items: items.slice(index, index + columns), height: TILE_ROW_HEIGHT })
      }
    }
    return nextRows
  }, [columns, filtered])

  const offsets = useMemo(() => {
    const values: number[] = []
    let offset = 0
    for (const row of rows) {
      values.push(offset)
      offset += row.height
    }
    return values
  }, [rows])
  const totalHeight = rows.reduce((sum, row) => sum + row.height, 0)
  const start = Math.max(0, firstVisible(offsets, scrollTop) - OVERSCAN)
  let end = start
  while (end < rows.length && (offsets[end] ?? 0) < scrollTop + height + OVERSCAN * TILE_ROW_HEIGHT) end += 1

  useEffect(() => onFilteredCount(filtered.length), [filtered.length, onFilteredCount])
  useEffect(() => {
    setScrollTop(0)
    viewportRef.current?.scrollTo({ top: 0 })
  }, [columns, deferredQuery, filters.node, filters.service, filters.statuses])

  return (
    <section className="instance-panel panel" aria-labelledby="instance-wall-title">
      <div className="panel-heading">
        <div>
          <p className="eyebrow">SERVICE REGISTRY</p>
          <h2 id="instance-wall-title" tabIndex={-1}>实例状态全景墙</h2>
        </div>
        <span className="panel-count">{filtered.length.toLocaleString('zh-CN')} / {instances.length.toLocaleString('zh-CN')}</span>
      </div>
      {filtered.length === 0 ? (
        <div className="empty-state">
          {instances.length === 0 ? <ServerOff aria-hidden="true" size={30} /> : <Ban aria-hidden="true" size={30} />}
          <h3>{instances.length === 0 ? '尚无注册实例' : '没有符合条件的实例'}</h3>
          <p>{instances.length === 0 ? '实例完成注册后会实时出现在这里。' : '调整搜索词或筛选条件后再试。'}</p>
          {instances.length > 0 && <button className="secondary-button" onClick={onClearFilters}>清除筛选</button>}
        </div>
      ) : (
        <div
          className="virtual-viewport"
          data-testid="instance-viewport"
          ref={viewportRef}
          onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
          aria-label="实例虚拟化列表"
        >
          <div className="virtual-canvas" style={{ height: totalHeight }}>
            {rows.slice(start, end).map((row, visibleIndex) => {
              const index = start + visibleIndex
              const top = offsets[index]
              if (row.type === 'header') {
                return (
                  <div className="service-row" style={{ transform: `translateY(${top}px)` }} key={`service-${row.service}`}>
                    <strong>{row.service}</strong><span>{row.count.toLocaleString('zh-CN')} 个实例</span>
                  </div>
                )
              }
              return (
                <div
                  className="instance-row"
                  style={{ transform: `translateY(${top}px)`, gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}
                  key={`${row.service}-${index}`}
                >
                  {row.items.map((instance) => (
                    <article className={`instance-tile tile-${instance.status.toLowerCase()}`} key={instanceKey(instance.service, instance.instance_id)}>
                      <button className="instance-primary" onClick={() => onSelect(instance)}>
                        <span className="tile-topline">
                          <StatusBadge status={instance.status} compact />
                          <ArrowUpRight aria-hidden="true" size={14} />
                        </span>
                        <strong title={instance.instance_id}>{instance.instance_id}</strong>
                        <span className="mono tile-address">{instance.host}:{instance.port}</span>
                        <span className="tile-meta">
                          <span>{instance.origin_node_id}</span>
                          <span>{instance.status === 'EVICTED' ? humanReason(instance.status_reason) : formatRelative(instance.last_heartbeat_at)}</span>
                        </span>
                      </button>
                      {canSimulateOffline && instance.demo && instance.status !== 'EVICTED' && (
                        <button
                          className="tile-offline"
                          aria-label={`模拟下线 ${instance.service} ${instance.instance_id}`}
                          onClick={() => onRequestOffline(instance)}
                        ><ServerOff aria-hidden="true" size={14} />模拟下线</button>
                      )}
                    </article>
                  ))}
                </div>
              )
            })}
          </div>
        </div>
      )}
    </section>
  )
}
