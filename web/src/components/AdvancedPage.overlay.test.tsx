// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// An overlay-mode template builds on a baseline image: the Advanced tab keeps
// that image's partition layout and holds the build while Edge Pack packages,
// which turn DKMS on, are selected. Overlay mode has to be known even when the
// overridden compose fails because of exactly those packages.

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AdvancedPage } from './AdvancedPage'
import { useStore } from '../store'
import { parseDiskFromYaml } from '../lib/disk'
import { minimalPtlPvRaw } from '../lib/fixtures'
import type { ComposeRequest, ComposeResponse, EdgePack, Manifest } from '../api/types'

const pack: EdgePack = {
  id: 'edge-pack',
  displayName: 'Edge Pack',
  repo: 'intel-eci',
  repoAvailable: true,
  baseRuntimes: [{ id: 'standard', displayName: 'Standard', available: true, package: { name: 'base-standard' } }],
  domains: [],
}

const mockApi = vi.hoisted(() => ({
  compose: vi.fn(),
  startBuild: vi.fn(),
  listPackageRepos: vi.fn(async () => ({ repos: [] })),
  getEdgePack: vi.fn(),
  searchPackages: vi.fn(async () => []),
  searchStreamUrl: () => '',
}))
vi.mock('../api/client', () => ({ api: mockApi }))

const TEMPLATE = 'ubuntu26/ubuntu26-x86_64-nvl-bkc-overlay-raw.yml'

const manifest: Manifest = {
  combinations: [
    { vertical: 'platform-enablement', sku: 'upstream-bkc', platform: 'nvl', os: 'ubuntu26', imageType: 'raw', template: TEMPLATE },
  ],
  verticals: [{ id: 'platform-enablement', displayName: 'Platform Enablement' }],
  skus: [{ id: 'upstream-bkc', displayName: 'Upstream BKC' }],
  platforms: [{ id: 'nvl', displayName: 'NVL (Nova Lake)' }],
  targets: [{ id: 'ubuntu26', displayName: 'Ubuntu 26.04', os: 'ubuntu', arch: 'x86_64' }],
}

function composeResponse(req: ComposeRequest): ComposeResponse {
  return {
    template: TEMPLATE,
    yaml: 'image:\n  name: ubuntu26-nvl-bkc-overlay\n',
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: 'ubuntu26-nvl-bkc-overlay', imageVersion: '26.04', description: '', architecture: 'x86_64',
      kernelVersion: '', packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
      baseImage: req.baseImage ?? '/path/to/ubuntu-26.04-base.raw',
      baseImageMissing: !req.baseImage,
    },
  }
}

beforeEach(() => {
  // As the server does: an Edge Pack package makes an overlay compose fail.
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => {
    if (req.packages?.some((p) => p.startsWith('base-standard'))) {
      throw new Error('matched template failed to load/validate: dkms is not supported in overlay mode')
    }
    return composeResponse(req)
  })
  mockApi.getEdgePack.mockReset().mockResolvedValue(pack)
  useStore.setState({
    manifest,
    selection: { vertical: 'platform-enablement', sku: 'upstream-bkc', platform: 'nvl', os: 'ubuntu26', kernel: '', imageType: 'raw' },
    // Kept from an earlier EdgePack selection on the same OS.
    addedPackages: [{ name: 'base-standard', version: '', repo: 'intel-eci' }],
    enabledRepos: [],
    // The failing compose seeds neither, so they are set up front.
    imageName: 'ubuntu26-nvl-bkc-overlay',
    imageNameEdited: true,
    disk: parseDiskFromYaml(minimalPtlPvRaw),
    diskEdited: false,
    baseImage: '',
    baseImageEdited: false,
  })
})

describe('AdvancedPage for an overlay template', () => {
  it('knows the mode and applies the safeguards even when the overridden compose fails', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Next' }).disabled).toBe(false))

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(await screen.findByText(/Edge Pack packages cannot be added/)).toBeTruthy()
    expect(screen.getByText('base-standard', { selector: 'span.font-mono' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(await screen.findByText(/keeps its partition\s+table and partitions/)).toBeTruthy()
    expect(screen.queryByText('Partition Table')).toBeNull()
  })

  it('asks for the base image under the selection, and again in the Compose dialog while it is wrong', async () => {
    useStore.setState({ addedPackages: [] })
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)

    // On the Target step, seeded with the template's placeholder, which the
    // server reports missing.
    const input = (await screen.findByLabelText(/Base Image/)) as HTMLInputElement
    await waitFor(() => expect(input.value).toBe('/path/to/ubuntu-26.04-base.raw'))
    expect((await screen.findByRole('alert')).textContent).toMatch(/placeholder/)

    await waitFor(() => expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Next' }).disabled).toBe(false))
    for (let i = 0; i < 3; i++) fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Compose Image' }))

    // The dialog asks for it too, and won't compose until it's right.
    const dialog = await screen.findByRole('dialog')
    const compose = () => within(dialog).getByRole<HTMLButtonElement>('button', { name: 'Compose Image' })
    expect(compose().disabled).toBe(true)
    fireEvent.change(within(dialog).getByLabelText(/Base Image/), { target: { value: '/images/base.raw' } })
    await waitFor(() =>
      expect(mockApi.compose).toHaveBeenCalledWith(expect.objectContaining({ baseImage: '/images/base.raw' })),
    )
    await waitFor(() => expect(compose().disabled).toBe(false))
  })
})
