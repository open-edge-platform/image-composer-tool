import { useEffect, useMemo, useState } from 'react'
import { useStore, cascadingOptions } from '../store'
import { api } from '../api/client'
import type { ComposeResponse, CredentialInput, NewUserInput } from '../api/types'
import { Select } from './Select'
import { credentialsComplete } from './CredentialsStep'
import { newUsersValid } from './NewUsersSection'
import { CredentialsDialog } from './CredentialsDialog'
import { ImportTemplateDialog } from './ImportTemplateDialog'
import { ImportedTemplateCard } from './ImportedTemplateCard'

interface BasicPageProps {
  onBuildStarted: (buildId: string) => void
  buildInProgress: boolean
  // True while visible; gates the compose fetch to prevent duplicate requests.
  active: boolean
  // Switches to the Advanced tab carrying the current selection. Selections live
  // in the shared store, so there is nothing to copy — this only changes the view.
  onEditInAdvanced: () => void
}

export function BasicPage({
  onBuildStarted,
  buildInProgress,
  active,
  onEditInAdvanced,
}: BasicPageProps) {
  const manifest = useStore((s) => s.manifest)
  const selection = useStore((s) => s.selection)
  const setField = useStore((s) => s.setField)
  const importedTemplate = useStore((s) => s.importedTemplate)
  const importedRequirements = useStore((s) => s.importedRequirements)
  const setImportedTemplate = useStore((s) => s.setImportedTemplate)

  const [review, setReview] = useState<ComposeResponse | null>(null)
  const [busy, setBusy] = useState(false)
  const [credentialsOpen, setCredentialsOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // True while the summary for a newly completed selection is in flight. The
  // previous summary stays on screen during a refetch, so this only drives the
  // placeholder shown before the first one arrives.
  const [loadingSummary, setLoadingSummary] = useState(false)
  // Logins for accounts the chosen template leaves without one. Deliberately
  // component state rather than the shared store: the store is persisted, and a
  // password must not outlive the build it was typed for.
  const [credentials, setCredentials] = useState<CredentialInput[]>([])
  // Accounts the user asked to add on top of the template's own. Transient for
  // the same reason credentials are: each entry carries a password.
  const [newUsers, setNewUsers] = useState<NewUserInput[]>([])

  const opts = useMemo(
    () => (manifest ? cascadingOptions(manifest, selection) : null),
    [manifest, selection],
  )

  const complete = !!opts?.matched

  // Auto-fetch the configuration summary whenever the selection is complete, so
  // it always reflects the current dropdowns (no manual "review" step). Guard
  // against out-of-order responses with a cancel flag. An imported template is
  // built verbatim, so there is no selection to summarise while one is active.
  useEffect(() => {
    if (!active || !complete || importedTemplate) {
      setReview(null)
      setLoadingSummary(false)
      return
    }
    // A different template asks for different accounts, so anything typed for
    // the previous one is dropped rather than silently sent with the new build.
    setCredentials([])
    setNewUsers([])
    let cancelled = false
    setLoadingSummary(true)
    api
      .compose(selection)
      .then((r) => {
        if (cancelled) return
        setReview(r)
        // A successful compose supersedes any earlier compose failure; without
        // this the banner from a transient error sat above a valid summary until
        // the user happened to touch a dropdown.
        setError(null)
        setLoadingSummary(false)
      })
      .catch((e) => {
        if (cancelled) return
        setError((e as Error).message)
        setLoadingSummary(false)
      })
    return () => {
      cancelled = true
    }
  }, [active, complete, selection, importedTemplate])

  if (!manifest || !opts) return <div className="p-8">Loading…</div>

  // An imported template replaces the selections entirely: it is already a
  // complete document, so there is nothing to compose it against.
  const usingImport = importedTemplate !== null

  // An imported template's accounts come from validating the document at import
  // time; a selection's come from the compose response.
  const requirements = usingImport ? importedRequirements : (review?.credentials ?? [])
  // The backend rejects a build whose template leaves an account without a
  // login, so the button is gated on the same rule rather than letting the
  // request fail. An added account must be complete for the same reason.
  const credentialsReady =
    credentialsComplete(requirements, credentials) && newUsersValid(newUsers, requirements)

  const onBuild = async () => {
    if (!complete || !credentialsReady) return
    try {
      setBusy(true)
      setError(null)
      const req = {
        ...selection,
        credentials: credentials.length > 0 ? credentials : undefined,
        users: newUsers.length > 0 ? newUsers : undefined,
      }
      // Re-compose against the current selection right before starting the
      // build, so the logged YAML matches the request even if it changed
      // since the last auto-fetch (or that fetch is still in flight).
      const fresh = await api.compose(req)
      console.log('Composing image with template YAML:\n' + fresh.yaml)
      const accepted = await api.startBuild(req)
      setCredentialsOpen(false)
      onBuildStarted(accepted.buildId)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const setSel = (k: Parameters<typeof setField>[0], v: string) => {
    setField(k, v)
    setError(null) // clear any stale compose error from the previous selection
  }

  // Build the imported document as-is. Unlike the selection path there is no
  // compose step: the server takes the template verbatim, so the only
  // client-side preparation already happened at import time. Any logins the
  // user supplied ride alongside it and are hashed server-side.
  const onBuildImported = async () => {
    if (!importedTemplate || !credentialsReady) return
    try {
      setBusy(true)
      setError(null)
      const accepted = await api.startBuildFromYaml(
        importedTemplate.yaml,
        credentials.length > 0 ? credentials : undefined,
        newUsers.length > 0 ? newUsers : undefined,
      )
      setCredentialsOpen(false)
      onBuildStarted(accepted.buildId)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  // Composing always goes through the dialog: besides any missing logins, it is
  // where accounts are added, which a template with nothing to ask for still
  // needs to offer.
  const onComposeClick = () => setCredentialsOpen(true)

  return (
    <div className="mx-auto max-w-screen-2xl px-10 py-8">
      <div className="mb-5 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="mb-1 text-2xl font-bold text-[#00285a]">Choose Image Configuration</h1>
          <p className="text-sm text-slate-500">
            Select a use case, SKU, and platform. Pre-configured defaults are applied
            based on your selection — or import your own template.
          </p>
        </div>
        <button
          type="button"
          className="flex items-center gap-2 rounded-md border-2 border-[#0071c5] bg-[#0071c5] px-5 py-2.5 font-semibold text-white shadow-sm hover:border-[#00285a] hover:bg-[#00285a]"
          onClick={() => setImportOpen(true)}
        >
          <svg
            aria-hidden="true"
            viewBox="0 0 20 20"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="h-4 w-4"
          >
            <path d="M10 13V3m0 0L6.5 6.5M10 3l3.5 3.5" />
            <path d="M3.5 12.5v2.5a1.5 1.5 0 0 0 1.5 1.5h10a1.5 1.5 0 0 0 1.5-1.5v-2.5" />
          </svg>
          Import Template
        </button>
      </div>

      {importedTemplate && (
        <div className="mb-5 rounded-lg border border-[#0071c5]/30 bg-blue-50 px-4 py-3 text-sm">
          <div className="flex flex-wrap items-center gap-3">
            <span className="font-semibold text-[#00285a]">Imported template:</span>
            <span className="text-slate-700">{importedTemplate.displayName}</span>
            {importedTemplate.fileName !== importedTemplate.displayName && (
              <span className="text-slate-500">from {importedTemplate.fileName}</span>
            )}
            <div className="ml-auto flex gap-3">
              <button
                type="button"
                className="font-semibold text-[#0071c5] hover:underline"
                onClick={() => setImportOpen(true)}
              >
                Replace
              </button>
              <button
                type="button"
                className="font-semibold text-slate-600 hover:underline"
                onClick={() => {
                  setImportedTemplate(null)
                  setCredentials([])
                  setNewUsers([])
                  setError(null)
                }}
              >
                Remove
              </button>
            </div>
          </div>
          <p className="mt-1.5 text-xs text-slate-600">
            Composing builds this template as supplied. Remove it to go back to the
            selections below, or switch to Advanced to change it before building.
          </p>
        </div>
      )}

      <div className="flex flex-col gap-6 lg:flex-row lg:items-start">
        {/* Dropdowns and the Compose/Edit actions share this column so the
            buttons sit right under the selections regardless of how tall the
            summary + credentials column on the right grows. */}
        <div className="w-full max-w-xl">
          <div
            className={
              'rounded-lg border border-slate-200 bg-white p-5 shadow-sm ' +
              (usingImport ? 'opacity-40' : '')
            }
            // inert, not pointer-events-none plus aria-hidden: that pair left
            // the selects tabbable while hiding them from assistive technology,
            // so a keyboard user could still change controls that are not there.
            inert={usingImport}
          >
            <Select
              label="Use Case"
              placeholder="-- Select Use Case --"
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
            {/* Kernel selector appears only when the manifest offers kernel variants
                (e.g. standard vs real-time) for the current selection. */}
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
            {/* Image Type (raw/iso) is auto-selected from the manifest for the
                chosen combination and not shown in Basic mode. */}
          </div>

          {error && <div className="mt-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>}

          <div className="mt-6 flex flex-wrap items-center gap-3">
            <button
              className="rounded-md bg-[#0071c5] px-5 py-2.5 font-semibold text-white hover:bg-[#00285a] disabled:cursor-not-allowed disabled:opacity-50"
              disabled={
                usingImport
                  ? busy || buildInProgress
                  : !complete || !review || loadingSummary || busy || buildInProgress
              }
              onClick={onComposeClick}
            >
              {busy
                ? 'Starting…'
                : buildInProgress
                  ? 'Composing…'
                  : usingImport
                    ? 'Compose Imported Image'
                    : 'Compose Image'}
            </button>
            {/* Deliberately not gated on `complete` — the prototype lets you open
                Advanced at any point and finish the selection there. */}
            <button
              type="button"
              className="rounded-md border border-slate-300 bg-white px-5 py-2.5 font-semibold text-[#00285a] hover:border-slate-400 hover:bg-slate-50"
              onClick={onEditInAdvanced}
            >
              Edit in Advanced
            </button>
            {!complete && !buildInProgress && !usingImport && (
              <span className="text-sm text-slate-500">
                Complete all selections to compose.
              </span>
            )}
            {buildInProgress && (
              <span className="text-sm text-amber-600">
                A compose is already in progress. Switch to the Compose Image tab to monitor it.
              </span>
            )}
          </div>
        </div>

        {/* Image configuration summary — always reflects the current selection
            (auto-fetched). "Your Selection" is intentionally omitted here since
            the dropdowns already show it; the Compose Image tab keeps it. */}
        {usingImport && (
          <div className="flex-1">
            <ImportedTemplateCard template={importedTemplate} />
          </div>
        )}

        {!usingImport && complete && review && (
          <div className="flex-1 text-xs">
            <div className="max-w-md rounded-lg border border-slate-200 bg-slate-50 p-4">
              <p className="mb-1.5 text-[10px] font-semibold uppercase tracking-wide text-slate-400">Image Configuration</p>
              <table className="w-full">
                <tbody>
                  {([
                    ['Image', `${review.summary.imageName}${review.summary.imageVersion ? ` (v${review.summary.imageVersion})` : ''}`],
                    review.summary.baseImage ? ['Base Image', review.summary.baseImage] : null,
                    review.summary.description ? ['Description', review.summary.description] : null,
                    ['Architecture', review.summary.architecture],
                    review.summary.kernelVersion ? ['Kernel', review.summary.kernelVersion] : null,
                    ['Packages', `${review.summary.packageCount} packages`],
                    review.summary.diskSize ? ['Disk', `${review.summary.diskSize}${review.summary.partitionTable ? `, ${review.summary.partitionTable.toUpperCase()}` : ''}${review.summary.partitionCount ? `, ${review.summary.partitionCount} partitions` : ''}`] : null,
                    review.summary.hostname ? ['Hostname', review.summary.hostname] : null,
                  ] as ([string, string] | null)[]).filter((r): r is [string, string] => r !== null).map(([k, v]) => (
                    <tr key={k}>
                      <td className="py-0.5 pr-3 font-semibold text-slate-500 w-24">{k}</td>
                      <td className="py-0.5 text-slate-700">{v}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* First-load placeholder only: a refetch keeps the previous summary on
            screen rather than flashing this in between. */}
        {!usingImport && complete && !review && loadingSummary && (
          <div className="flex-1 text-xs">
            <div className="max-w-md rounded-lg border border-dashed border-slate-300 p-4 text-center text-sm text-slate-400">
              Resolving template…
            </div>
          </div>
        )}
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
        onCompose={() => void (usingImport ? onBuildImported() : onBuild())}
      />

      <ImportTemplateDialog
        open={importOpen}
        onCancel={() => setImportOpen(false)}
        onImport={(t, reqs) => {
          setImportedTemplate(t, reqs)
          // A different template asks for different accounts, so anything typed
          // for the previous one is dropped rather than sent with this build.
          setCredentials([])
          setNewUsers([])
          setError(null)
          setImportOpen(false)
        }}
      />
    </div>
  )
}
