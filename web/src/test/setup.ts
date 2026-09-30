// Vitest setup for component tests.
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// jsdom implements no layout, so it ships no Element.scrollTo. Components that
// auto-scroll (BuildView's log pane) call it on mount and would otherwise throw.
if (!Element.prototype.scrollTo) {
  Element.prototype.scrollTo = vi.fn()
}

// Unmount anything a test rendered so the next test starts against an empty document.
afterEach(() => {
  cleanup()
})
