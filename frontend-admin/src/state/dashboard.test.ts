import { describe, expect, it } from 'vitest'
import { dashboardReducer, initialDashboardState } from './dashboard'
import { deliveryEvent, instance, snapshot } from '../test/fixtures'

describe('dashboardReducer', () => {
  it('normalizes snapshot and applies a newer status event once', () => {
    const original = instance(1)
    const loaded = dashboardReducer(initialDashboardState, { type: 'SNAPSHOT', response: snapshot([original]) })
    const delayed = instance(1, {
      status: 'DELAYED',
      status_reason: 'HEARTBEAT_DELAYED',
      version: { physical_ms: 3000, logical: 0, origin_node_id: 'node-2' },
    })
    const event = { ...deliveryEvent(12), type: 'INSTANCE_DELAYED', payload: { instance: delayed } }
    const changed = dashboardReducer(loaded, { type: 'EVENTS', events: [event, event] })

    expect(changed.instances[`${delayed.service}/${delayed.instance_id}`].status).toBe('DELAYED')
    expect(changed.summary.active).toBe(0)
    expect(changed.summary.delayed).toBe(1)
    expect(changed.eventCursor).toBe(12)
  })

  it('rejects an older HLC value and keeps the current status', () => {
    const current = instance(2, { status: 'EVICTED', version: { physical_ms: 5000, logical: 2, origin_node_id: 'node-2' } })
    const loaded = dashboardReducer(initialDashboardState, { type: 'SNAPSHOT', response: snapshot([current]) })
    const stale = instance(2, { status: 'ACTIVE', version: { physical_ms: 4000, logical: 9, origin_node_id: 'node-3' } })
    const changed = dashboardReducer(loaded, { type: 'EVENTS', events: [{ ...deliveryEvent(13), payload: { instance: stale } }] })

    expect(changed.instances[`${current.service}/${current.instance_id}`].status).toBe('EVICTED')
  })
})
