import '@testing-library/jest-dom/vitest'
import { afterEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'

class ResizeObserverMock implements ResizeObserver {
  readonly observed = new Set<Element>()
  constructor(private readonly callback: ResizeObserverCallback) {}
  observe(target: Element) {
    this.observed.add(target)
    this.callback([{ target, contentRect: target.getBoundingClientRect() } as ResizeObserverEntry], this)
  }
  unobserve(target: Element) { this.observed.delete(target) }
  disconnect() { this.observed.clear() }
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock)
Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: vi.fn() })

afterEach(() => cleanup())
