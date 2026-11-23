// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { DiskStep } from './DiskStep'
import { useStore } from '../store'
import { parseDiskFromYaml } from '../lib/disk'
import { minimalPtlPvRaw } from '../lib/fixtures'

describe('DiskStep for an overlay template', () => {
  beforeEach(() => {
    useStore.setState({
      disk: parseDiskFromYaml(minimalPtlPvRaw),
      diskEdited: false,
      selection: { ...useStore.getState().selection, imageType: 'raw' },
    })
  })

  it('offers the partition table and partitions for a template built from scratch', () => {
    const { container } = render(<DiskStep />)
    expect(screen.queryByText('Partition Table')).not.toBeNull()
    expect(container.textContent).toContain('Partitions')
    expect(screen.queryByLabelText('Max Size')).toBeNull()
  })

  it('keeps the baseline layout, offering only size, max size and artifacts', () => {
    const { container } = render(<DiskStep overlay />)
    expect(screen.queryByText('Partition Table')).toBeNull()
    expect(container.textContent).not.toContain('Partitions')
    expect(screen.getByText(/keeps its partition\s+table and partitions/)).not.toBeNull()
    expect(screen.getByLabelText('Disk Size')).not.toBeNull()
    expect(screen.getByLabelText('Max Size')).not.toBeNull()
  })

  it('offers the base image only for an overlay template, and shows what is wrong with it', () => {
    useStore.setState({ baseImage: '/path/to/ubuntu-26.04-base.raw', baseImageEdited: false })
    const { rerender } = render(<DiskStep />)
    expect(screen.queryByLabelText(/Base Image/)).toBeNull()

    rerender(<DiskStep overlay baseImageIssue="This is the template's placeholder." />)
    const input = screen.getByLabelText(/Base Image/) as HTMLInputElement
    expect(input.value).toBe('/path/to/ubuntu-26.04-base.raw')
    expect(screen.getByRole('alert').textContent).toBe("This is the template's placeholder.")

    fireEvent.change(input, { target: { value: '/images/base.raw' } })
    expect(useStore.getState()).toMatchObject({ baseImage: '/images/base.raw', baseImageEdited: true })
  })
})
