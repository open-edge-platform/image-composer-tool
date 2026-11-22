// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Ask AI: checking the edits the AI service suggests before any reaches the
// template. A package name or version must be one the target's repositories
// publish, the way the package picker only offers published ones; an edit
// that fails a check is reported, never applied. Edits that pass are applied
// through the same store actions as the same edit made by hand, so they show
// in the Changes rail and can be undone there.

import type { PackageSearchResult, TemplateEdit } from '../api/types'
import type { AddedPackage } from '../store'
import { packageEntryName } from './authorTemplate'
import { buildResolution } from './useVersionResolver'

// A checked edit, ready to apply.
export type PlannedEdit =
  // Add a package, or set the version of one the template or the user
  // already has (an empty version unpins). enableRepo is a repository the
  // pick comes from that the build doesn't read yet.
  | { kind: 'package'; pkg: AddedPackage; enableRepo?: string }
  // Put back a template package that was removed.
  | { kind: 'restore'; name: string }
  | { kind: 'remove-template'; name: string }
  | { kind: 'remove-added'; name: string }
  | { kind: 'disk-size'; size: string }
  | { kind: 'image-name'; name: string }

export interface AIEditPlan {
  apply: PlannedEdit[]
  skipped: SkippedEdit[]
}

// An edit that was not applied. missing names a package no repository
// publishes, for the caller to offer similar names instead.
export interface SkippedEdit {
  message: string
  missing?: string
}

export interface AIEditContext {
  // The template's own systemConfig.packages entries (ComposeResponse.basePackages).
  basePackages: string[]
  added: AddedPackage[]
  removed: string[]
  // Repositories a build reads now: the template's own, the target's default
  // ones, and those the user turned on.
  checkedRepos: Set<string>
  priorities: Record<string, number>
  // What a lookup of the edits' package names found across every repository
  // for the target, checked or not.
  found: Record<string, PackageSearchResult>
  // False when the lookup failed or a repository's index couldn't be read,
  // so a name it didn't find may still exist.
  lookupComplete: boolean
  // The disk layout's current size, or null when the template has none to
  // change (an ISO, or an overlay image).
  diskSize: string | null
}

// The package names a suggestion's edits need looked up.
export function editPackageNames(edits: TemplateEdit[]): string[] {
  return [...new Set(edits.filter((e) => e.package).map((e) => e.package))]
}

const DISK_UNITS: Record<string, string> = { g: 'GiB', gib: 'GiB', gb: 'GB', m: 'MiB', mib: 'MiB', mb: 'MiB' }

// normalizeDiskSize turns "32 GiB", "32G" or "32gb" into a template size
// string in one of the Disk step's units, or null when it isn't a size.
export function normalizeDiskSize(value: string): string | null {
  const m = /^([0-9]*\.?[0-9]+)\s*([a-zA-Z]+)$/.exec(value.trim())
  if (!m) return null
  const unit = DISK_UNITS[m[2].toLowerCase()]
  const amount = Number(m[1])
  return unit && amount > 0 ? `${amount}${unit}` : null
}

export function planAIEdits(edits: TemplateEdit[], ctx: AIEditContext): AIEditPlan {
  const plan: AIEditPlan = { apply: [], skipped: [] }
  const priorityOf = (repo: string) => ctx.priorities[repo] ?? 500

  // The template's packages by name, with the version it pins ('' unpinned).
  const template = new Map<string, string>()
  for (const entry of ctx.basePackages) {
    const name = packageEntryName(entry)
    if (name) template.set(name, entry === name ? '' : entry.slice(name.length + 1))
  }
  // Applied edits update these, so a later edit sees an earlier one.
  const added = new Map(ctx.added.map((p) => [p.name, p]))
  const removed = new Set(ctx.removed)

  const notFound = (name: string): SkippedEdit => ({
    message: ctx.lookupComplete
      ? `${name}: not found in the repositories for this target.`
      : `${name}: not found, but some repositories couldn't be read, so it may exist. Try again later.`,
    missing: name,
  })
  const skip = (message: string) => plan.skipped.push({ message })

  // The repo and version to pick for name at version ('' for unpinned), or
  // a reason it can't be picked.
  const pick = (name: string, version: string): { pkg: AddedPackage; enableRepo?: string } | SkippedEdit => {
    const hit = ctx.found[name]
    if (!hit) return notFound(name)
    const versions = hit.versions?.length ? hit.versions : [{ version: hit.version, repository: hit.repository }]
    let candidates = version ? versions.filter((v) => v.version === version) : versions
    if (candidates.length === 0) {
      return { message: `${name} ${version}: no repository publishes that version (newest: ${versions[0].version}).` }
    }
    // Prefer what a build already reads, as the package picker does.
    const checked = candidates.filter((v) => ctx.checkedRepos.has(v.repository))
    if (checked.length > 0) candidates = checked
    const best = version
      ? candidates.reduce((a, b) => (priorityOf(b.repository) > priorityOf(a.repository) ? b : a))
      : buildResolution({ ...hit, versions: candidates }, ctx.priorities)
    const repo = best.repository
    return {
      pkg: { name, version, repo },
      enableRepo: ctx.checkedRepos.has(repo) ? undefined : repo,
    }
  }

  const setVersion = (name: string, version: string) => {
    const r = pick(name, version)
    if ('message' in r) {
      plan.skipped.push(r)
      return
    }
    if (removed.delete(name)) plan.apply.push({ kind: 'restore', name })
    added.set(name, r.pkg)
    plan.apply.push({ kind: 'package', ...r })
  }

  for (const e of edits) {
    const name = e.package
    switch (e.op) {
      case 'add': {
        const inTemplate = template.has(name) && !removed.has(name)
        if (e.version) {
          setVersion(name, e.version)
        } else if (inTemplate || added.has(name)) {
          skip(`${name}: already in the template.`)
        } else if (template.has(name)) {
          // Removed earlier: adding it back is undoing the removal.
          removed.delete(name)
          plan.apply.push({ kind: 'restore', name })
        } else {
          setVersion(name, '')
        }
        break
      }
      case 'set_version': {
        const current = added.get(name)?.version ?? (template.has(name) && !removed.has(name) ? template.get(name) : undefined)
        if (current === e.version) {
          skip(e.version ? `${name}: already at ${e.version}.` : `${name}: already unpinned.`)
        } else {
          setVersion(name, e.version)
        }
        break
      }
      case 'remove': {
        if (added.has(name) && !template.has(name)) {
          added.delete(name)
          plan.apply.push({ kind: 'remove-added', name })
        } else if (template.has(name) && !removed.has(name)) {
          added.delete(name)
          removed.add(name)
          plan.apply.push({ kind: 'remove-template', name })
        } else {
          skip(`${name}: not in the template, so there is nothing to remove.`)
        }
        break
      }
      case 'set_disk_size': {
        const size = normalizeDiskSize(e.value)
        if (ctx.diskSize === null) {
          skip('Disk size: this template has no disk layout to resize.')
        } else if (!size) {
          skip(`Disk size: "${e.value}" is not a size such as 32GiB.`)
        } else if (size !== ctx.diskSize) {
          plan.apply.push({ kind: 'disk-size', size })
        }
        break
      }
      case 'set_image_name': {
        const imageName = e.value.trim()
        if (imageName) plan.apply.push({ kind: 'image-name', name: imageName })
        break
      }
    }
  }
  return plan
}
