// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Platform Enablement names its second dropdown "BKC Type". Its BKC types
// (Upstream BKC, Ubuntu LTS BKC) offer kernel versions in a third dropdown the
// type names (Mainline Kernel Version, HWE Kernel Version); a version or type
// with no template yet is listed but grayed out.

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BasicPage } from './BasicPage'
import { cascadeOrder, cascadingOptions, kernelLabelFor, skuLabelFor, useStore } from '../store'
import type { ComposeRequest, ComposeResponse, Manifest } from '../api/types'

const mockApi = vi.hoisted(() => ({
  compose: vi.fn(),
  startBuild: vi.fn(),
}))
vi.mock('../api/client', () => ({ api: mockApi }))

const BKC = 'ubuntu26/ubuntu26-x86_64-nvl-bkc-overlay-raw.yml'
const EDGEPACK = 'ubuntu26/ubuntu26-x86_64-edgepack-unattended-iso.yml'
const PE = 'platform-enablement'

const manifest: Manifest = {
  combinations: [
    { vertical: PE, sku: 'edgepack', platform: 'ptl', os: 'ubuntu26', imageType: 'iso', template: EDGEPACK },
    { vertical: PE, sku: 'upstream-bkc', platform: 'nvl', os: 'ubuntu26', kernel: 'mainline-7.0.14', imageType: 'raw', template: '' },
    { vertical: PE, sku: 'upstream-bkc', platform: 'nvl', os: 'ubuntu26', kernel: 'mainline-7.3', imageType: 'raw', template: BKC },
    { vertical: PE, sku: 'ubuntu-lts-bkc', platform: 'nvl', os: 'ubuntu26', kernel: 'hwe-7.0', imageType: 'raw', template: '' },
    // A use case asking for the platform before its SKU.
    { vertical: 'platform-first', sku: 'a', platform: 'ptl', os: 'ubuntu26', imageType: 'raw', template: BKC },
  ],
  verticals: [
    { id: PE, displayName: 'Platform Enablement', skuLabel: 'BKC Type' },
    { id: 'platform-first', displayName: 'Platform first', platformFirst: true },
  ],
  skus: [
    { id: 'edgepack', displayName: 'Base Platform Minimal Desktop' },
    { id: 'upstream-bkc', displayName: 'Upstream BKC', kernelLabel: 'Mainline Kernel Version' },
    { id: 'ubuntu-lts-bkc', displayName: 'Ubuntu LTS BKC', kernelLabel: 'HWE Kernel Version' },
    { id: 'a', displayName: 'A' },
  ],
  platforms: [
    { id: 'ptl', displayName: 'PTL' },
    { id: 'nvl', displayName: 'NVL (Nova Lake)' },
  ],
  targets: [{ id: 'ubuntu26', displayName: 'Ubuntu 26.04', os: 'ubuntu', arch: 'x86_64' }],
  kernels: [
    { id: 'mainline-7.0.14', displayName: '7.0.14' },
    { id: 'mainline-7.3', displayName: '7.3' },
    { id: 'hwe-7.0', displayName: 'HWE-7.0' },
  ],
}

const EMPTY = { vertical: '', sku: '', platform: '', os: '', kernel: '', imageType: '' }

function composeResponse(req: ComposeRequest): ComposeResponse {
  return {
    template: req.sku === 'upstream-bkc' ? BKC : EDGEPACK,
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

describe('dropdown labels', () => {
  it("names the SKU dropdown by the use case's skuLabel, else SKU", () => {
    expect(skuLabelFor(manifest, PE)).toBe('BKC Type')
    expect(skuLabelFor(manifest, 'platform-first')).toBe('SKU')
    expect(skuLabelFor(null, PE)).toBe('SKU')
  })

  it("names the kernel dropdown by the SKU's kernelLabel, else Kernel", () => {
    expect(kernelLabelFor(manifest, 'upstream-bkc')).toBe('Mainline Kernel Version')
    expect(kernelLabelFor(manifest, 'ubuntu-lts-bkc')).toBe('HWE Kernel Version')
    expect(kernelLabelFor(manifest, 'edgepack')).toBe('Kernel')
  })
})

describe('BKC types in the cascade', () => {
  it('lists the BKC types as SKUs, graying the one with no template yet', () => {
    useStore.getState().setField('vertical', PE)
    expect(cascadingOptions(manifest, useStore.getState().selection).skus).toEqual([
      { id: 'edgepack', label: 'Base Platform Minimal Desktop', disabled: false },
      { id: 'upstream-bkc', label: 'Upstream BKC', disabled: false },
      { id: 'ubuntu-lts-bkc', label: 'Ubuntu LTS BKC', disabled: true },
    ])
  })

  it("offers Upstream BKC's kernel versions by name, picking the buildable one", () => {
    useStore.getState().setField('vertical', PE)
    useStore.getState().setField('sku', 'upstream-bkc')
    const sel = useStore.getState().selection
    expect(sel).toMatchObject({ platform: 'nvl', os: 'ubuntu26', kernel: 'mainline-7.3', imageType: 'raw' })
    const opts = cascadingOptions(manifest, sel)
    expect(opts.kernels).toEqual([
      { id: 'mainline-7.0.14', label: '7.0.14', disabled: true },
      { id: 'mainline-7.3', label: '7.3', disabled: false },
    ])
    expect(opts.matched?.template).toBe(BKC)
  })

  it('still asks for the platform first where a use case says so', () => {
    expect(cascadeOrder(manifest, 'platform-first').slice(0, 3)).toEqual(['vertical', 'platform', 'sku'])
    expect(cascadeOrder(manifest, PE).slice(0, 3)).toEqual(['vertical', 'sku', 'platform'])
  })
})

describe('BasicPage dropdowns', () => {
  const renderPage = () =>
    render(<BasicPage active onBuildStarted={() => {}} buildInProgress={false} onEditInAdvanced={() => {}} />)

  it('reads BKC Type, then the kernel dropdown the type names, and composes with the kernel', async () => {
    renderPage()
    fireEvent.change(screen.getByLabelText(/^Use Case/), { target: { value: PE } })
    fireEvent.change(screen.getByLabelText(/^BKC Type/), { target: { value: 'upstream-bkc' } })

    const kernel = (await screen.findByLabelText(/^Mainline Kernel Version/)) as HTMLSelectElement
    expect(kernel.value).toBe('mainline-7.3')
    await waitFor(() =>
      expect(mockApi.compose).toHaveBeenCalledWith(expect.objectContaining({ sku: 'upstream-bkc', kernel: 'mainline-7.3' })),
    )
  })

  it('offers no kernel dropdown for a BKC type without kernel versions', async () => {
    renderPage()
    fireEvent.change(screen.getByLabelText(/^Use Case/), { target: { value: PE } })
    expect((screen.getByLabelText(/^BKC Type/) as HTMLSelectElement).value).toBe('edgepack')
    expect(screen.queryByLabelText(/Kernel/)).toBeNull()
  })
})
