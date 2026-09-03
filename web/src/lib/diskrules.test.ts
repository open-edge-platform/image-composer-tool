// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// These tests pin the transcription in diskrules.ts to the Go source it came
// from. They cannot detect the Go side changing — nothing here reads it — so
// each case names the file and line, and a failing build against a value this
// module allows means re-reading that source, not relaxing the test.

import { describe, expect, it } from 'vitest'
import {
  ARTIFACT_TYPES_DISK,
  COMPRESSIONS_DISK,
  FS_TYPES,
  PARTITION_TYPES,
  PARTITION_TYPE_GUIDS,
  artifactOptions,
  artifactSupport,
  isValidDiskSize,
  isValidOffset,
  isValidTypeUUID,
} from './diskrules'
import { parseDiskFromYaml, setLayoutMode, validateDisk } from './disk'
import { ALL_FIXTURES, minimalPtlPvRaw, roboticsJazzyIso } from './fixtures'

describe('isValidOffset — imagedisc.go:329 VerifyFileSize', () => {
  it('accepts every suffix in sizeSuffixesList (imagedisc.go:96)', () => {
    for (const v of ['1KiB', '1MiB', '1GiB', '1K', '1M', '1G', '1KB', '1MB', '1GB', '3073MiB']) {
      expect(isValidOffset(v), v).toBe(true)
    }
  })

  it('accepts "0", the rest-of-disk sentinel special-cased before the regex', () => {
    expect(isValidOffset('0')).toBe(true)
  })

  it('is stricter than it looks — these all fail at build time', () => {
    // `^(\d+)(.*)$` puts everything after the leading digits into the suffix,
    // which then has to match the table exactly.
    expect(isValidOffset('1.5GiB')).toBe(false) // suffix becomes ".5GiB"
    expect(isValidOffset('1mib')).toBe(false) // case-sensitive
    expect(isValidOffset('1 MiB')).toBe(false) // suffix becomes " MiB"
    expect(isValidOffset('1048576')).toBe(false) // empty suffix is not in the table
    expect(isValidOffset('1TiB')).toBe(false) // no TiB in the table
    expect(isValidOffset('')).toBe(false)
    expect(isValidOffset('MiB')).toBe(false)
  })
})

describe('isValidDiskSize', () => {
  it('matches the offset rule but without the "0" sentinel', () => {
    expect(isValidDiskSize('32GiB')).toBe(true)
    expect(isValidDiskSize('0')).toBe(false)
  })
})

describe('isValidTypeUUID', () => {
  it('accepts a full GPT GUID', () => {
    expect(isValidTypeUUID('4f68bce3-e8cd-4db1-96e7-fbcaf984b709')).toBe(true)
    expect(isValidTypeUUID('C12A7328-F81F-11D2-BA4B-00A0C93EC93B')).toBe(true)
  })

  it('accepts an sgdisk short code — config.go:439 documents "8300"', () => {
    expect(isValidTypeUUID('8300')).toBe(true)
    expect(isValidTypeUUID('ef00')).toBe(true)
  })

  it('rejects anything else', () => {
    expect(isValidTypeUUID('linux-root-amd64')).toBe(false)
    expect(isValidTypeUUID('4f68bce3')).toBe(false)
    expect(isValidTypeUUID('4f68bce3-e8cd-4db1-96e7-fbcaf984b70')).toBe(false)
  })
})

describe('partition type table — imagedisc.go:98-115', () => {
  it('every listed type has a GUID and vice versa', () => {
    expect(Object.keys(PARTITION_TYPE_GUIDS).sort()).toEqual([...PARTITION_TYPES].sort())
    for (const guid of Object.values(PARTITION_TYPE_GUIDS)) {
      expect(isValidTypeUUID(guid), guid).toBe(true)
    }
  })

  it('covers the types the shipped templates actually use', () => {
    // If a template used a type this table lacks, the UI would warn about a
    // value the builder handles perfectly well.
    const used = new Set<string>()
    for (const f of ALL_FIXTURES) {
      for (const p of parseDiskFromYaml(f.yaml)!.partitions) if (p.type) used.add(p.type)
    }
    expect([...used].filter((t) => !(PARTITION_TYPES as readonly string[]).includes(t))).toEqual([])
  })
})

describe('fsType — imagedisc.go:728 + :763', () => {
  it('covers every fsType the shipped templates use', () => {
    const used = new Set<string>()
    for (const f of ALL_FIXTURES) {
      for (const p of parseDiskFromYaml(f.yaml)!.partitions) if (p.fsType) used.add(p.fsType)
    }
    expect([...used].filter((t) => !(FS_TYPES as readonly string[]).includes(t))).toEqual([])
  })

  it('is rejected as an error, not a warning, when unlisted', () => {
    const model = parseDiskFromYaml(minimalPtlPvRaw)!
    model.partitions[0].fsType = 'btrfs'
    expect(validateDisk(model, { imageType: 'raw' }).errors.join(' ')).toMatch(
      /unsupported filesystem "btrfs"/,
    )
  })

  it('requires a filesystem at all', () => {
    const model = parseDiskFromYaml(minimalPtlPvRaw)!
    model.partitions[0].fsType = ''
    expect(validateDisk(model, { imageType: 'raw' }).errors.join(' ')).toMatch(
      /needs a filesystem type/,
    )
  })
})

