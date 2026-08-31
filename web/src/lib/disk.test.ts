// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import yaml from 'js-yaml'
import { describe, expect, it } from 'vitest'
import {
  appendPartition,
  computeOffsets,
  diskYamlFragment,
  newPartition,
  parseDiskFromYaml,
  suggestedSize,
  toDiskConfig,
  usedMiB,
  validateDisk,
} from './disk'
import type { DiskModel } from './disk'
import { ALL_FIXTURES, minimalPtlPvRaw, roboticsJazzyIso } from './fixtures'

// Reads the partitions back out of an emitted disk block, so the round-trip
// assertions compare offsets rather than YAML formatting.
function partitionsOf(text: string): Record<string, unknown>[] {
  const doc = yaml.load(text) as { disk?: { partitions?: Record<string, unknown>[] } }
  return doc.disk?.partitions ?? []
}

function seed(text: string): DiskModel {
  const model = parseDiskFromYaml(text)
  if (!model) throw new Error('fixture did not parse')
  return model
}

describe('parseDiskFromYaml -> toDiskConfig round-trip', () => {
  // The guard against preview/build divergence: for every combination the
  // manifest can actually build, seeding the editor and emitting it again with
  // no edits must reproduce the template's own start/end offsets exactly.
  it.each(ALL_FIXTURES)('reproduces $name offsets byte-for-byte', ({ yaml: text }) => {
    const original = partitionsOf(text)
    const emitted = partitionsOf(diskYamlFragment(seed(text)))

    expect(emitted).toHaveLength(original.length)
    original.forEach((p, i) => {
      expect(emitted[i].start).toBe(p.start)
      expect(emitted[i].end).toBe(p.end)
    })
  })

  it.each(ALL_FIXTURES)('preserves $name identity and mount fields', ({ yaml: text }) => {
    const original = partitionsOf(text)
    const emitted = partitionsOf(diskYamlFragment(seed(text)))

    original.forEach((p, i) => {
      for (const key of ['id', 'name', 'type', 'typeUUID', 'fsType', 'mountPoint', 'mountOptions']) {
        // The resolved template spells out empty keys; a user template omits
        // them. Anything non-empty has to survive unchanged.
        if (p[key] !== '' && p[key] !== undefined) expect(emitted[i][key]).toBe(p[key])
      }
      expect(emitted[i].flags ?? []).toEqual(p.flags ?? [])
    })
  })

  it('drops the empty keys the merged template spells out', () => {
    // resolve --full emits path: "", size: "", artifacts: [], typeUUID: "" etc.
    // because the Go structs have no omitempty. Echoing those into a user
    // template would be noise the schema does not require.
    const emitted = yaml.load(diskYamlFragment(seed(roboticsJazzyIso))) as {
      disk: Record<string, unknown>
    }
    expect(emitted.disk).not.toHaveProperty('path')
    expect(emitted.disk).not.toHaveProperty('size')
    expect(emitted.disk).not.toHaveProperty('artifacts')
    expect(partitionsOf(diskYamlFragment(seed(roboticsJazzyIso)))[0]).not.toHaveProperty('typeUUID')
  })
})

describe('seeding', () => {
  it('derives a size in MiB from start/end, and null for "rest of disk"', () => {
    const model = seed(minimalPtlPvRaw)
    // EFI 1MiB->1025MiB, SWAP 1025MiB->3073MiB, ROOT 3073MiB->"0".
    expect(model.partitions.map((p) => p.sizeMiB)).toEqual([1024, 2048, null])
    expect(model.firstStartMiB).toBe(1)
    expect(model.size).toBe('32GiB')
    expect(model.artifacts.map((a) => [a.type, a.compression])).toEqual([['raw', 'gz']])
  })

  it('keeps an ISO template sizeless rather than inventing a default', () => {
    const model = seed(roboticsJazzyIso)
    expect(model.size).toBe('')
    expect(model.artifacts).toEqual([])
    expect(model.partitionTableType).toBe('gpt')
    expect(model.partitions.map((p) => p.id)).toEqual(['boot', 'rootfs'])
  })

  it('returns null when there is no disk block to seed from', () => {
    expect(parseDiskFromYaml('image:\n  name: x\n')).toBeNull()
    expect(parseDiskFromYaml('this: [is not, valid: yaml')).toBeNull()
    expect(parseDiskFromYaml('')).toBeNull()
  })
})

