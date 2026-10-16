// Log-pane tests for BuildView.
//
// The pane is fed by an SSE stream that emits hundreds of lines a second while
// packages are being resolved, and the server buffers them without a cap. The
// view used to append each arrival to state as it landed, which re-cleaned and
// re-reconciled every line already on screen once per new line — O(n) work per
// line over an unbounded n. That saturated the browser's main thread badly
// enough that clicks elsewhere in the view never got dispatched: the artefact
// table's template download did nothing for the whole of a long compose, then
// fired once the stream stopped. These tests pin the two limits that bound the
// cost — arrivals are coalesced into bursts, and only the tail of a long log is
// kept in the DOM — plus the ingest-time cleaning that moving work off the
// render path depends on.
import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const buildDetails = vi.fn()
const buildArtifacts = vi.fn()
const logFileText = vi.fn()

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      buildDetails: (...a: unknown[]) => buildDetails(...a),
      buildArtifacts: (...a: unknown[]) => buildArtifacts(...a),
      logFileText: (...a: unknown[]) => logFileText(...a),
    },
  }
})

// Minimal EventSource stand-in: jsdom has none. BuildView registers one handler
// per event type, so a single-entry map per type is enough.
class FakeEventSource {
  static last: FakeEventSource | null = null
  listeners = new Map<string, (e: MessageEvent) => void>()
  closed = false

  url: string

  constructor(url: string) {
    this.url = url
    FakeEventSource.last = this
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, fn)
  }
  close() {
    this.closed = true
  }
  emit(type: string, data: unknown) {
    this.listeners.get(type)?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

const DETAILS = {
  buildId: 'abc',
  status: 'running',
  command: 'sudo -n ict build minimal.yml',
  template: 'minimal.yml',
  templateUrl: '/api/v1/builds/abc/template',
  templatePath: '/var/tmp/ict/builds/abc/template.yml',
  workDir: '/var/tmp/ict/builds/abc/work',
  cacheDir: '/var/tmp/ict/builds/abc/cache',
  hasLogFile: false,
}

async function renderActiveBuild() {
  const { BuildView } = await import('./BuildView')
  let result!: ReturnType<typeof render>
  await act(async () => {
    result = render(
      <BuildView
        buildId="abc"
        onRetry={async () => {}}
        retrying={false}
        retryError={null}
        onStatusChange={() => {}}
        isActive
      />,
    )
  })
  // connect() runs synchronously at the end of the mount effect, so the stream
  // is already open by the time the render settles.
  return { ...result, es: FakeEventSource.last! }
}

describe('BuildView log pane', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', FakeEventSource)
    FakeEventSource.last = null
    buildDetails.mockResolvedValue(DETAILS)
    buildArtifacts.mockResolvedValue([])
    logFileText.mockResolvedValue('')
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('coalesces a burst of lines into one update instead of rendering each', async () => {
    const { LOG_FLUSH_MS } = await import('./BuildView')
    const { es } = await renderActiveBuild()

    await act(async () => {
      for (let i = 1; i <= 50; i++) es.emit('log', { message: `line ${i}` })
    })
    // Still buffered: an arrival on its own must not reach the DOM, or the
    // per-line render cost this guards against is back.
    expect(screen.queryByText('line 1')).toBeNull()

    await act(async () => {
      vi.advanceTimersByTime(LOG_FLUSH_MS)
    })
    // The whole burst lands together — coalescing must not drop lines.
    expect(screen.getByText('line 1')).toBeTruthy()
    expect(screen.getByText('line 50')).toBeTruthy()
  })

  it('strips ANSI escapes once at ingest rather than on every render', async () => {
    const { LOG_FLUSH_MS } = await import('./BuildView')
    const { es } = await renderActiveBuild()

    await act(async () => {
      es.emit('log', { message: '\x1b[32mINFO\x1b[0m chroot ready' })
      es.emit('log', { message: '\x1b[2K' }) // pure escape: renders as nothing
      vi.advanceTimersByTime(LOG_FLUSH_MS)
    })

    expect(screen.getByText('INFO chroot ready')).toBeTruthy()
  })

  it('flushes lines still buffered when the build concludes', async () => {
    const { es } = await renderActiveBuild()

    await act(async () => {
      es.emit('log', { message: 'the very last line' })
      // No timer advance: the build ends inside the flush window, which is
      // exactly when a tail would be lost if concluding did not flush.
      es.emit('complete', { status: 'success', artifacts: [] })
    })

    expect(screen.getByText('the very last line')).toBeTruthy()
  })

  it('keeps only the most recent lines in the DOM and reports the rest as hidden', async () => {
    const { LOG_FLUSH_MS, LOG_RENDER_LIMIT } = await import('./BuildView')
    const hidden = 120
    const total = LOG_RENDER_LIMIT + hidden
    const { container, es } = await renderActiveBuild()

    await act(async () => {
      for (let i = 1; i <= total; i++) es.emit('log', { message: `line ${i}` })
      vi.advanceTimersByTime(LOG_FLUSH_MS)
    })

    expect(screen.queryByText('line 1')).toBeNull()
    expect(screen.queryByText(`line ${hidden}`)).toBeNull()
    expect(screen.getByText(`line ${hidden + 1}`)).toBeTruthy() // first kept line
    expect(screen.getByText(`line ${total}`)).toBeTruthy()
    // The count is surfaced so a truncated pane never reads as the full log.
    expect(container.textContent).toContain(`${hidden} earlier lines are not shown`)
  })
})
