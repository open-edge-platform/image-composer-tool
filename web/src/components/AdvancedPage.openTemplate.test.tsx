// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Author mode's Open Template: a saved template's metadata.basedOn selects its
// use case, the template is composed in place of the use case's own, and Save
// writes back to the opened file.

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AdvancedPage } from './AdvancedPage'
import { useStore } from '../store'
import type { ComposeRequest, ComposeResponse, Manifest } from '../api/types'

const mockApi = vi.hoisted(() => ({
  compose: vi.fn(),
  startBuild: vi.fn(),
  listPackageRepos: vi.fn(async () => ({ repos: [] })),
  getEdgePack: vi.fn(async () => null),
  searchPackages: vi.fn(async () => []),
  lookupPackages: vi.fn(async () => ({ packages: [], unavailableRepos: [] })),
  searchStreamUrl: () => '',
}))
vi.mock('../api/client', () => ({ api: mockApi }))

const BKC = 'ubuntu26/nvl-bkc.yml'
const EDGEPACK = 'ubuntu24/edgepack.yml'
const CURATED = 'image:\n  name: nvl-bkc\nsystemConfig:\n  packages:\n    - vim\n'
const SAVED = `metadata:\n  basedOn: ${BKC}\nimage:\n  name: my-bkc\nsystemConfig:\n  packages:\n    - vim\n    - htop\n`

const manifest: Manifest = {
  combinations: [
    { vertical: 'platform-enablement', sku: 'edgepack', platform: 'wcl', os: 'ubuntu24', imageType: 'iso', template: EDGEPACK },
    { vertical: 'upstream-bkc', sku: 'mainline-7.0.14', platform: 'nvl', os: 'ubuntu26', imageType: 'raw', template: BKC },
  ],
  verticals: [
    { id: 'platform-enablement', displayName: 'Platform Enablement' },
    { id: 'upstream-bkc', displayName: 'Upstream BKC' },
  ],
  skus: [
    { id: 'edgepack', displayName: 'EdgePack' },
    { id: 'mainline-7.0.14', displayName: '7.0.14' },
  ],
  platforms: [
    { id: 'wcl', displayName: 'WCL' },
    { id: 'nvl', displayName: 'NVL' },
  ],
  targets: [
    { id: 'ubuntu24', displayName: 'Ubuntu 24.04', os: 'ubuntu', arch: 'x86_64' },
    { id: 'ubuntu26', displayName: 'Ubuntu 26.04', os: 'ubuntu', arch: 'x86_64' },
  ],
}

function composeResponse(req: ComposeRequest): ComposeResponse {
  const template = req.vertical === 'upstream-bkc' ? BKC : EDGEPACK
  const text = req.templateYaml ?? CURATED
  return {
    template,
    yaml: text,
    templateYaml: text,
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: req.templateYaml ? 'my-bkc' : 'nvl-bkc', imageVersion: '26.04', description: '', architecture: 'x86_64',
      kernelVersion: '', packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
    },
  }
}

const written: string[] = []

beforeEach(() => {
  written.length = 0
  mockApi.startBuild.mockReset().mockResolvedValue({ buildId: 'b1', status: 'not-started', logsUrl: '' })
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => composeResponse(req))
  useStore.setState({
    manifest,
    capabilities: { authoring: true },
    advancedMode: 'author',
    addedPackages: [],
    removedPackages: [],
    selection: { vertical: 'platform-enablement', sku: 'edgepack', platform: 'wcl', os: 'ubuntu24', kernel: '', imageType: 'iso' },
  })
  const handle = {
    name: 'my-bkc.yml',
    getFile: async () => ({ text: async () => SAVED }),
    createWritable: async () => ({ write: async (d: string) => void written.push(d), close: async () => {} }),
  }
  ;(window as unknown as Record<string, unknown>).showOpenFilePicker = vi.fn(async () => [handle])
})

afterEach(() => {
  delete (window as unknown as Record<string, unknown>).showOpenFilePicker
})

describe('Open Template', () => {
  it('selects the use case basedOn names, composes the file, and saves back to it', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Open Template…' }))

    await screen.findByText('my-bkc.yml')
    expect(useStore.getState().selection).toMatchObject({ vertical: 'upstream-bkc', platform: 'nvl', os: 'ubuntu26' })
    await waitFor(() =>
      expect(mockApi.compose).toHaveBeenLastCalledWith(expect.objectContaining({ vertical: 'upstream-bkc', templateYaml: SAVED })),
    )

    await waitFor(() => expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Next' }).disabled).toBe(false))
    for (let i = 0; i < 3; i++) fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect((await screen.findByLabelText('Edited template')).textContent).toBe(SAVED)
    expect(screen.getByText(/No changes yet/)).toBeTruthy()

    // Save goes straight back to the opened file, without asking where.
    fireEvent.click(screen.getByRole('button', { name: 'Save Template' }))
    await waitFor(() => expect(written).toEqual([SAVED]))
  })

  it('goes back to the use case template on request', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Open Template…' }))
    await screen.findByText('my-bkc.yml')

    fireEvent.click(screen.getByRole('button', { name: "Start from the use case's template instead" }))
    await waitFor(() => expect(mockApi.compose).toHaveBeenLastCalledWith(expect.not.objectContaining({ templateYaml: SAVED })))
    expect(screen.queryByText('my-bkc.yml')).toBeNull()
  })

  it('opens a template without basedOn for the current selection, and says so', async () => {
    const plain = 'image:\n  name: hand-made\n'
    ;(window as unknown as Record<string, unknown>).showOpenFilePicker = vi.fn(async () => [
      { name: 'hand.yml', getFile: async () => ({ text: async () => plain }), createWritable: async () => ({}) },
    ])
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    await waitFor(() => expect(mockApi.compose).toHaveBeenCalled())
    fireEvent.click(await screen.findByRole('button', { name: 'Open Template…' }))

    await screen.findByText(/opened for the current selection/)
    expect(useStore.getState().selection.vertical).toBe('platform-enablement')
    await waitFor(() => expect(mockApi.compose).toHaveBeenLastCalledWith(expect.objectContaining({ templateYaml: plain })))
  })

  it('builds the edited template itself, with only the logins beside it', async () => {
    render(<AdvancedPage active onBuildStarted={() => {}} buildInProgress={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Open Template…' }))
    await screen.findByText('my-bkc.yml')
    useStore.getState().setPackageRemoved('vim', true)

    await waitFor(() => expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Next' }).disabled).toBe(false))
    for (let i = 0; i < 3; i++) fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Compose Image' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Compose Image' }))

    await waitFor(() => expect(mockApi.startBuild).toHaveBeenCalledTimes(1))
    const req = mockApi.startBuild.mock.calls[0][0] as ComposeRequest
    expect(req).toMatchObject({ vertical: 'upstream-bkc', platform: 'nvl', os: 'ubuntu26' })
    expect(req.templateYaml).toBe(SAVED.replace('    - vim\n', ''))
    expect(req.packages).toBeUndefined()
    expect(req.disk).toBeUndefined()
  })
})
