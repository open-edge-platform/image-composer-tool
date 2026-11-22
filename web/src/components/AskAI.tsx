// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Ask AI: the user says what to change in plain words, the AI service
// suggests edits, and each one that passes the checks in lib/aiEdits.ts is
// applied the way the same edit made by hand would be. The edits then show
// in the Changes rail, tagged AI, where each can be undone. Nothing is saved
// or built until the user does so.

import { useState, type FormEvent, type ReactNode } from 'react'
import { api } from '../api/client'
import type { PackageRepo } from '../api/types'
import { editPackageNames, planAIEdits, type PlannedEdit } from '../lib/aiEdits'
import { packageEntryName } from '../lib/authorTemplate'
import { useStore, type AddedPackage } from '../store'

interface AskAIProps {
  os: string
  // The template with the user's changes so far, as Save would write it.
  templateText: string
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

interface Outcome {
  note: string
  applied: string[]
  skipped: string[]
}

export function AskAI({ os, templateText, basePackages, templateRepos, templateOnlyRepos, overlay }: AskAIProps) {
  const [request, setRequest] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [outcome, setOutcome] = useState<Outcome | null>(null)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!request.trim() || busy) return
    setBusy(true)
    setError(null)
    setOutcome(null)
    try {
      const suggestion = await api.suggestTemplateEdits(templateText, request)

      // Look the names up in every repository for the target, so a package
      // from a repository not yet checked can still be picked (and that
      // repository turned on), as picking a search hit does.
      const names = editPackageNames(suggestion.edits)
      let repos: PackageRepo[] = []
      let found = {}
      let lookupComplete = true
      if (names.length > 0) {
        const catalog = (await api.listPackageRepos(os)).repos
        repos = [...catalog, ...templateOnlyRepos.filter((r) => !catalog.some((c) => c.id === r.id))]
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
      const plan = planAIEdits(suggestion.edits, {
        basePackages,
        added: state.addedPackages,
        removed: state.removedPackages,
        checkedRepos,
        priorities,
        found,
        lookupComplete,
        diskSize: state.disk && (overlay || state.disk.size) ? state.disk.size : null,
      })
      const labelOf = (id: string) => repos.find((r) => r.id === id)?.displayName ?? id
      setOutcome({
        note: suggestion.note,
        applied: applyPlan(plan.apply, labelOf, new Set(basePackages.map((e) => packageEntryName(e)))),
        skipped: [...plan.skipped, ...suggestion.unsupported],
      })
      setRequest('')
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
          disabled={busy || !request.trim()}
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
          {outcome.note && <p className="text-slate-700">AI: {outcome.note}</p>}
          {outcome.applied.length > 0 ? (
            <p className="text-emerald-800">Applied: {outcome.applied.join('; ')}.</p>
          ) : (
            <p className="text-slate-600">No changes were applied.</p>
          )}
          {outcome.skipped.length > 0 && (
            <ul className="list-inside list-disc text-amber-800">
              {outcome.skipped.map((s) => (
                <li key={s}>{s}</li>
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
  const lines: string[] = []
  const ai: Record<string, AddedPackage | 'removed'> = {}
  for (const e of edits) {
    switch (e.kind) {
      case 'package':
        if (e.enableRepo) s.setRepoEnabled(e.enableRepo, true)
        s.setPackage(e.pkg, { releaseRepo: false })
        ai[e.pkg.name] = e.pkg
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
        lines.push(`kept ${e.name}`)
        break
      case 'remove-template':
        s.setPackageRemoved(e.name, true)
        ai[e.name] = 'removed'
        lines.push(`removed ${e.name}`)
        break
      case 'remove-added':
        s.removePackage(e.name, { releaseRepo: false })
        lines.push(`removed ${e.name}`)
        break
      case 'disk-size': {
        const disk = useStore.getState().disk
        if (disk) {
          s.setDisk({ ...disk, size: e.size })
          lines.push(`disk size ${e.size}`)
        }
        break
      }
      case 'image-name':
        s.setImageName(e.name)
        lines.push(`image name ${e.name}`)
        break
    }
  }
  s.markAIPackages(ai)
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
