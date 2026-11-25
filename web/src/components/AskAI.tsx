// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Ask AI: the user says what to change in plain words, the AI service
// suggests edits, and each one that passes the checks in lib/aiEdits.ts is
// applied the way the same edit made by hand would be. The edits then show
// in the Changes rail, tagged AI, where each can be undone. Nothing is saved
// or built until the user does so.

import { useState, type FormEvent, type ReactNode } from 'react'
import { api } from '../api/client'
import type { PackageRepo, PackageSearchResult, TemplateEdit } from '../api/types'
import { describeChanges, editPackageNames, expandPatternEdits, planAIEdits, type PlannedEdit, type SkippedEdit } from '../lib/aiEdits'
import { packageEntryName } from '../lib/authorTemplate'
import { useStore, type AIChanges } from '../store'

interface AskAIProps {
  os: string
  // The template with the user's changes so far, as Save would write it.
  // Undefined while it (re)loads.
  templateText: string | undefined
  // ComposeResponse.basePackages: the template's own package entries.
  basePackages: string[]
  // Repositories the template declares (ComposeResponse.templateRepos), and
  // those of them the catalog doesn't list.
  templateRepos: string[]
  templateOnlyRepos: PackageRepo[]
  // An overlay can grow its baseline image even when the template sets no
  // size; any other template can be resized only when it sets one (an ISO
  // sizes its own media).
  overlay: boolean
}

// A skipped edit, with similar package names to offer when it named one
// no repository publishes.
interface Skipped {
  message: string
  missing?: string
  alternatives?: string[]
}

interface Outcome {
  note: string
  applied: string[]
  skipped: Skipped[]
  // A question answered in the note, with no edits asked for.
  answerOnly?: boolean
}

const MAX_ALTERNATIVES = 3
// A whole repository's pins can be dozens of lines; the rail lists them all.
const MAX_APPLIED_LISTED = 6

