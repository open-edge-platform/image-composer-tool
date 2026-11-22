// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Ask AI: checking the edits the AI service suggests before any reaches the
// template. A package name or version must be one the target's repositories
// publish, the way the package picker only offers published ones; an edit
// that fails a check is reported, never applied. Edits that pass are applied
// through the same store actions as the same edit made by hand, so they show
// in the Changes rail and can be undone there.

import type { PackageRepo, PackageSearchResult, TemplateEdit } from '../api/types'
import type { AddedPackage } from '../store'
import { packageEntryName } from './authorTemplate'
import type { BaseLock } from './baseLock'
import { MAX_SELECTED_PACKAGES, planTemplateUpdates, sameVersion } from './templateUpdates'
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
  // Every repository for the target, for a pin_latest that names one.
  repos: Pick<PackageRepo, 'id' | 'displayName' | 'url'>[]
}

// The package names a suggestion's edits need looked up. A pin_latest for a
// whole repository needs every package the template has or the user added,
// to find those that come from it.
export function editPackageNames(edits: TemplateEdit[], everyPackage: () => string[]): string[] {
  const names = edits.filter((e) => e.package).map((e) => e.package)
  if (edits.some((e) => e.op === 'pin_latest' && !e.package)) names.push(...everyPackage())
  return [...new Set(names)]
}

// matchRepo finds the repository the user named, in their words ("ppa",
// "intel", part of a URL). Several matches narrow to the ones the build
// reads; still several is ambiguous.
export function matchRepo(
  words: string,
  repos: AIEditContext['repos'],
  checked: Set<string>,
): AIEditContext['repos'][number] | AIEditContext['repos'] {
  const tokens = words.toLowerCase().split(/[\s,]+/).filter((t) => t && !['repo', 'repository', 'the'].includes(t))
  const hay = (r: AIEditContext['repos'][number]) => `${r.id} ${r.displayName} ${r.url}`.toLowerCase()
  let matches = repos.filter((r) => tokens.length > 0 && tokens.every((t) => hay(r).includes(t)))
  if (matches.length > 1) {
    const inUse = matches.filter((r) => checked.has(r.id))
    if (inUse.length > 0) matches = inUse
  }
  return matches.length === 1 ? matches[0] : matches
}

const MAX_LISTED = 5
const listNames = (names: string[]) =>
  names.slice(0, MAX_LISTED).join(', ') + (names.length > MAX_LISTED ? ` and ${names.length - MAX_LISTED} more` : '')

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

  // Pins every package from one repository to the newest version it
  // publishes, by the rules of the Packages step's "Update to latest
  // version" for that repository: removed packages stay removed, a template
  // pin whose version the repository doesn't publish stays as it is, and an
  // unpinned template package a build installs from another repository
  // stays unpinned.
  const pinRepoLatest = (words: string) => {
    const match = matchRepo(words, ctx.repos, ctx.checkedRepos)
    if (Array.isArray(match)) {
      skip(
        match.length === 0
          ? `No repository matches "${words}".`
          : `"${words}" matches ${match.length} repositories (${listNames(match.map((r) => r.displayName))}); say which one.`,
      )
      return
    }
    if (!ctx.lookupComplete) {
      skip(`Couldn't read every repository right now, so nothing from ${match.displayName} was pinned: the newest version could be in what's missing. Try again.`)
      return
    }
    const repo = match.id
    const inRepo = (name: string) => {
      const hit = ctx.found[name]
      const versions = hit?.versions?.length ? hit.versions : hit ? [{ version: hit.version, repository: hit.repository }] : []
      const here = versions.filter((v) => v.repository === repo)
      return here.length > 0 ? { ...hit!, version: here[0].version, repository: repo, versions: here } : undefined
    }
    const pins: AddedPackage[] = []
    const current: string[] = []
    // Packages the user added from this repository.
    for (const p of added.values()) {
      if (p.repo !== repo || template.has(p.name)) continue
      const hit = inRepo(p.name)
      if (!hit) continue
      if (p.version === hit.version) current.push(p.name)
      else pins.push({ name: p.name, version: hit.version, repo })
    }
    // The template's own packages this repository publishes.
    const elsewhere: string[] = []
    const templateNames: string[] = []
    const found: Record<string, PackageSearchResult> = {}
    for (const [name, pinned] of template) {
      if (removed.has(name)) continue
      const hit = inRepo(name)
      if (!hit) continue
      const override = added.get(name)
      if (override) {
        if (override.repo === repo && override.version === hit.version) current.push(name)
        else if (override.repo === repo || pinned) pins.push({ name, version: hit.version, repo })
        continue
      }
      if (!pinned) {
        // What a build installs for an unpinned name across the repositories
        // it reads; one from elsewhere stays unpinned.
        const all = ctx.found[name]
        const checkedVersions = (all.versions ?? []).filter((v) => ctx.checkedRepos.has(v.repository))
        const wins = checkedVersions.length > 0 ? buildResolution({ ...all, versions: checkedVersions }, ctx.priorities).repository : repo
        if (wins !== repo) {
          elsewhere.push(name)
          continue
        }
      } else if (sameVersion(hit.version, pinned)) {
        current.push(name)
        continue
      }
      templateNames.push(name)
      found[name] = hit
    }
    const lock: BaseLock = {
      info: (name) => {
        const pinned = template.get(name)
        return { locked: pinned !== undefined, currentVersion: pinned || undefined, currentIsFloating: pinned === '' }
      },
      ensureResolved: () => {},
      concreteNames: [...template.keys()],
    }
    const updates = planTemplateUpdates(templateNames, lock, found)
    pins.push(...updates.pins)
    if (added.size + updates.pins.length > MAX_SELECTED_PACKAGES) {
      skip(`Pinning ${pins.length} packages from ${match.displayName} would go over the ${MAX_SELECTED_PACKAGES} changes a build accepts, so none were pinned.`)
      return
    }
    for (const pkg of pins) {
      added.set(pkg.name, pkg)
      plan.apply.push({ kind: 'package', pkg, enableRepo: ctx.checkedRepos.has(repo) ? undefined : repo })
    }
    if (pins.length === 0) skip(`Nothing from ${match.displayName} needed pinning.`)
    if (current.length > 0) skip(`${current.length} already at the newest version in ${match.displayName}.`)
    if (updates.unverifiable.length > 0) {
      skip(`Kept ${updates.unverifiable.length} template pin(s) as they are (${listNames(updates.unverifiable)}): ${match.displayName} doesn't publish the pinned version.`)
    }
    if (elsewhere.length > 0) {
      skip(`Kept ${elsewhere.length} unpinned (${listNames(elsewhere)}): a build installs them from another repository.`)
    }
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
      case 'pin_latest': {
        if (!name) {
          pinRepoLatest(e.value)
          break
        }
        const hit = ctx.found[name]
        if (!hit) {
          plan.skipped.push(notFound(name))
          break
        }
        // The newest version in the repository a build would install it from.
        const r = pick(name, '')
        if ('message' in r) {
          plan.skipped.push(r)
          break
        }
        const newest = (hit.versions ?? []).find((v) => v.repository === r.pkg.repo)?.version ?? hit.version
        const current = added.get(name)?.version ?? (template.has(name) && !removed.has(name) ? template.get(name) : undefined)
        if (current !== undefined && current !== '' && sameVersion(newest, current)) skip(`${name}: already at the newest version, ${newest}.`)
        else setVersion(name, newest)
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
