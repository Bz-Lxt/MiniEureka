import type { ClusterNode, DashboardSnapshotResponse, EventEnvelope, InstanceRecord } from '../types'

export function instance(index = 1, overrides: Partial<InstanceRecord> = {}): InstanceRecord {
  return {
    service: `service-${Math.floor(index / 20)}`,
    instance_id: `instance-${String(index).padStart(5, '0')}`,
    registration_id: `reg-${index}`,
    host: `10.0.${Math.floor(index / 255)}.${index % 255}`,
    port: 8000 + (index % 100),
    protocol: 'http',
    metadata: { zone: 'cn-east-1a' },
    status: 'ACTIVE',
    status_reason: 'HEARTBEAT_OK',
    generation: 1,
    lease_id: `lease-${index}`,
    lease_epoch: { physical_ms: 1000 + index, logical: 0, origin_node_id: 'node-1' },
    version: { physical_ms: 1000 + index, logical: 0, origin_node_id: 'node-1' },
    origin_node_id: 'node-1',
    registered_at: '2026-08-26T08:00:00Z',
    last_heartbeat_at: '2026-08-26T08:00:10Z',
    updated_at: '2026-08-26T08:00:10Z',
    evicted_at: null,
    demo: true,
    ...overrides,
  }
}

export function node(id: string, status: ClusterNode['status'] = 'ALIVE'): ClusterNode {
  return {
    node_id: id,
    http_address: `http://${id}:8080`,
    gossip_address: `${id}:7946`,
    status,
    incarnation: 1,
    last_seen_at: '2026-08-26T08:00:10Z',
    version: { physical_ms: 1000, logical: 0, origin_node_id: id },
  }
}

export function deliveryEvent(seq = 11): EventEnvelope {
  return {
    seq,
    schema_version: 1,
    stream_node_id: 'node-1',
    stream_boot_id: 'boot-a',
    type: 'GOSSIP_HOP',
    event_id: 'evt-offline-7',
    entity_key: 'orders/orders-7',
    revision: `${1000 + seq}-0-node-1`,
    origin_node_id: 'node-1',
    occurred_at: '2026-08-26T08:00:12Z',
    payload: {},
    delivery: {
      attempt_id: `attempt-${seq}`,
      source_node_id: 'node-1',
      target_node_id: 'node-2',
      hop: 1,
      result: 'APPLIED',
      latency_ms: 2,
    },
  }
}

export function snapshot(instances: InstanceRecord[]): DashboardSnapshotResponse {
  return {
    data: {
      summary: {
        services: new Set(instances.map((item) => item.service)).size,
        instances: instances.length,
        active: instances.filter((item) => item.status === 'ACTIVE').length,
        delayed: instances.filter((item) => item.status === 'DELAYED').length,
        evicted: instances.filter((item) => item.status === 'EVICTED').length,
        nodes: 2,
        gossip_rate: 2,
        updated_at: '2026-08-26T08:00:12Z',
      },
      instances,
      nodes: [node('node-1'), node('node-2')],
      edges: [{ source_node_id: 'node-1', target_node_id: 'node-2', state: 'ALIVE', last_success_at: '2026-08-26T08:00:12Z', latency_ms: 2 }],
      recent_events: [deliveryEvent()],
      capabilities: { demo_enabled: true, simulate_offline: true, network_faults: true },
    },
    meta: { snapshot_revision: '1000-0-node-1', event_cursor: 10, next_cursor: null, has_more: false },
  }
}
