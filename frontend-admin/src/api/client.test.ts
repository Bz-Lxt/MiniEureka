import { afterEach, describe, expect, it, vi } from 'vitest'
import { instance } from '../test/fixtures'
import { simulateOffline } from './client'

const originalCryptoDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'crypto')

afterEach(() => {
  vi.restoreAllMocks()
  if (originalCryptoDescriptor) Object.defineProperty(globalThis, 'crypto', originalCryptoDescriptor)
})

describe('simulateOffline', () => {
  it('uses getRandomValues when randomUUID is unavailable and sends unique UUID v4 operation ids', async () => {
    let sequence = 0
    const getRandomValues = vi.fn(<T extends ArrayBufferView | null>(target: T): T => {
      if (!(target instanceof Uint8Array)) throw new TypeError('expected Uint8Array')
      sequence += 1
      target.forEach((_, index) => { target[index] = (index + sequence) & 0xff })
      return target as T
    })
    Object.defineProperty(globalThis, 'crypto', {
      configurable: true,
      value: { getRandomValues },
    })

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      json: async () => ({ data: { event_id: 'evt-offline', status: 'EVICTED' } }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const target = instance(7)
    await simulateOffline(target)
    await simulateOffline(target)

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const operationIds = fetchMock.mock.calls.map(([, init]) => {
      const body = JSON.parse(String((init as RequestInit).body)) as { operation_id: string; lease_id: string }
      expect(body.lease_id).toBe(target.lease_id)
      return body.operation_id
    })
    expect(operationIds[0]).not.toBe(operationIds[1])
    for (const operationId of operationIds) {
      expect(operationId).toMatch(/^op-console-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    }
    expect(getRandomValues).toHaveBeenCalledTimes(2)
  })
})