describe('computeOffsets', () => {
  it('lays partitions out contiguously from the first start', () => {
    const model = seed(minimalPtlPvRaw)
    expect(computeOffsets(model)).toEqual([
      { start: '1MiB', end: '1025MiB' },
      { start: '1025MiB', end: '3073MiB' },
      { start: '3073MiB', end: '0' },
    ])
  })

  it('recomputes downstream offsets when a size changes', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions[0].sizeMiB = 512
    expect(computeOffsets(model)).toEqual([
      { start: '1MiB', end: '513MiB' },
      { start: '513MiB', end: '2561MiB' },
      { start: '2561MiB', end: '0' },
    ])
  })

  it('recomputes offsets when partitions are reordered', () => {
    const model = seed(minimalPtlPvRaw)
    ;[model.partitions[0], model.partitions[1]] = [model.partitions[1], model.partitions[0]]
    expect(computeOffsets(model)).toEqual([
      { start: '1MiB', end: '2049MiB' },
      { start: '2049MiB', end: '3073MiB' },
      { start: '3073MiB', end: '0' },
    ])
  })

  it('closes the gap when a partition is removed', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions.splice(1, 1)
    expect(computeOffsets(model)).toEqual([
      { start: '1MiB', end: '1025MiB' },
      { start: '1025MiB', end: '0' },
    ])
  })

  it('appends a new partition after the existing ones', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions[2].sizeMiB = 4096
    model.partitions.push({ ...newPartition(), id: 'data', sizeMiB: null })
    expect(computeOffsets(model).map((o) => o.end)).toEqual(['1025MiB', '3073MiB', '7169MiB', '0'])
  })

  // Regression: adding a partition to a seeded layout used to append after the
  // rootfs, which is the "rest of disk" partition. Its end of "0" does not
  // advance the cursor, so the new partition started at the same offset as the
  // one before it and the browser showed two partitions overlapping.
  it('keeps a "rest" partition last when a partition is added', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions = appendPartition(model.partitions, {
      ...newPartition(),
      id: 'data',
      sizeMiB: 1024,
    })
    expect(model.partitions.map((p) => p.id)).toEqual(['EFI', 'SWAP', 'data', 'ROOT'])
    expect(computeOffsets(model)).toEqual([
      { start: '1MiB', end: '1025MiB' },
      { start: '1025MiB', end: '3073MiB' },
      { start: '3073MiB', end: '4097MiB' },
      { start: '4097MiB', end: '0' },
    ])
    expect(validateDisk(model).errors).toEqual([])
  })

  it('appends normally when no partition takes the remainder', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions[2].sizeMiB = 8192
    model.partitions = appendPartition(model.partitions, { ...newPartition(), id: 'data' })
    expect(model.partitions.map((p) => p.id)).toEqual(['EFI', 'SWAP', 'ROOT', 'data'])
  })

  it('forces the last partition to "0" when the fill-disk option is set', () => {
    // internal/config/config.go: extendLastPartitionToFillDisk overrides the
    // final partition's end, so the preview has to agree with the builder.
    const model = { ...seed(minimalPtlPvRaw), extendLastPartitionToFillDisk: true }
    model.partitions[2].sizeMiB = 4096
    const offsets = computeOffsets(model)
    expect(offsets[2]).toEqual({ start: '3073MiB', end: '0' })
    expect(toDiskConfig(model).extendLastPartitionToFillDisk).toBe(true)
  })
})

describe('usedMiB / suggestedSize', () => {
  it('counts fixed partitions plus the leading gap, ignoring the rest partition', () => {
    expect(usedMiB(seed(minimalPtlPvRaw))).toBe(1 + 1024 + 2048)
  })

  it('rounds a suggestion up to whole GiB', () => {
    expect(suggestedSize(seed(minimalPtlPvRaw))).toBe('4GiB')
    expect(suggestedSize(seed(roboticsJazzyIso))).toBe('1GiB')
  })
})

