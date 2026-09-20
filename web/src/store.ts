import { create } from 'zustand'
import type { Manifest, Combination } from './api/types'
import type { DiskModel } from './lib/disk'

// Selection state for the Basic tab.
export interface Selection {
  vertical: string
  sku: string
  platform: string
  os: string
  kernel: string
  imageType: string
}

// A package the user added in the Packages step. `version` is '' for a
// floating "latest" pick, or a specific version string when pinned.
export interface AddedPackage {
  name: string
  version: string
  repo: string
}

// Encodes a package pick for the boundary where a single string is needed
// (e.g. a future compose request field). Kept as a record + this encoder,
// rather than encoding at rest, so nothing has to parse `name_version` back
// apart — rpm names may themselves contain `_`.
export function encodePackage(p: AddedPackage): string {
  return p.version ? `${p.name}_${p.version}` : p.name
}

export interface PackageMutationOptions {
  // Whether losing a package's repo dependency should also drop that repo
  // from enabledRepos once nothing else needs it. Defaults to true. Callers
  // that might be releasing a repo the user checked deliberately (rather
  // than one this same flow enabled implicitly on their behalf) should
  // compute this via confirmRepoRelease first rather than hardcoding it —
  // see that function's comment.
  releaseRepo?: boolean
}

// orphanedRepos reports which repos among `namesToRemove`'s current
// bindings would end up with nothing left depending on them if those names
// were removed from addedPackages. Modeling a re-pin as "remove the old
// name" and calling this with `addedPackages` from before the update is
// exactly right: the entry being re-pinned no longer counts toward its old
// repo's dependents, and whatever it's changing to is unaffected here (that
// repo gets enabled, or was already, elsewhere).
export function orphanedRepos(addedPackages: AddedPackage[], namesToRemove: string[]): string[] {
  const removing = new Set(namesToRemove)
  const affectedRepos = new Set(
    addedPackages.filter((p) => removing.has(p.name)).map((p) => p.repo),
  )
  const remaining = addedPackages.filter((p) => !removing.has(p.name))
  const stillNeeded = new Set(remaining.map((p) => p.repo))
  return [...affectedRepos].filter((r) => !stillNeeded.has(r))
}

// confirmRepoRelease decides whether a removal or re-pin that's about to
// orphan one or more repos should actually uncheck them. A repo can be
// checked because the user deliberately browsed it, not merely because
// something happened to be added from it, so losing its last dependent
// package shouldn't silently uncheck it without asking — unlike enabling a
// repo (a clear statement of interest either way), *disabling* one drops
// real state (any other packages it might gain later start from scratch),
// which is worth a confirmation. Returns true immediately, no prompt, when
// nothing would actually be orphaned — the overwhelmingly common case.
//
// isBaseRepo excludes a target's always-on repo(s) from that check
// entirely: enabledRepos never lists one to begin with (its checkbox stays
// checked via `enabledByDefault`, not membership), so "uncheck this too?"
// would be both meaningless (nothing to uncheck) and confusing (the base
// template needs it regardless of what's added).
export function confirmRepoRelease(
  addedPackages: AddedPackage[],
  repoLabelFor: (repo: string) => string,
  namesToRemove: string[],
  isBaseRepo: (repo: string) => boolean = () => false,
): boolean {
  const repos = orphanedRepos(addedPackages, namesToRemove).filter((r) => !isBaseRepo(r))
  if (repos.length === 0) return true
  const plural = repos.length > 1
  const list = repos.map(repoLabelFor).join(', ')
  // Bare confirm(), not window.confirm — identical in a real browser, but
  // lets a test stub it without needing a DOM environment.
  return confirm(
    `No other selected package will use ${plural ? 'these repositories' : 'this repository'} anymore: ` +
      `${list}. Uncheck ${plural ? 'them' : 'it'} too?`,
  )
}

