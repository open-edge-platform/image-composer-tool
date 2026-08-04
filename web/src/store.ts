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

interface AppState {
  manifest: Manifest | null
  selection: Selection
  setManifest: (m: Manifest) => void
  setField: (key: keyof Selection, value: string) => void
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
  setManifest: (m) => set({ manifest: m }),
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
      return { selection }
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