describe('validateDisk', () => {
  it('accepts every buildable template as seeded', () => {
    for (const { name, yaml: text } of ALL_FIXTURES) {
      expect({ name, ...validateDisk(seed(text)) }).toMatchObject({ errors: [] })
    }
  })

  it('requires a disk name', () => {
    const model = { ...seed(minimalPtlPvRaw), name: '  ' }
    expect(validateDisk(model).errors).toContain('Disk name is required.')
  })

  it('rejects a "rest" partition that is not last', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions[0].sizeMiB = null
    expect(validateDisk(model).errors).toContain(
      'Only the last partition can use the remaining disk space.',
    )
  })

  it('rejects partitions that overflow the disk', () => {
    const model = { ...seed(minimalPtlPvRaw), size: '2GiB' }
    expect(validateDisk(model).errors.join(' ')).toMatch(/Partitions need 3073 MiB/)
  })

  it('rejects a zero-size partition that is not the rest partition', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions[0].sizeMiB = 0
    expect(validateDisk(model).errors.join(' ')).toMatch(/needs a size greater than zero/)
  })

  it('applies the maxSize rules the builder enforces', () => {
    const base = seed(minimalPtlPvRaw)
    expect(validateDisk({ ...base, maxSize: '16GiB' }).errors).toContain(
      'Max size must be greater than the disk size.',
    )
    expect(validateDisk({ ...base, size: '', maxSize: '64GiB' }).errors).toContain(
      'Max size requires a disk size to be set.',
    )
    expect(validateDisk({ ...base, maxSize: '64GiB' }).errors).toEqual([])
  })

  it('rejects an unparseable size', () => {
    expect(validateDisk({ ...seed(minimalPtlPvRaw), size: 'big' }).errors.join(' ')).toMatch(
      /is not a valid size/,
    )
  })

  // These rules live in the template loader, not the JSON schema
  // (internal/config/validate/validate.go), so a disk block can be
  // schema-clean and still be rejected before the build starts.
  describe('extendLastPartitionToFillDisk', () => {
    const withFill = (text: string): DiskModel => ({
      ...seed(text),
      extendLastPartitionToFillDisk: true,
    })

    it('is rejected for ISO images', () => {
      expect(validateDisk(withFill(roboticsJazzyIso), { imageType: 'iso' }).errors).toContain(
        'Filling the remaining disk space is not supported for ISO images.',
      )
    })

    it('requires the last RAW partition to be the root filesystem', () => {
      const model = withFill(minimalPtlPvRaw)
      // Give the seeded rootfs a size first: it is the "rest" partition, and
      // an interior "rest" is its own (separate) error.
      model.partitions[2].sizeMiB = 8192
      model.partitions.push({ ...newPartition(), id: 'data', mountPoint: '/data' })
      expect(validateDisk(model, { imageType: 'raw' }).errors.join(' ')).toMatch(
        /requires the last partition to be the root filesystem \("\/"\), not "\/data"/,
      )
    })

    it('accepts a RAW layout whose last partition is the rootfs', () => {
      expect(validateDisk(withFill(minimalPtlPvRaw), { imageType: 'raw' }).errors).toEqual([])
    })

    it('leaves other image types alone, as the loader does', () => {
      const model = withFill(minimalPtlPvRaw)
      // Give the seeded rootfs a size first: it is the "rest" partition, and
      // an interior "rest" is its own (separate) error.
      model.partitions[2].sizeMiB = 8192
      model.partitions.push({ ...newPartition(), id: 'data', mountPoint: '/data' })
      expect(validateDisk(model, { imageType: 'img' }).errors).toEqual([])
    })

    it('imposes nothing when the flag is off', () => {
      expect(validateDisk(seed(roboticsJazzyIso), { imageType: 'iso' }).errors).toEqual([])
    })
  })

  it('rejects an artifact with no format', () => {
    const model = seed(minimalPtlPvRaw)
    model.artifacts = [{ key: 'a', type: '', compression: 'gz' }]
    expect(validateDisk(model).errors).toContain('Output artifact 1 needs a format.')
  })
})

describe('toDiskConfig', () => {
  it('emits artifacts with and without compression', () => {
    const model = seed(roboticsJazzyIso)
    model.artifacts = [
      { key: 'a', type: 'qcow2', compression: 'xz' },
      { key: 'b', type: 'vhdx', compression: '' },
    ]
    expect(toDiskConfig(model).artifacts).toEqual([
      { type: 'qcow2', compression: 'xz' },
      { type: 'vhdx' },
    ])
  })

  it('carries the partition table type through both ways', () => {
    const model = seed(minimalPtlPvRaw)
    expect(toDiskConfig(model).partitionTableType).toBe('gpt')
    expect(toDiskConfig({ ...model, partitionTableType: 'mbr' }).partitionTableType).toBe('mbr')
  })

  it('emits nothing for the fill-disk flag when it is off', () => {
    expect(toDiskConfig(seed(minimalPtlPvRaw))).not.toHaveProperty(
      'extendLastPartitionToFillDisk',
    )
  })
})
