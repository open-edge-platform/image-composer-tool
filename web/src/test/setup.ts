// Vitest setup for component tests.
import { cleanup, configure } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// jsdom implements no layout, so it ships no Element.scrollTo. Components that
// auto-scroll (BuildView's log pane) call it on mount and would otherwise throw.
if (!Element.prototype.scrollTo) {
  Element.prototype.scrollTo = vi.fn()
}
// window.scrollTo exists but only logs "Not implemented"; UserGuidePage calls it
// on every topic change.
window.scrollTo = vi.fn()

// Unmount anything a test rendered so the next test starts against an empty document.
afterEach(() => {
  cleanup()
})

// findBy* and waitFor give up after one second by default. The page tests wait
// on debounced composes and chained mocked requests, and with the whole suite
// running in parallel on a busy machine that can take longer, which made them
// fail now and then. A longer limit only slows a test that is failing anyway.
configure({ asyncUtilTimeout: 5000 })
