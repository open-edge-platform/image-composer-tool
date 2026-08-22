import { useEffect, useMemo, useState } from 'react'
import { useStore, cascadingOptions } from '../store'
import type { Selection } from '../store'
import { api } from '../api/client'
import type { ComposeRequest, ComposeResponse } from '../api/types'
import { Select } from './Select'
import { PackagesStep } from './PackagesStep'
import { Input } from './Input'

// How long to wait after the last keystroke in Image Name before re-composing.
// Each compose call with an override generates and deletes a server-side file
// (internal/api/service/delta.go), so debouncing avoids a write+delete pair per
// keystroke. Build itself never waits on this — it always sends whatever is
// currently typed.
const IMAGE_NAME_DEBOUNCE_MS = 400

// The Advanced tab is a wizard, mirroring the prototype's step flow. Only the
// first step (Target / "Choose Image Configuration") is built out today; the
// remaining steps host the resolved-template preview and placeholders for work
// that lands in later tasks.
const STEPS = ['Target', 'Packages', 'Disk', 'Review'] as const

interface AdvancedPageProps {
  active: boolean
  onBuildStarted: (buildId: string) => void
  buildInProgress: boolean
}

// `active` prevents duplicate compose fetches while both pages stay mounted (hidden via CSS).
export function AdvancedPage({ active, onBuildStarted, buildInProgress }: AdvancedPageProps) {
  const manifest = useStore((s) => s.manifest)
  const selection = useStore((s) => s.selection)
  const setField = useStore((s) => s.setField)
  const imageName = useStore((s) => s.imageName)
  const imageNameEdited = useStore((s) => s.imageNameEdited)
  const setImageName = useStore((s) => s.setImageName)
  const seedImageName = useStore((s) => s.seedImageName)

  const [step, setStep] = useState(0)
  const [composed, setComposed] = useState<ComposeResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const opts = useMemo(
    () => (manifest ? cascadingOptions(manifest, selection) : null),
    [manifest, selection],
  )

  const complete = !!opts?.matched

  // Debounced so an override compose call (which generates and removes a
  // server-side delta file) fires once after the user pauses, not per keystroke.
  const [debouncedImageName, setDebouncedImageName] = useState(imageName)
  useEffect(() => {
    const t = setTimeout(() => setDebouncedImageName(imageName), IMAGE_NAME_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [imageName])

  // The request the backend actually resolves against: the cascade selection,
  // plus the image name override — only once the user has edited it, and only
  // the settled (debounced) value, so an in-progress edit doesn't compose
  // against a half-typed name.
  const composeReq: ComposeRequest = useMemo(
    () => ({ ...selection, imageName: imageNameEdited ? debouncedImageName : undefined }),
    [selection, imageNameEdited, debouncedImageName],
  )

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
      .compose(composeReq)
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
  }, [active, complete, composeReq])

  if (!manifest || !opts) return <div className="p-8">Loading…</div>

  const setSel = (k: keyof Selection, v: string) => {
    setField(k, v)
    setError(null) // clear any stale compose error from the previous selection
  }

  const onBuild = async () => {
    if (!complete) return
    try {
      setBusy(true)
      setError(null)
      // Build always sends whatever is currently typed, not the debounced value
      // the Review preview is showing — a discrete action shouldn't wait on a
      // typing pause.
      const buildReq: ComposeRequest = {
        ...selection,
        imageName: imageNameEdited ? imageName : undefined,
      }
      // Re-compose against buildReq (the current, non-debounced image name)
      // right before starting the build, so the logged YAML matches what the
      // build actually runs rather than the debounced preview.
      const fresh = await api.compose(buildReq)
      console.log('Composing image with template YAML:\n' + fresh.yaml)
      const accepted = await api.startBuild(buildReq)
      onBuildStarted(accepted.buildId)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const copyYaml = () => composed && navigator.clipboard.writeText(composed.yaml)

  const exportYaml = () => {
    if (!composed) return
    const blob = new Blob([composed.yaml], { type: 'text/yaml' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = composed.template || `${imageName || 'image'}.yml`
    a.click()
    URL.revokeObjectURL(a.href)
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

      {/* The Packages step holds a 240px rail beside a package pane, which is
          cramped at the 3xl the single-column steps use. */}
      <div
        className={
          'mx-auto rounded-lg border border-slate-200 bg-white p-6 shadow-sm ' +
          (step === 1 ? 'max-w-5xl' : 'max-w-3xl')
        }
      >
        {step === 0 && (
          <TargetStep
            opts={opts}
            selection={selection}
            setSel={setSel}
            imageName={imageName}
            setImageName={setImageName}
          />
        )}

        {step === 1 && <PackagesStep os={selection.os} active={active} />}

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
              <>
                {/* Section 1: Review Image Configuration — the same summary shape
                    the Basic tab shows, so both panes agree on what will compose. */}
                <div className="mb-4 rounded-lg border border-slate-200 bg-slate-50 p-4 text-xs">
                  <table className="w-full">
                    <tbody>
                      {([
                        ['Image', `${composed.summary.imageName}${composed.summary.imageVersion ? ` (v${composed.summary.imageVersion})` : ''}`],
                        composed.summary.baseImage ? ['Base Image', composed.summary.baseImage] : null,
                        composed.summary.description ? ['Description', composed.summary.description] : null,
                        ['Architecture', composed.summary.architecture],
                        composed.summary.kernelVersion ? ['Kernel', composed.summary.kernelVersion] : null,
                        ['Packages', `${composed.summary.packageCount} packages`],
                        composed.summary.diskSize ? ['Disk', `${composed.summary.diskSize}${composed.summary.partitionTable ? `, ${composed.summary.partitionTable.toUpperCase()}` : ''}${composed.summary.partitionCount ? `, ${composed.summary.partitionCount} partitions` : ''}`] : null,
                        composed.summary.hostname ? ['Hostname', composed.summary.hostname] : null,
                      ] as ([string, string] | null)[]).filter((r): r is [string, string] => r !== null).map(([k, v]) => (
                        <tr key={k}>
                          <td className="py-0.5 pr-3 font-semibold text-slate-500 w-24">{k}</td>
                          <td className="py-0.5 text-slate-700">{v}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>

                {/* Section 2: Generated YAML — the resolved template, verbatim. */}
                <div className="rounded-lg border border-slate-200 bg-white">
                  <div className="flex items-center justify-between border-b border-slate-200 px-4 py-2">
                    <span className="text-sm font-semibold text-[#00285a]">Generated YAML</span>
                    <button
                      type="button"
                      onClick={copyYaml}
                      title="Copy YAML to clipboard"
                      className="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                    >
                      Copy
                    </button>
                  </div>
                  <pre className="max-h-[60vh] overflow-auto px-4 py-3 font-mono text-xs leading-relaxed text-slate-700">
                    {composed.yaml}
                  </pre>
                </div>

                <div className="mt-4 flex flex-wrap items-center gap-3">
                  <button
                    type="button"
                    onClick={exportYaml}
                    className="rounded-md border border-slate-300 bg-white px-5 py-2.5 font-semibold text-[#00285a] hover:border-slate-400 hover:bg-slate-50"
                  >
                    Export YAML
                  </button>
                  <button
                    type="button"
                    onClick={onBuild}
                    disabled={busy || buildInProgress}
                    className="rounded-md bg-[#0071c5] px-5 py-2.5 font-semibold text-white hover:bg-[#00285a] disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {busy ? 'Starting…' : buildInProgress ? 'Composing…' : 'Compose Image'}
                  </button>
                  {buildInProgress && (
                    <span className="text-sm text-amber-600">
                      A compose is already in progress. Switch to the Compose Image tab to monitor it.
                    </span>
                  )}
                </div>
              </>
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
          {step < STEPS.length - 1 && (
            <button
              type="button"
              onClick={() => setStep((s) => Math.min(STEPS.length - 1, s + 1))}
              className="rounded-md bg-[#0071c5] px-4 py-2 text-sm font-semibold text-white hover:bg-[#005a9e]"
            >
              Next
            </button>
          )}
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
