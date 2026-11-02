// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Advanced wizard must not move past Target, or mark it done, while a
// compulsory field is empty — and those fields carry a required asterisk.

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AdvancedPage } from './AdvancedPage'
import { useStore } from '../store'
import type { ComposeRequest, ComposeResponse, EdgePack, Manifest } from '../api/types'
import { edgepackIso } from '../lib/fixtures'

const mockApi = vi.hoisted(() => ({
  compose: vi.fn(),
  startBuild: vi.fn(),
  listPackageRepos: vi.fn(),
  getEdgePack: vi.fn(),
  searchPackages: vi.fn(async () => []),
  searchStreamUrl: () => '',
}))
vi.mock('../api/client', () => ({ api: mockApi }))

const TEMPLATE = 'ubuntu24/ubuntu24-x86_64-edgepack-unattended-iso.yml'

const manifest: Manifest = {
  combinations: [
    { vertical: 'platform-enablement', sku: 'edgepack', platform: 'wcl', os: 'ubuntu24', imageType: 'iso', template: TEMPLATE },
  ],
  verticals: [{ id: 'platform-enablement', displayName: 'Platform Enablement' }],
  skus: [{ id: 'edgepack', displayName: 'EdgePack' }],
  platforms: [{ id: 'wcl', displayName: 'WCL' }],
  targets: [{ id: 'ubuntu24', displayName: 'Ubuntu 24.04', os: 'ubuntu', arch: 'x86_64' }],
}

function composeResponse(req: ComposeRequest): ComposeResponse {
  return {
    template: TEMPLATE,
    yaml: 'image:\n  name: edgepack\n',
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: 'edgepack', imageVersion: '24.04', description: '', architecture: 'x86_64', kernelVersion: '',
      packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
    },
  }
}

const FULL = { vertical: 'platform-enablement', sku: 'edgepack', platform: 'wcl', os: 'ubuntu24', kernel: '', imageType: 'iso' }

const nextButton = () => screen.getByRole<HTMLButtonElement>('button', { name: 'Next' })
const pill = (label: string) => screen.getByRole<HTMLButtonElement>('button', { name: new RegExp(label) })

beforeEach(() => {
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => composeResponse(req))
  mockApi.getEdgePack.mockReset().mockResolvedValue(null)
  mockApi.listPackageRepos.mockReset().mockResolvedValue({ repos: [] })
  useStore.setState({
    manifest, selection: FULL, imageName: '', imageNameEdited: false,
    disk: null, diskEdited: false, diskSeed: null,
  })
})

describe('AdvancedPage Target step gating', () => {
  it('blocks Next and later steps while a required selection is missing', () => {
    useStore.setState({ selection: { ...FULL, platform: '', os: '' } })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)

    expect(nextButton().disabled).toBe(true)
    expect(pill('Packages').disabled).toBe(true)
    expect(pill('Review').disabled).toBe(true)
    expect(screen.getByText(/Fill in the required Target fields: Platform, Operating System, Image Name\./)).toBeTruthy()
  })

  it('blocks Next when the Image Name is cleared', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))

    fireEvent.change(screen.getByLabelText(/^Image Name/), { target: { value: '   ' } })
    expect(nextButton().disabled).toBe(true)
    expect(screen.getByText(/Fill in the required Target fields: Image Name\./)).toBeTruthy()
  })

  it('marks Target done only once every required field is filled', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    expect(pill('Target').className).toContain('bg-emerald-500')

    // Cleared from the shared store (e.g. via Basic) while on a later step.
    useStore.setState({ imageName: '' })
    await waitFor(() => expect(pill('Target').className).not.toContain('bg-emerald-500'))
    expect(nextButton().disabled).toBe(true)
  })
})

describe('AdvancedPage Target step required markers', () => {
  it('marks every compulsory Target field with an asterisk', () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    for (const label of ['Use Case', 'SKU', 'Platform', 'Operating System', 'Image Name']) {
      expect(screen.getByText(label, { selector: 'label' }).textContent).toBe(`${label} *`)
    }
  })
})

describe('AdvancedPage Packages step status', () => {
  const goPastPackages = async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    // Nothing on Packages is compulsory, so Next is available straight away.
    expect(nextButton().disabled).toBe(false)
    fireEvent.click(nextButton())
  }

  it('stays grey when nothing was changed on the Packages step', async () => {
    useStore.setState({ addedPackages: [], enabledRepos: [] })
    await goPastPackages()
    expect(pill('Packages').className).not.toContain('bg-emerald-500')
    expect(pill('Target').className).toContain('bg-emerald-500')
  })

  it('turns green once a package was added', async () => {
    useStore.setState({ addedPackages: [{ name: 'curl', version: '', repo: 'ubuntu24' }], enabledRepos: [] })
    await goPastPackages()
    expect(pill('Packages').className).toContain('bg-emerald-500')
  })
})

