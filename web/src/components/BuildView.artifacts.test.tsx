// Artifact-table tests for BuildView.
//
// The Artifact column must follow whatever type the server reported: "Image"
// for an image, "Bill of Materials" for an SBOM, and any other type shown as
// itself. The UI previously applied an `uppercase` CSS class to a value it never
// questioned, which hid the real bug — every artifact reaching it was already
// typed "image" server-side — so these tests pin the contract at the UI
// boundary: each row shows its own type, and the reloaded-history view agrees
// with the live completion view.
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

// typeOf returns the rendered Artifact cell for the row naming `artifactName`.
function typeOf(artifactName: string): string {
  const row = screen.getByText(artifactName).closest('tr')
  if (!row) throw new Error(`no row for ${artifactName}`)
  return within(row).getAllByRole('cell')[0].textContent ?? ''
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
    .map((r) => within(r).getAllByRole('cell')[1]?.textContent?.trim() ?? '')
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
    expect(typeOf(IMAGE_ARTIFACT.name)).toBe('Image')
    expect(typeOf(SBOM_ARTIFACT.name)).toBe('Bill of Materials')
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
    expect(typeOf(IMAGE_ARTIFACT.name)).toBe('Image')
    expect(typeOf(SBOM_ARTIFACT.name)).toBe('Bill of Materials')
  })

  it('does not relabel an SBOM as an image when it is the only artifact', async () => {
    buildArtifacts.mockResolvedValue([SBOM_ARTIFACT])
    await renderBuildView(false)

    await screen.findByText(SBOM_ARTIFACT.name)
    expect(typeOf(SBOM_ARTIFACT.name)).toBe('Bill of Materials')
    expect(screen.queryByText('Image')).toBeNull()
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
    expect(typeOf(IMAGE_ARTIFACT.name)).toBe('Image')
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
    expect(fromLive).toEqual(['Image', 'Bill of Materials'])
  })
})

describe('BuildView artifacts table layout', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', FakeEventSource)
    FakeEventSource.last = null
    buildDetails.mockResolvedValue(DETAILS)
    buildArtifacts.mockResolvedValue([IMAGE_ARTIFACT, SBOM_ARTIFACT])
    logFileText.mockResolvedValue('')
  })

  it('is headed Artifacts, with Artifact, Name, Size, Location and Actions columns', async () => {
    await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)

    expect(screen.getByRole('heading', { name: 'Artifacts' })).toBeTruthy()
    expect(
      screen.getAllByRole('columnheader').map((h) => h.textContent?.trim()),
    ).toEqual(['Artifact', 'Name', 'Size', 'Location', 'Actions'])
  })

  it('shows each row as kind, file name, size, location, then the download', async () => {
    await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)

    expect(cellsOf(IMAGE_ARTIFACT.name)).toEqual([
      'Image',
      IMAGE_ARTIFACT.name,
      IMAGE_ARTIFACT.size,
      IMAGE_ARTIFACT.path,
      '',
    ])
    expect(cellsOf(SBOM_ARTIFACT.name)).toEqual([
      'Bill of Materials',
      SBOM_ARTIFACT.name,
      SBOM_ARTIFACT.size,
      SBOM_ARTIFACT.path,
      '',
    ])
  })

  it('is headed Partial artifacts after a failed build', async () => {
    buildDetails.mockResolvedValue({ ...DETAILS, status: 'failed', errMsg: 'boom' })
    await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)

    expect(screen.getByRole('heading', { name: 'Partial artifacts' })).toBeTruthy()
  })
})