export function AskAI({ os, templateText, basePackages, templateRepos, templateOnlyRepos, overlay }: AskAIProps) {
  const [request, setRequest] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [outcome, setOutcome] = useState<Outcome | null>(null)

  // The changes so far, for a question about them. Repository names come
  // from the catalog; an id stands in if it can't be fetched.
  const changesSoFar = async (): Promise<string> => {
    const catalog = await api.listPackageRepos(os).then((r) => r.repos, () => [] as PackageRepo[])
    const label = (id: string) => [...catalog, ...templateOnlyRepos].find((r) => r.id === id)?.displayName ?? id
    const s = useStore.getState()
    const sizeChanged = s.diskEdited && s.disk && s.diskSeed && s.disk.size !== s.diskSeed.size
    return describeChanges({
      basePackages,
      added: s.addedPackages,
      removed: s.removedPackages,
      enabledRepos: s.enabledRepos,
      diskSize: sizeChanged ? { from: s.diskSeed!.size, to: s.disk!.size } : undefined,
      diskLayoutEdited: s.diskEdited,
      imageName: s.imageNameEdited ? s.imageName : undefined,
      repoLabel: label,
    })
  }

  // The name of the repository the Packages step is browsing, for the AI to
  // know what "this repository" means. Undefined when none is.
  const browsingName = async (): Promise<string | undefined> => {
    const id = useStore.getState().browsedRepo
    if (!id) return undefined
    const catalog = await api.listPackageRepos(os).then((r) => r.repos, () => [] as PackageRepo[])
    return [...catalog, ...templateOnlyRepos].find((r) => r.id === id)?.displayName ?? id
  }

  // Checks edits and applies those that pass. Names are looked up in every
  // repository for the target, so a package from a repository not yet
  // checked can still be picked (and that repository turned on), as picking
  // a search hit does.
  const runEdits = async (suggested: TemplateEdit[]): Promise<{ applied: string[]; skipped: Skipped[] }> => {
    // Every repository for the target: the catalog plus the template's own.
    let allRepos: PackageRepo[] | null = null
    const loadRepos = async () => {
      if (!allRepos) {
        const catalog = (await api.listPackageRepos(os)).repos
        allRepos = [...catalog, ...templateOnlyRepos.filter((r) => !catalog.some((c) => c.id === r.id))]
      }
      return allRepos
    }
    // A name pattern (gir*) becomes an add for each package it matches.
    let edits = suggested
    let patternSkipped: SkippedEdit[] = []
    if (suggested.some((e) => e.op === 'add_matching')) {
      const known = await loadRepos()
      const st = useStore.getState()
      const inTemplate = new Set(basePackages.map((e) => packageEntryName(e)).filter((n): n is string => !!n))
      const expanded = await expandPatternEdits(suggested, {
        search: async (prefix, repoIds, offset) => {
          const r = await api.searchPackages({ os, repos: repoIds, q: prefix, limit: 100, offset })
          return { names: r.packages.map((p) => p.name), total: r.total }
        },
        repos: known,
        checkedRepos: new Set([...known.filter((r) => r.enabledByDefault).map((r) => r.id), ...templateRepos, ...st.enabledRepos]),
        present: (n) => (inTemplate.has(n) && !st.removedPackages.includes(n)) || st.addedPackages.some((p) => p.name === n),
        browsed: st.browsedRepo,
      })
      edits = expanded.edits
      patternSkipped = expanded.skipped
    }

    const names = editPackageNames(edits, () => {
      const state = useStore.getState()
      return [
        ...basePackages.map((e) => packageEntryName(e)).filter((n): n is string => !!n),
        ...state.addedPackages.map((p) => p.name),
      ]
    })
    let repos: PackageRepo[] = []
    let found: Record<string, PackageSearchResult> = {}
    let lookupComplete = true
    if (names.length > 0) {
      repos = await loadRepos()
      try {
        const r = await api.lookupPackages({ os, repos: repos.map((r) => r.id), names })
        found = Object.fromEntries(r.packages.map((p) => [p.name, p]))
        lookupComplete = r.unavailableRepos.length === 0
      } catch {
        lookupComplete = false
      }
    }

    const state = useStore.getState()
    const checkedRepos = new Set([
      ...repos.filter((r) => r.enabledByDefault).map((r) => r.id),
      ...templateRepos,
      ...state.enabledRepos,
    ])
    const priorities = Object.fromEntries(repos.filter((r) => r.priority != null).map((r) => [r.id, r.priority!]))
    const plan = planAIEdits(edits, {
      basePackages,
      added: state.addedPackages,
      removed: state.removedPackages,
      checkedRepos,
      priorities,
      found,
      lookupComplete,
      diskSize: state.disk && (overlay || state.disk.size) ? state.disk.size : null,
      repos,
    })
    const labelOf = (id: string) => repos.find((r) => r.id === id)?.displayName ?? id
    const applied = applyPlan(plan.apply, labelOf, new Set(basePackages.map((e) => packageEntryName(e))))
    // The closest names a search finds for a missing one, such as docker.io
    // for "docker", to offer instead.
    const skipped = await Promise.all(
      plan.skipped.map(async (s): Promise<Skipped> => {
        if (!s.missing || s.missing.length < 2) return s
        try {
          const r = await api.searchPackages({ os, repos: repos.map((r) => r.id), q: s.missing, limit: 50 })
          // Nearest first: from a repository the build already reads (Ubuntu's
          // docker.io before Docker's own docker-ce), then the shortest name.
          const rank = (p: PackageSearchResult) => [checkedRepos.has(p.repository) ? 0 : 1, p.name.length]
          const alternatives = [
            ...new Set(
              r.packages
                .filter((p) => p.name !== s.missing)
                .sort((a, b) => rank(a)[0] - rank(b)[0] || rank(a)[1] - rank(b)[1] || a.name.localeCompare(b.name))
                .map((p) => p.name),
            ),
          ].slice(0, MAX_ALTERNATIVES)
          return alternatives.length > 0 ? { ...s, alternatives } : s
        } catch {
          return s
        }
      }),
    )
    return { applied, skipped: [...patternSkipped, ...skipped] }
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!request.trim() || busy || templateText === undefined) return
    setBusy(true)
    setError(null)
    setOutcome(null)
    try {
      const suggestion = await api.suggestTemplateEdits(templateText, request, await changesSoFar(), await browsingName())
      const { applied, skipped } = await runEdits(suggestion.edits)
      setOutcome({
        note: suggestion.note,
        applied,
        skipped: [...skipped, ...suggestion.unsupported.map((message) => ({ message }))],
        answerOnly: suggestion.edits.length === 0 && suggestion.unsupported.length === 0 && !!suggestion.note,
      })
      setRequest('')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  // Adds the package the user picked in place of a missing one.
  const onAlternative = async (item: Skipped, name: string) => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const { applied, skipped } = await runEdits([{ op: 'add', package: name, version: '', value: '' }])
      setOutcome((o) =>
        o && {
          ...o,
          applied: [...o.applied, ...applied],
          skipped: [...o.skipped.filter((s) => s !== item), ...skipped],
        },
      )
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto mb-5 max-w-6xl rounded-lg border border-violet-200 bg-violet-50/60 p-3 text-sm">
      <form onSubmit={(e) => void onSubmit(e)} className="flex items-center gap-3">
        <label htmlFor="ask-ai" className="shrink-0 font-semibold text-violet-900">
          ✨ Ask AI
        </label>
        <input
          id="ask-ai"
          value={request}
          onChange={(e) => setRequest(e.target.value)}
          maxLength={2000}
          disabled={busy}
          placeholder="e.g. add docker, remove snapd, make the disk 32 GiB"
          className="min-w-0 flex-1 rounded-md border border-slate-300 bg-white px-3 py-1.5 focus:border-violet-400 focus:outline-none disabled:bg-slate-50"
        />
        <button
          type="submit"
          disabled={busy || !request.trim() || templateText === undefined}
          className="rounded-md bg-violet-700 px-4 py-1.5 font-semibold text-white hover:bg-violet-800 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? 'Thinking…' : 'Suggest'}
        </button>
      </form>
      <p className="mt-1.5 text-xs text-slate-500">
        Changes appear in the Changes rail, tagged AI, where you can undo them. Nothing is saved until you save.
      </p>
      {error && (
        <p role="alert" className="mt-2 text-xs text-red-700">
          {linkify(error)}
        </p>
      )}
      {outcome && (
        <div className="mt-2 space-y-1 text-xs" aria-live="polite">
          {outcome.note && <p className="whitespace-pre-line text-slate-700">AI: {outcome.note}</p>}
          {outcome.answerOnly ? null : outcome.applied.length > 0 ? (
            <p className="text-emerald-800">
              Applied:{' '}
              {outcome.applied.length > MAX_APPLIED_LISTED
                ? `${outcome.applied.slice(0, MAX_APPLIED_LISTED).join('; ')}; and ${outcome.applied.length - MAX_APPLIED_LISTED} more (see the Changes rail).`
                : `${outcome.applied.join('; ')}.`}
            </p>
          ) : (
            <p className="text-slate-600">No changes were applied.</p>
          )}
          {outcome.skipped.length > 0 && (
            <ul className="list-inside list-disc text-amber-800">
              {outcome.skipped.map((s) => (
                <li key={s.message}>
                  {s.message}
                  {s.alternatives && (
                    <span className="ml-1">
                      Did you mean{' '}
                      {s.alternatives.map((a) => (
                        <button
                          key={a}
                          type="button"
                          disabled={busy}
                          onClick={() => void onAlternative(s, a)}
                          className="mx-0.5 rounded border border-amber-300 bg-white px-1.5 font-mono text-[11px] text-amber-900 hover:border-amber-500 disabled:opacity-50"
                        >
                          {a}
                        </button>
                      ))}
                      ?
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

// applyPlan applies checked edits through the store, as the Packages, Disk
// and Target steps do for the same edits by hand, and returns a line for
// each. Repositories are never released here: a pick that moves a package
// off a repository leaves the repository for the user to uncheck.
function applyPlan(
  edits: PlannedEdit[],
  labelOf: (repo: string) => string,
  templateNames: Set<string | null>,
): string[] {
  const s = useStore.getState()
  // The state before these edits, for Undo AI changes. A change Ask AI made
  // earlier keeps its own "before", so undoing goes back past both.
  const before = {
    added: s.addedPackages,
    removed: s.removedPackages,
    repos: s.enabledRepos,
    disk: s.disk,
    diskEdited: s.diskEdited,
    imageName: s.imageName,
    imageNameEdited: s.imageNameEdited,
    ai: s.aiChanges,
  }
  const lines: string[] = []
  const touched = new Set<string>()
  const turnedOn: string[] = []
  let diskChanged = false
  let nameChanged = false
  for (const e of edits) {
    switch (e.kind) {
      case 'package':
        if (e.enableRepo) {
          s.setRepoEnabled(e.enableRepo, true)
          if (!before.repos.includes(e.enableRepo)) turnedOn.push(e.enableRepo)
        }
        s.setPackage(e.pkg, { releaseRepo: false })
        touched.add(e.pkg.name)
        lines.push(
          (templateNames.has(e.pkg.name)
            ? e.pkg.version
              ? `pinned ${e.pkg.name} to ${e.pkg.version}`
              : `unpinned ${e.pkg.name}`
            : `added ${e.pkg.name}${e.pkg.version ? ` ${e.pkg.version}` : ''}`) +
            (e.enableRepo ? ` (turned on ${labelOf(e.enableRepo)})` : ''),
        )
        break
      case 'restore':
        s.setPackageRemoved(e.name, false)
        touched.add(e.name)
        lines.push(`kept ${e.name}`)
        break
      case 'remove-template':
        s.setPackageRemoved(e.name, true)
        touched.add(e.name)
        lines.push(`removed ${e.name}`)
        break
      case 'remove-added':
        s.removePackage(e.name, { releaseRepo: false })
        touched.add(e.name)
        lines.push(`removed ${e.name}`)
        break
      case 'disk-size': {
        const disk = useStore.getState().disk
        if (disk) {
          s.setDisk({ ...disk, size: e.size })
          diskChanged = true
          lines.push(`disk size ${e.size}`)
        }
        break
      }
      case 'image-name':
        s.setImageName(e.name)
        nameChanged = true
        lines.push(`image name ${e.name}`)
        break
    }
  }

  const now = useStore.getState()
  const packages: AIChanges['packages'] = {}
  for (const name of touched) {
    const earlier = before.ai.packages[name]
    const was = earlier?.before ?? before.added.find((p) => p.name === name) ?? null
    const wasRemoved = earlier ? earlier.wasRemoved : before.removed.includes(name)
    const pick = now.addedPackages.find((p) => p.name === name) ?? null
    const removed = now.removedPackages.includes(name)
    // Back where it started: nothing for Ask AI to own or undo.
    if (removed === wasRemoved && pick?.version === was?.version && pick?.repo === was?.repo) continue
    packages[name] = { after: removed ? 'removed' : pick, before: was, wasRemoved }
  }
  s.recordAIChanges({
    packages,
    repos: turnedOn,
    disk: diskChanged ? (before.ai.disk ?? { before: before.disk, beforeEdited: before.diskEdited }) : undefined,
    imageName: nameChanged
      ? (before.ai.imageName ?? { before: before.imageName, beforeEdited: before.imageNameEdited })
      : undefined,
  })
  return lines
}

// linkify turns the URLs in a message into links, such as the page where a
// user gets a new token.
function linkify(text: string): ReactNode[] {
  return text.split(/(https?:\/\/[^\s]+[^\s.,;)])/).map((part, i) =>
    i % 2 === 1 ? (
      <a key={i} href={part} target="_blank" rel="noreferrer" className="font-semibold underline">
        {part}
      </a>
    ) : (
      part
    ),
  )
}
