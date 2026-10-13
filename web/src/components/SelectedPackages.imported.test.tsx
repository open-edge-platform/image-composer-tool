// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// The Selected rail carries two lists that must not be confused: what the user
// picked (editable, and what the compose delta is built from) and what an
// imported template already declares (shown so the rail isn't empty for an
// import, but not a selection).

import { act, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { SelectedPackages } from './SelectedPackages'
import { useStore } from '../store'
import type { PackageRepo } from '../api/types'
import { parseImportedTemplate } from '../lib/importedTemplate'

const repos: PackageRepo[] = [
  { id: 'base', displayName: 'Ubuntu Base', url: 'http://example/base', enabledByDefault: true },
  { id: 'docker', displayName: 'Docker', url: 'http://example/docker', enabledByDefault: false },
]

const templateWith = (packages: string[]) =>
  parseImportedTemplate(
    'handheld.yml',
    [
      'image:',
      '  name: imported-image',
      'systemConfig:',
      '  packages:',
      ...packages.map((p) => `    - ${p}`),
      '',
    ].join('\n'),
  )

beforeEach(() => {
  useStore.setState({ addedPackages: [], enabledRepos: [], importedTemplate: null })
})

describe('SelectedPackages', () => {
  it('lists every package an imported template declares', () => {
    useStore.getState().setImportedTemplate(templateWith(['vim', 'bluez', 'openssh-server']))
    render(<SelectedPackages repos={repos} />)

    for (const name of ['vim', 'bluez', 'openssh-server']) {
      expect(screen.getByTitle(name)).toBeTruthy()
    }
  })

  it('counts imported packages alongside added ones', () => {
    useStore.getState().setImportedTemplate(templateWith(['vim', 'bluez']))
    useStore.getState().setPackage({ name: 'htop', version: '', repo: 'base' })
    render(<SelectedPackages repos={repos} />)

    expect(screen.getByText('Selected (3)')).toBeTruthy()
  })

  it('attributes imported packages to the template rather than a repository', () => {
    useStore.getState().setImportedTemplate(templateWith(['vim']))
    render(<SelectedPackages repos={repos} />)

    expect(screen.getByText('From imported-image.yml')).toBeTruthy()
  })

  // A pinned entry is `name_version`, and rpm names may contain `_`, so the
  // entry is shown whole rather than guessed apart into a name and a version.
  it('shows a pinned entry exactly as the template wrote it', () => {
    useStore.getState().setImportedTemplate(templateWith(['linux-image-6.18-intel_260713t060441z-r2']))
    render(<SelectedPackages repos={repos} />)

    expect(screen.getByTitle('linux-image-6.18-intel_260713t060441z-r2')).toBeTruthy()
  })

  // Clearing is for the user's own picks; an imported template's list is not
  // one, so the control must not appear to offer removing it.
  it('offers Clear only when the user has added something', () => {
    useStore.getState().setImportedTemplate(templateWith(['vim']))
    const { rerender } = render(<SelectedPackages repos={repos} />)
    expect(screen.queryByRole('button', { name: 'Clear' })).toBeNull()

    act(() => useStore.getState().setPackage({ name: 'htop', version: '', repo: 'base' }))
    rerender(<SelectedPackages repos={repos} />)
    expect(screen.getByRole('button', { name: 'Clear' })).toBeTruthy()
  })

  it('keeps an added package removable while an import is active', () => {
    useStore.getState().setImportedTemplate(templateWith(['vim']))
    useStore.getState().setPackage({ name: 'htop', version: '', repo: 'base' })
    render(<SelectedPackages repos={repos} />)

    expect(screen.getByLabelText('Remove htop')).toBeTruthy()
    // The imported entry has no remove control of its own.
    expect(screen.queryByLabelText('Remove vim')).toBeNull()
  })

  it('still reports an empty rail when neither list has anything', () => {
    render(<SelectedPackages repos={repos} />)
    const rail = screen.getByText('Selected (0)').closest('div')!.parentElement!
    expect(within(rail).getByText('No packages added yet.')).toBeTruthy()
  })
})
