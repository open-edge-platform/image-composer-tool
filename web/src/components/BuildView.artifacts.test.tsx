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

// The details panel only renders when there is a summary to show, so tests that
// assert on the panel supply one.
const SUMMARY = {
  vertical: 'retail',
  sku: 'desktop-virtualization',
  platform: 'arl',
  os: 'debian13',
  imageType: 'iso',
  imageName: 'debian13-x86_64-desktop-virtualization',
  imageVersion: '1.0.0',
  architecture: 'x86_64',
  packageCount: 128,
}

const DETAILS = {
  buildId: 'abc',
  status: 'success',
  command: 'sudo -n ict build minimal.yml',
  template: 'minimal.yml',
  templateUrl: '/api/v1/builds/abc/template',
  templatePath: '/var/tmp/ict/builds/abc/template.yml',
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

// cellsOf returns every cell's text for the row naming `artifactName`.
function cellsOf(artifactName: string): string[] {
  const row = screen.getByText(artifactName).closest('tr')
  if (!row) throw new Error(`no row for ${artifactName}`)
  return within(row)
    .getAllByRole('cell')
    .map((c) => c.textContent?.trim() ?? '')
}

// downloadHrefOf returns the row's download link target.
function downloadHrefOf(artifactName: string): string {
  const row = screen.getByText(artifactName).closest('tr')
  if (!row) throw new Error(`no row for ${artifactName}`)
  const link = within(row).getByRole('link') as HTMLAnchorElement
  return link.getAttribute('href') ?? ''
}

// rowNames returns the Name column, top to bottom.
function rowNames(): string[] {
  return screen
    .getAllByRole('row')
    .slice(1) // drop the header
    .map((r) => within(r).getAllByRole('cell')[0]?.textContent?.trim() ?? '')
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

// The template the build ran against is listed in this table rather than in the
// Compose details panel, which used to carry it alongside the raw command. It is
// not a build output, so it has no size and is served by its own endpoint.
describe('BuildView template row', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', FakeEventSource)
    FakeEventSource.last = null
    buildDetails.mockResolvedValue(DETAILS)
    buildArtifacts.mockResolvedValue([IMAGE_ARTIFACT, SBOM_ARTIFACT])
    logFileText.mockResolvedValue('')
  })

  it('lists the template with its own type, path and download link', async () => {
    await renderBuildView(false)

    await screen.findByText(DETAILS.template)
    expect(cellsOf(DETAILS.template)).toEqual([
      'minimal.yml',
      'TEMPLATE',
      '—', // not a build output, so the server reports no size
      DETAILS.templatePath,
      '',
    ])
    expect(downloadHrefOf(DETAILS.template)).toBe(DETAILS.templateUrl)
  })

  it('appends the template after the build outputs', async () => {
    await renderBuildView(false)

    await screen.findByText(DETAILS.template)
    expect(rowNames()).toEqual([IMAGE_ARTIFACT.name, SBOM_ARTIFACT.name, DETAILS.template])
  })

  // A build whose outputs were cleaned up, or one that failed before producing
  // any, still has a template — the table must appear for it.
  it('shows the table for a build with no artifacts of its own', async () => {
    buildArtifacts.mockResolvedValue([])
    await renderBuildView(false)

    await screen.findByText(DETAILS.template)
    expect(rowNames()).toEqual([DETAILS.template])
    expect(typeOf(DETAILS.template)).toBe('TEMPLATE')
  })

  // templatePath is omitempty in the contract, so an older server sends none.
  it('renders a dash when the server reports no template path', async () => {
    buildDetails.mockResolvedValue({ ...DETAILS, templatePath: undefined })
    await renderBuildView(false)

    await screen.findByText(DETAILS.template)
    expect(cellsOf(DETAILS.template)[3]).toBe('—')
    expect(downloadHrefOf(DETAILS.template)).toBe(DETAILS.templateUrl)
  })

  it('keeps the download link pointing at the artifact endpoint for real outputs', async () => {
    await renderBuildView(false)

    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(downloadHrefOf(IMAGE_ARTIFACT.name)).toBe(
      `/api/v1/builds/abc/artifacts/${encodeURIComponent(IMAGE_ARTIFACT.name)}`,
    )
  })

  it('no longer shows the raw command or a Template line in the expanded details panel', async () => {
    buildDetails.mockResolvedValue({ ...DETAILS, summary: SUMMARY })
    await renderBuildView(false)
    await screen.findByText(DETAILS.template)

    // The panel is collapsed by default, so open it before asserting absence —
    // otherwise this passes for the wrong reason.
    await act(async () => {
      screen.getByRole('button', { name: /Compose details/ }).click()
    })
    expect(screen.getByText(/your selection, image configuration/)).toBeTruthy()

    expect(screen.queryByText(DETAILS.command)).toBeNull()
    expect(screen.queryByText('Command')).toBeNull()
    // "Template" as a standalone label is gone; the name now appears only as the
    // artifact row's Name cell.
    expect(screen.queryByText('Template')).toBeNull()
    expect(screen.getAllByText(DETAILS.template)).toHaveLength(1)
  })
})

// A build with no configuration summary (a YAML submission) has nothing left to
// put in the details panel now that the command and template have moved out, so
// the expandable is not offered at all.
describe('BuildView details panel', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', FakeEventSource)
    FakeEventSource.last = null
    buildDetails.mockResolvedValue(DETAILS)
    buildArtifacts.mockResolvedValue([IMAGE_ARTIFACT])
    logFileText.mockResolvedValue('')
  })

  it('is not offered when the build has no summary', async () => {
    await renderBuildView(false)

    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(screen.queryByRole('button', { name: /Compose details/ })).toBeNull()
  })

  it('is offered when the build has a summary', async () => {
    buildDetails.mockResolvedValue({ ...DETAILS, summary: SUMMARY })
    await renderBuildView(false)

    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(screen.getByRole('button', { name: /Compose details/ })).toBeTruthy()
  })
})
