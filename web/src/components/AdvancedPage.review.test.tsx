// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The review step lists what this selection changes, open by default, and keeps
// the raw delta YAML behind a collapsed toggle.

import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AdvancedPage } from './AdvancedPage'
import { useStore } from '../store'
import type { ComposeRequest, ComposeResponse, Manifest } from '../api/types'

const mockApi = vi.hoisted(() => ({
  compose: vi.fn(),
  startBuild: vi.fn(),
  // What the Packages step loads on the way to Review.
  listPackageRepos: vi.fn(async () => ({ repos: [] })),
  getEdgePack: vi.fn(async () => null),
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

const DELTA = 'extends: ubuntu24/ubuntu24-x86_64-edgepack-unattended-iso.yml\n'

function composeResponse(req: ComposeRequest): ComposeResponse {
  return {
    template: TEMPLATE,
    yaml: 'image:\n  name: edgepack\n',
    deltaYaml: DELTA,
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: 'my-image', imageVersion: '24.04', description: '', architecture: 'x86_64', kernelVersion: '',
      packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
      advanced: { imageName: 'my-image', packages: ['vim'], repos: ['intel-graphics'] },
    },
  }
}

beforeEach(() => {
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => composeResponse(req))
  useStore.setState({
    manifest,
    selection: { vertical: 'platform-enablement', sku: 'edgepack', platform: 'wcl', os: 'ubuntu24', kernel: '', imageType: 'iso' },
  })
})

describe('AdvancedPage review', () => {
  it('shows the Advanced changes open and the delta YAML collapsed', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    for (let i = 0; i < 3; i++) fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    const changes = await screen.findByRole('button', { name: /Advanced changes/ })
    expect(changes.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByText('vim')).toBeTruthy()
    expect(screen.getByText('intel-graphics')).toBeTruthy()

    const yaml = screen.getByRole('button', { name: /Your changes/ })
    expect(yaml.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByText(/^extends:/)).toBeNull()

    fireEvent.click(yaml)
    expect(screen.getByText(/^extends:/)).toBeTruthy()
  })
})
