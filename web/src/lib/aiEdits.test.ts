// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it, vi } from 'vitest'
import type { PackageSearchResult, TemplateEdit } from '../api/types'
import {
  describeChanges,
  editPackageNames,
  expandPatternEdits,
  matchRepo,
  normalizeDiskSize,
  patternMatcher,
  planAIEdits,
  type AIEditContext,
} from './aiEdits'

const edit = (op: TemplateEdit['op'], pkg = '', version = '', value = ''): TemplateEdit => ({
  op,
  package: pkg,
  version,
  value,
})

const hit = (name: string, ...versions: [string, string][]): PackageSearchResult => ({
  name,
  version: versions[0][0],
  repository: versions[0][1],
  versions: versions.map(([version, repository]) => ({ version, repository })),
})

const ctx = (over: Partial<AIEditContext> = {}): AIEditContext => ({
  basePackages: ['snapd', 'linux-image-7.3-intel_261014t080608z-resolute1', 'libva*'],
  added: [],
  removed: [],
  checkedRepos: new Set(['ubuntu', 'nvl-ppa']),
  priorities: { 'nvl-ppa': 1001 },
  found: {
    'docker.io': hit('docker.io', ['27.5.1', 'ubuntu']),
    'docker-ce': hit('docker-ce', ['28.0.0', 'docker']),
    'linux-image-7.3-intel': hit('linux-image-7.3-intel', ['261101t000000z-resolute1', 'nvl-ppa'], ['261014t080608z-resolute1', 'nvl-ppa']),
    // Newer in Ubuntu, but the PPA's higher priority wins, as in a build.
    'libnss-libvirt': hit('libnss-libvirt', ['5.8', 'ubuntu'], ['5.7~ppa', 'nvl-ppa']),
    snapd: hit('snapd', ['2.66', 'ubuntu']),
  },
  lookupComplete: true,
  diskSize: '16GiB',
  repos: [
    { id: 'ubuntu', displayName: 'Ubuntu 26.04 Base', url: 'http://archive.ubuntu.com/ubuntu' },
    { id: 'nvl-ppa', displayName: 'ppa.launchpadcontent.net', url: 'https://ppa.launchpadcontent.net/intel/nvl/ubuntu' },
    { id: 'docker', displayName: 'Docker CE', url: 'https://download.docker.com/linux/ubuntu' },
  ],
  ...over,
})