interface AppState {
  manifest: Manifest | null
  selection: Selection
  // Advanced-tab override for the image name. Kept out of `Selection` so it never
  // leaks into the compose request body; it seeds from the resolved template's
  // imageName and, once the user types, `imageNameEdited` guards it from reseeds.
  imageName: string
  imageNameEdited: boolean
  // Repos the user turned on in the Packages step. A repo the catalog marks
  // enabledByDefault is always on and is never listed here — its toggle renders
  // disabled. Kept out of `Selection` for the same reason imageName is: the
  // compose request has no field for it.
  enabledRepos: string[]
  // Packages the user added in the Packages step. Kept out of `Selection` for
  // the same reason: the compose request has no field for it yet.
  addedPackages: AddedPackage[]
  // Advanced-tab disk edit model, seeded from the resolved template's disk block
  // and edited client-side from there. Null until the first compose resolves (or
  // after a selection change, which invalidates the previous template's layout).
  // Same seed-until-edited contract as imageName above.
  disk: DiskModel | null
  diskEdited: boolean
  // The last layout seeded from a compose, kept aside so "Reset to template
  // defaults" has something to restore. Without it, resetting would blank the
  // step until the next compose, and a compose only fires on a selection change.
  diskSeed: DiskModel | null
  setManifest: (m: Manifest) => void
  setField: (key: keyof Selection, value: string) => void
  // User-typed image name (marks it edited so seedImageName stops overwriting it).
  setImageName: (value: string) => void
  // Default image name from the resolved template; ignored once the user edits.
  seedImageName: (value: string) => void
  // Disabling a repo also drops any packages added from it — a disabled repo's
  // packages have nowhere to resolve from at compose time.
  setRepoEnabled: (repo: string, on: boolean) => void
  // Upserts by name: adding a package already present (e.g. re-pinning its
  // version) replaces the existing entry rather than duplicating it.
  setPackage: (p: AddedPackage, opts?: PackageMutationOptions) => void
  removePackage: (name: string, opts?: PackageMutationOptions) => void
  // Batch forms of setPackage/removePackage for "Select all": one state
  // update for the whole list instead of one per package.
  setPackages: (pkgs: AddedPackage[]) => void
  removePackages: (names: string[], opts?: PackageMutationOptions) => void
  clearPackages: () => void
  // User edit to the disk layout (marks it edited so seedDisk stops overwriting it).
  setDisk: (value: DiskModel) => void
  // Disk layout from the resolved template; ignored once the user edits.
  seedDisk: (value: DiskModel | null) => void
  // Drop the user's edits so the next compose reseeds from the template.
  resetDisk: () => void
}

const emptySelection: Selection = {
  vertical: '',
  sku: '',
  platform: '',
  os: '',
  kernel: '',
  imageType: '',
}

