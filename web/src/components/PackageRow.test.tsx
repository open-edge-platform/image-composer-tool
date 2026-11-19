// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// A package row: the Pinned/Unpinned tag and the version in effect beside the
// name, the browsed repository's version chips on the line below, and what
// choosing a chip does.

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PackageRow } from './PackageRow'
import type { AddedPackage } from '../store'
import type { PackageVersion } from '../api/types'

const label = (id: string) => ({ base: 'Ubuntu Base', snapshot: 'Snapshot' })[id] ?? id

function renderRow(props: {
  versions: PackageVersion[]
  selection?: AddedPackage
  locked?: boolean
  currentVersion?: string
  currentIsFloating?: boolean
  resolvedVersion?: string
  resolvedRepo?: string
  pinnedFrom?: { repoLabel?: string; onBrowse?: () => void }
  onBrowseRepo?: (id: string) => void
}) {
  const onToggle = vi.fn()
  const onChooseVersion = vi.fn()
  render(
    <PackageRow
      name="pkg"
      version={props.versions[0]?.version ?? ''}
      versions={props.versions}
      repoLabelFor={label}
      selection={props.selection}
      onToggle={onToggle}
      onChooseVersion={onChooseVersion}
      locked={props.locked}
      currentVersion={props.currentVersion}
      currentIsFloating={props.currentIsFloating}
      resolvedVersion={props.resolvedVersion}
      resolvedRepo={props.resolvedRepo}
      pinnedFrom={props.pinnedFrom}
      browsedRepoId="base"
      onBrowseRepo={props.onBrowseRepo}
    />,
  )
  return { onToggle, onChooseVersion }
}

const base = (...versions: string[]) => versions.map((version) => ({ version, repository: 'base' }))
const tag = () => screen.queryByText(/^(Pinned|Unpinned)$/, { selector: 'span' })?.textContent
const headerChip = () =>
  screen.queryByTitle('Pinned version') ?? screen.queryByTitle('The version Unpinned currently resolves to')
const via = () => screen.queryByText(/^via /, { selector: 'span' })?.textContent
const chip = (version: string) => screen.getByRole('button', { name: version }) as HTMLButtonElement
const pressed = () =>
  screen
    .queryAllByRole('button')
    .filter((b) => b.getAttribute('aria-pressed') === 'true')
    .map((b) => b.textContent)

describe('PackageRow — beside the name', () => {
  it('shows just the version for a package nothing selects', () => {
    renderRow({ versions: base('3.3.0-4') })
    expect(tag()).toBeUndefined()
    expect(headerChip()).toBeNull()
  })

  it('tags a pin from this repo, with its version and no "via"', () => {
    renderRow({ versions: base('1:6.19-1'), selection: { name: 'pkg', version: '1:6.19-1', repo: 'base' } })
    expect(tag()).toBe('Pinned')
    expect(headerChip()?.textContent).toBe('1:6.19-1')
    expect(headerChip()?.className).not.toContain('border-dashed')
    expect(via()).toBeUndefined()
  })

  it('links "via" to where a template pin comes from when it is another repo', () => {
    // ffmpeg, browsing Ubuntu base: the template's pin is the snapshot's build.
    const onBrowse = vi.fn()
    renderRow({
      versions: base('7:8.0.1-3ubuntu2'),
      locked: true,
      currentVersion: '7:9.0.1-1ppa1~resolute1',
      currentIsFloating: false,
      pinnedFrom: { repoLabel: 'Snapshot', onBrowse },
    })
    expect(tag()).toBe('Pinned')
    expect(headerChip()?.textContent).toBe('7:9.0.1-1ppa1~resolute1')
    fireEvent.click(screen.getByRole('button', { name: 'Snapshot' }))
    expect(onBrowse).toHaveBeenCalled()
  })

  it('tags an unpinned package, with what it resolves to dashed, here and among the chips', () => {
    renderRow({
      versions: base('1:34.0.5-12build1', '1:34.0.5-11'),
      locked: true,
      currentVersion: '1:34.0.5-12build1',
      currentIsFloating: true,
    })
    expect(tag()).toBe('Unpinned')
    expect(headerChip()?.textContent).toBe('1:34.0.5-12build1')
    expect(headerChip()?.className).toContain('border-dashed')
    expect(chip('1:34.0.5-12build1').className).toContain('border-dashed')
    expect(chip('1:34.0.5-11').className).not.toContain('border-dashed')
    expect(pressed()).toEqual([])
  })

  it('links "via" to the repo an unpinned pick resolves from when it is another one', () => {
    const onBrowseRepo = vi.fn()
    renderRow({
      versions: base('2:9.1.0016-1'),
      selection: { name: 'pkg', version: '', repo: 'base' },
      resolvedVersion: '2:9.2-1ppa1',
      resolvedRepo: 'snapshot',
      onBrowseRepo,
    })
    expect(tag()).toBe('Unpinned')
    expect(headerChip()?.textContent).toBe('2:9.2-1ppa1')
    fireEvent.click(screen.getByRole('button', { name: 'Snapshot' }))
    expect(onBrowseRepo).toHaveBeenCalledWith('snapshot')
  })

  it('tags an unpinned package before its version is known, with no chip', () => {
    renderRow({ versions: base('2:9.1.0016-1'), selection: { name: 'pkg', version: '', repo: 'base' } })
    expect(tag()).toBe('Unpinned')
    expect(headerChip()).toBeNull()
  })
})

