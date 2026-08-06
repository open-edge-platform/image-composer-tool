import { useEffect, useMemo, useState } from 'react'
import { useStore, cascadingOptions } from '../store'
import { api } from '../api/client'
import type { ComposeResponse } from '../api/types'
import { Select } from './Select'

// AdvancedPage is the first slice of the Advanced tab: it reuses the Basic tab's
// cascading selection to resolve a pre-authored template, then shows that
// template's YAML read-only. It is a preview only — editing, validation, package
// selection and export land in later PRs. The selection lives in the shared
// store, so switching between Basic and Advanced keeps the same choices.
//
// `active` is true only while the Advanced tab is the visible one. Both pages
// stay mounted (hidden via CSS) so their state survives tab switches, but the
// compose fetch is gated on `active` so a hidden page doesn't issue duplicate
// requests for the same selection.
export function AdvancedPage({ active }: { active: boolean }) {
  const manifest = useStore((s) => s.manifest)
  const selection = useStore((s) => s.selection)
  const setField = useStore((s) => s.setField)

  const [composed, setComposed] = useState<ComposeResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const opts = useMemo(
    () => (manifest ? cascadingOptions(manifest, selection) : null),
    [manifest, selection],
  )

  const complete = !!opts?.matched

  // Resolve the template YAML whenever the selection is complete and this tab is
  // visible, mirroring the Basic tab's auto-fetch. Guard against out-of-order
  // responses with a cancel flag so a slow earlier request can't overwrite a
  // newer selection's result. Clear any prior result/error up front so a stale
  // template never lingers while the new one loads (or after the selection
  // becomes incomplete).
  useEffect(() => {
    setError(null)
    if (!active || !complete) {
      setComposed(null)
      return
    }
    setComposed(null)
    let cancelled = false
    api
      .compose(selection)
      .then((r) => {
        if (!cancelled) setComposed(r)
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [active, complete, selection])

  if (!manifest || !opts) return <div className="p-8">Loading…</div>

  const setSel = (k: Parameters<typeof setField>[0], v: string) => {
    setField(k, v)
    setError(null) // clear any stale compose error from the previous selection
  }

  return (
    <div className="mx-auto max-w-screen-2xl px-10 py-8">
      <h1 className="mb-1 text-2xl font-bold text-[#00285a]">Advanced Template Preview</h1>
      <p className="mb-5 text-sm text-slate-500">
        Select a combination to see the exact pre-authored template it resolves to.
        This is a read-only preview — editing and export will arrive in a later update.
      </p>

      <div className="flex flex-col gap-6 lg:flex-row lg:items-start">
        <div className="w-full max-w-xl rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
          <Select
            label="Targeted Vertical"
            placeholder="-- Select Vertical --"
            value={selection.vertical}
            options={opts.verticals}
            onChange={(v) => setSel('vertical', v)}
          />
          <Select
            label="SKU"
            placeholder="-- Select SKU --"
            value={selection.sku}
            options={opts.skus}
            disabled={!selection.vertical}
            onChange={(v) => setSel('sku', v)}
          />
          <Select
            label="Platform"
            placeholder="-- Select Platform --"
            value={selection.platform}
            options={opts.platforms}
            disabled={!selection.sku && opts.skus.length > 0}
            onChange={(v) => setSel('platform', v)}
          />
          <Select
            label="Operating System"
            placeholder="-- Select Operating System --"
            value={selection.os}
            options={opts.oses}
            disabled={!selection.platform}
            onChange={(v) => setSel('os', v)}
          />
          {opts.kernels.length > 0 && (
            <Select
              label="Kernel"
              placeholder="-- Select Kernel --"
              value={selection.kernel}
              options={opts.kernels}
              disabled={!selection.os}
              onChange={(v) => setSel('kernel', v)}
            />
          )}
        </div>

        {/* Read-only YAML preview of the resolved template. */}
        <div className="flex-1">
          {error && (
            <div className="mb-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>
          )}
          {complete && composed ? (
            <div className="rounded-lg border border-slate-200 bg-white shadow-sm">
              <div className="flex items-center justify-between border-b border-slate-200 px-4 py-2">
                <span className="text-xs font-semibold uppercase tracking-wide text-slate-500">
                  Template
                </span>
                <span className="font-mono text-xs text-slate-400">{composed.template}</span>
              </div>
              <pre className="max-h-[70vh] overflow-auto px-4 py-3 font-mono text-xs leading-relaxed text-slate-700">
                {composed.yaml}
              </pre>
            </div>
          ) : (
            <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center text-sm text-slate-400">
              Complete all selections to preview the template.
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
