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
  partitionSizeMiB,
  setLayoutMode,
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

describe('partition index', () => {
  // Regression: `index` was not read or written, so a template that set one had
  // it silently dropped on the way through the editor. No template in this repo
  // uses it (the Go field is `*int` with omitempty), which is exactly why it
  // needs a test rather than being caught by the golden round-trip.
  it('round-trips a partition index', () => {
    const withIndex = minimalPtlPvRaw.replace('- name: EFI', '- name: EFI\n          index: 1')
    const model = seed(withIndex)
    expect(model.partitions.map((p) => p.index)).toEqual([1, null, null])
    expect(partitionsOf(diskYamlFragment(model))[0].index).toBe(1)
  })

  it('omits index when unset rather than emitting null', () => {
    expect(partitionsOf(diskYamlFragment(seed(minimalPtlPvRaw)))[0]).not.toHaveProperty('index')
  })
})

describe('layout modes', () => {
  it('seeds in size mode', () => {
    expect(seed(minimalPtlPvRaw).layoutMode).toBe('size')
  })

  it('carries the template offsets verbatim for offset mode to start from', () => {
    const model = seed(minimalPtlPvRaw)
    expect(model.partitions.map((p) => [p.start, p.end])).toEqual([
      ['1MiB', '1025MiB'],
      ['1025MiB', '3073MiB'],
      ['3073MiB', '0'],
    ])
  })

  it('emits identical YAML in either mode when nothing is edited', () => {
    const sized = seed(minimalPtlPvRaw)
    expect(diskYamlFragment(setLayoutMode(sized, 'offset'))).toBe(diskYamlFragment(sized))
  })

  it('writes the computed offsets down when switching to offset mode', () => {
    const model = seed(minimalPtlPvRaw)
    model.partitions[0].sizeMiB = 512
    const offset = setLayoutMode(model, 'offset')
    expect(offset.partitions.map((p) => [p.start, p.end])).toEqual([
      ['1MiB', '513MiB'],
      ['513MiB', '2561MiB'],
      ['2561MiB', '0'],
    ])
  })

  it('derives sizes back from hand-edited offsets when switching to size mode', () => {
    const offset = setLayoutMode(seed(minimalPtlPvRaw), 'offset')
    offset.partitions[0] = { ...offset.partitions[0], start: '2MiB', end: '1026MiB' }
    offset.partitions[1] = { ...offset.partitions[1], start: '1026MiB', end: '5122MiB' }
    const sized = setLayoutMode(offset, 'size')
    expect(sized.partitions.map((p) => p.sizeMiB)).toEqual([1024, 4096, null])
    // The first start offset is picked up too, so re-emitting reproduces it.
    expect(sized.firstStartMiB).toBe(2)
    expect(computeOffsets(sized)[0]).toEqual({ start: '2MiB', end: '1026MiB' })
  })

  it('keeps a partition size when its offsets do not parse', () => {
    const offset = setLayoutMode(seed(minimalPtlPvRaw), 'offset')
    offset.partitions[1] = { ...offset.partitions[1], end: 'oops' }
    expect(setLayoutMode(offset, 'size').partitions[1].sizeMiB).toBe(2048)
  })

  it('emits hand-typed offsets untouched, gaps and all', () => {
    const offset = setLayoutMode(seed(minimalPtlPvRaw), 'offset')
    // Deliberate 1024MiB gap after the first partition.
    offset.partitions[1] = { ...offset.partitions[1], start: '2049MiB', end: '4097MiB' }
    const emitted = partitionsOf(diskYamlFragment(offset))
    expect(emitted.map((p) => [p.start, p.end])).toEqual([
      ['1MiB', '1025MiB'],
      ['2049MiB', '4097MiB'],
      ['3073MiB', '0'],
    ])
  })

  it('reports the size a partition derives from its offsets', () => {
    const offset = setLayoutMode(seed(minimalPtlPvRaw), 'offset')
    expect(partitionSizeMiB(offset, 0)).toBe(1024)
    expect(partitionSizeMiB(offset, 2)).toBeNull() // rest of disk
    offset.partitions[0] = { ...offset.partitions[0], end: 'nope' }
    expect(partitionSizeMiB(offset, 0)).toBeUndefined()
  })

  it('measures used space as the high-water mark in offset mode', () => {
    const offset = setLayoutMode(seed(minimalPtlPvRaw), 'offset')
    // Summing spans would report 3072 and miss the gap entirely.
    offset.partitions[1] = { ...offset.partitions[1], start: '2049MiB', end: '4097MiB' }
    offset.partitions[2] = { ...offset.partitions[2], start: '4097MiB' }
    expect(usedMiB(offset)).toBe(4097)
  })
})

