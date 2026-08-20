import { useEffect, useMemo, useState } from 'react'
import { useStore, cascadingOptions } from '../store'
import type { Selection } from '../store'
import { api } from '../api/client'
import type { ComposeResponse } from '../api/types'
import { Select } from './Select'
import { Input } from './Input'

// The Advanced tab is a wizard, mirroring the prototype's step flow. Only the
// first step (Target / "Choose Image Configuration") is built out today; the
// remaining steps host the resolved-template preview and placeholders for work
// that lands in later tasks.
const STEPS = ['Target', 'Packages', 'Disk', 'Review'] as const

// `active` prevents duplicate compose fetches while both pages stay mounted (hidden via CSS).
export function AdvancedPage({ active }: { active: boolean }) {
  const manifest = useStore((s) => s.manifest)
  const selection = useStore((s) => s.selection)
  const setField = useStore((s) => s.setField)
  const imageName = useStore((s) => s.imageName)
  const setImageName = useStore((s) => s.setImageName)
  const seedImageName = useStore((s) => s.seedImageName)

  const [step, setStep] = useState(0)
  const [composed, setComposed] = useState<ComposeResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const opts = useMemo(
    () => (manifest ? cascadingOptions(manifest, selection) : null),
    [manifest, selection],
  )

  const complete = !!opts?.matched

  // Entering Advanced always lands on the first step, mirroring the prototype's
  // enterAdvanced(). The page stays mounted while hidden (tabs toggle via CSS),
  // so without this the wizard resumes wherever it was last left — including
  // when arriving from Basic's "Edit in Advanced".
  useEffect(() => {
    if (active) setStep(0)
  }, [active])

  // Guard against out-of-order responses; clear stale state before each new fetch.
  useEffect(() => {
    setError(null)
    setComposed(null)
    setLoading(false)
    if (!active || !complete) return
    let cancelled = false
    setLoading(true)
    api
      .compose(selection)
      .then((r) => {
        if (!cancelled) {
          setComposed(r)
          setLoading(false)
          // Seed the editable image name from the resolved template (no-op once
          // the user has edited — guarded by the *Edited flag in the store).
          seedImageName(r.summary.imageName)
        }
      })
      .catch((e) => {
        if (!cancelled) { setError((e as Error).message); setLoading(false) }
      })
    return () => {
      cancelled = true
    }
  }, [active, complete, selection])

  if (!manifest || !opts) return <div className="p-8">Loading…</div>

  const setSel = (k: keyof Selection, v: string) => {
    setField(k, v)
    setError(null) // clear any stale compose error from the previous selection
  }

  return (
    <div className="mx-auto max-w-screen-2xl px-10 py-8">
      {/* Step progress indicator (matches the prototype's numbered dots + lines). */}
      <div className="mb-6 flex items-center justify-center gap-1">
        {STEPS.map((label, i) => (
          <div key={label} className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setStep(i)}
              className={
                'flex h-8 items-center gap-2 rounded-full px-3 text-xs font-semibold transition-colors ' +
                (i === step
                  ? 'bg-[#0071c5] text-white'
                  : i < step
                    ? 'bg-emerald-500 text-white'
                    : 'bg-slate-200 text-slate-500 hover:bg-slate-300')
              }
            >
              <span className="flex h-5 w-5 items-center justify-center rounded-full bg-white/25 text-[11px]">
                {i < step ? '✓' : i + 1}
              </span>
              {label}
            </button>
            {i < STEPS.length - 1 && (
              <div className={'h-0.5 w-6 ' + (i < step ? 'bg-emerald-500' : 'bg-slate-200')} />
            )}
          </div>
        ))}
      </div>

      <div className="mx-auto max-w-3xl rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
        {step === 0 && (
          <TargetStep
            opts={opts}
            selection={selection}
            setSel={setSel}
            imageName={imageName}
            setImageName={setImageName}
          />
        )}

        {step === 1 && (
          <div>
            <h2 className="mb-1 text-lg font-bold text-[#00285a]">Repositories &amp; Packages</h2>
            <p className="text-sm text-slate-500">
              Repository and package selection will arrive in a later update.
            </p>
          </div>
        )}

        {step === 2 && (
          <div>
            <h2 className="mb-1 text-lg font-bold text-[#00285a]">Disk Layout</h2>
            <p className="text-sm text-slate-500">
              Disk and partition editing will arrive in a later update.
            </p>
          </div>
        )}

        {step === 3 && (
          <div>
            <h2 className="mb-1 text-lg font-bold text-[#00285a]">Review Image Configuration</h2>
            <p className="mb-5 text-sm text-slate-500">
              Review the pre-authored template this combination resolves to.
            </p>
            {error && (
              <div className="mb-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>
            )}
            {complete && composed ? (
              <div className="rounded-lg border border-slate-200 bg-white">
                <div className="flex items-center justify-between border-b border-slate-200 px-4 py-2">
                  <span className="text-xs font-semibold uppercase tracking-wide text-slate-500">
                    Template
                  </span>
                  <span className="font-mono text-xs text-slate-400">{composed.template}</span>
                </div>
                <pre className="max-h-[60vh] overflow-auto px-4 py-3 font-mono text-xs leading-relaxed text-slate-700">
                  {composed.yaml}
                </pre>
              </div>
            ) : complete && loading ? (
              <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center text-sm text-slate-400">
                Resolving template…
              </div>
            ) : (
              <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center text-sm text-slate-400">
                Complete all selections in the Target step to preview the template.
              </div>
            )}
          </div>
        )}

        {/* Back / Next navigation. */}
        <div className="mt-6 flex justify-between border-t border-slate-200 pt-4">
          <button
            type="button"
            onClick={() => setStep((s) => Math.max(0, s - 1))}
            disabled={step === 0}
            className="rounded-md border border-slate-300 px-4 py-2 text-sm font-semibold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
          >
            Back
          </button>
          <button
            type="button"
            onClick={() => setStep((s) => Math.min(STEPS.length - 1, s + 1))}
            disabled={step === STEPS.length - 1}
            className="rounded-md bg-[#0071c5] px-4 py-2 text-sm font-semibold text-white hover:bg-[#005a9e] disabled:cursor-not-allowed disabled:opacity-50"
          >
            Next
          </button>
        </div>
      </div>
    </div>
  )
}

// Step 1: "Choose Image Configuration" — the selection cascade (shared with the
// Basic tab), plus the editable Image Name override.
function TargetStep({
  opts,
  selection,
  setSel,
  imageName,
  setImageName,
}: {
  opts: NonNullable<ReturnType<typeof cascadingOptions>>
  selection: Selection
  setSel: (k: keyof Selection, v: string) => void
  imageName: string
  setImageName: (v: string) => void
}) {
  return (
    <div>
      <h2 className="mb-1 text-lg font-bold text-[#00285a]">Choose Image Configuration</h2>
      <p className="mb-5 text-sm text-slate-500">
        Select a targeted vertical, SKU, and platform. Pre-configured defaults are
        applied based on your selection.
      </p>

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

      <p className="-mt-2 mb-4 text-xs text-slate-400">
        These are the same options the Basic tab offers; whichever pane you change,
        both stay in step.
      </p>

      <hr className="my-6 border-slate-200" />

      <Input
        label="Image Name"
        placeholder="Image name"
        value={imageName}
        onChange={setImageName}
      />
      <p className="-mt-2 text-xs text-slate-400">
        Defaults to the matched template's image name; once you edit it, your name
        is kept.
      </p>
    </div>
  )
}