describe('AdvancedPage Packages step issues', () => {
  const pack: EdgePack = {
    id: 'edge-pack',
    displayName: 'Edge Pack',
    repo: 'intel-eci',
    repoAvailable: true,
    baseRuntimes: [{ id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } }],
    domains: [{ id: 'media', displayName: 'Media', available: true, packages: [{ name: 'media-ffmpeg' }] }],
  }
  const media = { name: 'media-ffmpeg', version: '', repo: 'intel-eci' }
  const runtime = { name: 'base-standard', version: '', repo: 'intel-eci' }

  const visitPackagesThenLeave = async () => {
    mockApi.getEdgePack.mockResolvedValue(pack)
    mockApi.listPackageRepos.mockResolvedValue({
      repos: [{ id: 'intel-eci', displayName: 'Intel ECI', url: 'https://example.com', enabledByDefault: false }],
    })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    await screen.findByText(/Media/)
  }

  it('turns red for domain packages selected with no base runtime', async () => {
    useStore.setState({ addedPackages: [media], enabledRepos: ['intel-eci'] })
    await visitPackagesThenLeave()
    expect(screen.getByText(/which will not build/)).toBeTruthy()
    // Nothing on Packages is compulsory, so the issue does not block Next.
    expect(nextButton().disabled).toBe(false)
    fireEvent.click(nextButton())

    expect(pill('Packages').className).toContain('bg-red-500')
    expect(pill('Packages').textContent).toContain('!')
  })

  it('stays green when the domain packages have a base runtime', async () => {
    useStore.setState({ addedPackages: [runtime, media], enabledRepos: ['intel-eci'] })
    await visitPackagesThenLeave()
    fireEvent.click(nextButton())

    expect(pill('Packages').className).toContain('bg-emerald-500')
  })
})

describe('AdvancedPage Disk step status', () => {
  const goToDisk = async () => {
    mockApi.compose.mockImplementation(async (req: ComposeRequest) => ({ ...composeResponse(req), yaml: edgepackIso }))
    useStore.setState({ addedPackages: [], enabledRepos: [] })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    fireEvent.click(nextButton())
    return screen.findByLabelText<HTMLInputElement>(/^Disk Name/)
  }

  it('marks the compulsory Disk fields with an asterisk', async () => {
    const name = await goToDisk()
    expect(name.labels?.[0].textContent).toBe('Disk Name *')
    fireEvent.click(screen.getByRole('button', { name: /Partitions/ }))
    expect(screen.getByText('FS').textContent).toBe('FS *')
  })

  it('stays grey when the template layout was left as it is', async () => {
    await goToDisk()
    fireEvent.click(nextButton())
    expect(pill('Disk').className).not.toContain('bg-emerald-500')
    expect(pill('Disk').className).not.toContain('bg-red-500')
  })

  it('turns green once the layout was edited without errors', async () => {
    const name = await goToDisk()
    fireEvent.change(name, { target: { value: 'My_Disk' } })
    fireEvent.click(nextButton())
    expect(pill('Disk').className).toContain('bg-emerald-500')
  })

  it('blocks Next and turns red while a compulsory field is empty', async () => {
    const name = await goToDisk()
    fireEvent.change(name, { target: { value: '' } })
    expect(screen.getByText('Disk name is required.')).toBeTruthy()
    expect(nextButton().disabled).toBe(true)
    expect(pill('Review').disabled).toBe(true)
    expect(screen.getByText('Fix the errors on the Disk step before continuing.')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(pill('Disk').className).toContain('bg-red-500')
  })
})

describe('AdvancedPage Packages step issues during a recompose', () => {
  it('does not turn red while the template package list is still loading', async () => {
    const pack: EdgePack = {
      id: 'edge-pack',
      displayName: 'Edge Pack',
      repo: 'intel-eci',
      repoAvailable: true,
      baseRuntimes: [{ id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } }],
      domains: [{ id: 'media', displayName: 'Media', available: true, packages: [{ name: 'media-ffmpeg' }] }],
    }
    mockApi.getEdgePack.mockResolvedValue(pack)
    mockApi.listPackageRepos.mockResolvedValue({
      repos: [{ id: 'intel-eci', displayName: 'Intel ECI', url: 'https://example.com', enabledByDefault: false }],
    })
    // The template ships the runtime itself, so a domain package added on top
    // is not stranded. Only the first compose answers; the recompose an added
    // package triggers stays in flight, leaving basePackages unknown.
    mockApi.compose
      .mockImplementationOnce(async (req: ComposeRequest) => ({ ...composeResponse(req), basePackages: ['base-standard'] }))
      .mockImplementation(() => new Promise(() => {}))
    useStore.setState({ addedPackages: [], enabledRepos: [] })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    await screen.findByText(/Media/)

    useStore.setState({ addedPackages: [{ name: 'media-ffmpeg', version: '', repo: 'intel-eci' }], enabledRepos: ['intel-eci'] })
    await waitFor(() => expect(mockApi.compose).toHaveBeenCalledTimes(2))
    fireEvent.click(nextButton())

    expect(pill('Packages').className).not.toContain('bg-red-500')
  })
})

