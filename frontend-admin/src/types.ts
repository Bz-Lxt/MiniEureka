export type InstanceStatus = 'ACTIVE' | 'DELAYED' | 'EVICTED'
export type MemberStatus = 'ALIVE' | 'SUSPECT' | 'DEAD'

export interface HLCVersion {
  physical_ms: number
  logical: number
  origin_node_id: string
}

export interface InstanceRecord {
  service: string
  instance_id: string
  registration_id?: string
  host: string
  port: number
  protocol: 'http' | 'https' | 'grpc' | 'tcp'
  metadata: Record<string, string>
  status: InstanceStatus
  status_reason: string
  generation: number
  lease_id: string
  lease_epoch?: HLCVersion
  version: HLCVersion
  origin_node_id: string
  registered_at: string
  last_heartbeat_at: string
  updated_at: string
  evicted_at: string | null
  demo: boolean
}

export interface ClusterNode {
  node_id: string
  http_address: string
  gossip_address: string
  status: MemberStatus
  incarnation: number
  last_seen_at: string
  version: HLCVersion
}

export interface TopologyEdge {
  source_node_id: string
  target_node_id: string
  state: string
  last_success_at: string | null
  latency_ms: number | null
}

export interface Delivery {
  attempt_id: string
  source_node_id: string
  target_node_id: string
  hop: number
  result: string
  latency_ms?: number
}

export interface EventEnvelope {
  seq: number
  schema_version: number
  stream_node_id: string
  stream_boot_id: string
  type: string
  event_id: string
  entity_key: string
  revision: string
  origin_node_id: string
  occurred_at: string
  payload: {
    instance?: InstanceRecord
    member?: ClusterNode
    node?: ClusterNode
    edges?: TopologyEdge[]
    [key: string]: unknown
  }
  delivery?: Delivery
}

export interface DashboardSummary {
  services: number
  instances: number
  active: number
  delayed: number
  evicted: number
  nodes: number
  gossip_rate: number
  updated_at: string
}

export interface Capabilities {
  demo_enabled: boolean
  simulate_offline: boolean
  network_faults: boolean
}

export interface DashboardSnapshotData {
  summary: DashboardSummary
  instances: InstanceRecord[]
  nodes: ClusterNode[]
  edges: TopologyEdge[]
  recent_events: EventEnvelope[]
  capabilities: Capabilities
}

export interface DashboardSnapshotMeta {
  snapshot_revision: string
  event_cursor: number
  next_cursor: string | null
  has_more: boolean
}

export interface DashboardSnapshotResponse {
  data: DashboardSnapshotData
  meta: DashboardSnapshotMeta
}

export interface EventsResponse {
  data: EventEnvelope[]
  meta?: {
    next_cursor?: number | null
    has_more?: boolean
  }
}

export interface ApiErrorEnvelope {
  error?: {
    code?: string
    message?: string
    request_id?: string
  }
}

export type ConnectionMode = 'connecting' | 'realtime' | 'polling' | 'offline'

export interface Filters {
  query: string
  service: string
  node: string
  statuses: Set<InstanceStatus>
}
