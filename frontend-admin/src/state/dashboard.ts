import type {
  Capabilities,
  ClusterNode,
  ConnectionMode,
  DashboardSnapshotResponse,
  DashboardSummary,
  EventEnvelope,
  InstanceRecord,
  InstanceStatus,
  TopologyEdge,
} from '../types'

const emptySummary: DashboardSummary = {
  services: 0,
  instances: 0,
  active: 0,
  delayed: 0,
  evicted: 0,
  nodes: 0,
  gossip_rate: 0,
  updated_at: '',
}

export interface DashboardState {
  summary: DashboardSummary
  instances: Record<string, InstanceRecord>
  serviceCounts: Record<string, number>
  nodes: Record<string, ClusterNode>
  edges: TopologyEdge[]
  events: EventEnvelope[]
  capabilities: Capabilities
  snapshotRevision: string
  eventCursor: number
  connection: ConnectionMode
  loading: boolean
  error: string | null
}

export const initialDashboardState: DashboardState = {
  summary: emptySummary,
  instances: {},
  serviceCounts: {},
  nodes: {},
  edges: [],
  events: [],
  capabilities: { demo_enabled: false, simulate_offline: false, network_faults: false },
  snapshotRevision: '',
  eventCursor: 0,
  connection: 'connecting',
  loading: true,
  error: null,
}

export type DashboardAction =
  | { type: 'SNAPSHOT'; response: DashboardSnapshotResponse }
  | { type: 'EVENTS'; events: EventEnvelope[] }
  | { type: 'CONNECTION'; mode: ConnectionMode }
  | { type: 'ERROR'; message: string | null }
  | { type: 'LOADING'; loading: boolean }

function statusField(status: InstanceStatus): 'active' | 'delayed' | 'evicted' {
  return status.toLowerCase() as 'active' | 'delayed' | 'evicted'
}

function eventKey(event: EventEnvelope): string {
  return `${event.stream_boot_id}:${event.seq}:${event.delivery?.attempt_id ?? ''}`
}

function compareInstanceVersion(left: InstanceRecord['version'], right: InstanceRecord['version']): number {
  if (left.physical_ms !== right.physical_ms) return left.physical_ms - right.physical_ms
  if (left.logical !== right.logical) return left.logical - right.logical
  return left.origin_node_id.localeCompare(right.origin_node_id)
}

export function dashboardReducer(state: DashboardState, action: DashboardAction): DashboardState {
  switch (action.type) {
    case 'SNAPSHOT': {
      const instances: Record<string, InstanceRecord> = {}
      const serviceCounts: Record<string, number> = {}
      for (const instance of action.response.data.instances ?? []) {
        instances[`${instance.service}/${instance.instance_id}`] = instance
        serviceCounts[instance.service] = (serviceCounts[instance.service] ?? 0) + 1
      }
      const nodes = Object.fromEntries(
        (action.response.data.nodes ?? []).map((node) => [node.node_id, node]),
      )
      const events = [...(action.response.data.recent_events ?? [])]
        .sort((a, b) => b.seq - a.seq)
        .slice(0, 500)
      return {
        ...state,
        summary: action.response.data.summary,
        instances,
        serviceCounts,
        nodes,
        edges: action.response.data.edges ?? [],
        events,
        capabilities: action.response.data.capabilities,
        snapshotRevision: action.response.meta.snapshot_revision,
        eventCursor: action.response.meta.event_cursor,
        loading: false,
        error: null,
      }
    }
    case 'EVENTS': {
      if (action.events.length === 0) return state
      const ordered = [...action.events].sort((a, b) => a.seq - b.seq)
      let instances = state.instances
      let serviceCounts = state.serviceCounts
      let nodes = state.nodes
      let edges = state.edges
      let summary = state.summary
      let cursor = state.eventCursor
      const accepted: EventEnvelope[] = []

      for (const event of ordered) {
        if (event.seq <= cursor) continue
        cursor = Math.max(cursor, event.seq)
        accepted.push(event)

        const incoming = event.payload.instance
        if (incoming) {
          const key = `${incoming.service}/${incoming.instance_id}`
          const previous = instances[key]
          if (!previous || compareInstanceVersion(previous.version, incoming.version) < 0) {
            if (instances === state.instances) instances = { ...instances }
            if (serviceCounts === state.serviceCounts) serviceCounts = { ...serviceCounts }
            summary = { ...summary, updated_at: event.occurred_at }
            if (!previous) {
              instances[key] = incoming
              serviceCounts[incoming.service] = (serviceCounts[incoming.service] ?? 0) + 1
              summary.instances += 1
              summary[statusField(incoming.status)] += 1
              summary.services = Object.keys(serviceCounts).length
            } else {
              instances[key] = incoming
              if (previous.status !== incoming.status) {
                summary[statusField(previous.status)] -= 1
                summary[statusField(incoming.status)] += 1
              }
            }
          }
        } else if (event.type === 'INSTANCE_REMOVED') {
          const previous = instances[event.entity_key]
          if (previous) {
            if (instances === state.instances) instances = { ...instances }
            if (serviceCounts === state.serviceCounts) serviceCounts = { ...serviceCounts }
            summary = { ...summary, updated_at: event.occurred_at }
            delete instances[event.entity_key]
            summary.instances -= 1
            summary[statusField(previous.status)] -= 1
            const count = (serviceCounts[previous.service] ?? 1) - 1
            if (count <= 0) delete serviceCounts[previous.service]
            else serviceCounts[previous.service] = count
            summary.services = Object.keys(serviceCounts).length
          }
        }

        const member = event.payload.member ?? event.payload.node
        if (member) {
          if (nodes === state.nodes) nodes = { ...nodes }
          nodes[member.node_id] = member
          summary = { ...summary, nodes: Object.keys(nodes).length, updated_at: event.occurred_at }
        }
        if (event.payload.edges) edges = event.payload.edges
      }

      if (accepted.length === 0) return state
      const existing = new Set(state.events.map(eventKey))
      const fresh = accepted.filter((event) => !existing.has(eventKey(event)))
      const events = [...fresh.reverse(), ...state.events].slice(0, 500)
      return { ...state, instances, serviceCounts, nodes, edges, summary, events, eventCursor: cursor }
    }
    case 'CONNECTION':
      return { ...state, connection: action.mode }
    case 'ERROR':
      return { ...state, error: action.message }
    case 'LOADING':
      return { ...state, loading: action.loading }
    default:
      return state
  }
}
