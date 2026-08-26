import { Activity, DatabaseZap, Radio, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { simulateOffline } from './api/client'
import { ClusterTopology } from './components/ClusterTopology'
import { ConnectionBanner, DashboardSkeleton, ToastRegion, type ToastMessage } from './components/Feedback'
import { FilterBar } from './components/FilterBar'
import { GossipTimeline } from './components/GossipTimeline'
import { InstanceDrawer } from './components/InstanceDrawer'
import { InstanceWall } from './components/InstanceWall'
import { OfflineDialog } from './components/OfflineDialog'
import { OverviewCards } from './components/OverviewCards'
import { useDashboardData } from './hooks/useDashboardData'
import type { Filters, InstanceRecord, InstanceStatus } from './types'
import { formatDateTime, instanceKey } from './utils'

const ALL_STATUSES: InstanceStatus[] = ['ACTIVE', 'DELAYED', 'EVICTED']

function initialFilters(): Filters {
  const params = new URLSearchParams(window.location.search)
  const requested = (params.get('status') ?? '').split(',').filter((value): value is InstanceStatus => ALL_STATUSES.includes(value as InstanceStatus))
  return {
    query: params.get('q') ?? '',
    service: params.get('service') ?? '',
    node: params.get('node') ?? '',
    statuses: new Set(requested.length > 0 ? requested : ALL_STATUSES),
  }
}

export default function App() {
  const { state, reload, dismissError } = useDashboardData()
  const [filters, setFilters] = useState<Filters>(initialFilters)
  const [resultCount, setResultCount] = useState(0)
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [selectedFallback, setSelectedFallback] = useState<InstanceRecord | null>(null)
  const [offlineTarget, setOfflineTarget] = useState<InstanceRecord | null>(null)
  const [toasts, setToasts] = useState<ToastMessage[]>([])
  const toastSequence = useRef(0)

  const instances = useMemo(() => Object.values(state.instances), [state.instances])
  const nodes = useMemo(() => Object.values(state.nodes).sort((a, b) => a.node_id.localeCompare(b.node_id)), [state.nodes])
  const services = useMemo(() => Object.keys(state.serviceCounts).sort((a, b) => a.localeCompare(b)), [state.serviceCounts])
  const selected = selectedKey ? state.instances[selectedKey] ?? selectedFallback : null

  const dismissToast = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
  }, [])
  const notify = useCallback((toast: Omit<ToastMessage, 'id'>) => {
    toastSequence.current += 1
    setToasts((current) => [...current.slice(-3), { ...toast, id: toastSequence.current }])
  }, [])

  useEffect(() => {
    if (!state.error) return
    notify({ kind: 'error', title: '请求失败', message: state.error })
    dismissError()
  }, [dismissError, notify, state.error])

  useEffect(() => {
    const params = new URLSearchParams()
    if (filters.query) params.set('q', filters.query)
    if (filters.service) params.set('service', filters.service)
    if (filters.node) params.set('node', filters.node)
    if (filters.statuses.size !== ALL_STATUSES.length) params.set('status', [...filters.statuses].join(','))
    if (selectedKey) params.set('instance', selectedKey)
    const query = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`)
  }, [filters, selectedKey])

  const clearFilters = useCallback(() => {
    setFilters({ query: '', service: '', node: '', statuses: new Set(ALL_STATUSES) })
  }, [])
  const selectInstance = useCallback((instance: InstanceRecord) => {
    setSelectedFallback(instance)
    setSelectedKey(instanceKey(instance.service, instance.instance_id))
  }, [])
  const closeDrawer = useCallback(() => setSelectedKey(null), [])
  const closeOfflineDialog = useCallback(() => setOfflineTarget(null), [])
  const confirmOffline = useCallback(async () => {
    if (!offlineTarget) return
    const eventId = await simulateOffline(offlineTarget)
    notify({
      kind: 'success',
      title: '已提交模拟下线',
      message: `事件 ${eventId} 已创建，可在 Gossip 时间线追踪传播。`,
    })
    setOfflineTarget(null)
  }, [notify, offlineTarget])
  const setOverviewStatus = useCallback((status: InstanceStatus | null) => {
    setFilters((current) => ({ ...current, statuses: new Set(status ? [status] : ALL_STATUSES) }))
  }, [])
  const overviewStatus = filters.statuses.size === 1 ? [...filters.statuses][0] : null

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">跳到主内容</a>
      <header className="app-header">
        <div className="brand-lockup">
          <span className="brand-mark"><DatabaseZap aria-hidden="true" size={22} /></span>
          <div><h1>Mini Eureka</h1><p>分布式服务注册中心</p></div>
        </div>
        <div className="header-status">
          <span className={`connection-pill connection-${state.connection}`}>
            {state.connection === 'realtime' ? <Radio aria-hidden="true" size={14} /> : <RefreshCw aria-hidden="true" size={14} />}
            {state.connection === 'realtime' ? '实时连接' : state.connection === 'polling' ? '轮询同步' : state.connection === 'offline' ? '连接中断' : '正在连接'}
          </span>
          <span className="header-node"><Activity aria-hidden="true" size={14} />{nodes.filter((node) => node.status === 'ALIVE').length}/{nodes.length} 节点在线</span>
          <span className="last-updated">最后更新 <time dateTime={state.summary.updated_at}>{formatDateTime(state.summary.updated_at)}</time></span>
        </div>
      </header>
      <ConnectionBanner mode={state.connection} onRetry={reload} />

      <main id="main-content">
        {state.loading && instances.length === 0 ? <DashboardSkeleton /> : (
          <>
            <OverviewCards summary={state.summary} activeStatus={overviewStatus} onStatusSelect={setOverviewStatus} />
            <FilterBar filters={filters} services={services} nodes={nodes} resultCount={resultCount} onChange={setFilters} />
            <div className="dashboard-grid">
              <InstanceWall
                instances={instances}
                filters={filters}
                canSimulateOffline={state.capabilities.demo_enabled && state.capabilities.simulate_offline}
                onSelect={selectInstance}
                onRequestOffline={setOfflineTarget}
                onFilteredCount={setResultCount}
                onClearFilters={clearFilters}
              />
              <aside className="cluster-column" aria-label="集群传播状态">
                <ClusterTopology nodes={nodes} edges={state.edges} events={state.events} onNodeFilter={(node) => setFilters((current) => ({ ...current, node }))} />
                <GossipTimeline events={state.events} />
              </aside>
            </div>
          </>
        )}
      </main>

      {selected && <InstanceDrawer instance={selected} stale={!state.instances[selectedKey ?? '']} onClose={closeDrawer} />}
      {offlineTarget && <OfflineDialog instance={offlineTarget} onClose={closeOfflineDialog} onConfirm={confirmOffline} />}
      <ToastRegion toasts={toasts} onDismiss={dismissToast} />
    </div>
  )
}
