// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The BKC use cases offer a kernel version where other use cases offer a SKU.
// The version is the combination's sku, and the use case's skuLabel renames the
// dropdown; a version with no template yet is listed but grayed out.

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BasicPage } from './BasicPage'
import { cascadeOrder, cascadingOptions, skuLabelFor, useStore } from '../store'
import type { ComposeRequest, ComposeResponse, Manifest } from '../api/types'

const mockApi = vi.hoisted(() => ({
  compose: vi.fn(),
  startBuild: vi.fn(),
}))
vi.mock('../api/client', () => ({ api: mockApi }))

const BKC = 'ubuntu26/ubuntu26-x86_64-nvl-bkc-overlay-raw.yml'
const EDGEPACK = 'ubuntu26/ubuntu26-x86_64-edgepack-unattended-iso.yml'

const manifest: Manifest = {
  combinations: [
    { vertical: 'platform-enablement', sku: 'edgepack', platform: 'ptl', os: 'ubuntu26', imageType: 'iso', template: EDGEPACK },
    { vertical: 'upstream-bkc', sku: 'mainline-7.0.14', platform: 'nvl', os: 'ubuntu26', imageType: 'raw', template: BKC },
    { vertical: 'upstream-bkc', sku: 'mainline-7.1.13', platform: 'nvl', os: 'ubuntu26', imageType: 'raw', template: '' },
    // A second platform with its own kernel version, so the version list visibly
    // depends on the platform.
    { vertical: 'upstream-bkc', sku: 'mainline-7.2.11', platform: 'ptl', os: 'ubuntu26', imageType: 'raw', template: BKC },
    { vertical: 'ubuntu-lts-bkc', sku: 'hwe-7.0', platform: 'nvl', os: 'ubuntu26', imageType: 'raw', template: '' },
  ],
  verticals: [
    { id: 'platform-enablement', displayName: 'Platform Enablement' },
    { id: 'upstream-bkc', displayName: 'Platform Enablement - Upstream BKC', skuLabel: 'Mainline Kernel Version', platformFirst: true },
    { id: 'ubuntu-lts-bkc', displayName: 'Platform Enablement - Ubuntu LTS BKC', skuLabel: 'HWE Kernel Version', platformFirst: true },
  ],
  skus: [
    { id: 'edgepack', displayName: 'EdgePack Minimal Desktop' },
    { id: 'mainline-7.0.14', displayName: '7.0.14' },
    { id: 'mainline-7.1.13', displayName: '7.1.13' },
    { id: 'mainline-7.2.11', displayName: '7.2.11' },
    { id: 'hwe-7.0', displayName: 'HWE-7.0' },
  ],
  platforms: [
    { id: 'ptl', displayName: 'PTL' },
    { id: 'nvl', displayName: 'NVL (Nova Lake)' },
  ],
  targets: [{ id: 'ubuntu26', displayName: 'Ubuntu 26.04', os: 'ubuntu', arch: 'x86_64' }],
}

const EMPTY = { vertical: '', sku: '', platform: '', os: '', kernel: '', imageType: '' }

function composeResponse(req: ComposeRequest): ComposeResponse {
  return {
    template: req.vertical === 'upstream-bkc' ? BKC : EDGEPACK,
    yaml: 'image:\n  name: x\n',
    summary: {
      vertical: req.vertical, sku: req.sku ?? '', platform: req.platform, os: req.os, imageType: req.imageType,
      imageName: 'x', imageVersion: '26.04', description: '', architecture: 'x86_64', kernelVersion: '',
      packageCount: 1, diskSize: '', partitionCount: 0, partitionTable: '', hostname: '',
    },
    credentials: [],
  }
}

beforeEach(() => {
  mockApi.compose.mockReset().mockImplementation(async (req: ComposeRequest) => composeResponse(req))
  useStore.setState({ manifest, selection: { ...EMPTY } })
})

describe('skuLabelFor', () => {
  it("uses the use case's skuLabel, else SKU", () => {
    expect(skuLabelFor(manifest, 'upstream-bkc')).toBe('Mainline Kernel Version')
    expect(skuLabelFor(manifest, 'ubuntu-lts-bkc')).toBe('HWE Kernel Version')
    expect(skuLabelFor(manifest, 'platform-enablement')).toBe('SKU')
    expect(skuLabelFor(manifest, '')).toBe('SKU')
    expect(skuLabelFor(null, 'upstream-bkc')).toBe('SKU')
  })
})

