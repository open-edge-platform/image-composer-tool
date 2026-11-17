// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// A template package's own choice — the version it pins, or Unpinned when it
// leaves the package unpinned — is disabled as redundant. Once the user
// overrides it, choosing it is the way back, so it drops the override.

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PackageRow } from './PackageRow'
import type { AddedPackage } from '../store'

const versions = [
  { version: '0.42-4build1', repository: 'ubuntu-resolute-base' },
  { version: '0.42-1ppa1~resolute3', repository: 'snapshot' },
]

function renderRow(opts: { selection?: AddedPackage; currentIsFloating: boolean }) {
  const onToggle = vi.fn()
  const onChooseVersion = vi.fn()
  render(
    <PackageRow
      name="spice-client-gtk"
      version="0.42-4build1"
      versions={versions}
      repoLabelFor={(id) => id}
      selection={opts.selection}
      onToggle={onToggle}
      onChooseVersion={onChooseVersion}
      locked
      currentVersion={opts.currentIsFloating ? '0.42-4build1' : '0.42-1ppa1~resolute3'}
      currentIsFloating={opts.currentIsFloating}
    />,
  )
  return { onToggle, onChooseVersion }
}

const chip = (name: RegExp) => screen.getByRole('button', { name }) as HTMLButtonElement

describe('PackageRow — returning to the template’s own choice', () => {
  it('keeps a template pin’s own chip disabled while nothing overrides it', () => {
    renderRow({ currentIsFloating: false })
    expect(chip(/^0\.42-1ppa1~resolute3$/).disabled).toBe(true)
  })

  it('drops the override when the template pin’s own chip is chosen', () => {
    const { onToggle, onChooseVersion } = renderRow({
      currentIsFloating: false,
      selection: { name: 'spice-client-gtk', version: '0.42-4build1', repo: 'ubuntu-resolute-base' },
    })
    const own = chip(/^0\.42-1ppa1~resolute3$/)
    expect(own.disabled).toBe(false)
    fireEvent.click(own)
    expect(onToggle).toHaveBeenCalledWith(false)
    expect(onChooseVersion).not.toHaveBeenCalled()
  })

  it('keeps Unpinned disabled on an unpinned template package while nothing overrides it', () => {
    renderRow({ currentIsFloating: true })
    expect(chip(/^Unpinned$/).disabled).toBe(true)
  })

  it('drops the override when Unpinned is chosen on an unpinned template package', () => {
    const { onToggle, onChooseVersion } = renderRow({
      currentIsFloating: true,
      selection: { name: 'spice-client-gtk', version: '0.42-1ppa1~resolute3', repo: 'snapshot' },
    })
    fireEvent.click(chip(/^Unpinned$/))
    expect(onToggle).toHaveBeenCalledWith(false)
    expect(onChooseVersion).not.toHaveBeenCalled()
  })
})

describe('PackageRow — which chip reads as selected', () => {
  // apt-transport-https as the NVL BKC template lists it: unpinned, with no
  // known current version yet (as in "Show only selected").
  const renderFloating = (selection?: AddedPackage) =>
    render(
      <PackageRow
        name="apt-transport-https"
        version="3.2.1ubuntu0.1"
        versions={[
          { version: '3.2.1ubuntu0.1', repository: 'base' },
          { version: '3.2.0', repository: 'base' },
        ]}
        repoLabelFor={(id) => id}
        selection={selection}
        onToggle={() => {}}
        onChooseVersion={() => {}}
        locked
        currentIsFloating
      />,
    )
  const pressed = () =>
    screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') === 'true').map((b) => b.textContent)

  it('marks only the picked version over an unpinned template entry, not Unpinned too', () => {
    renderFloating({ name: 'apt-transport-https', version: '3.2.0', repo: 'base' })
    expect(pressed()).toEqual(['3.2.0'])
  })

  it('marks Unpinned when nothing is picked over an unpinned template entry', () => {
    renderFloating()
    expect(pressed()).toEqual(['Unpinned'])
  })

  it('marks Unpinned when Unpinned itself is the pick', () => {
    renderFloating({ name: 'apt-transport-https', version: '', repo: 'base' })
    expect(pressed()).toEqual(['Unpinned'])
  })
})