describe('planAIEdits', () => {
  it('adds a package from a checked repository, unpinned', () => {
    const plan = planAIEdits([edit('add', 'docker.io')], ctx())
    expect(plan.apply).toEqual([{ kind: 'package', pkg: { name: 'docker.io', version: '', repo: 'ubuntu' }, enableRepo: undefined }])
    expect(plan.skipped).toEqual([])
  })

  it('picks the repository a build would install from', () => {
    const plan = planAIEdits([edit('add', 'libnss-libvirt')], ctx())
    expect(plan.apply[0]).toMatchObject({ pkg: { repo: 'nvl-ppa' } })
  })

  it('turns on the repository a package needs when none checked has it', () => {
    const plan = planAIEdits([edit('add', 'docker-ce')], ctx())
    expect(plan.apply).toEqual([{ kind: 'package', pkg: { name: 'docker-ce', version: '', repo: 'docker' }, enableRepo: 'docker' }])
  })

  it('never applies a name or version no repository publishes', () => {
    const plan = planAIEdits(
      [edit('add', 'docker-compose-v9'), edit('set_version', 'linux-image-7.3-intel', '999')],
      ctx(),
    )
    expect(plan.apply).toEqual([])
    expect(plan.skipped).toEqual([
      { message: 'docker-compose-v9: not found in the repositories for this target.', missing: 'docker-compose-v9' },
      { message: 'linux-image-7.3-intel 999: no repository publishes that version (newest: 261101t000000z-resolute1).' },
    ])
  })

  it('says a missing name may exist when a repository could not be read', () => {
    const plan = planAIEdits([edit('add', 'nothing')], ctx({ lookupComplete: false }))
    expect(plan.skipped[0].message).toMatch(/couldn't be read/)
  })

  it('repins and unpins template packages, and skips no-op changes', () => {
    const plan = planAIEdits(
      [
        edit('set_version', 'linux-image-7.3-intel', '261101t000000z-resolute1'),
        edit('set_version', 'snapd', ''),
        edit('add', 'snapd'),
      ],
      ctx(),
    )
    expect(plan.apply).toEqual([
      { kind: 'package', pkg: { name: 'linux-image-7.3-intel', version: '261101t000000z-resolute1', repo: 'nvl-ppa' }, enableRepo: undefined },
    ])
    expect(plan.skipped.map((s) => s.message)).toEqual(['snapd: already unpinned.', 'snapd: already in the template.'])
  })

  it('removes template and added packages, and puts back a removed one', () => {
    const plan = planAIEdits(
      [edit('remove', 'snapd'), edit('remove', 'docker.io'), edit('remove', 'vim'), edit('add', 'snapd')],
      ctx({ added: [{ name: 'docker.io', version: '', repo: 'ubuntu' }] }),
    )
    expect(plan.apply).toEqual([
      { kind: 'remove-template', name: 'snapd' },
      { kind: 'remove-added', name: 'docker.io' },
      { kind: 'restore', name: 'snapd' },
    ])
    expect(plan.skipped.map((s) => s.message)).toEqual(['vim: not in the template, so there is nothing to remove.'])
  })

  it('pinning a removed template package puts it back first', () => {
    const plan = planAIEdits([edit('set_version', 'snapd', '2.66')], ctx({ removed: ['snapd'] }))
    expect(plan.apply.map((e) => e.kind)).toEqual(['restore', 'package'])
  })

  it('sets the disk size and image name', () => {
    const plan = planAIEdits([edit('set_disk_size', '', '', '32 GiB'), edit('set_image_name', '', '', 'nvl-docker')], ctx())
    expect(plan.apply).toEqual([
      { kind: 'disk-size', size: '32GiB' },
      { kind: 'image-name', name: 'nvl-docker' },
    ])
  })

  it("won't resize a template without a disk layout, or to a non-size", () => {
    expect(planAIEdits([edit('set_disk_size', '', '', '32GiB')], ctx({ diskSize: null })).skipped).toEqual([
      { message: 'Disk size: this template has no disk layout to resize.' },
    ])
    expect(planAIEdits([edit('set_disk_size', '', '', 'big')], ctx()).skipped[0].message).toMatch(/not a size/)
  })
})

describe('planAIEdits pin_latest', () => {
  // The NVL PPA at priority 600, over Ubuntu (500) and under oneAPI (900).
  const ppaCtx = () =>
    ctx({
      basePackages: [
        'linux-image-7.3-intel_261014t080608z-resolute1',
        'libnss-libvirt',
        'snapd',
        'mesa_1.0',
        'gone',
        'vpl',
      ],
      added: [{ name: 'htop-ppa', version: '', repo: 'nvl-ppa' }],
      removed: ['gone'],
      checkedRepos: new Set(['ubuntu', 'nvl-ppa', 'oneapi']),
      priorities: { 'nvl-ppa': 600, oneapi: 900 },
      found: {
        'linux-image-7.3-intel': hit('linux-image-7.3-intel', ['261101t000000z-resolute1', 'nvl-ppa'], ['261014t080608z-resolute1', 'nvl-ppa']),
        'libnss-libvirt': hit('libnss-libvirt', ['5.8', 'ubuntu'], ['5.7~ppa', 'nvl-ppa']),
        snapd: hit('snapd', ['2.66', 'ubuntu']),
        mesa: hit('mesa', ['2.0', 'nvl-ppa']),
        gone: hit('gone', ['1.0', 'nvl-ppa']),
        vpl: hit('vpl', ['2.0', 'oneapi'], ['1.0', 'nvl-ppa']),
        'htop-ppa': hit('htop-ppa', ['3.0', 'nvl-ppa']),
      },
    })

  it('pins what comes from the named repository, by the Update to latest version rules', () => {
    const plan = planAIEdits([edit('pin_latest', '', '', 'ppa')], ppaCtx())
    expect(plan.apply.map((e) => e.kind === 'package' && `${e.pkg.name} ${e.pkg.version} ${e.pkg.repo}`)).toEqual([
      'htop-ppa 3.0 nvl-ppa',
      'linux-image-7.3-intel 261101t000000z-resolute1 nvl-ppa',
      // Unpinned, and the PPA outranks Ubuntu, so a build installs the PPA's.
      'libnss-libvirt 5.7~ppa nvl-ppa',
    ])
    expect(plan.skipped.map((s) => s.message)).toEqual([
      "Kept 1 template pin(s) as they are (mesa): ppa.launchpadcontent.net doesn't publish the pinned version.",
      'Kept 1 unpinned (vpl): a build installs them from another repository.',
    ])
  })

  it('asks which repository when the words match several, or none', () => {
    const c = ctx({ checkedRepos: new Set() })
    expect(planAIEdits([edit('pin_latest', '', '', 'ubuntu')], c).skipped[0].message).toMatch(/matches 3 repositories/)
    expect(planAIEdits([edit('pin_latest', '', '', 'gentoo')], c).skipped[0].message).toBe('No repository matches "gentoo".')
  })

  it('pins nothing when a repository could not be read', () => {
    const plan = planAIEdits([edit('pin_latest', '', '', 'ppa')], { ...ppaCtx(), lookupComplete: false })
    expect(plan.apply).toEqual([])
    expect(plan.skipped[0].message).toMatch(/Couldn't read every repository/)
  })

  it('reads a version of "latest" as pin_latest', () => {
    const plan = planAIEdits([edit('set_version', 'libnss-libvirt', 'latest')], ppaCtx())
    expect(plan.apply).toEqual([
      { kind: 'package', pkg: { name: 'libnss-libvirt', version: '5.7~ppa', repo: 'nvl-ppa' }, enableRepo: undefined },
    ])
  })

  it('pins one package to the newest version where a build gets it', () => {
    const plan = planAIEdits([edit('pin_latest', 'libnss-libvirt'), edit('pin_latest', 'snapd')], ppaCtx())
    expect(plan.apply).toEqual([
      { kind: 'package', pkg: { name: 'libnss-libvirt', version: '5.7~ppa', repo: 'nvl-ppa' }, enableRepo: undefined },
      { kind: 'package', pkg: { name: 'snapd', version: '2.66', repo: 'ubuntu' }, enableRepo: undefined },
    ])
  })
})

describe('describeChanges', () => {
  it('lists the changes as the Changes rail groups them', () => {
    const text = describeChanges({
      basePackages: ['libnss-libvirt', 'snapd', 'mesa_1.0'],
      added: [
        { name: 'docker.io', version: '', repo: 'ubuntu' },
        { name: 'libnss-libvirt', version: '12.0', repo: 'nvl-ppa' },
      ],
      removed: ['snapd'],
      enabledRepos: ['docker'],
      diskSize: { from: '64GiB', to: '80GiB' },
      diskLayoutEdited: true,
      imageName: 'nvl-docker',
      repoLabel: (id) => ({ ubuntu: 'Ubuntu', 'nvl-ppa': 'NVL PPA', docker: 'Docker CE' })[id] ?? id,
    })
    expect(text).toBe(
      [
        'Added:',
        '- docker.io latest (unpinned), from Ubuntu',
        'Modified:',
        '- libnss-libvirt: latest (unpinned) -> 12.0, from NVL PPA',
        'Removed:',
        '- snapd',
        'Repositories turned on: Docker CE',
        'Disk size: 64GiB -> 80GiB',
        'Image name: nvl-docker',
      ].join('\n'),
    )
  })

  it('is empty with no changes', () => {
    expect(describeChanges({ basePackages: [], added: [], removed: [], enabledRepos: [], repoLabel: (id) => id })).toBe('')
  })
})

describe('helpers', () => {
  it('normalizeDiskSize', () => {
    expect(normalizeDiskSize('32G')).toBe('32GiB')
    expect(normalizeDiskSize('32 gb')).toBe('32GB')
    expect(normalizeDiskSize('512MiB')).toBe('512MiB')
    expect(normalizeDiskSize('0GiB')).toBeNull()
    expect(normalizeDiskSize('32 parsecs')).toBeNull()
  })

  it('editPackageNames lists each package once', () => {
    expect(editPackageNames([edit('add', 'a'), edit('remove', 'a'), edit('set_disk_size', '', '', '1G')], () => ['x'])).toEqual(['a'])
    expect(editPackageNames([edit('pin_latest', '', '', 'ppa')], () => ['x', 'y'])).toEqual(['x', 'y'])
  })
})

describe('matchRepo', () => {
  const af = {
    id: 'af01p',
    displayName: 'af01p-png.devtools.intel.com …/resolute/20261023-0019 (resolute)',
    url: 'https://af01p-png.devtools.intel.com/artifactory/hspe-edge-png-local/ubuntu/resolute/resolute/20261023-0019',
  }
  const ubuntu = { id: 'ubuntu-base', displayName: 'Ubuntu 26.04 Base', url: 'http://archive.ubuntu.com/ubuntu' }

  it('matches a repository by its URL, whatever the scheme or trailing slash', () => {
    for (const words of [`${af.url}/`, af.url, af.url.replace('https://', ''), 'af01p']) {
      expect(matchRepo(words, [af, ubuntu], new Set())).toBe(af)
    }
  })
})

describe('patternMatcher', () => {
  it('searches by the literal start and matches whole names', () => {
    const m = patternMatcher('gir1.2-gst*')!
    expect(m.prefix).toBe('gir1.2-gst')
    expect(m.test('gir1.2-gstreamer-1.0')).toBe(true)
    expect(m.test('gir1.2-gtk-3.0')).toBe(false)
    expect(m.test('xgir1.2-gst')).toBe(false)
    expect(patternMatcher('lib?a-drm*')!.test('libva-drm2')).toBe(true)
  })

  it('needs two literal characters first', () => {
    expect(patternMatcher('*')).toBeNull()
    expect(patternMatcher('g*')).toBeNull()
    expect(patternMatcher('gi*')).not.toBeNull()
  })
})

describe('expandPatternEdits', () => {
  // af01p has a handful of gir packages; Ubuntu's archive has hundreds.
  const af = [
    'gir1.2-gst-plugins-bad-1.0',
    'gir1.2-gst-plugins-base-1.0',
    'gir1.2-gstreamer-1.0',
    'gir1.2-spiceclientglib-2.0',
  ]
  const ubuntu = Array.from({ length: 120 }, (_, i) => `gir1.2-pkg${i}`)
  const repos = [
    { id: 'af01p', displayName: 'af01p-png.devtools.intel.com …/resolute', url: 'https://af01p-png.devtools.intel.com/x' },
    { id: 'ubuntu-base', displayName: 'Ubuntu 26.04 Base', url: 'http://archive.ubuntu.com/ubuntu' },
  ]
  const search = vi.fn(async (prefix: string, scope: string[], offset: number) => {
    const all = [...(scope.includes('af01p') ? af : []), ...(scope.includes('ubuntu-base') ? ubuntu : [])].filter((n) =>
      n.startsWith(prefix),
    )
    return { names: all.slice(offset, offset + 100), total: all.length }
  })
  const ctx = (present: string[] = []) => ({
    search,
    repos,
    checkedRepos: new Set(['af01p', 'ubuntu-base']),
    present: (n: string) => present.includes(n),
  })
  const matching = (pattern: string, value = ''): TemplateEdit => ({ op: 'add_matching', package: pattern, version: '', value })

  it('adds every match from the named repository, noting the ones already there', async () => {
    const r = await expandPatternEdits([matching('gir*', 'af01p')], ctx(['gir1.2-gst-plugins-bad-1.0']))
    expect(r.edits.map((e) => [e.op, e.package])).toEqual([
      ['add', 'gir1.2-gst-plugins-base-1.0'],
      ['add', 'gir1.2-gstreamer-1.0'],
      ['add', 'gir1.2-spiceclientglib-2.0'],
    ])
    expect(r.skipped.map((s) => s.message)).toEqual(['1 matching gir* already in the template: gir1.2-gst-plugins-bad-1.0.'])
  })

  it('reports a pattern that matches too many in the repositories in use, adding none', async () => {
    const r = await expandPatternEdits([matching('gir*')], ctx())
    expect(r.edits).toEqual([])
    expect(r.skipped[0].message).toMatch(/^gir\*: 124 packages in the repositories in use match, more than the 50/)
    // It paged through every result to count them.
    expect(search).toHaveBeenCalledWith('gir', ['af01p', 'ubuntu-base'], 100)
  })

  it('reports no match, an unknown repository, and a pattern too short to search', async () => {
    const r = await expandPatternEdits([matching('xyz*'), matching('gir*', 'nowhere'), matching('*')], ctx())
    expect(r.edits).toEqual([])
    expect(r.skipped.map((s) => s.message)).toEqual([
      'xyz*: no package in the repositories in use matches.',
      'gir*: no repository matches "nowhere".',
      '*: a pattern needs at least two letters before its first * or ?.',
    ])
  })

  it('adds from the browsed repository when the pattern names none, and from all on request', async () => {
    const browsing = { ...ctx(), browsed: 'af01p' }
    const r = await expandPatternEdits([matching('gir*')], browsing)
    expect(r.edits.map((e) => e.package)).toEqual(af)
    // "all" still searches every repository in use: too many here.
    const all = await expandPatternEdits([matching('gir*', 'all')], browsing)
    expect(all.edits).toEqual([])
    expect(all.skipped[0].message).toMatch(/in the repositories in use match/)
  })

  it('leaves other edits as they are', async () => {
    const other: TemplateEdit = { op: 'remove', package: 'vim', version: '', value: '' }
    expect((await expandPatternEdits([other], ctx())).edits).toEqual([other])
  })
})
