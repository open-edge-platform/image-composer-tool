// Artifact-table tests for BuildView.
//
// The artifact type shown to the user must be whatever the server reported. The
// UI previously applied an `uppercase` CSS class to a value it never questioned,
// which hid the real bug — every artifact reaching it was already typed "image"
// server-side — so these tests pin the contract at the UI boundary: each row
// shows its own type, and the reloaded-history view agrees with the live
// completion view.
import { act, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Artifact } from '../api/types'

const buildDetails = vi.fn()
const buildArtifacts = vi.fn()
const logFileText = vi.fn()

// Stub only the three calls this view makes to load a build; the rest of the
// client (URL builders) stays real so the mock doesn't drift as it grows.
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

// Minimal EventSource stand-in: jsdom has none, and these tests only need to
// deliver a single 'complete' event to the live view.
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

const IMAGE_ARTIFACT: Artifact = {
  name: 'minimal-os-image-ubuntu-26.04.raw.gz',
  type: 'image',
  path: '/var/tmp/ict/builds/abc/imagebuild/minimal/minimal-os-image-ubuntu-26.04.raw.gz',
  size: '1.13 GB',
}
const SBOM_ARTIFACT: Artifact = {
  name: 'spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json',
  type: 'sbom',
  path: '/var/tmp/ict/builds/abc/imagebuild/minimal/spdx_manifest_deb_minimal-os-image-ubuntu_20260707_165343.json',
  size: '204 KB',
}

const DETAILS = {
  buildId: 'abc',
  status: 'success',
  command: 'sudo -n ict build minimal.yml',
  template: 'minimal.yml',
  templateUrl: '/api/v1/builds/abc/template',
  workDir: '/var/tmp/ict/builds/abc/work',
  cacheDir: '/var/tmp/ict/builds/abc/cache',
  hasLogFile: true,
}

// typeOf returns the rendered Type cell for the row naming `artifactName`.
function typeOf(artifactName: string): string {
  const row = screen.getByText(artifactName).closest('tr')
  if (!row) throw new Error(`no row for ${artifactName}`)
  return within(row).getAllByRole('cell')[1].textContent ?? ''
}

// Renders the view and lets its mount-time loads settle. The component fires
// several fetches on mount, so the render is wrapped in act() to flush their
// resolutions before a test asserts.
async function renderBuildView(isActive: boolean) {
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
        isActive={isActive}
      />,
    )
  })
  return result
}

describe('BuildView artifact types', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', FakeEventSource)
    FakeEventSource.last = null
    buildDetails.mockResolvedValue(DETAILS)
    buildArtifacts.mockResolvedValue([IMAGE_ARTIFACT, SBOM_ARTIFACT])
    logFileText.mockResolvedValue('')
  })

  it('shows each artifact with the type the server reported (history view)', async () => {
    await renderBuildView(false)

    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(typeOf(IMAGE_ARTIFACT.name)).toBe('IMAGE')
    expect(typeOf(SBOM_ARTIFACT.name)).toBe('SBOM')
  })

  it('shows each artifact with the type the server reported (live completion)', async () => {
    await renderBuildView(true)

    await waitFor(() => expect(FakeEventSource.last).not.toBeNull())
    await act(async () => {
      FakeEventSource.last!.emit('complete', {
        status: 'success',
        artifacts: [IMAGE_ARTIFACT, SBOM_ARTIFACT],
      })
    })

    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(typeOf(IMAGE_ARTIFACT.name)).toBe('IMAGE')
    expect(typeOf(SBOM_ARTIFACT.name)).toBe('SBOM')
  })

  it('does not relabel an SBOM as an image when it is the only artifact', async () => {
    buildArtifacts.mockResolvedValue([SBOM_ARTIFACT])
    await renderBuildView(false)

    await screen.findByText(SBOM_ARTIFACT.name)
    expect(typeOf(SBOM_ARTIFACT.name)).toBe('SBOM')
    expect(screen.queryByText('IMAGE')).toBeNull()
  })

  it('surfaces a type it does not recognise instead of defaulting it to IMAGE', async () => {
    const future: Artifact = {
      name: 'minimal-os.attestation.jsonl',
      type: 'attestation',
      path: '/var/tmp/ict/builds/abc/imagebuild/minimal/minimal-os.attestation.jsonl',
      size: '4 KB',
    }
    const unknown: Artifact = {
      name: 'UPLOAD-MANIFEST.txt',
      type: 'unknown',
      path: '/var/tmp/ict/builds/abc/imagebuild/UPLOAD-MANIFEST.txt',
      size: '3.81 KB',
    }
    buildArtifacts.mockResolvedValue([IMAGE_ARTIFACT, future, unknown])
    await renderBuildView(false)

    await screen.findByText(future.name)
    expect(typeOf(future.name)).toBe('ATTESTATION')
    expect(typeOf(unknown.name)).toBe('UNKNOWN')
    expect(typeOf(IMAGE_ARTIFACT.name)).toBe('IMAGE')
  })

  it('reports a missing type as UNKNOWN rather than IMAGE', async () => {
    buildArtifacts.mockResolvedValue([
      { name: 'mystery.bin', path: '/var/tmp/ict/builds/abc/mystery.bin' } as Artifact,
    ])
    await renderBuildView(false)

    await screen.findByText('mystery.bin')
    expect(typeOf('mystery.bin')).toBe('UNKNOWN')
  })

  it('history and live views render identical types for the same artifacts', async () => {
    const arts = [IMAGE_ARTIFACT, SBOM_ARTIFACT]

    const history = await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)
    const fromHistory = arts.map((a) => typeOf(a.name))
    history.unmount()

    await renderBuildView(true)
    await waitFor(() => expect(FakeEventSource.last).not.toBeNull())
    await act(async () => {
      FakeEventSource.last!.emit('complete', { status: 'success', artifacts: arts })
    })
    await screen.findByText(IMAGE_ARTIFACT.name)
    const fromLive = arts.map((a) => typeOf(a.name))

    expect(fromLive).toEqual(fromHistory)
    expect(fromLive).toEqual(['IMAGE', 'SBOM'])
  })
})
