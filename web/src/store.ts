import { create } from 'zustand'
import type { Manifest, Combination } from './api/types'

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
  setPackage: (p: AddedPackage) => void
  removePackage: (name: string) => void
  clearPackages: () => void
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
  setPackage: (p) =>
    set((state) => ({
      addedPackages: [...state.addedPackages.filter((x) => x.name !== p.name), p],
    })),
  removePackage: (name) =>
    set((state) => ({
      addedPackages: state.addedPackages.filter((p) => p.name !== name),
    })),
  clearPackages: () => set({ addedPackages: [] }),
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
      const osChanged = selection.os !== state.selection.os
      return {
        selection,
        imageNameEdited: false,
        ...(osChanged ? { enabledRepos: [], addedPackages: [] } : {}),
      }
    }),
}))

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
