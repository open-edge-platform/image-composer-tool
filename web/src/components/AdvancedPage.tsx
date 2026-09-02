import { useEffect, useMemo, useState } from 'react'
import { useStore, cascadingOptions, encodePackage } from '../store'
import type { Selection } from '../store'
import { api } from '../api/client'
import type { ComposeRequest, ComposeResponse } from '../api/types'
import { Select } from './Select'
import { PackagesStep } from './PackagesStep'
import { Input } from './Input'
import { DiskStep } from './DiskStep'
import { parseDiskFromYaml, toDiskConfig } from '../lib/disk'

// How long to wait after the last keystroke in Image Name before re-composing.
// Each compose call with an override generates and deletes a server-side file
// (internal/api/service/delta.go), so debouncing avoids a write+delete pair per
// keystroke. Build itself never waits on this — it always sends whatever is
// currently typed.
const IMAGE_NAME_DEBOUNCE_MS = 400

// The disk override is debounced for the same reason as the image name, and by
// the same amount: the Disk step's fields include free text (disk name, mount
// points, hand-typed offsets), so an undebounced override would write and delete
// a server-side delta file on every keystroke.
const DISK_DEBOUNCE_MS = 400

// The Advanced tab is a wizard, mirroring the prototype's step flow. Only the
// first step (Target / "Choose Image Configuration") is built out today; the
// remaining steps host the resolved-template preview and placeholders for work
// that lands in later tasks.
const STEPS = ['Target', 'Packages', 'Disk', 'Review'] as const

// The template views the Review step can show.
type TemplateView = 'delta' | 'base' | 'resolved'

