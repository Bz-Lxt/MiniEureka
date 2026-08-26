import { Filter, RotateCcw, Search, X } from 'lucide-react'
import type { ClusterNode, Filters, InstanceStatus } from '../types'
import { StatusBadge } from './StatusBadge'

const statuses: InstanceStatus[] = ['ACTIVE', 'DELAYED', 'EVICTED']

interface FilterBarProps {
  filters: Filters
  services: string[]
  nodes: ClusterNode[]
  resultCount: number
  onChange: (filters: Filters) => void
}

export function FilterBar({ filters, services, nodes, resultCount, onChange }: FilterBarProps) {
  const toggleStatus = (status: InstanceStatus) => {
    const next = new Set(filters.statuses)
    if (next.has(status)) next.delete(status)
    else next.add(status)
    onChange({ ...filters, statuses: next })
  }
  const clear = () => onChange({ query: '', service: '', node: '', statuses: new Set(statuses) })
  const activeCount = Number(Boolean(filters.query)) + Number(Boolean(filters.service)) +
    Number(Boolean(filters.node)) + Number(filters.statuses.size !== statuses.length)

  return (
    <section className="filter-shell" aria-labelledby="filter-title">
      <div className="filter-heading">
        <div>
          <h2 id="filter-title"><Filter aria-hidden="true" size={17} />实例筛选</h2>
          <p>{resultCount.toLocaleString('zh-CN')} 个匹配实例</p>
        </div>
        {activeCount > 0 && (
          <button className="text-button" onClick={clear}>
            <RotateCcw aria-hidden="true" size={14} />清除筛选 ({activeCount})
          </button>
        )}
      </div>
      <div className="filter-controls">
        <label className="search-control">
          <span className="sr-only">搜索服务或实例 ID</span>
          <Search aria-hidden="true" size={17} />
          <input
            value={filters.query}
            onChange={(event) => onChange({ ...filters, query: event.target.value })}
            placeholder="搜索服务、实例 ID 或地址"
            type="search"
          />
          {filters.query && (
            <button
              className="input-clear"
              aria-label="清除搜索"
              onClick={() => onChange({ ...filters, query: '' })}
              type="button"
            ><X aria-hidden="true" size={15} /></button>
          )}
        </label>
        <label className="select-control">
          <span>服务</span>
          <select value={filters.service} onChange={(event) => onChange({ ...filters, service: event.target.value })}>
            <option value="">全部服务</option>
            {services.map((service) => <option key={service} value={service}>{service}</option>)}
          </select>
        </label>
        <label className="select-control">
          <span>来源节点</span>
          <select value={filters.node} onChange={(event) => onChange({ ...filters, node: event.target.value })}>
            <option value="">全部节点</option>
            {nodes.map((node) => <option key={node.node_id} value={node.node_id}>{node.node_id}</option>)}
          </select>
        </label>
        <fieldset className="status-filter">
          <legend>实例状态</legend>
          <div>
            {statuses.map((status) => (
              <label key={status}>
                <input
                  type="checkbox"
                  checked={filters.statuses.has(status)}
                  onChange={() => toggleStatus(status)}
                />
                <StatusBadge status={status} compact />
              </label>
            ))}
          </div>
        </fieldset>
      </div>
    </section>
  )
}
