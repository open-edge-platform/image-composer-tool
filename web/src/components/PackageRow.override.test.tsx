// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// A template package's own choice — the version it pins, or Latest when it
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

  it('keeps Latest disabled on an unpinned template package while nothing overrides it', () => {
    renderRow({ currentIsFloating: true })
    expect(chip(/^Latest$/).disabled).toBe(true)
  })

  it('drops the override when Latest is chosen on an unpinned template package', () => {
    const { onToggle, onChooseVersion } = renderRow({
      currentIsFloating: true,
      selection: { name: 'spice-client-gtk', version: '0.42-1ppa1~resolute3', repo: 'snapshot' },
    })
    fireEvent.click(chip(/^Latest$/))
    expect(onToggle).toHaveBeenCalledWith(false)
    expect(onChooseVersion).not.toHaveBeenCalled()
  })
})