// templateViews lists the views available for a compose result, most specific
// to the user's own choices first.
//
// Delta and Base exist only when something was overridden: with no overrides no
// delta is generated, and the resolved template *is* the base, so offering
// either would show the same bytes under two names.
function templateViews(
  composed: ComposeResponse | null,
): { key: TemplateView; label: string; yaml: string; hint: string }[] {
  if (!composed) return []
  const out: { key: TemplateView; label: string; yaml: string; hint: string }[] = []
  if (composed.deltaYaml) {
    out.push({
      key: 'delta',
      label: 'Your changes',
      yaml: composed.deltaYaml,
      hint: `Only what Advanced mode adds, as a template extending ${composed.template}. This is the file the build resolves.`,
    })
  }
  if (composed.baseYaml) {
    out.push({
      key: 'base',
      label: 'Base template',
      yaml: composed.baseYaml,
      hint: `${composed.template} on its own, without your changes.`,
    })
  }
  out.push({
    key: 'resolved',
    label: out.length ? 'Resolved' : 'Generated YAML',
    yaml: composed.yaml,
    hint: out.length
      ? 'The two above merged — the complete template this build runs.'
      : '',
  })
  return out
}

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
  const addedPackages = useStore((s) => s.addedPackages)
  const enabledRepos = useStore((s) => s.enabledRepos)
  const seedDisk = useStore((s) => s.seedDisk)
  const disk = useStore((s) => s.disk)
  const diskEdited = useStore((s) => s.diskEdited)

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

  // Package picks are encoded once here, and memoised on the encoded strings
  // rather than on addedPackages itself: the store hands back a new array on
  // every selection change, and comparing the joined encoding means re-pinning a
  // package to the version it already had doesn't trigger a recompose.
  const packagesKey = useMemo(() => addedPackages.map(encodePackage).sort().join('\n'), [addedPackages])
  const reposKey = useMemo(() => [...enabledRepos].sort().join('\n'), [enabledRepos])

  // Serialised for the same reason packages are keyed on their joined encoding:
  // the model is a fresh object on every edit, so memoising on it directly would
  // recompose on every keystroke. The JSON of the *emitted* block is the right
  // key — it ignores model fields that never reach the template (React row keys,
  // the layout mode, the derived side of the size/offset pair), so switching
  // between size- and offset-based editing does not trigger a recompose.
  //
  // Sent only once the user has edited: an unedited model round-trips to the
  // template's own disk block, so sending it would generate a delta that changes
  // nothing while making the Review pane claim an override.
  const diskKey = useMemo(
    () => (diskEdited && disk ? JSON.stringify(toDiskConfig(disk)) : ''),
    [diskEdited, disk],
  )
  const [debouncedDiskKey, setDebouncedDiskKey] = useState(diskKey)
  useEffect(() => {
    const t = setTimeout(() => setDebouncedDiskKey(diskKey), DISK_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [diskKey])

  // The request the backend actually resolves against: the cascade selection,
  // plus the Advanced-mode overrides. The image name is sent only once the user
  // has edited it, and only the settled (debounced) value, so an in-progress
  // edit doesn't compose against a half-typed name. Packages and repos need no
  // debounce of their own — each arrives from a discrete click, not a keystroke.
  const composeReq: ComposeRequest = useMemo(
    () => ({
      ...selection,
      imageName: imageNameEdited ? debouncedImageName : undefined,
      // Omitted rather than sent empty, so a selection with nothing added is
      // "no override" and the backend resolves the curated template directly.
      packages: packagesKey ? packagesKey.split('\n') : undefined,
      repos: reposKey ? reposKey.split('\n') : undefined,
      disk: debouncedDiskKey ? JSON.parse(debouncedDiskKey) : undefined,
    }),
    [selection, imageNameEdited, debouncedImageName, packagesKey, reposKey, debouncedDiskKey],
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
          // Seed the editable image name and disk layout from the resolved
          // template (both no-ops once the user has edited — guarded by the
          // *Edited flags in the store). The disk block is read out of the
          // resolved YAML because the compose summary only carries aggregates
          // (size/count/table), not the partition list the Disk step edits.
          seedImageName(r.summary.imageName)
          seedDisk(parseDiskFromYaml(r.yaml))
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
      // Spread composeReq rather than rebuilding from `selection`, so every
      // override reaches the build. Rebuilding listed only imageName, which
      // silently dropped the Packages step's picks (and would have dropped the
      // disk layout too) from every Advanced build.
      //
      // The two debounced fields are then re-read live: a discrete action
      // shouldn't wait on a typing pause, so the build gets whatever is
      // currently on screen rather than the value the Review preview settled on.
      const buildReq: ComposeRequest = {
        ...composeReq,
        imageName: imageNameEdited ? imageName : undefined,
        disk: diskEdited && disk ? toDiskConfig(disk) : undefined,
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

  // Which of the template views the Review step is showing. Deliberately not
  // reset per compose: an override compose fires on a debounce, so resetting
  // would yank the user back off "Resolved" every time they touched the image
  // name. A view that stops existing (the last override was removed) falls back
  // to the first available one instead.
  const [view, setView] = useState<TemplateView>('delta')
  const views = templateViews(composed)
  const shownView = views.find((v) => v.key === view) ?? views[0]
  const shownYaml = shownView?.yaml ?? ''

  const copyYaml = () => shownYaml && navigator.clipboard.writeText(shownYaml)

  const exportYaml = () => {
    if (!composed || !shownYaml) return
    const blob = new Blob([shownYaml], { type: 'text/yaml' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    // The delta is a distinct artifact from the resolved template — exporting
    // both under the curated parent's name would produce two different files
    // that claim to be the same one.
    const base = composed.template || `${imageName || 'image'}.yml`
    a.download = shownView?.key === 'delta' ? base.replace(/(\.ya?ml)?$/i, '.delta.yml') : base
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

      {/* The Packages step holds a 240px rail plus a 300px selected-packages
          rail beside the package pane, which is cramped at the 3xl the
          single-column steps use. The Disk step needs the width for its
          eight-column partition table (name/label/fs/size/mount/start/end
          plus row actions). */}
      <div
        className={
          'mx-auto rounded-lg border border-slate-200 bg-white p-6 shadow-sm ' +
          (step === 1 ? 'max-w-6xl' : step === 2 ? 'max-w-5xl' : 'max-w-3xl')
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

        {step === 2 && <DiskStep />}

        {step === 3 && (
          <div>
            <h2 className="mb-1 text-lg font-bold text-[#00285a]">Review Image Configuration</h2>
            <p className="mb-5 text-sm text-slate-500">
              Review the template this combination resolves to. Where you changed
              something, your changes are shown separately from the pre-authored
              template they extend.
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

                {/* Pinned a version for a package the curated template already
                    lists? Both entries survive the merge, so say so rather than
                    letting the resolved YAML look like a duplicate bug. */}
                {composed.pinConflicts && composed.pinConflicts.length > 0 && (
                  <div className="mb-4 rounded border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
                    <span className="font-semibold">
                      {composed.pinConflicts.length === 1
                        ? 'One pinned package is also provided by the base template'
                        : `${composed.pinConflicts.length} pinned packages are also provided by the base template`}
                      :
                    </span>{' '}
                    {composed.pinConflicts.join(', ')}. Template inheritance combines
                    package lists rather than replacing them, so the base
                    template&apos;s own unpinned entry stays alongside your pinned
                    one and the build may install either version. Choose
                    &quot;Latest&quot; for these packages to leave the base
                    template&apos;s entry to decide.
                  </div>
                )}

                {/* Section 2: the template, in up to three views. Delta is what
                    Advanced mode contributed, Base the curated template it
                    extends, Resolved the merge of the two — which is what a
                    build actually runs. Only Resolved exists when nothing has
                    been overridden. */}
                <div className="rounded-lg border border-slate-200 bg-white">
                  <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-2">
                    {views.length > 1 ? (
                      <div className="flex gap-1" role="tablist" aria-label="Template view">
                        {views.map((v) => (
                          <button
                            key={v.key}
                            type="button"
                            role="tab"
                            aria-selected={v.key === shownView?.key}
                            onClick={() => setView(v.key)}
                            title={v.hint}
                            className={
                              'rounded-md px-2.5 py-1 text-xs font-semibold transition-colors ' +
                              (v.key === shownView?.key
                                ? 'bg-[#0071c5] text-white'
                                : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900')
                            }
                          >
                            {v.label}
                          </button>
                        ))}
                      </div>
                    ) : (
                      <span className="text-sm font-semibold text-[#00285a]">Generated YAML</span>
                    )}
                    <button
                      type="button"
                      onClick={copyYaml}
                      title="Copy YAML to clipboard"
                      className="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900"
                    >
                      Copy
                    </button>
                  </div>
                  {shownView?.hint && (
                    <p className="border-b border-slate-100 bg-slate-50 px-4 py-1.5 text-[11px] text-slate-500">
                      {shownView.hint}
                    </p>
                  )}
                  <pre className="max-h-[60vh] overflow-auto px-4 py-3 font-mono text-xs leading-relaxed text-slate-700">
                    {shownYaml}
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
