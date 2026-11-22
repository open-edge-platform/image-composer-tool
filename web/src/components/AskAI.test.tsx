// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AskAI } from './AskAI'
import { isAIChange, useStore } from '../store'
import type { TemplateEditSuggestion } from '../api/types'

const mockApi = vi.hoisted(() => ({
  suggestTemplateEdits: vi.fn(),
  listPackageRepos: vi.fn(),
  lookupPackages: vi.fn(),
}))
vi.mock('../api/client', () => ({ api: mockApi }))

const suggestion: TemplateEditSuggestion = {
  note: 'Added Docker and removed snapd.',
  edits: [
    { op: 'add', package: 'docker.io', version: '', value: '' },
    { op: 'add', package: 'docker-ce', version: '', value: '' },
    { op: 'remove', package: 'snapd', version: '', value: '' },
    { op: 'set_disk_size', package: '', version: '', value: '32GiB' },
  ],
  unsupported: ['Partitions cannot be added.'],
}

const disk = {
  layoutMode: 'size' as const,
  name: 'disk',
  size: '16GiB',
  maxSize: '',
  partitionTableType: 'gpt' as const,
  extendLastPartitionToFillDisk: true,
  artifacts: [],
  partitions: [],
  firstStartMiB: 1,
  path: '',
  selectionPolicy: null,
}

const renderAskAI = () =>
  render(
    <AskAI
      os="ubuntu26"
      templateText={'systemConfig:\n  packages:\n    - snapd\n'}
      basePackages={['snapd']}
      templateRepos={[]}
      templateOnlyRepos={[]}
      overlay={false}
    />,
  )

const ask = (text: string) => {
  fireEvent.change(screen.getByLabelText(/Ask AI/), { target: { value: text } })
  fireEvent.click(screen.getByRole('button', { name: 'Suggest' }))
}

beforeEach(() => {
  mockApi.suggestTemplateEdits.mockReset().mockResolvedValue(suggestion)
  mockApi.listPackageRepos.mockReset().mockResolvedValue({
    repos: [{ id: 'ubuntu', displayName: 'Ubuntu', url: '', enabledByDefault: true }],
  })
  mockApi.lookupPackages.mockReset().mockResolvedValue({
    packages: [{ name: 'docker.io', version: '27.5.1', repository: 'ubuntu' }],
    unavailableRepos: [],
  })
  useStore.setState({ addedPackages: [], removedPackages: [], enabledRepos: [], aiPackages: {}, disk, diskEdited: false })
})

describe('AskAI', () => {
  it('applies the checked edits, tags them AI, and lists what it skipped', async () => {
    renderAskAI()
    ask('add docker, remove snapd, 32 GiB disk')

    await screen.findByText(/^Applied:/)
    expect(mockApi.suggestTemplateEdits).toHaveBeenCalledWith(
      'systemConfig:\n  packages:\n    - snapd\n',
      'add docker, remove snapd, 32 GiB disk',
    )
    expect(mockApi.lookupPackages).toHaveBeenCalledWith({
      os: 'ubuntu26',
      repos: ['ubuntu'],
      names: ['docker.io', 'docker-ce', 'snapd'],
    })

    const s = useStore.getState()
    expect(s.addedPackages).toEqual([{ name: 'docker.io', version: '', repo: 'ubuntu' }])
    expect(s.removedPackages).toEqual(['snapd'])
    expect(s.disk?.size).toBe('32GiB')
    expect(s.diskEdited).toBe(true)
    expect(isAIChange(s, 'docker.io')).toBe(true)
    expect(isAIChange(s, 'snapd')).toBe(true)

    expect(screen.getByText('AI: Added Docker and removed snapd.')).toBeTruthy()
    expect(screen.getByText('Applied: added docker.io; removed snapd; disk size 32GiB.')).toBeTruthy()
    // docker-ce isn't in any repository, so it was never added.
    expect(screen.getByText('docker-ce: not found in the repositories for this target.')).toBeTruthy()
    expect(screen.getByText('Partitions cannot be added.')).toBeTruthy()
  })

  it('drops the AI tag once the change is undone or edited by hand', async () => {
    renderAskAI()
    ask('add docker')
    await screen.findByText(/^Applied:/)

    useStore.getState().setPackageRemoved('snapd', false)
    useStore.getState().setPackage({ name: 'docker.io', version: '27.5.1', repo: 'ubuntu' })
    const s = useStore.getState()
    expect(isAIChange(s, 'snapd')).toBe(false)
    expect(isAIChange(s, 'docker.io')).toBe(false)
  })

  it('links to where to get a new token when the service rejects it', async () => {
    mockApi.suggestTemplateEdits.mockRejectedValue(
      new Error('the AI service rejected the token; it may have expired. Get a new one at https://gnai.intel.com/auth/oauth2/sso'),
    )
    renderAskAI()
    ask('add docker')

    const link = await screen.findByRole('link', { name: 'https://gnai.intel.com/auth/oauth2/sso' })
    expect(link.getAttribute('href')).toBe('https://gnai.intel.com/auth/oauth2/sso')
    expect(useStore.getState().addedPackages).toEqual([])
  })

  it('does nothing for an empty request', async () => {
    renderAskAI()
    expect((screen.getByRole('button', { name: 'Suggest' }) as HTMLButtonElement).disabled).toBe(true)
    await waitFor(() => expect(mockApi.suggestTemplateEdits).not.toHaveBeenCalled())
  })
})
