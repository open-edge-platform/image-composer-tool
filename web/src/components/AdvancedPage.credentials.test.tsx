// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Logins typed into the credentials dialog belong to one build. Kept afterwards,
// they rode along on the review compose, marked the account satisfied and let
// the next Compose Image start without asking.

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

// Like the server: the admin account counts as satisfied once a request carries a login for it.
function composeResponse(req: ComposeRequest): ComposeResponse {
  return {
    template: TEMPLATE,
    yaml: 'image:\n  name: edgepack\n',
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: 'edgepack', imageVersion: '24.04', description: '', architecture: 'x86_64', kernelVersion: '',
      packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
    },
    credentials: [{ user: 'admin', sudo: true, required: true, satisfied: (req.credentials?.length ?? 0) > 0 }],
  }
}

beforeEach(() => {
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => composeResponse(req))
  mockApi.startBuild.mockReset().mockResolvedValue({ buildId: 'b1', status: 'not-started', logsUrl: '' })
  useStore.setState({
    manifest,
    selection: { vertical: 'platform-enablement', sku: 'edgepack', platform: 'wcl', os: 'ubuntu24', kernel: '', imageType: 'iso' },
  })
})

describe('AdvancedPage credentials', () => {
  it('asks for credentials again for the next build of the same selection', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    for (let i = 0; i < 3; i++) fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    fireEvent.click(await screen.findByRole('button', { name: 'Compose Image' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText(/Password/), { target: { value: 'hunter2' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Compose Image' }))
    await waitFor(() => expect(mockApi.startBuild).toHaveBeenCalledTimes(1))
    expect(mockApi.startBuild.mock.calls[0][0].credentials).toEqual([{ user: 'admin', password: 'hunter2' }])

    // Wait for the review to re-resolve after the build started, then compose again.
    const composesAfterBuild = mockApi.compose.mock.calls.length
    await waitFor(() => expect(mockApi.compose.mock.calls.length).toBeGreaterThan(composesAfterBuild), { timeout: 3000 })
    fireEvent.click(await screen.findByRole('button', { name: 'Compose Image' }))

    expect(await screen.findByRole('dialog')).toBeTruthy()
    expect(mockApi.startBuild).toHaveBeenCalledTimes(1)
  })
})