describe('offset-mode validation', () => {
  const offsetSeed = () => setLayoutMode(seed(minimalPtlPvRaw), 'offset')

  it('accepts the seeded layout', () => {
    expect(validateDisk(offsetSeed(), { imageType: 'raw' })).toEqual({ errors: [], warnings: [] })
  })

  it('warns about a gap without blocking it', () => {
    const model = offsetSeed()
    model.partitions[1] = { ...model.partitions[1], start: '2049MiB' }
    const { errors, warnings } = validateDisk(model, { imageType: 'raw' })
    expect(errors).toEqual([])
    expect(warnings).toContain('Gap of 1024 MiB between partitions 1 and 2.')
  })

  it('warns about an overlap', () => {
    const model = offsetSeed()
    model.partitions[1] = { ...model.partitions[1], start: '513MiB' }
    expect(validateDisk(model, { imageType: 'raw' }).warnings).toContain(
      'Partitions 1 and 2 overlap by 512 MiB.',
    )
  })

  it('rejects offsets that are not valid sizes', () => {
    const model = offsetSeed()
    model.partitions[0] = { ...model.partitions[0], end: 'later' }
    expect(validateDisk(model, { imageType: 'raw' }).errors.join(' ')).toMatch(
      /offsets that are not valid sizes/,
    )
  })

  it('rejects a partition that ends at or before it starts', () => {
    const model = offsetSeed()
    model.partitions[0] = { ...model.partitions[0], start: '1025MiB', end: '1025MiB' }
    expect(validateDisk(model, { imageType: 'raw' }).errors).toContain(
      'Partition 1 (EFI) ends at or before it starts.',
    )
  })

  it('still rejects an interior "rest" partition', () => {
    const model = offsetSeed()
    model.partitions[0] = { ...model.partitions[0], end: '0' }
    expect(validateDisk(model, { imageType: 'raw' }).errors).toContain(
      'Only the last partition can use the remaining disk space.',
    )
  })

  it('slots a new partition into the gap before the rest partition', () => {
    const model = offsetSeed()
    const partitions = appendPartition(model, { ...newPartition(), id: 'data', sizeMiB: 1024 })
    expect(partitions.map((p) => p.id)).toEqual(['EFI', 'SWAP', 'data', 'ROOT'])
    expect([partitions[2].start, partitions[2].end]).toEqual(['3073MiB', '4097MiB'])
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
    model.partitions = appendPartition(model, {
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
    model.partitions = appendPartition(model, { ...newPartition(), id: 'data' })
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

  // disk.size / disk.maxSize go through the same suffix table as the partition
  // offsets (config.go:1405 and validate/validate.go:257, both mirroring
  // imagedisc.TranslateSizeStrToBytes). lib/size.ts's parser is deliberately
  // looser than that, so these have to be checked against the strict rule.
  it.each(['big', '1.5GiB', '32gib', '1TiB', '34359738368', '32 GiB'])(
    'rejects disk size %s, which the builder would refuse',
    (size) => {
      expect(validateDisk({ ...seed(minimalPtlPvRaw), size }).errors.join(' ')).toMatch(
        /is not a size the builder accepts/,
      )
    },
  )

  // Asserted against the format rule only: a well-formed size can still be too
  // small for the partitions, which is a separate error.
  it.each(['32GiB', '4096MiB', '8GB', '512KiB', '32G', '4M'])(
    'accepts the format of disk size %s',
    (size) => {
      expect(validateDisk({ ...seed(minimalPtlPvRaw), size }).errors.join(' ')).not.toMatch(
        /is not a size the builder accepts/,
      )
    },
  )

  it('applies the same rule to maxSize', () => {
    expect(
      validateDisk({ ...seed(minimalPtlPvRaw), maxSize: '1.5TiB' }).errors.join(' '),
    ).toMatch(/Max size "1\.5TiB" is not a size the builder accepts/)
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
