// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// BuildView with a remote build executor: artifacts that live at a URL link
// there directly, and the stepper draws the executor's own phases.
import { act, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Artifact, ServerInfo } from '../api/types'
import { resetServerInfo } from '../lib/serverInfo'

const buildDetails = vi.fn()
const buildArtifacts = vi.fn()
const logFileText = vi.fn()
const getServerInfo = vi.fn()

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      buildDetails: (...a: unknown[]) => buildDetails(...a),
      buildArtifacts: (...a: unknown[]) => buildArtifacts(...a),
      logFileText: (...a: unknown[]) => logFileText(...a),
      getServerInfo: (...a: unknown[]) => getServerInfo(...a),
    },
  }
})

class FakeEventSource {
  static last: FakeEventSource | null = null
  listeners = new Map<string, (e: MessageEvent) => void>()
  url: string
  constructor(url: string) {
    this.url = url
    FakeEventSource.last = this
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, fn)
  }
  close() {}
  emit(type: string, data: unknown) {
    this.listeners.get(type)?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

const REMOTE_INFO: ServerInfo = {
  version: '2026.3.0',
  commit: 'abc',
  buildDate: '2026-11-11',
  executor: {
    name: 'jenkins-farm',
    maxConcurrent: 10,
    phases: [
      { id: 'dispatching', label: 'Preparing infra' },
      { id: 'building', label: 'Building image' },
      { id: 'publishing', label: 'Publishing artifacts' },
    ],
  },
  auth: { mode: 'trusted-header' },
}

const PUBLISHED: Artifact = {
  name: 'edgepack-os-image-ubuntu-unattended-26.04.iso',
  type: 'image',
  url: 'https://artifacts.example.com/worker-01/20261111-1637/edgepack-os-image-ubuntu-unattended-26.04.iso',
  size: '3.20 GB',
}

const DETAILS = {
  buildId: 'abc',
  status: 'running',
  command: '',
  template: 'edgepack.yml',
  templateUrl: '/api/v1/builds/abc/template',
  workDir: '/w',
  cacheDir: '/c',
  hasLogFile: false,
  executor: 'jenkins-farm',
  meta: { worker: 'worker-01' },
}

async function renderActive() {
  const { BuildView } = await import('./BuildView')
  await act(async () => {
    render(
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
}

describe('BuildView with a remote executor', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', FakeEventSource)
    FakeEventSource.last = null
    resetServerInfo()
    getServerInfo.mockResolvedValue(REMOTE_INFO)
    buildDetails.mockResolvedValue(DETAILS)
    buildArtifacts.mockResolvedValue([])
    logFileText.mockResolvedValue('')
  })

  it('draws the stepper from the executor phases and follows phase events', async () => {
    await renderActive()
    await screen.findByText('Preparing infra')
    expect(screen.getByText('Building image')).toBeTruthy()
    expect(screen.getByText('Publishing artifacts')).toBeTruthy()
    expect(screen.queryByText('Resolving & downloading packages')).toBeNull()

    await waitFor(() => expect(FakeEventSource.last).not.toBeNull())
    await act(async () => {
      FakeEventSource.last!.emit('phase', { phase: 'publishing', label: 'Publishing artifacts' })
    })
    const active = screen.getByText('Publishing artifacts')
    expect(active.className).toContain('font-semibold')
  })

  it('links a published artifact to its URL instead of the download endpoint', async () => {
    // Completion refreshes the list from the server, so the server has to agree.
    buildArtifacts.mockResolvedValue([PUBLISHED])
    await renderActive()
    await waitFor(() => expect(FakeEventSource.last).not.toBeNull())
    await act(async () => {
      FakeEventSource.last!.emit('complete', { status: 'success', artifacts: [PUBLISHED] })
    })
    const row = (await screen.findByText(PUBLISHED.name)).closest('tr')!
    const link = within(row).getByRole('link') as HTMLAnchorElement
    expect(link.getAttribute('href')).toBe(PUBLISHED.url)
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.hasAttribute('download')).toBe(false)
    expect(within(row).getByText(PUBLISHED.url!)).toBeTruthy()
  })
})
