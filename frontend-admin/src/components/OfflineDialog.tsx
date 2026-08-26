import { AlertTriangle, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useModalFocus } from '../hooks/useModalFocus'
import type { InstanceRecord } from '../types'

export function OfflineDialog({ instance, onClose, onConfirm }: {
  instance: InstanceRecord
  onClose: () => void
  onConfirm: () => Promise<void>
}) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  useModalFocus(dialogRef, onClose, submitting)

  useEffect(() => {
    if (!error) return
    const timer = window.setTimeout(() => setError(null), 5_000)
    return () => window.clearTimeout(timer)
  }, [error])

  const submit = async () => {
    setSubmitting(true)
    setError(null)
    try {
      await onConfirm()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '模拟下线失败，请重试')
      setSubmitting(false)
    }
  }

  return (
    <div className="modal-layer dialog-layer" role="presentation">
      <div ref={dialogRef} className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="offline-title" aria-describedby="offline-description">
        <div className="dialog-icon"><AlertTriangle aria-hidden="true" size={23} /></div>
        <div className="dialog-copy">
          <h2 id="offline-title">确认模拟实例下线</h2>
          <p id="offline-description">
            将停止演示实例 <strong>{instance.service} / {instance.instance_id}</strong> 的心跳并创建显式摘除事件。你可以在拓扑中追踪真实 Gossip 传播。
          </p>
        </div>
        {error && (
          <div className="dialog-error" role="alert">
            <span>{error}</span>
            <button className="icon-button small" aria-label="关闭错误提示" onClick={() => setError(null)}><X aria-hidden="true" size={14} /></button>
          </div>
        )}
        <div className="dialog-actions">
          <button className="secondary-button" onClick={onClose} disabled={submitting} data-autofocus>取消</button>
          <button className="danger-button" onClick={() => void submit()} disabled={submitting}>
            {submitting ? '正在下线…' : '模拟下线'}
          </button>
        </div>
      </div>
    </div>
  )
}
