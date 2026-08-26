import { CheckCircle2, RefreshCw, Wifi, WifiOff, X } from 'lucide-react'
import { useEffect } from 'react'
import type { ConnectionMode } from '../types'

export interface ToastMessage {
  id: number
  kind: 'success' | 'error'
  title: string
  message: string
}

export function ConnectionBanner({ mode, onRetry }: { mode: ConnectionMode; onRetry: () => void }) {
  if (mode === 'realtime' || mode === 'connecting') return null
  return (
    <div className={`connection-banner banner-${mode}`} role="status">
      {mode === 'polling' ? <Wifi aria-hidden="true" size={17} /> : <WifiOff aria-hidden="true" size={17} />}
      <span>{mode === 'polling' ? '实时连接中断，已切换 HTTP 轮询，数据仍会更新。' : '当前无法连接注册中心，正在保留最后一次已知状态。'}</span>
      <button className="text-button" onClick={onRetry}><RefreshCw aria-hidden="true" size={14} />立即重试</button>
    </div>
  )
}

function Toast({ toast, onDismiss }: { toast: ToastMessage; onDismiss: (id: number) => void }) {
  useEffect(() => {
    const timer = window.setTimeout(() => onDismiss(toast.id), 5_000)
    return () => window.clearTimeout(timer)
  }, [onDismiss, toast.id])
  return (
    <li className={`toast toast-${toast.kind}`}>
      {toast.kind === 'success' ? <CheckCircle2 aria-hidden="true" size={18} /> : <WifiOff aria-hidden="true" size={18} />}
      <div><strong>{toast.title}</strong><p>{toast.message}</p></div>
      <button className="icon-button small" aria-label={`关闭${toast.title}提示`} onClick={() => onDismiss(toast.id)}><X aria-hidden="true" size={14} /></button>
    </li>
  )
}

export function ToastRegion({ toasts, onDismiss }: { toasts: ToastMessage[]; onDismiss: (id: number) => void }) {
  return <ol className="toast-region" aria-label="操作通知" aria-live="polite">{toasts.map((toast) => <Toast key={toast.id} toast={toast} onDismiss={onDismiss} />)}</ol>
}

export function DashboardSkeleton() {
  return (
    <div className="dashboard-skeleton" aria-label="正在加载控制台" role="status">
      <div className="skeleton-overview">{Array.from({ length: 6 }, (_, index) => <span key={index} />)}</div>
      <div className="skeleton-toolbar" />
      <div className="skeleton-main"><div /><aside /></div>
      <p>正在连接注册中心并获取实时快照…</p>
    </div>
  )
}