describe('PackageRow — the version chips', () => {
  it('has no Unpinned chip: a selected package with no version chosen is unpinned', () => {
    renderRow({ versions: base('3.2.1ubuntu0.1', '3.2.0'), selection: { name: 'pkg', version: '', repo: 'base' } })
    expect(screen.queryByRole('button', { name: 'Unpinned' })).toBeNull()
    expect(pressed()).toEqual([])
  })

  it('pins the chosen version', () => {
    const { onChooseVersion } = renderRow({
      versions: base('3.2.1ubuntu0.1', '3.2.0'),
      selection: { name: 'pkg', version: '', repo: 'base' },
    })
    fireEvent.click(chip('3.2.0'))
    expect(onChooseVersion).toHaveBeenCalledWith({ version: '3.2.0', repository: 'base' })
  })

  it('unpins when the pinned chip is chosen again, keeping the package added', () => {
    const { onChooseVersion, onToggle } = renderRow({
      versions: base('3.2.1ubuntu0.1', '3.2.0'),
      selection: { name: 'pkg', version: '3.2.0', repo: 'base' },
    })
    expect(pressed()).toEqual(['3.2.0'])
    fireEvent.click(chip('3.2.0'))
    expect(onChooseVersion).toHaveBeenCalledWith({ version: '', repository: 'base' })
    expect(onToggle).not.toHaveBeenCalled()
  })

  it('marks the template’s own pin selected, but not clickable', () => {
    renderRow({ versions: base('0.42-4build1', '0.42-1ppa1~resolute3'), locked: true, currentVersion: '0.42-1ppa1~resolute3', currentIsFloating: false })
    expect(pressed()).toEqual(['0.42-1ppa1~resolute3'])
    expect(chip('0.42-1ppa1~resolute3').disabled).toBe(true)
  })

  it('drops an override when its chip is chosen again', () => {
    const { onToggle } = renderRow({
      versions: base('3.2.1ubuntu0.1', '3.2.0'),
      locked: true,
      currentIsFloating: true,
      selection: { name: 'pkg', version: '3.2.1ubuntu0.1', repo: 'base' },
    })
    fireEvent.click(chip('3.2.1ubuntu0.1'))
    expect(onToggle).toHaveBeenCalledWith(false)
  })

  it('drops an override when the template’s own pin is chosen', () => {
    const { onToggle, onChooseVersion } = renderRow({
      versions: base('0.42-4build1', '0.42-1ppa1~resolute3'),
      locked: true,
      currentVersion: '0.42-1ppa1~resolute3',
      currentIsFloating: false,
      selection: { name: 'pkg', version: '0.42-4build1', repo: 'base' },
    })
    expect(pressed()).toEqual(['0.42-4build1'])
    fireEvent.click(chip('0.42-1ppa1~resolute3'))
    expect(onToggle).toHaveBeenCalledWith(false)
    expect(onChooseVersion).not.toHaveBeenCalled()
  })

  it('keeps a pinned chip visible even when it is past the first few versions', () => {
    renderRow({
      versions: base('1:6.19-4', '1:6.19-3', '1:6.19-2', '1:6.19-1'),
      selection: { name: 'pkg', version: '1:6.19-1', repo: 'base' },
    })
    expect(pressed()).toEqual(['1:6.19-1'])
  })
})

describe('PackageRow — a template package', () => {
  const modified = () => screen.queryByText('Modified', { selector: 'span' })

  it('has no line saying it is in the template: the greyed checkbox says so', () => {
    renderRow({ versions: base('3.2.1ubuntu0.1'), locked: true, currentIsFloating: true })
    expect(screen.queryByText(/Already in template/)).toBeNull()
    expect((screen.getByRole('checkbox') as HTMLInputElement).disabled).toBe(true)
    expect(modified()).toBeNull()
  })

  it('is tagged Modified once a version overrides the template having it unpinned', () => {
    // apt-transport-https, unpinned in the template, pinned to 3.2.1ubuntu0.1.
    renderRow({
      versions: base('3.2.1ubuntu0.1'),
      locked: true,
      currentIsFloating: true,
      selection: { name: 'pkg', version: '3.2.1ubuntu0.1', repo: 'base' },
    })
    expect(modified()?.getAttribute('title')).toBe('Template: unpinned · Yours: 3.2.1ubuntu0.1')
  })

  it('is tagged Modified with what the template pinned once a different version overrides it', () => {
    renderRow({
      versions: base('0.42-4build1', '0.42-1ppa1~resolute3'),
      locked: true,
      currentVersion: '0.42-1ppa1~resolute3',
      currentIsFloating: false,
      selection: { name: 'pkg', version: '0.42-4build1', repo: 'base' },
    })
    expect(modified()?.getAttribute('title')).toBe('Template: pinned to 0.42-1ppa1~resolute3 · Yours: 0.42-4build1')
  })

  it('is not tagged Modified while it is left as it is', () => {
    renderRow({ versions: base('0.42-1ppa1~resolute3'), locked: true, currentVersion: '0.42-1ppa1~resolute3', currentIsFloating: false })
    expect(modified()).toBeNull()
  })

  it('is not tagged Modified when it is an ordinary added package', () => {
    renderRow({ versions: base('3.3.0-4'), selection: { name: 'pkg', version: '3.3.0-4', repo: 'base' } })
    expect(modified()).toBeNull()
  })
})
