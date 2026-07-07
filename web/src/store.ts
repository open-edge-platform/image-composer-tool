import { create } from 'zustand'
import type { Manifest, Combination } from './api/types'

// Selection state for the Basic tab.
export interface Selection {
  vertical: string
  sku: string
  platform: string
  os: string
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
      if (key === 'vertical') {
        selection.sku = ''
        selection.platform = ''
        selection.os = ''
        selection.imageType = ''
      } else if (key === 'sku') {
        selection.platform = ''
        selection.os = ''
        selection.imageType = ''
      } else if (key === 'platform') {
        selection.os = ''
        selection.imageType = ''
      } else if (key === 'os') {
        selection.imageType = ''
      }
      return { selection }
    }),
}))

// --- Derived cascading option helpers (pure functions over the manifest) ---

function labelFor(options: { id: string; displayName: string }[], id: string): string {
  return options.find((o) => o.id === id)?.displayName ?? id
}

// Distinct ids present in combinations, optionally filtered by prior selections.
function distinct(
  combos: Combination[],
  field: keyof Combination,
  filter: Partial<Selection>,
): string[] {
  const out: string[] = []
  for (const c of combos) {
    const matches = Object.entries(filter).every(
      ([k, v]) => !v || c[k as keyof Combination] === v,
    )
    if (matches && c[field] && !out.includes(c[field] as string)) {
      out.push(c[field] as string)
    }
  }
  return out
}

export interface DropdownOption {
  id: string
  label: string
}

export function cascadingOptions(
  manifest: Manifest,
  selection: Selection,
): {
  verticals: DropdownOption[]
  skus: DropdownOption[]
  platforms: DropdownOption[]
  oses: DropdownOption[]
  imageTypes: DropdownOption[]
  matched: Combination | null
} {
  const c = manifest.combinations
  const map = (ids: string[], labels: { id: string; displayName: string }[]) =>
    ids.map((id) => ({ id, label: labelFor(labels, id) }))

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
  const imageTypeIds = distinct(c, 'imageType', {
    vertical: selection.vertical,
    sku: selection.sku,
    platform: selection.platform,
    os: selection.os,
  })
  const imageTypes = imageTypeIds.map((id) => ({ id, label: id.toUpperCase() }))

  const matched =
    c.find(
      (x) =>
        x.vertical === selection.vertical &&
        (x.sku || '') === selection.sku &&
        x.platform === selection.platform &&
        x.os === selection.os &&
        x.imageType === selection.imageType,
    ) ?? null

  return { verticals, skus, platforms, oses, imageTypes, matched }
}
