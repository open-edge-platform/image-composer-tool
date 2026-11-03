import { useEffect, useMemo, useState } from 'react'
import { useStore, cascadingOptions, encodePackage } from '../store'
import type { Selection } from '../store'
import { api } from '../api/client'
import type {
  ComposeRequest,
  ComposeResponse,
  CredentialInput,
  CredentialRequirement,
  EdgePack,
  NewUserInput,
} from '../api/types'
import { Select } from './Select'
import { PackagesStep } from './PackagesStep'
import { Input } from './Input'
import { DiskStep } from './DiskStep'
import { AdvancedChangesCard } from './AdvancedChangesCard'
import { credentialsComplete } from './CredentialsStep'
import { newUsersValid } from './NewUsersSection'
import { CredentialsDialog } from './CredentialsDialog'
import { parseDiskFromYaml, toDiskConfig, validateDisk } from '../lib/disk'
import { useBaseLock } from '../lib/baseLock'
import { strandedPackages } from '../lib/edgepack'

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

// Credentials are debounced like the image name and disk block: the password
// and key fields are free text, so an undebounced override would write and
// delete a server-side delta file on every keystroke.
const CREDENTIALS_DEBOUNCE_MS = 400

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
  const [credentialsOpen, setCredentialsOpen] = useState(false)
  // Logins for accounts the resolved template leaves without one. Component
  // state, not the shared store: the store is persisted and a password must not
  // outlive the build it was typed for.
  const [credentials, setCredentials] = useState<CredentialInput[]>([])
  // The accounts the last successful compose reported. Held separately because
  // the effect clears `composed` before each refetch: without this the
  // credential fields would vanish from the open dialog the moment typing in it
  // triggered a recompose, and the build would read as needing no login.
  const [lastRequirements, setLastRequirements] = useState<CredentialRequirement[]>([])
  // Accounts the user asked to add on top of the template's own, held for the
  // same reason credentials are: each entry carries a password.
  const [newUsers, setNewUsers] = useState<NewUserInput[]>([])
  // The matched template's own package list from the last successful compose,
  // null until one lands for this selection. Held for the same reason
  // lastRequirements is: the effect clears `composed` before each refetch, and
  // the Packages check below must keep seeing what the template ships while a
  // recompose is in flight.
  const [lastBasePackages, setLastBasePackages] = useState<string[] | null>(null)
  // The Edge Pack for the selected OS, for the same check. Fetched here rather
  // than taken from the Packages step, which unmounts once the wizard moves on
  // and starts each visit with nothing loaded. Keyed by OS so a stale pack is
  // never read against a different target.
  const [edgePack, setEdgePack] = useState<{ os: string; pack: EdgePack | null } | null>(null)

  const opts = useMemo(
    () => (manifest ? cascadingOptions(manifest, selection) : null),
    [manifest, selection],
  )

  const complete = !!opts?.matched

  // Every field the Target step requires, by label, that is still empty. SKU and
  // Kernel only count when the cascade offers them — the selectors are hidden
  // (or skippable) otherwise. The wizard does not move past Target, and Target
  // does not show as done, until this is empty and the selection resolves to a
  // template.
  const missingTarget = opts
    ? ([
        ['Use Case', !selection.vertical],
        ['SKU', opts.skus.length > 0 && !selection.sku],
        ['Platform', !selection.platform],
        ['Operating System', !selection.os],
        ['Kernel', opts.kernels.length > 0 && !selection.kernel],
        ['Image Name', !imageName.trim()],
      ] as [string, boolean][]).filter(([, missing]) => missing).map(([label]) => label)
    : []
  const targetComplete = complete && missingTarget.length === 0

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
  // Debounced for the same reason the image name is: a credential is typed, and
  // every override compose writes and removes a server-side delta file. The
  // build itself re-reads the live value, so nothing waits on a typing pause.
  const [debouncedCredentials, setDebouncedCredentials] = useState<CredentialInput[]>([])
  const credentialsKey = useMemo(() => JSON.stringify(credentials), [credentials])
  useEffect(() => {
    const t = setTimeout(() => setDebouncedCredentials(JSON.parse(credentialsKey)), CREDENTIALS_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [credentialsKey])

  // Accounts the resolved template declares, as the compose response reports
  // them. An account this request creates comes back in that list too, so it is
  // taken out again: left in, the name the user just added would read as
  // "already taken" and block the build they are trying to start.
  const composedRequirements = useMemo(
    () =>
      (composed?.credentials ?? []).filter((r) => !newUsers.some((u) => u.user === r.user)),
    [composed, newUsers],
  )

  // Debounced like the credentials, and only once every entry is complete: a
  // half-typed account is rejected by the backend, which would replace the
  // Review pane with an error while the user is still filling the form in.
  const [debouncedNewUsers, setDebouncedNewUsers] = useState<NewUserInput[]>([])
  const newUsersKey = useMemo(() => JSON.stringify(newUsers), [newUsers])
  const newUsersReady = newUsersValid(newUsers, composedRequirements)
  useEffect(() => {
    if (!newUsersReady) return
    const t = setTimeout(() => setDebouncedNewUsers(JSON.parse(newUsersKey)), CREDENTIALS_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [newUsersKey, newUsersReady])

  // A different template asks for different accounts, so anything typed for the
  // previous one is dropped rather than being sent with the next build.
  useEffect(() => {
    setCredentials([])
    setNewUsers([])
    setLastRequirements([])
    setLastBasePackages(null)
  }, [selection])

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
  // A layout the Disk step has already flagged is never sent. The server would
  // reject it with the same complaint the step is showing inline, so posting it
  // buys a round trip and a second, redundant error banner. Holding the previous
  // key instead means the Review pane keeps showing the last good resolve while
  // the user fixes the field, rather than flicking back to the un-overridden
  // template and implying their edits were discarded.
  //
  // Gated on diskEdited: an unedited model is seeded from the curated template
  // and never sent, so a curated layout the step cannot represent must not
  // block composing that template as-is.
  const diskInvalid = useMemo(
    () => diskEdited && !!disk && validateDisk(disk, { imageType: selection.imageType }).errors.length > 0,
    [diskEdited, disk, selection.imageType],
  )
  const [debouncedDiskKey, setDebouncedDiskKey] = useState(diskKey)
  useEffect(() => {
    if (diskInvalid) return
    const t = setTimeout(() => setDebouncedDiskKey(diskKey), DISK_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [diskKey, diskInvalid])

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
      disk: diskEdited && debouncedDiskKey ? JSON.parse(debouncedDiskKey) : undefined,
      credentials: debouncedCredentials.length > 0 ? debouncedCredentials : undefined,
      users: debouncedNewUsers.length > 0 ? debouncedNewUsers : undefined,
    }),
    [selection, imageNameEdited, debouncedImageName, packagesKey, reposKey, diskEdited,
      debouncedDiskKey, debouncedCredentials, debouncedNewUsers],
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
          if (r.credentials) setLastRequirements(r.credentials)
          setLastBasePackages(r.basePackages ?? [])
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

  // Fetched once per OS. A failed fetch stores nothing, so it is told apart
  // from an OS with no Edge Pack and retried on the next step change — entering
  // Packages included, where the step fetches its own copy and may show the
  // inline warning. Until one succeeds no issue is raised.
  const os = selection.os
  useEffect(() => {
    if (!active || !os || edgePack?.os === os) return
    let cancelled = false
    api
      .getEdgePack(os)
      .then((pack) => { if (!cancelled) setEdgePack({ os, pack }) })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [active, os, edgePack?.os, step])

  // Domain packages selected with no base runtime beneath them will not build,
  // which is what the Edge Packs tab warns about inline. Derived here from the
  // store rather than reported by the Packages step, so it stays correct while
  // that step is unmounted or still loading. Only `locked` is read off the
  // base lock, which needs no repos and makes no requests.
  const pack = edgePack?.os === os ? edgePack.pack : null
  const baseLock = useBaseLock(os, null, lastBasePackages ?? [])
  const packagesHaveIssues =
    !!pack && lastBasePackages !== null && strandedPackages(pack, addedPackages, baseLock).length > 0

  if (!manifest || !opts) return <div className="p-8">Loading…</div>

  const setSel = (k: keyof Selection, v: string) => {
    setField(k, v)
    setError(null) // clear any stale compose error from the previous selection
  }

  // Once composed, the template's own resolved accounts are the authority; the
  // last known list covers the gap while a recompose is in flight.
  const requirements = composed
    ? composedRequirements
    : lastRequirements.length > 0
      ? lastRequirements
      : []
  // Same gate as Basic: the backend rejects a build whose template leaves an
  // account without a login, so stop here rather than letting it fail.
  const credentialsReady = credentialsComplete(requirements, credentials) && newUsersReady

  const onBuild = async () => {
    if (!complete || !credentialsReady) return
    // Unlike a compose, a build cannot fall back to the last good layout: it has
    // to run what is on screen or nothing. Say so rather than posting a request
    // the server will reject.
    if (diskInvalid) {
      setError('Fix the errors on the Disk step before composing.')
      return
    }
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
        credentials: credentials.length > 0 ? credentials : undefined,
        users: newUsers.length > 0 ? newUsers : undefined,
      }
      // Re-compose against buildReq (the current, non-debounced image name)
      // right before starting the build, so the logged YAML matches what the
      // build actually runs rather than the debounced preview.
      const fresh = await api.compose(buildReq)
      console.log('Composing image with template YAML:\n' + fresh.yaml)
      const accepted = await api.startBuild(buildReq)
      setCredentialsOpen(false)
      // Kept, they would mark the accounts satisfied and the next build would skip the prompt.
      setCredentials([])
      onBuildStarted(accepted.buildId)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const onComposeClick = () => {
    if (diskInvalid) {
      setError('Fix the errors on the Disk step before composing.')
      return
    }
    // Always through the dialog: besides any missing logins, it is where
    // accounts are added, which a template with nothing to ask for still offers.
    setCredentialsOpen(true)
  }

  // A step reads as done once the user is past it — except Target, which also
  // needs every required field filled (the selection is shared with Basic, so
  // it can be cleared there while the wizard sits on a later step), and
  // Packages and Disk, which come pre-filled from the template and so only read
  // as done once the user has actually changed something there.
  const packagesChanged = addedPackages.length > 0 || enabledRepos.length > 0
  const stepDone = (i: number) =>
    i < step &&
    (i === 0 ? targetComplete : i === 1 ? packagesChanged : i === 2 ? diskEdited && !diskInvalid : true)
  // Red, ahead of green, for a step whose values will not build.
  const stepHasIssues = (i: number) => (i === 1 && packagesHaveIssues) || (i === 2 && diskInvalid)
  // The wizard does not move past Target with a field missing, nor past Disk
  // while it shows an error — a required field left empty there included.
  const canLeaveStep = targetComplete && !(step === 2 && diskInvalid)

  // Empty until something is overridden: with no overrides no delta is
  // generated, and the build runs the curated template unchanged.
  const deltaYaml = composed?.deltaYaml ?? ''

  const exportYaml = () => {
    if (!composed || !deltaYaml) return
    const blob = new Blob([deltaYaml], { type: 'text/yaml' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    // The delta is a distinct artifact from the template it extends — exporting
    // it under the curated parent's name would produce two different files that
    // claim to be the same one.
    const base = composed.template || `${imageName || 'image'}.yml`
    a.download = base.replace(/(\.ya?ml)?$/i, '.delta.yml')
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
              // Later steps stay locked until Target is filled in, and Review
              // until the Disk step's errors are fixed.
              disabled={(i > 0 && !targetComplete) || (i > 2 && diskInvalid)}
              className={
                'flex h-8 items-center gap-2 rounded-full px-3 text-xs font-semibold transition-colors disabled:cursor-not-allowed ' +
                (i === step
                  ? 'bg-[#0071c5] text-white'
                  : stepHasIssues(i)
                    ? 'bg-red-500 text-white'
                    : stepDone(i)
                      ? 'bg-emerald-500 text-white'
                      : 'bg-slate-200 text-slate-500 hover:bg-slate-300 disabled:hover:bg-slate-200')
              }
            >
              <span className="flex h-5 w-5 items-center justify-center rounded-full bg-white/25 text-[11px]">
                {i === step ? i + 1 : stepHasIssues(i) ? '!' : stepDone(i) ? '✓' : i + 1}
              </span>
              {label}
            </button>
            {i < STEPS.length - 1 && (
              <div className={'h-0.5 w-6 ' + (stepDone(i) && !stepHasIssues(i) ? 'bg-emerald-500' : 'bg-slate-200')} />
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
        {/* A failed resolve leaves Image Name unseeded, which locks Next behind
            what reads as an unfilled field. Say why, rather than leaving the
            error on a Review step the user cannot reach. */}
        {step === 0 && error && (
          <div className="mb-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>
        )}
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
          <PackagesStep
            os={selection.os}
            active={active}
            basePackages={composed?.basePackages ?? []}
          />
        )}

        {step === 2 && <DiskStep />}

        {step === 3 && (
          <div>
            <h2 className="mb-1 text-lg font-bold text-[#00285a]">Review Image Configuration</h2>
            <p className="mb-5 text-sm text-slate-500">
              Review what this combination will compose. The template below is
              only what you changed, as a delta extending the pre-authored
              template it is based on.
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

                {/* What this selection changes relative to the template, item
                    by item — open here, since reviewing it is the point. */}
                {composed.summary.advanced ? (
                  <div className="mb-4">
                    <AdvancedChangesCard
                      changes={composed.summary.advanced}
                      defaultOpen
                      frame="rounded-lg border border-slate-200 bg-white p-4"
                    />
                  </div>
                ) : (
                  <p className="mb-4 rounded-lg border border-slate-200 bg-white px-4 py-3 text-xs text-slate-500">
                    No changes yet — this build runs {composed.template} as-is.
                    Edit the image name, packages, repositories, or disk layout
                    and your changes appear here.
                  </p>
                )}

                {/* Pinned a version for a package the curated template already
                    lists? Both entries survive the merge, so say so rather than
                    letting the resolved template look like a duplicate bug. */}
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

                <div className="mt-4 flex flex-wrap items-center gap-3">
                  <button
                    type="button"
                    onClick={exportYaml}
                    disabled={!deltaYaml}
                    title={deltaYaml ? undefined : 'Nothing overridden yet, so there is no delta to export'}
                    className="rounded-md border border-slate-300 bg-white px-5 py-2.5 font-semibold text-[#00285a] hover:border-slate-400 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    Export YAML
                  </button>
                  <button
                    type="button"
                    onClick={onComposeClick}
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
            <div className="flex items-center gap-3">
              {!canLeaveStep && (
                <span className="text-xs text-amber-600">
                  {!targetComplete
                    ? missingTarget.length > 0
                      ? `Fill in the required Target fields: ${missingTarget.join(', ')}.`
                      : 'This selection does not match an available template.'
                    : 'Fix the errors on the Disk step before continuing.'}
                </span>
              )}
              <button
                type="button"
                onClick={() => setStep((s) => Math.min(STEPS.length - 1, s + 1))}
                disabled={!canLeaveStep}
                className="rounded-md bg-[#0071c5] px-4 py-2 text-sm font-semibold text-white hover:bg-[#005a9e] disabled:cursor-not-allowed disabled:opacity-50"
              >
                Next
              </button>
            </div>
          )}
        </div>
      </div>
      <CredentialsDialog
        open={credentialsOpen}
        requirements={requirements}
        credentials={credentials}
        onChange={setCredentials}
        newUsers={newUsers}
        onNewUsersChange={setNewUsers}
        credentialsReady={credentialsReady}
        busy={busy}
        buildInProgress={buildInProgress}
        error={error}
        onCancel={() => setCredentialsOpen(false)}
        onCompose={() => void onBuild()}
      />
    </div>
  )
}

// Marks a field the Target step will not let the wizard move past while empty.
const REQUIRED = <span className="text-red-600"> *</span>

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
        Select a use case, SKU, and platform. Pre-configured defaults are
        applied based on your selection. Fields marked{' '}
        <span className="font-semibold text-red-600">*</span> are required.
      </p>

      <Select
        label="Use Case"
        labelSuffix={REQUIRED}
        placeholder="-- Select Use Case --"
        value={selection.vertical}
        options={opts.verticals}
        onChange={(v) => setSel('vertical', v)}
      />
      <Select
        label="SKU"
        labelSuffix={REQUIRED}
        placeholder="-- Select SKU --"
        value={selection.sku}
        options={opts.skus}
        disabled={!selection.vertical}
        onChange={(v) => setSel('sku', v)}
      />
      <Select
        label="Platform"
        labelSuffix={REQUIRED}
        placeholder="-- Select Platform --"
        value={selection.platform}
        options={opts.platforms}
        disabled={!selection.sku && opts.skus.length > 0}
        onChange={(v) => setSel('platform', v)}
      />
      <Select
        label="Operating System"
        labelSuffix={REQUIRED}
        placeholder="-- Select Operating System --"
        value={selection.os}
        options={opts.oses}
        disabled={!selection.platform}
        onChange={(v) => setSel('os', v)}
      />
      {opts.kernels.length > 0 && (
        <Select
          label="Kernel"
          labelSuffix={REQUIRED}
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
        labelSuffix={REQUIRED}
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
