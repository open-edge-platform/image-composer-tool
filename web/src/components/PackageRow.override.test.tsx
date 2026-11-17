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

describe('PackageRow — what the header says the build installs', () => {
  const header = () => screen.getByText(/^(Pinned to|Resolved to|Unpinned)/, { selector: 'span' }).textContent

  it('says "Pinned to" the template pin, and marks its chip selected but not clickable', () => {
    renderRow({ currentIsFloating: false })
    expect(header()).toBe('Pinned to 0.42-1ppa1~resolute3')
    const own = chip(/^0\.42-1ppa1~resolute3$/)
    expect(own.getAttribute('aria-pressed')).toBe('true')
    expect(own.disabled).toBe(true)
    expect(own.className).toContain('bg-[#0071c5]')
  })

  it('says "Pinned to" an override, ahead of the template’s own pin', () => {
    renderRow({
      currentIsFloating: false,
      selection: { name: 'spice-client-gtk', version: '0.42-4build1', repo: 'ubuntu-resolute-base' },
    })
    expect(header()).toBe('Pinned to 0.42-4build1')
  })

  it('says "Resolved to" for an unpinned template package, and marks Unpinned selected', () => {
    // adb, unpinned in the template, resolving to Ubuntu's build.
    render(
      <PackageRow
        name="adb"
        version="1:34.0.5-12build1"
        versions={[{ version: '1:34.0.5-12build1', repository: 'base' }]}
        repoLabelFor={(id) => id}
        selection={undefined}
        onToggle={() => {}}
        onChooseVersion={() => {}}
        locked
        currentVersion="1:34.0.5-12build1"
        currentIsFloating
      />,
    )
    expect(header()).toBe('Resolved to 1:34.0.5-12build1')
    const unpinned = chip(/^Unpinned$/)
    expect(unpinned.getAttribute('aria-pressed')).toBe('true')
    expect(unpinned.className).toContain('bg-[#0071c5]')
  })

  it('says "Resolved to" for an unpinned pick, naming where it resolves from', () => {
    render(
      <PackageRow
        name="vim"
        version="2:9.1.0016-1"
        versions={[{ version: '2:9.1.0016-1', repository: 'base' }]}
        repoLabelFor={(id) => ({ base: 'Ubuntu Base', snapshot: 'Snapshot' })[id] ?? id}
        selection={{ name: 'vim', version: '', repo: 'base' }}
        onToggle={() => {}}
        onChooseVersion={() => {}}
        resolvedVersion="2:9.2-1ppa1"
        resolvedRepo="snapshot"
      />,
    )
    expect(header()).toBe('Resolved to 2:9.2-1ppa1')
    expect(screen.getByText('via Snapshot')).toBeTruthy()
  })

  it('shows just the version on a catalog row nothing selects', () => {
    render(
      <PackageRow
        name="htop"
        version="3.3.0-4"
        versions={[{ version: '3.3.0-4', repository: 'base' }]}
        repoLabelFor={(id) => id}
        selection={undefined}
        onToggle={() => {}}
        onChooseVersion={() => {}}
      />,
    )
    expect(screen.queryByText(/^(Pinned to|Resolved to|Unpinned)/, { selector: 'span' })).toBeNull()
    expect(screen.getAllByText('3.3.0-4').length).toBeGreaterThan(0)
  })
})

describe('PackageRow — the template line once overridden', () => {
  const templateLine = () => screen.getByText(/^Already in template/).textContent

  it('asks to pin an unpinned template package only until it is', () => {
    renderRow({ currentIsFloating: true })
    expect(templateLine()).toBe('Already in template (unpinned; pin a chip to freeze it)')
  })

  it('says the template had it unpinned once a version overrides it', () => {
    // apt-transport-https, unpinned in the template, pinned to 3.2.1ubuntu0.1.
    renderRow({
      currentIsFloating: true,
      selection: { name: 'spice-client-gtk', version: '0.42-4build1', repo: 'ubuntu-resolute-base' },
    })
    expect(templateLine()).toBe('Already in template (unpinned)')
    expect(screen.getByText('Overrides with 0.42-4build1')).toBeTruthy()
  })

  it('says what the template pinned once a different version overrides it', () => {
    renderRow({
      currentIsFloating: false,
      selection: { name: 'spice-client-gtk', version: '0.42-4build1', repo: 'ubuntu-resolute-base' },
    })
    expect(templateLine()).toBe('Already in template (pinned to 0.42-1ppa1~resolute3)')
  })

  it('says nothing more for a template pin left as it is', () => {
    renderRow({ currentIsFloating: false })
    expect(templateLine()).toBe('Already in template')
  })
})
