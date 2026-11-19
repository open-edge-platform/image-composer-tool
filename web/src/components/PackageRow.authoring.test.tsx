// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// In Author mode a template package's checkbox is live: unchecking it removes
// the package from the template, and a removed package can be put back.

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PackageRow } from './PackageRow'
import type { AddedPackage } from '../store'

function renderRow(props: { removable?: boolean; removed?: boolean; selection?: AddedPackage; onRevert?: () => void }) {
  const onToggle = vi.fn()
  const onChooseVersion = vi.fn()
  render(
    <PackageRow
      name="vim"
      version="2:9.1"
      versions={[{ version: '2:9.1', repository: 'base' }, { version: '2:9.2', repository: 'base' }]}
      repoLabelFor={(id) => id}
      onToggle={onToggle}
      onChooseVersion={onChooseVersion}
      locked
      currentVersion="2:9.1"
      browsedRepoId="base"
      {...props}
      selection={props.selection}
    />,
  )
  return { onToggle, onChooseVersion, box: screen.getByRole('checkbox') as HTMLInputElement }
}

describe('PackageRow in Author mode', () => {
  it('keeps a template package locked outside Author mode', () => {
    const { box } = renderRow({})
    expect(box.checked).toBe(true)
    expect(box.disabled).toBe(true)
  })

  it('lets a template package be unchecked to remove it', () => {
    const { box, onToggle } = renderRow({ removable: true })
    expect(box.disabled).toBe(false)
    fireEvent.click(box)
    expect(onToggle).toHaveBeenCalledWith(false)
  })

  it('shows a removed package unchecked and marked Removed, ready to put back', () => {
    const { box, onToggle } = renderRow({ removable: true, removed: true })
    expect(box.checked).toBe(false)
    expect(screen.getByText('Removed')).toBeTruthy()
    fireEvent.click(box)
    expect(onToggle).toHaveBeenCalledWith(true)
  })

  it('reverts a version change without removing the package', () => {
    const onRevert = vi.fn()
    const { onToggle } = renderRow({
      removable: true,
      selection: { name: 'vim', version: '2:9.2', repo: 'base' },
      onRevert,
    })
    fireEvent.click(screen.getByRole('button', { name: '2:9.2' }))
    expect(onRevert).toHaveBeenCalled()
    expect(onToggle).not.toHaveBeenCalled()
  })
})
