import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { OfflineDialog } from './OfflineDialog'
import { instance } from '../test/fixtures'

describe('OfflineDialog', () => {
  it('uses an accessible custom dialog and submits only once', async () => {
    const user = userEvent.setup()
    const confirm = vi.fn().mockResolvedValue(undefined)
    render(<OfflineDialog instance={instance(7)} onClose={vi.fn()} onConfirm={confirm} />)

    expect(screen.getByRole('dialog', { name: '确认模拟实例下线' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '模拟下线' }))
    expect(confirm).toHaveBeenCalledTimes(1)
  })

  it('cancels without submitting', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    const confirm = vi.fn()
    render(<OfflineDialog instance={instance(8)} onClose={close} onConfirm={confirm} />)
    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(close).toHaveBeenCalledOnce()
    expect(confirm).not.toHaveBeenCalled()
  })
})