// The template the build ran against is shown in the Compose details panel, not
// in the artifacts table: the table lists what the build produced, and the
// template is an input. The panel's Template line keeps its own download link,
// served by the template endpoint rather than the by-name artifact download.
describe('BuildView template in Compose details', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', FakeEventSource)
    FakeEventSource.last = null
    buildDetails.mockResolvedValue(DETAILS)
    buildArtifacts.mockResolvedValue([IMAGE_ARTIFACT, SBOM_ARTIFACT])
    logFileText.mockResolvedValue('')
  })

  async function openDetails() {
    await act(async () => {
      screen.getByRole('button', { name: /Compose details/ }).click()
    })
  }

  it('does not list the template in the artifacts table', async () => {
    await renderBuildView(false)

    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(rowNames()).toEqual([IMAGE_ARTIFACT.name, SBOM_ARTIFACT.name])
    expect(screen.queryByText('TEMPLATE')).toBeNull()
    // Collapsed panel: the template name appears nowhere on the page yet.
    expect(screen.queryByText(DETAILS.template)).toBeNull()
  })

  it('shows no artifacts table for a build with no outputs of its own', async () => {
    buildArtifacts.mockResolvedValue([])
    await renderBuildView(false)

    await screen.findByRole('button', { name: /Compose details/ })
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('shows a Template line with a download link in the expanded panel', async () => {
    buildDetails.mockResolvedValue({ ...DETAILS, summary: SUMMARY })
    await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)

    expect(screen.getByText(/template, paths/)).toBeTruthy()
    await openDetails()

    const label = screen.getByText('Template')
    const line = label.parentElement as HTMLElement
    expect(within(line).getByText(DETAILS.template)).toBeTruthy()
    const link = within(line).getByRole('link', { name: /Download template/ }) as HTMLAnchorElement
    expect(link.getAttribute('href')).toBe(DETAILS.templateUrl)
    expect(link.getAttribute('download')).toBe(DETAILS.template)
    // The template appears once — in the panel, not in the table as well.
    expect(screen.getAllByText(DETAILS.template)).toHaveLength(1)
  })

  // A YAML-submitted build has no summary but still ran against a template, so
  // the panel is offered for the Template line alone.
  it('offers the panel and the template for a build with no summary', async () => {
    await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)

    await openDetails()
    expect(screen.queryByText('Basic Selection')).toBeNull()
    expect(screen.getByText(DETAILS.template)).toBeTruthy()
  })

  it('keeps the download link pointing at the artifact endpoint for real outputs', async () => {
    await renderBuildView(false)

    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(downloadHrefOf(IMAGE_ARTIFACT.name)).toBe(
      `/api/v1/builds/abc/artifacts/${encodeURIComponent(IMAGE_ARTIFACT.name)}`,
    )
  })

  it('does not show the raw command in the expanded details panel', async () => {
    buildDetails.mockResolvedValue({ ...DETAILS, summary: SUMMARY })
    await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)
    expect(screen.getByText(/template, paths/)).toBeTruthy()

    await openDetails()
    expect(screen.queryByText(DETAILS.command)).toBeNull()
    expect(screen.queryByText('Command')).toBeNull()
  })

  it('lists each Advanced-tab change by name, capping long lists behind Show more', async () => {
    const packages = Array.from({ length: 11 }, (_, i) => `pkg-${i}`)
    buildDetails.mockResolvedValue({
      ...DETAILS,
      summary: {
        ...SUMMARY,
        advanced: {
          imageName: 'my-image',
          packages,
          repos: ['intel-gpu'],
          disk: { size: '32GiB', partitionTable: 'gpt', partitions: [{ id: 'root', fsType: 'ext4', mountPoint: '/' }] },
          newUsers: ['alice'],
        },
      },
    })
    await renderBuildView(false)
    await screen.findByText(IMAGE_ARTIFACT.name)
    await openDetails()
    // Open by default: the entries are listed without expanding the card.
    const toggle = screen.getByRole('button', { name: /Advanced customization/ })
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(toggle.textContent).toContain('image name, 11 packages, 1 repo, disk layout, 1 new user')

    expect(screen.getByText('my-image')).toBeTruthy()
    expect(screen.getByText('intel-gpu')).toBeTruthy()
    expect(screen.getByText('alice')).toBeTruthy()
    expect(screen.getByText('root · ext4 · /')).toBeTruthy()
    expect(screen.getByText('pkg-7')).toBeTruthy()
    expect(screen.queryByText('pkg-8')).toBeNull()

    await act(async () => {
      screen.getByRole('button', { name: 'Show 3 more' }).click()
    })
    expect(screen.getByText('pkg-10')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Show less' })).toBeTruthy()
  })
})
