import { startTransition, useCallback, useEffect, useReducer, useRef } from 'react'
import { ApiError, fetchDashboardSnapshot, fetchEvents, websocketURL } from '../api/client'
import { dashboardReducer, initialDashboardState } from '../state/dashboard'
import type { EventEnvelope } from '../types'

const POLL_INTERVAL_MS = 3_000
const SNAPSHOT_REFRESH_MS = 30_000
const EVENT_BATCH_MS = 50

function readableError(error: unknown): string {
  if (error instanceof ApiError) {
    const suffix = error.requestId ? `（请求 ${error.requestId}）` : ''
    return `${error.message}${suffix}`
  }
  if (error instanceof Error) return error.message
  return '无法连接注册中心，请稍后重试'
}

export function useDashboardData() {
  const [state, dispatch] = useReducer(dashboardReducer, initialDashboardState)
  const reloadRef = useRef<() => void>(() => undefined)

  useEffect(() => {
    let stopped = false
    let socket: WebSocket | null = null
    let pollTimer: number | null = null
    let snapshotTimer: number | null = null
    let reconnectTimer: number | null = null
    let batchTimer: number | null = null
    let retryAttempt = 0
    let cursor = 0
    let loadingSnapshot = false
    let queuedEvents: EventEnvelope[] = []
    let snapshotController: AbortController | null = null

    const clearTimer = (timer: number | null) => {
      if (timer !== null) window.clearTimeout(timer)
    }

    const flushEvents = () => {
      batchTimer = null
      if (queuedEvents.length === 0 || stopped) return
      const batch = queuedEvents
      queuedEvents = []
      cursor = Math.max(cursor, ...batch.map((event) => event.seq))
      startTransition(() => dispatch({ type: 'EVENTS', events: batch }))
    }

    const enqueue = (events: EventEnvelope[]) => {
      if (events.length === 0) return
      queuedEvents.push(...events)
      if (batchTimer === null) batchTimer = window.setTimeout(flushEvents, EVENT_BATCH_MS)
    }

    const stopPolling = () => {
      clearTimer(pollTimer)
      clearTimer(snapshotTimer)
      pollTimer = null
      snapshotTimer = null
    }

    const syncSnapshot = async (initial = false): Promise<boolean> => {
      if (loadingSnapshot || stopped) return false
      loadingSnapshot = true
      snapshotController?.abort()
      snapshotController = new AbortController()
      if (initial) dispatch({ type: 'LOADING', loading: true })
      try {
        const response = await fetchDashboardSnapshot(snapshotController.signal)
        if (stopped) return false
        cursor = response.meta.event_cursor
        dispatch({ type: 'SNAPSHOT', response })
        return true
      } catch (error) {
        if (!stopped && !(error instanceof DOMException && error.name === 'AbortError')) {
          dispatch({ type: 'ERROR', message: readableError(error) })
          dispatch({ type: 'CONNECTION', mode: 'offline' })
          if (initial) dispatch({ type: 'LOADING', loading: false })
        }
        return false
      } finally {
        loadingSnapshot = false
      }
    }

    const pollOnce = async () => {
      if (stopped) return
      try {
        const events = await fetchEvents(cursor)
        if (stopped) return
        enqueue(events)
        dispatch({ type: 'CONNECTION', mode: 'polling' })
      } catch (error) {
        if (error instanceof ApiError && error.code === 'cursor_expired') {
          await syncSnapshot(false)
        } else if (!stopped) {
          dispatch({ type: 'CONNECTION', mode: 'offline' })
          dispatch({ type: 'ERROR', message: readableError(error) })
        }
      }
    }

    const startPolling = () => {
      if (pollTimer !== null || stopped) return
      dispatch({ type: 'CONNECTION', mode: 'polling' })
      void pollOnce()
      pollTimer = window.setInterval(() => void pollOnce(), POLL_INTERVAL_MS)
      snapshotTimer = window.setInterval(() => void syncSnapshot(false), SNAPSHOT_REFRESH_MS)
    }

    const connect = () => {
      if (stopped || typeof WebSocket === 'undefined') {
        startPolling()
        return
      }
      socket?.close()
      dispatch({ type: 'CONNECTION', mode: retryAttempt === 0 ? 'connecting' : 'polling' })
      socket = new WebSocket(websocketURL(cursor))

      socket.addEventListener('open', () => {
        if (stopped) return
        retryAttempt = 0
        stopPolling()
        dispatch({ type: 'CONNECTION', mode: 'realtime' })
        dispatch({ type: 'ERROR', message: null })
      })
      socket.addEventListener('message', (message) => {
        try {
          const event = JSON.parse(String(message.data)) as EventEnvelope
          if (event.type === 'CONNECTED') return
          if (event.type === 'RESYNC_REQUIRED') {
            socket?.close()
            void syncSnapshot(false)
            return
          }
          enqueue([event])
        } catch {
          dispatch({ type: 'ERROR', message: '收到无法解析的实时事件，正在重新同步' })
          socket?.close()
          void syncSnapshot(false)
        }
      })
      socket.addEventListener('close', () => {
        if (stopped) return
        startPolling()
        retryAttempt += 1
        const base = Math.min(30_000, 1_000 * 2 ** Math.min(retryAttempt - 1, 5))
        const delay = Math.round(base * (0.8 + Math.random() * 0.4))
        clearTimer(reconnectTimer)
        reconnectTimer = window.setTimeout(connect, delay)
      })
      socket.addEventListener('error', () => socket?.close())
    }

    const bootstrap = async (initial = true) => {
      const loaded = await syncSnapshot(initial)
      if (loaded && !stopped) connect()
      else if (!stopped) {
        startPolling()
        clearTimer(reconnectTimer)
        reconnectTimer = window.setTimeout(() => void bootstrap(false), 3_000)
      }
    }

    reloadRef.current = () => void bootstrap(false)
    const handleVisibility = () => {
      if (document.visibilityState === 'visible') void syncSnapshot(false)
    }
    document.addEventListener('visibilitychange', handleVisibility)
    void bootstrap(true)

    return () => {
      stopped = true
      document.removeEventListener('visibilitychange', handleVisibility)
      snapshotController?.abort()
      socket?.close()
      stopPolling()
      clearTimer(reconnectTimer)
      clearTimer(batchTimer)
    }
  }, [])

  const reload = useCallback(() => reloadRef.current(), [])
  const dismissError = useCallback(() => dispatch({ type: 'ERROR', message: null }), [])
  return { state, reload, dismissError }
}