export const useStore = create<AppState>((set) => ({
  manifest: null,
  selection: emptySelection,
  imageName: '',
  imageNameEdited: false,
  enabledRepos: [],
  addedPackages: [],
  disk: null,
  diskEdited: false,
  diskSeed: null,
  setManifest: (m) => set({ manifest: m }),
  setImageName: (value) => set({ imageName: value, imageNameEdited: true }),
  seedImageName: (value) =>
    set((state) => (state.imageNameEdited ? {} : { imageName: value })),
  setRepoEnabled: (repo, on) =>
    set((state) => ({
      enabledRepos: on
        ? state.enabledRepos.includes(repo)
          ? state.enabledRepos
          : [...state.enabledRepos, repo]
        : state.enabledRepos.filter((r) => r !== repo),
      ...(on ? {} : { addedPackages: state.addedPackages.filter((p) => p.repo !== repo) }),
    })),
  // Re-pinning to a different repo can leave the one it just left with
  // nothing depending on it anymore — dropped the same way disabling that
  // repo manually would already drop this package, just from the other
  // side of the relationship. A repo the user enabled by hand with nothing
  // added from it yet is never touched: this only fires as a consequence of
  // a package actually moving away from a repo, never as an ambient sweep.
  setPackage: (p, opts) =>
    set((state) => {
      const previous = state.addedPackages.find((x) => x.name === p.name)
      const addedPackages = [...state.addedPackages.filter((x) => x.name !== p.name), p]
      if (opts?.releaseRepo === false) return { addedPackages }
      return {
        addedPackages,
        enabledRepos: releaseIfOrphaned(state.enabledRepos, previous, addedPackages),
      }
    }),
  removePackage: (name, opts) =>
    set((state) => {
      const removed = state.addedPackages.find((p) => p.name === name)
      const addedPackages = state.addedPackages.filter((p) => p.name !== name)
      if (opts?.releaseRepo === false) return { addedPackages }
      return {
        addedPackages,
        enabledRepos: releaseIfOrphaned(state.enabledRepos, removed, addedPackages),
      }
    }),
  setPackages: (pkgs) =>
    set((state) => {
      const names = new Set(pkgs.map((p) => p.name))
      // A bulk add ("Select all") only ever grows the set, so nothing it
      // replaces can be left without a repo to drop.
      return { addedPackages: [...state.addedPackages.filter((x) => !names.has(x.name)), ...pkgs] }
    }),
  removePackages: (names, opts) =>
    set((state) => {
      const drop = new Set(names)
      const addedPackages = state.addedPackages.filter((p) => !drop.has(p.name))
      if (opts?.releaseRepo === false) return { addedPackages }
      const removedRepos = new Set(
        state.addedPackages.filter((p) => drop.has(p.name)).map((p) => p.repo),
      )
      const stillNeeded = new Set(addedPackages.map((p) => p.repo))
      return {
        addedPackages,
        enabledRepos: state.enabledRepos.filter((r) => !removedRepos.has(r) || stillNeeded.has(r)),
      }
    }),
  clearPackages: () =>
    set((state) => {
      const usedRepos = new Set(state.addedPackages.map((p) => p.repo))
      return {
        addedPackages: [],
        enabledRepos: state.enabledRepos.filter((r) => !usedRepos.has(r)),
      }
    }),
  setDisk: (value) => set({ disk: value, diskEdited: true }),
  seedDisk: (value) =>
    // A compose that resolves to a template with no disk block leaves whatever
    // is on screen alone rather than blanking the step. The seed is recorded
    // even when the user has edited, so Reset always has a target.
    set((state) =>
      value === null ? {} : state.diskEdited ? { diskSeed: value } : { disk: value, diskSeed: value },
    ),
  resetDisk: () => set((state) => ({ disk: state.diskSeed, diskEdited: false })),
  setField: (key, value) =>
    set((state) => {
      const selection = { ...state.selection, [key]: value }
      // Reset downstream fields when an upstream one changes, so the cascade
      // never leaves an invalid combination selected.
      // Cascade order: vertical → sku → platform → os → kernel → imageType.
      if (key === 'vertical') {
        selection.sku = ''
        selection.platform = ''
        selection.os = ''
        selection.kernel = ''
        selection.imageType = ''
      } else if (key === 'sku') {
        selection.platform = ''
        selection.os = ''
        selection.kernel = ''
        selection.imageType = ''
      } else if (key === 'platform') {
        selection.os = ''
        selection.kernel = ''
        selection.imageType = ''
      } else if (key === 'os') {
        selection.kernel = ''
        selection.imageType = ''
      } else if (key === 'kernel') {
        selection.imageType = ''
      }
      // Auto-fill each downstream dimension with its first available option, so
      // selecting a vertical immediately populates a valid default combination
      // the user can tweak, rather than forcing a click through every dropdown.
      if (state.manifest) {
        autoFillCascade(state.manifest, selection)
      }
      // Any selection change resolves to a (possibly) different template, so let
      // the image name re-track the new default until edited again.
      //
      // Enabled repos and added packages are dropped only when the target OS
      // actually changes, because repo ids (and the packages resolved from
      // them) are scoped to a target — keeping them would leave an ubuntu24
      // repo/package enabled under a Debian target. Unlike imageName, which
      // re-seeds itself from the next compose response, there is no server-side
      // default to fall back to, so empty is the only sane value. Comparing the
      // post-cascade os (autoFillCascade above may refill it with the same
      // value) means a SKU or platform tweak within one OS keeps the user's
      // toggles.
      //
      // The disk layout is dropped on *any* selection change, unlike repos and
      // packages: it is not a user pick scoped to a target, it is a copy of the
      // matched template's own partition table, and a SKU or platform tweak can
      // resolve to a different template. It is cleared outright rather than just
      // unflagged, because leaving the previous template's partitions on screen
      // until the next compose lands would show a layout that no longer applies.
      const osChanged = selection.os !== state.selection.os
      return {
        selection,
        imageNameEdited: false,
        disk: null,
        diskEdited: false,
        diskSeed: null,
        ...(osChanged ? { enabledRepos: [], addedPackages: [] } : {}),
      }
    }),
}))

// releaseIfOrphaned drops `changed`'s previous repo from enabledRepos when
// nothing in `remaining` still references it. `changed` is the entry as it
// was before the update being applied (undefined if it's a brand new pick,
// which can't orphan anything); `remaining` is addedPackages after that
// update. Used by both a re-pin (repo moves) and a removal (repo drops out
// entirely) — in a removal, `changed` and the one entry missing from
// `remaining` are the same thing.
function releaseIfOrphaned(
  enabledRepos: string[],
  changed: AddedPackage | undefined,
  remaining: AddedPackage[],
): string[] {
  if (!changed) return enabledRepos
  if (remaining.some((p) => p.repo === changed.repo)) return enabledRepos
  return enabledRepos.filter((r) => r !== changed.repo)
}

