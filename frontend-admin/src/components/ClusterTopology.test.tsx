import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ClusterTopology } from './ClusterTopology'
import { deliveryEvent, node } from '../test/fixtures'

describe('ClusterTopology', () => {
  it('keeps deterministic coordinates and binds flights to a real event id', () => {
    const nodes = [node('node-1'), node('node-2'), node('node-3')]
    const edges = [{ source_node_id: 'node-1', target_node_id: 'node-2', state: 'ALIVE', last_success_at: null, latency_ms: 2 }]
    const { container, rerender } = render(
      <ClusterTopology nodes={nodes} edges={edges} events={[deliveryEvent()]} onNodeFilter={vi.fn()} />,
    )
    const before = container.querySelector('[data-node-id="node-1"]')?.getAttribute('transform')
    expect(container.querySelector('[data-event-id="evt-offline-7"]')).toHaveAttribute('data-hop', '1')

    rerender(<ClusterTopology nodes={nodes} edges={edges} events={[deliveryEvent(), deliveryEvent(12)]} onNodeFilter={vi.fn()} />)
    expect(container.querySelector('[data-node-id="node-1"]')?.getAttribute('transform')).toBe(before)
  })
})