describe('AdvancedPage Packages status survives the step unmounting', () => {
  const pack: EdgePack = {
    id: 'edge-pack',
    displayName: 'Edge Pack',
    repo: 'intel-eci',
    repoAvailable: true,
    baseRuntimes: [{ id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } }],
    domains: [{ id: 'media', displayName: 'Media', available: true, packages: [{ name: 'media-ffmpeg' }] }],
  }
  const media = { name: 'media-ffmpeg', version: '', repo: 'intel-eci' }
  const runtime = { name: 'base-standard', version: '', repo: 'intel-eci' }
  const never = () => new Promise<never>(() => {})

  beforeEach(() => {
    mockApi.listPackageRepos.mockResolvedValue({
      repos: [{ id: 'intel-eci', displayName: 'Intel ECI', url: 'https://example.com', enabledByDefault: false }],
    })
  })

  it('turns red for a runtime cleared while a recompose is in flight', async () => {
    mockApi.getEdgePack.mockResolvedValue(pack)
    // Only the first compose answers; every recompose after it stays in flight.
    mockApi.compose
      .mockImplementationOnce(async (req: ComposeRequest) => ({ ...composeResponse(req), basePackages: [] }))
      .mockImplementation(never)
    useStore.setState({ addedPackages: [runtime, media], enabledRepos: ['intel-eci'] })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    await screen.findByText(/Media/)

    // Start a recompose first, so the runtime is cleared while one is in flight.
    useStore.setState({ enabledRepos: ['intel-eci', 'extra'] })
    await waitFor(() => expect(mockApi.compose).toHaveBeenCalledTimes(2))
    useStore.setState({ addedPackages: [media] })
    fireEvent.click(nextButton())

    expect(pill('Packages').className).toContain('bg-red-500')
  })

  it('stays red when the step is revisited and left before its Edge Pack reloads', async () => {
    // The page's own fetch and the first visit answer; the revisit's never does.
    mockApi.getEdgePack.mockResolvedValueOnce(pack).mockResolvedValueOnce(pack).mockImplementation(never)
    useStore.setState({ addedPackages: [media], enabledRepos: ['intel-eci'] })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    await screen.findByText(/which will not build/)
    fireEvent.click(nextButton())
    expect(pill('Packages').className).toContain('bg-red-500')

    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    fireEvent.click(nextButton())

    expect(pill('Packages').className).toContain('bg-red-500')
  })
})

describe('AdvancedPage Target step compose failure', () => {
  it('shows the compose error on the Target step that it blocks', async () => {
    mockApi.compose.mockRejectedValue(new Error('template not found'))
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)

    expect(await screen.findByText('template not found')).toBeTruthy()
    expect(nextButton().disabled).toBe(true)
  })
})

describe('AdvancedPage Edge Pack fetch failure', () => {
  it('retries a failed Edge Pack fetch instead of treating it as no Edge Pack', async () => {
    mockApi.listPackageRepos.mockResolvedValue({
      repos: [{ id: 'intel-eci', displayName: 'Intel ECI', url: 'https://example.com', enabledByDefault: false }],
    })
    const pack: EdgePack = {
      id: 'edge-pack',
      displayName: 'Edge Pack',
      repo: 'intel-eci',
      repoAvailable: true,
      baseRuntimes: [{ id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } }],
      domains: [{ id: 'media', displayName: 'Media', available: true, packages: [{ name: 'media-ffmpeg' }] }],
    }
    // The page's first fetch fails; everything after it, the step's own included, succeeds.
    mockApi.getEdgePack.mockRejectedValueOnce(new Error('timeout')).mockResolvedValue(pack)
    useStore.setState({ addedPackages: [{ name: 'media-ffmpeg', version: '', repo: 'intel-eci' }], enabledRepos: ['intel-eci'] })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(nextButton().disabled).toBe(false))
    fireEvent.click(nextButton())
    await screen.findByText(/which will not build/)

    fireEvent.click(nextButton())
    await waitFor(() => expect(pill('Packages').className).toContain('bg-red-500'))
  })
})