// autoFillCascade mutates `selection`, setting each empty downstream field to the
// first option available for the current upstream choices. Walks the cascade in
// order so each step sees the defaults picked by the previous one.
function autoFillCascade(manifest: Manifest, selection: Selection): void {
  const order: (keyof Selection)[] = ['sku', 'platform', 'os', 'kernel', 'imageType']
  const optsKey: Record<string, keyof ReturnType<typeof cascadingOptions>> = {
    sku: 'skus',
    platform: 'platforms',
    os: 'oses',
    kernel: 'kernels',
    imageType: 'imageTypes',
  }
  for (const field of order) {
    if (selection[field]) continue
    const opts = cascadingOptions(manifest, selection)
    const list = opts[optsKey[field]] as DropdownOption[]
    // Skip grayed-out (unavailable) options so we never auto-select a
    // combination that has no template behind it.
    const first = list.find((o) => !o.disabled)
    if (first) {
      selection[field] = first.id
    }
  }
}

// --- Derived cascading option helpers (pure functions over the manifest) ---

function labelFor(options: { id: string; displayName: string }[], id: string): string {
  return options.find((o) => o.id === id)?.displayName ?? id
}

// Distinct ids present in combinations, optionally filtered by prior selections.
// For each id, `available` is true when at least one matching combination has a
// template behind it; ids reachable only through template-less (planned but not
// ready) combinations are returned with available=false so the UI can gray them.
function distinct(
  combos: Combination[],
  field: keyof Combination,
  filter: Partial<Selection>,
): { id: string; available: boolean }[] {
  const order: string[] = []
  const availById = new Map<string, boolean>()
  for (const c of combos) {
    const matches = Object.entries(filter).every(
      ([k, v]) => !v || c[k as keyof Combination] === v,
    )
    if (!matches) continue
    const id = c[field] as string
    if (!id) continue
    if (!availById.has(id)) order.push(id)
    // Treat a whitespace-only template as unavailable too, so a formatting slip
    // in the manifest cannot accidentally enable a planned combination.
    availById.set(id, (availById.get(id) ?? false) || c.template.trim() !== '')
  }
  return order.map((id) => ({ id, available: availById.get(id) ?? false }))
}

export interface DropdownOption {
  id: string
  label: string
  disabled?: boolean
}

export function cascadingOptions(
  manifest: Manifest,
  selection: Selection,
): {
  verticals: DropdownOption[]
  skus: DropdownOption[]
  platforms: DropdownOption[]
  oses: DropdownOption[]
  kernels: DropdownOption[]
  imageTypes: DropdownOption[]
  matched: Combination | null
} {
  const c = manifest.combinations
  const map = (
    entries: { id: string; available: boolean }[],
    labels: { id: string; displayName: string }[],
  ): DropdownOption[] =>
    entries.map(({ id, available }) => ({
      id,
      label: labelFor(labels, id),
      disabled: !available,
    }))

  const verticals = map(distinct(c, 'vertical', {}), manifest.verticals)
  const skus = map(
    distinct(c, 'sku', { vertical: selection.vertical }),
    manifest.skus,
  )
  const platforms = map(
    distinct(c, 'platform', { vertical: selection.vertical, sku: selection.sku }),
    manifest.platforms,
  )
  const oses = map(
    distinct(c, 'os', {
      vertical: selection.vertical,
      sku: selection.sku,
      platform: selection.platform,
    }),
    manifest.targets,
  )

  // Kernel is an optional dimension: only combinations that carry a kernel value
  // contribute. When none do, kernels is empty and the UI omits the selector —
  // so RT vs standard is surfaced only where the metadata actually offers it.
  const kernelIds = distinct(c, 'kernel', {
    vertical: selection.vertical,
    sku: selection.sku,
    platform: selection.platform,
    os: selection.os,
  })
  const kernelLabels: Record<string, string> = { standard: 'Standard', rt: 'Real-Time' }
  const kernels = kernelIds.map(({ id, available }) => ({
    id,
    label: kernelLabels[id] ?? id,
    disabled: !available,
  }))

  const imageTypeIds = distinct(c, 'imageType', {
    vertical: selection.vertical,
    sku: selection.sku,
    platform: selection.platform,
    os: selection.os,
    ...(kernels.length > 0 ? { kernel: selection.kernel } : {}),
  })
  const imageTypes = imageTypeIds.map(({ id, available }) => ({
    id,
    label: id.toUpperCase(),
    disabled: !available,
  }))

  const matched =
    c.find(
      (x) =>
        x.template.trim() !== '' &&
        x.vertical === selection.vertical &&
        (x.sku || '') === selection.sku &&
        x.platform === selection.platform &&
        x.os === selection.os &&
        (x.kernel || '') === selection.kernel &&
        x.imageType === selection.imageType,
    ) ?? null

  return { verticals, skus, platforms, oses, kernels, imageTypes, matched }
}
