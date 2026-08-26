import type {
  DashboardSnapshotResponse,
  EventEnvelope,
  EventsResponse,
  InstanceRecord,
} from '../types'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
  }
}

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...init?.headers,
    },
  })

  const payload = (await response.json().catch(() => ({}))) as {
    error?: { code?: string; message?: string; request_id?: string }
  }
  if (!response.ok) {
    throw new ApiError(
      response.status,
      payload.error?.code ?? 'request_failed',
      payload.error?.message ?? `请求失败（HTTP ${response.status}）`,
      payload.error?.request_id,
    )
  }
  return payload as T
}

export async function fetchDashboardSnapshot(signal?: AbortSignal): Promise<DashboardSnapshotResponse> {
  let cursor: string | null = null
  let merged: DashboardSnapshotResponse | null = null
  const seen = new Set<string>()

  for (let page = 0; page < 100; page += 1) {
    const params = new URLSearchParams({ limit: '10000' })
    if (cursor) params.set('cursor', cursor)
    const response = await requestJSON<DashboardSnapshotResponse>(
      `/api/v1/dashboard/snapshot?${params.toString()}`,
      { signal },
    )

    if (!merged) {
      merged = {
        data: {
          ...response.data,
          instances: [],
        },
        meta: { ...response.meta },
      }
    } else if (merged.meta.snapshot_revision !== response.meta.snapshot_revision) {
      throw new ApiError(409, 'snapshot_changed', '快照在分页期间发生变化，请重新同步')
    }

    for (const instance of response.data.instances ?? []) {
      const key = `${instance.service}/${instance.instance_id}`
      if (!seen.has(key)) {
        seen.add(key)
        merged.data.instances.push(instance)
      }
    }
    merged.meta.event_cursor = Math.max(merged.meta.event_cursor, response.meta.event_cursor)
    merged.meta.next_cursor = response.meta.next_cursor
    merged.meta.has_more = response.meta.has_more

    if (!response.meta.has_more || !response.meta.next_cursor) return merged
    cursor = response.meta.next_cursor
  }

  throw new ApiError(500, 'snapshot_too_many_pages', '快照分页超过安全上限')
}

export async function fetchEvents(cursor: number, signal?: AbortSignal): Promise<EventEnvelope[]> {
  const response = await requestJSON<EventsResponse>(
    `/api/v1/gossip/events?cursor=${encodeURIComponent(cursor)}&limit=200`,
    { signal },
  )
  return response.data ?? []
}

function secureRandomUUID(): string {
  const cryptoProvider = globalThis.crypto
  if (cryptoProvider && typeof cryptoProvider.randomUUID === 'function') {
    try {
      return cryptoProvider.randomUUID()
    } catch {
      // Some non-secure browser contexts expose randomUUID but reject calls.
      // getRandomValues remains the cryptographically secure fallback.
    }
  }
  if (!cryptoProvider || typeof cryptoProvider.getRandomValues !== 'function') {
    throw new Error('当前环境不支持安全随机数，无法创建操作标识')
  }

  const bytes = cryptoProvider.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export async function simulateOffline(instance: InstanceRecord): Promise<string> {
  const operationId = `op-console-${secureRandomUUID()}`
  const response = await requestJSON<{ data: { event_id: string; status: string } }>(
    `/api/v1/demo/services/${encodeURIComponent(instance.service)}/instances/${encodeURIComponent(instance.instance_id)}/offline`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ lease_id: instance.lease_id, operation_id: operationId }),
    },
  )
  return response.data.event_id
}

export function websocketURL(cursor: number): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}/api/v1/events/ws?cursor=${encodeURIComponent(cursor)}`
}