describe('artifact combinations', () => {
  it('offers only the qemu-img conversions that exist (imageconvert.go:246)', () => {
    expect(artifactOptions('raw').types).toEqual(ARTIFACT_TYPES_DISK)
    // `tar` is in the schema enum but has no conversion case.
    expect(artifactOptions('raw').types).not.toContain('tar')
  })

  it('offers only the compressors that exist (compression.go:58)', () => {
    expect(artifactOptions('raw').compressions).toEqual(COMPRESSIONS_DISK)
    // `gzip` and `bz2` are in the schema enum but CompressFile has no case.
    expect(artifactOptions('raw').compressions).not.toContain('gzip')
    expect(artifactOptions('raw').compressions).not.toContain('bz2')
  })

  it('routes wsl2 to the tar+gz pipeline (wsl2maker.go:106-123)', () => {
    const o = artifactOptions('wsl2')
    expect(o.types).toEqual(['tar'])
    expect(o.compressions).toEqual(['gz', 'gzip'])
    expect(o.compressionRequired).toBe(true)
  })

  it('knows iso and img never run the pipeline', () => {
    expect(artifactSupport('iso')).toBe('ignored')
    expect(artifactSupport('img')).toBe('ignored')
    expect(artifactSupport('raw')).toBe('disk')
    expect(artifactSupport('wsl2')).toBe('wsl2')
  })

  it('rejects a schema-legal but unbuildable combination', () => {
    const model = parseDiskFromYaml(minimalPtlPvRaw)!
    model.artifacts = [{ key: 'a', type: 'tar', compression: 'bz2' }]
    const { errors } = validateDisk(model, { imageType: 'raw' })
    expect(errors.join(' ')).toMatch(/cannot be written as "tar"/)
    expect(errors.join(' ')).toMatch(/"bz2" compression is not implemented/)
  })

  it('accepts the template default and a qcow2 conversion', () => {
    const model = parseDiskFromYaml(minimalPtlPvRaw)!
    expect(validateDisk(model, { imageType: 'raw' }).errors).toEqual([])
    model.artifacts = [{ key: 'a', type: 'qcow2', compression: 'xz' }]
    expect(validateDisk(model, { imageType: 'raw' }).errors).toEqual([])
  })

  it('warns rather than errors when the image type ignores artifacts', () => {
    const model = parseDiskFromYaml(roboticsJazzyIso)!
    model.artifacts = [{ key: 'a', type: 'qcow2', compression: '' }]
    const { errors, warnings } = validateDisk(model, { imageType: 'iso' })
    expect(errors).toEqual([])
    expect(warnings.join(' ')).toMatch(/ignored for ISO images/)
  })

  it('requires an artifact for wsl2', () => {
    const model = parseDiskFromYaml(roboticsJazzyIso)!
    expect(validateDisk(model, { imageType: 'wsl2' }).errors).toContain(
      'A WSL2 image needs exactly one tar artifact with gz compression.',
    )
  })

  it('every shipped template validates against its own image type', () => {
    for (const f of ALL_FIXTURES) {
      const imageType = f.name.includes('iso') ? 'iso' : 'raw'
      expect({ name: f.name, ...validateDisk(parseDiskFromYaml(f.yaml)!, { imageType }) })
        .toMatchObject({ errors: [] })
    }
  })
})

describe('soft field rules', () => {
  const raw = () => parseDiskFromYaml(minimalPtlPvRaw)!

  it('warns about an unrecognised partition type instead of failing', () => {
    const model = raw()
    model.partitions[0].type = 'linux-root-riscv64'
    const { errors, warnings } = validateDisk(model, { imageType: 'raw' })
    expect(errors).toEqual([])
    expect(warnings.join(' ')).toMatch(/type will be left at the default/)
  })

  it('warns when typeUUID contradicts type, and says which one wins', () => {
    const model = raw()
    model.partitions[0].typeUUID = PARTITION_TYPE_GUIDS['linux-home']
    expect(validateDisk(model, { imageType: 'raw' }).warnings.join(' ')).toMatch(
      /typeUUID does not match type "esp".*the typeUUID is used/,
    )
  })

  it('accepts a typeUUID that matches its type, in either case', () => {
    const model = raw()
    model.partitions[0].typeUUID = PARTITION_TYPE_GUIDS.esp.toUpperCase()
    expect(validateDisk(model, { imageType: 'raw' }).warnings).toEqual([])
  })

  it('warns about an unknown flag', () => {
    const model = raw()
    model.partitions[0].flags = ['boot', 'sparkly']
    expect(validateDisk(model, { imageType: 'raw' }).warnings.join(' ')).toMatch(
      /unrecognised flag "sparkly"; it will be ignored/,
    )
  })

  it('checks hand-typed offsets only in offset mode', () => {
    const sized = raw()
    sized.partitions[0].start = 'garbage' // ignored: size mode derives its own
    expect(validateDisk(sized, { imageType: 'raw' }).errors).toEqual([])

    const offset = setLayoutMode(raw(), 'offset')
    offset.partitions[0] = { ...offset.partitions[0], start: '1.5MiB' }
    expect(validateDisk(offset, { imageType: 'raw' }).errors.join(' ')).toMatch(
      /Start "1\.5MiB" is not a size the builder accepts/,
    )
  })
})
