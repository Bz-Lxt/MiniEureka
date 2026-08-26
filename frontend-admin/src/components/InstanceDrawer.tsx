import { Copy, Server, X } from 'lucide-react'
import { useCallback, useRef, useState } from 'react'
import { useModalFocus } from '../hooks/useModalFocus'
import type { InstanceRecord } from '../types'
import { formatDateTime, formatRelative, formatVersion, humanReason } from '../utils'
import { StatusBadge } from './StatusBadge'

export function InstanceDrawer({ instance, stale, onClose }: {
  instance: InstanceRecord
  stale: boolean
  onClose: () => void
}) {
  const drawerRef = useRef<HTMLElement>(null)
  const [copied, setCopied] = useState<string | null>(null)
  useModalFocus(drawerRef, onClose)

  const copy = useCallback(async (label: string, value: string) => {
    await navigator.clipboard?.writeText(value)
    setCopied(label)
    window.setTimeout(() => setCopied(null), 1_500)
  }, [])

  return (
    <div className="modal-layer" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <aside
        className="instance-drawer"
        ref={drawerRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="instance-drawer-title"
      >
        <div className="drawer-header">
          <div>
            <p className="eyebrow">INSTANCE DETAIL</p>
            <h2 id="instance-drawer-title">{instance.instance_id}</h2>
            <p>{instance.service}</p>
          </div>
          <button className="icon-button" aria-label="关闭实例详情" onClick={onClose} data-autofocus>
            <X aria-hidden="true" size={19} />
          </button>
        </div>
        <div className="drawer-content">
          {stale && <div className="inline-warning" role="status">该实例已不在当前快照中，以下为最后一次已知状态。</div>}
          <div className="detail-lead">
            <span className="detail-server-icon"><Server aria-hidden="true" size={24} /></span>
            <div><StatusBadge status={instance.status} /><p>{humanReason(instance.status_reason)}</p></div>
          </div>
          <dl className="detail-grid">
            <div><dt>地址</dt><dd className="mono">{instance.protocol}://{instance.host}:{instance.port}</dd></div>
            <div><dt>来源节点</dt><dd className="mono">{instance.origin_node_id}</dd></div>
            <div><dt>租约代次</dt><dd>{instance.generation}</dd></div>
            <div><dt>租约年龄</dt><dd>{formatRelative(instance.last_heartbeat_at)}</dd></div>
            <div><dt>注册时间</dt><dd title={instance.registered_at}>{formatDateTime(instance.registered_at)}</dd></div>
            <div><dt>最近心跳</dt><dd title={instance.last_heartbeat_at}>{formatDateTime(instance.last_heartbeat_at)}</dd></div>
            <div><dt>更新时间</dt><dd title={instance.updated_at}>{formatDateTime(instance.updated_at)}</dd></div>
            <div><dt>摘除时间</dt><dd title={instance.evicted_at ?? ''}>{formatDateTime(instance.evicted_at)}</dd></div>
          </dl>
          <section className="detail-section" aria-labelledby="lease-title">
            <div className="detail-section-title">
              <h3 id="lease-title">版本与租约</h3>
            </div>
            {[
              ['HLC 版本', formatVersion(instance.version)],
              ['租约 epoch', formatVersion(instance.lease_epoch)],
              ['Lease ID', instance.lease_id],
            ].map(([label, value]) => (
              <div className="copy-row" key={label}>
                <span>{label}</span><code>{value}</code>
                <button className="icon-button small" onClick={() => void copy(label, value)} aria-label={`复制${label}`}>
                  <Copy aria-hidden="true" size={14} />
                </button>
              </div>
            ))}
            {copied && <p className="copy-feedback" role="status">已复制{copied}</p>}
          </section>
          <section className="detail-section" aria-labelledby="metadata-title">
            <div className="detail-section-title"><h3 id="metadata-title">元数据</h3></div>
            {Object.keys(instance.metadata ?? {}).length === 0 ? (
              <p className="muted">无元数据</p>
            ) : (
              <div className="metadata-table" role="table" aria-label="实例元数据">
                {Object.entries(instance.metadata).map(([key, value]) => (
                  <div className="metadata-row" role="row" key={key}>
                    <span role="cell" className="mono">{key}</span>
                    <span role="cell">{value}</span>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
      </aside>
    </div>
  )
}
