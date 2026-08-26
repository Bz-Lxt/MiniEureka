import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { InstanceWall } from './InstanceWall'
import { instance } from '../test/fixtures'
import type { Filters } from '../types'

const allFilters: Filters = { query: '', service: '', node: '', statuses: new Set(['ACTIVE', 'DELAYED', 'EVICTED']) }

describe('InstanceWall', () => {
  it('virtualizes a 10k snapshot and retains accessible state text', async () => {
    const instances = Array.from({ length: 10_000 }, (_, index) => instance(index))
    const onCount = vi.fn()
    const { container } = render(
      <InstanceWall
        instances={instances}
        filters={allFilters}
        canSimulateOffline
        onSelect={vi.fn()}
        onRequestOffline={vi.fn()}
        onFilteredCount={onCount}
        onClearFilters={vi.fn()}
      />,
    )
    await waitFor(() => expect(onCount).toHaveBeenLastCalledWith(10_000))
    expect(container.querySelectorAll('.instance-tile').length).toBeLessThan(100)
    expect(screen.getAllByText('活跃').length).toBeGreaterThan(0)
  })

  it('shows a clear action when filters match no instance', () => {
    render(
      <InstanceWall
        instances={[instance(1)]}
        filters={{ ...allFilters, query: 'missing-instance' }}
        canSimulateOffline={false}
        onSelect={vi.fn()}
        onRequestOffline={vi.fn()}
        onFilteredCount={vi.fn()}
        onClearFilters={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: '清除筛选' })).toBeInTheDocument()
  })
})