describe('BKC kernel versions in the cascade', () => {
  it('picks the one buildable mainline version and grays the planned ones', () => {
    useStore.getState().setField('vertical', 'upstream-bkc')
    const sel = useStore.getState().selection
    expect(sel).toMatchObject({ sku: 'mainline-7.0.14', platform: 'nvl', os: 'ubuntu26', imageType: 'raw' })

    const opts = cascadingOptions(manifest, sel)
    expect(opts.skus).toEqual([
      { id: 'mainline-7.0.14', label: '7.0.14', disabled: false },
      { id: 'mainline-7.1.13', label: '7.1.13', disabled: true },
    ])
    expect(opts.matched?.template).toBe(BKC)
  })

  it('asks for the platform first, and lists the kernel versions for that platform', () => {
    expect(cascadeOrder(manifest, 'upstream-bkc').slice(0, 3)).toEqual(['vertical', 'platform', 'sku'])
    expect(cascadeOrder(manifest, 'platform-enablement').slice(0, 3)).toEqual(['vertical', 'sku', 'platform'])

    useStore.getState().setField('vertical', 'upstream-bkc')
    // Every platform the use case offers, whatever version was picked.
    expect(cascadingOptions(manifest, useStore.getState().selection).platforms.map((p) => p.id)).toEqual(['nvl', 'ptl'])

    useStore.getState().setField('sku', 'mainline-7.0.14')
    useStore.getState().setField('platform', 'ptl')
    const sel = useStore.getState().selection
    // Changing the platform cleared the version and picked PTL's only one.
    expect(sel).toMatchObject({ platform: 'ptl', sku: 'mainline-7.2.11', os: 'ubuntu26' })
    expect(cascadingOptions(manifest, sel).skus.map((s) => s.id)).toEqual(['mainline-7.2.11'])
  })

  it('lists the LTS use case but grays it while no HWE version has a template', () => {
    const lts = cascadingOptions(manifest, EMPTY).verticals.find((v) => v.id === 'ubuntu-lts-bkc')
    expect(lts).toEqual({ id: 'ubuntu-lts-bkc', label: 'Platform Enablement - Ubuntu LTS BKC', disabled: true })
  })
})

describe('BasicPage SKU dropdown label', () => {
  const renderPage = () =>
    render(<BasicPage active onBuildStarted={() => {}} buildInProgress={false} onEditInAdvanced={() => {}} />)

  it('reads "Mainline Kernel Version" under Upstream BKC', async () => {
    renderPage()
    fireEvent.change(screen.getByLabelText(/^Use Case/), { target: { value: 'upstream-bkc' } })
    await waitFor(() => expect(mockApi.compose).toHaveBeenCalled())

    const kernel = screen.getByLabelText(/^Mainline Kernel Version/) as HTMLSelectElement
    expect(kernel.value).toBe('mainline-7.0.14')
    expect(screen.queryByLabelText(/^SKU/)).toBeNull()
  })

  it('shows Platform above the kernel version for a BKC use case only', async () => {
    renderPage()
    const labels = () => screen.getAllByRole('combobox').map((el) => el.id)

    fireEvent.change(screen.getByLabelText(/^Use Case/), { target: { value: 'upstream-bkc' } })
    await waitFor(() => expect(mockApi.compose).toHaveBeenCalled())
    expect(labels().slice(0, 3)).toEqual(['select-use-case', 'select-platform', 'select-mainline-kernel-version'])

    fireEvent.change(screen.getByLabelText(/^Use Case/), { target: { value: 'platform-enablement' } })
    expect(labels().slice(0, 3)).toEqual(['select-use-case', 'select-sku', 'select-platform'])
  })

  it('keeps "SKU" for a use case without a skuLabel', async () => {
    renderPage()
    fireEvent.change(screen.getByLabelText(/^Use Case/), { target: { value: 'platform-enablement' } })
    await waitFor(() => expect(mockApi.compose).toHaveBeenCalled())

    expect((screen.getByLabelText(/^SKU/) as HTMLSelectElement).value).toBe('edgepack')
  })
})
