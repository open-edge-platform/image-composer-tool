// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// An overlay template builds on a base image on the server. The Basic tab
// asks for it under the selection, sends it, and asks again in the Compose
// dialog while it is wrong.

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BasicPage } from './BasicPage'
import { useStore } from '../store'
import type { ComposeRequest, ComposeResponse, Manifest } from '../api/types'

const mockApi = vi.hoisted(() => ({ compose: vi.fn(), startBuild: vi.fn() }))
vi.mock('../api/client', () => ({ api: mockApi }))

const TEMPLATE = 'ubuntu26/nvl-bkc.yml'
const manifest: Manifest = {
  combinations: [{ vertical: 'upstream-bkc', sku: 'mainline-7.3', platform: 'nvl', os: 'ubuntu26', imageType: 'raw', template: TEMPLATE }],
  verticals: [{ id: 'upstream-bkc', displayName: 'Upstream BKC' }],
  skus: [{ id: 'mainline-7.3', displayName: '7.3' }],
  platforms: [{ id: 'nvl', displayName: 'NVL' }],
  targets: [{ id: 'ubuntu26', displayName: 'Ubuntu 26.04', os: 'ubuntu', arch: 'x86_64' }],
}

// Only /images/base.raw is on the server.
function composeResponse(req: ComposeRequest): ComposeResponse {
  const baseImage = req.baseImage ?? '/path/to/ubuntu-26.04-base.raw'
  return {
    template: TEMPLATE,
    yaml: 'image:\n  name: nvl\n',
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: 'nvl', imageVersion: '26.04', description: '', architecture: 'x86_64', kernelVersion: '',
      packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
      baseImage,
      baseImageMissing: baseImage !== '/images/base.raw',
    },
    credentials: [],
  }
}

beforeEach(() => {
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => composeResponse(req))
  mockApi.startBuild.mockReset().mockResolvedValue({ buildId: 'b1', status: 'not-started', logsUrl: '' })
  useStore.setState({
    manifest,
    baseImage: '',
    baseImageEdited: false,
    selection: { vertical: 'upstream-bkc', sku: 'mainline-7.3', platform: 'nvl', os: 'ubuntu26', kernel: '', imageType: 'raw' },
  })
})

describe('BasicPage base image', () => {
  it('asks for it under the selection, and in the Compose dialog until the server has it', async () => {
    render(<BasicPage active onBuildStarted={() => {}} buildInProgress={false} onEditInAdvanced={() => {}} />)

    const input = (await screen.findByLabelText(/Base Image/)) as HTMLInputElement
    await waitFor(() => expect(input.value).toBe('/path/to/ubuntu-26.04-base.raw'))
    expect((await screen.findByRole('alert')).textContent).toMatch(/placeholder/)

    fireEvent.click(screen.getByRole('button', { name: 'Compose Image' }))
    const dialog = await screen.findByRole('dialog')
    const compose = () => within(dialog).getByRole<HTMLButtonElement>('button', { name: 'Compose Image' })
    expect(compose().disabled).toBe(true)

    fireEvent.change(within(dialog).getByLabelText(/Base Image/), { target: { value: '/images/base.raw' } })
    await waitFor(() => expect(compose().disabled).toBe(false))
    fireEvent.click(compose())
    await waitFor(() => expect(mockApi.startBuild).toHaveBeenCalled())
    expect(mockApi.startBuild.mock.calls[0][0]).toMatchObject({ baseImage: '/images/base.raw' })
  })
})
