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

// The Pinned/Unpinned tag beside the name, and the version chip the header
// shows when none of the row's own chips does.
const tagText = () => screen.queryByText(/^(Pinned|Unpinned)$/, { selector: 'span' })?.textContent
const headerChip = () =>
  screen.queryByTitle('Pinned version') ?? screen.queryByTitle('The version Unpinned currently resolves to')

describe('PackageRow — what the header says the build installs', () => {

  it('leaves a template pin on screen to its chip, selected but not clickable', () => {
    renderRow({ currentIsFloating: false })
    expect(headerChip()).toBeNull()
    const own = chip(/^0\.42-1ppa1~resolute3$/)
    expect(own.getAttribute('aria-pressed')).toBe('true')
    expect(own.disabled).toBe(true)
    expect(own.className).toContain('bg-[#0071c5]')
  })

  it('leaves an override on screen to its chip, ahead of the template’s own pin', () => {
    renderRow({
      currentIsFloating: false,
      selection: { name: 'spice-client-gtk', version: '0.42-4build1', repo: 'ubuntu-resolute-base' },
    })
    expect(headerChip()).toBeNull()
    expect(chip(/^0\.42-4build1$/).getAttribute('aria-pressed')).toBe('true')
  })

  it('shows a pin from another repository as a chip beside the name', () => {
    // ffmpeg, browsing Ubuntu base: the template's pin is the snapshot's build.
    render(
      <PackageRow
        name="ffmpeg"
        version="7:8.0.1-3ubuntu2"
        versions={[{ version: '7:8.0.1-3ubuntu2', repository: 'base' }]}
        repoLabelFor={(id) => id}
        selection={undefined}
        onToggle={() => {}}
        onChooseVersion={() => {}}
        locked
        currentVersion="7:9.0.1-1ppa1~resolute1"
        currentIsFloating={false}
      />,
    )
    expect(tagText()).toBe('Pinned')
    expect(headerChip()?.textContent).toBe('7:9.0.1-1ppa1~resolute1')
    expect(headerChip()?.getAttribute('title')).toBe('Pinned version')
  })

  it('keeps a pinned chip visible even when it is past the first few versions', () => {
    // ethtool, pinned to an older build than the few chips shown by default.
    const many = ['1:6.19-4', '1:6.19-3', '1:6.19-2', '1:6.19-1'].map((version) => ({ version, repository: 'base' }))
    render(
      <PackageRow
        name="ethtool"
        version="1:6.19-4"
        versions={many}
        repoLabelFor={(id) => id}
        selection={{ name: 'ethtool', version: '1:6.19-1', repo: 'base' }}
        onToggle={() => {}}
        onChooseVersion={() => {}}
      />,
    )
    expect(chip(/^1:6\.19-1$/).getAttribute('aria-pressed')).toBe('true')
    expect(headerChip()).toBeNull()
  })

  it('tags an unpinned template package, and marks Unpinned selected and its version dashed', () => {
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
    expect(tagText()).toBe('Unpinned')
    expect(headerChip()).toBeNull()
    expect(chip(/^1:34\.0\.5-12build1$/).className).toContain('border-dashed')
    const unpinned = chip(/^Unpinned$/)
    expect(unpinned.getAttribute('aria-pressed')).toBe('true')
    expect(unpinned.className).toContain('bg-[#0071c5]')
  })

  it('shows what an unpinned pick resolves to from another repository as a dashed chip', () => {
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
    expect(tagText()).toBe('Unpinned')
    expect(headerChip()?.textContent).toBe('2:9.2-1ppa1')
    expect(headerChip()?.className).toContain('border-dashed')
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
    expect(tagText()).toBeUndefined()
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

describe('PackageRow — browsing one repository', () => {
  const via = () => screen.queryByText(/^via /, { selector: 'span' })

  it('names neither the pin nor the browsed repo when its chip shows the pin', () => {
    // ffmpeg, browsing the snapshot its template pin comes from.
    render(
      <PackageRow
        name="ffmpeg"
        version="7:9.0.1-1ppa1~resolute1"
        versions={[{ version: '7:9.0.1-1ppa1~resolute1', repository: 'snapshot' }]}
        repoLabelFor={(id) => id}
        selection={undefined}
        onToggle={() => {}}
        onChooseVersion={() => {}}
        locked
        currentVersion="7:9.0.1-1ppa1~resolute1"
        currentIsFloating={false}
        browsedRepoId="snapshot"
      />,
    )
    expect(tagText()).toBe('Pinned')
    expect(headerChip()).toBeNull()
    expect(via()).toBeNull()
    expect(chip(/^7:9\.0\.1-1ppa1~resolute1$/).getAttribute('aria-pressed')).toBe('true')
  })

  it('leaves an unpinned template package to its highlighted Unpinned chip', () => {
    // adb, unpinned in the template, its current version not yet known.
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
        currentIsFloating
        browsedRepoId="base"
      />,
    )
    expect(tagText()).toBe('Unpinned')
    expect(headerChip()).toBeNull()
    expect(via()).toBeNull()
    expect(chip(/^Unpinned$/).getAttribute('aria-pressed')).toBe('true')
  })

  it('still names the repository a resolution comes from when it is another one', () => {
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
        browsedRepoId="base"
      />,
    )
    expect(headerChip()?.textContent).toBe('2:9.2-1ppa1')
    expect(via()?.textContent).toBe('via Snapshot')
  })
})

describe('PackageRow — the version Unpinned resolves to', () => {
  it('marks it dashed among the chips when this repo publishes it', () => {
    render(
      <PackageRow
        name="vim"
        version="2:9.1.0016-1"
        versions={[
          { version: '2:9.1.0016-1', repository: 'base' },
          { version: '2:9.1.0000-1', repository: 'base' },
        ]}
        repoLabelFor={(id) => id}
        selection={{ name: 'vim', version: '', repo: 'base' }}
        onToggle={() => {}}
        onChooseVersion={() => {}}
        resolvedVersion="2:9.1.0016-1"
        resolvedRepo="base"
        browsedRepoId="base"
      />,
    )
    expect(headerChip()).toBeNull()
    expect(chip(/^2:9\.1\.0016-1$/).className).toContain('border-dashed')
    expect(chip(/^2:9\.1\.0016-1$/).getAttribute('aria-pressed')).toBe('false')
    expect(chip(/^2:9\.1\.0000-1$/).className).not.toContain('border-dashed')
    expect(chip(/^Unpinned$/).getAttribute('aria-pressed')).toBe('true')
  })

  it('links "via" to the repo it resolves from, when that is another one', () => {
    const onBrowseRepo = vi.fn()
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
        browsedRepoId="base"
        onBrowseRepo={onBrowseRepo}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Snapshot' }))
    expect(onBrowseRepo).toHaveBeenCalledWith('snapshot')
  })
})
