// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// A GitHub token typed for an OEP Installer build belongs to that build: it is
// sent beside the selection to startBuild, never in a compose request, and is
// gone when the dialog opens again.

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BasicPage } from './BasicPage'
import { useStore } from '../store'
import type { ComposeRequest, ComposeResponse, Manifest } from '../api/types'

const mockApi = vi.hoisted(() => ({
  compose: vi.fn(),
  startBuild: vi.fn(),
}))
vi.mock('../api/client', () => ({ api: mockApi }))

const TEMPLATE = 'ubuntu24-x86_64-fedaero-handheld-blueprint-docker-unattended-iso.yml'

const manifest: Manifest = {
  combinations: [
    { vertical: 'fed-aero', sku: 'handheld', platform: 'ptl', os: 'ubuntu24', imageType: 'iso', template: TEMPLATE },
  ],
  verticals: [{ id: 'fed-aero', displayName: 'Fed Aero' }],
  skus: [{ id: 'handheld', displayName: 'Handheld' }],
  platforms: [{ id: 'ptl', displayName: 'PTL' }],
  targets: [{ id: 'ubuntu24', displayName: 'Ubuntu 24.04', os: 'ubuntu', arch: 'x86_64' }],
}

function composeResponse(req: ComposeRequest): ComposeResponse {
  return {
    template: TEMPLATE,
    yaml: 'image:\n  name: handheld\n',
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: 'handheld', imageVersion: '24.04', description: '', architecture: 'x86_64', kernelVersion: '',
      packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
    },
    credentials: [{ user: 'admin', sudo: true, required: false, satisfied: true }],
    oepInstaller: { uses: true, serverToken: false },
  }
}

beforeEach(() => {
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => composeResponse(req))
  mockApi.startBuild.mockReset().mockResolvedValue({ buildId: 'b1', status: 'not-started', logsUrl: '' })
  useStore.setState({
    manifest,
    selection: { vertical: 'fed-aero', sku: 'handheld', platform: 'ptl', os: 'ubuntu24', kernel: '', imageType: 'iso' },
  })
})

describe('BasicPage OEP Installer token', () => {
  it('sends the token with the build only, then forgets it', async () => {
    render(<BasicPage active onBuildStarted={() => {}} buildInProgress={false} onEditInAdvanced={() => {}} />)
    await waitFor(() => expect(mockApi.compose).toHaveBeenCalled())

    fireEvent.click(await screen.findByRole('button', { name: 'Compose Image' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('GitHub token for the OEP Installer'), {
      target: { value: 'github_pat_secret' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Compose Image' }))
    await waitFor(() => expect(mockApi.startBuild).toHaveBeenCalledTimes(1))
    expect(mockApi.startBuild.mock.calls[0][1]).toBe('github_pat_secret')
    expect(JSON.stringify(mockApi.startBuild.mock.calls[0][0])).not.toContain('github_pat_secret')
    expect(JSON.stringify(mockApi.compose.mock.calls)).not.toContain('github_pat_secret')

    fireEvent.click(await screen.findByRole('button', { name: 'Compose Image' }))
    const again = await screen.findByRole('dialog')
    expect(within(again).getByLabelText<HTMLInputElement>('GitHub token for the OEP Installer').value).toBe('')
  })
})
