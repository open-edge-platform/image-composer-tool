import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from './api/client'
import { isActiveStatus, type BuildStatus, type ComposeRequest } from './api/types'
import { useStore } from './store'
import { BasicPage } from './components/BasicPage'
import { BuildImagePage } from './components/BuildImagePage'

type LoadState = 'loading' | 'ready' | 'error'
type View = 'basic' | 'advanced' | 'builds'

export default function App() {
  const setManifest = useStore((s) => s.setManifest)
  const [state, setState] = useState<LoadState>('loading')
  const [error, setError] = useState<string | null>(null)

  const [view, setView] = useState<View>('basic')
  const [buildId, setBuildId] = useState<string | null>(null)
  const [retrying, setRetrying] = useState(false)
  const [retryError, setRetryError] = useState<string | null>(null)
  const [buildStatus, setBuildStatus] = useState<BuildStatus>('idle')

  const selection = useStore((s) => s.selection)
  const selectionRef = useRef(selection)
  selectionRef.current = selection

  const load = useCallback(() => {
    setState('loading')
    setError(null)
    api
      .getManifest()
      .then((m) => {
        setManifest(m)
        setState('ready')
      })
      .catch((e) => {
        setError((e as Error).message)
        setState('error')
      })
  }, [setManifest])

  useEffect(load, [load])

  // On load, adopt any in-flight build from the server so the UI reflects it
  // after a refresh: disables the Compose button and shows the nav indicator
  // (otherwise buildStatus resets to 'idle' and a duplicate build could start).
  // A build mid-cancel still holds the server's single-build slot, so adopt those
  // too — otherwise Compose looks available and the start would 409.
  useEffect(() => {
    if (state !== 'ready') return
    api
      .listBuilds()
      .then(({ builds }) => {
        const active = builds.find((b) => isActiveStatus(b.status))
        if (active) {
          setBuildId(active.id)
          setBuildStatus(active.status as BuildStatus)
        }
      })
      .catch(() => {})
  }, [state])

  const onBuildStarted = (id: string) => {
    setBuildId(id)
    setBuildStatus('running')
    setView('builds')
    // Clear any earlier retry failure: it described a start that never happened,
    // and a fresh build has now started. Leaving it set made the Compose page
    // show a "could not start" banner above a build that was visibly running.
    setRetryError(null)
  }

  const onBuildStatusChange = (s: BuildStatus) => setBuildStatus(s)

  // Retry a build as a fresh compose.
  //
  // `req` is the retried build's own recorded selection, read back from the
  // server. Prefer it over the store: the store holds whatever the Basic tab
  // currently has selected, which after a page refresh is empty (it isn't
  // persisted) — the server then matches no template and rejects the start with
  // "no template maps to the selected combination". It is also simply the wrong
  // input when retrying an older history entry, which should rebuild *that*
  // configuration rather than the one on screen.
  //
  // The start can legitimately fail — most importantly with 409 while the
  // previous build's teardown still holds the server's single-build slot — so the
  // optimistic 'running' must be rolled back and the reason shown, or the nav
  // indicator would spin forever on a build that never started.
  const onRetry = useCallback(async (req?: ComposeRequest) => {
    const compose = req ?? selectionRef.current
    // Neither source knows what to build. Say so instead of posting an empty
    // selection and surfacing the server's "no template maps..." rejection,
    // which describes a bad combination rather than a missing one.
    if (!compose.vertical || !compose.platform || !compose.os || !compose.imageType) {
      setRetryError(
        'this compose has no recorded configuration to retry — choose one on the Basic tab and compose from there',
      )
      return
    }
    setRetrying(true)
    setRetryError(null)
    setBuildStatus('running')
    try {
      const accepted = await api.startBuild(compose)
      setBuildId(accepted.buildId)
    } catch (e) {
      setRetryError((e as Error).message)
      setBuildStatus('failed')
    } finally {
      setRetrying(false)
    }
  }, [])

  const tabs: { id: View; label: string; enabled: boolean }[] = [
    { id: 'basic', label: 'Basic', enabled: true },
    { id: 'advanced', label: 'Advanced', enabled: false },
    { id: 'builds', label: 'Compose Image', enabled: true },
  ]

  return (
    <div className="min-h-full">
      <nav className="flex items-center gap-6 bg-[#00285a] px-6 py-3 text-white">
        <img src="/intel-logo.svg" alt="Intel" className="h-8 w-auto" />
        <span className="font-bold">Image Composer Tool</span>
        <div className="flex gap-1">
          {tabs.map((t) => (
            <button
              key={t.id}
              disabled={!t.enabled}
              onClick={() => t.enabled && setView(t.id)}
              className={
                'rounded px-3 py-1 text-sm ' +
                (view === t.id
                  ? 'bg-[#0071c5] text-white'
                  : t.enabled
                    ? 'text-slate-200 hover:bg-white/10'
                    : 'cursor-not-allowed text-slate-500')
              }
              title={t.enabled ? undefined : 'Coming soon'}
            >
              {t.label}
            </button>
          ))}
        </div>
        {/* Build status indicator — right side of nav */}
        <div className="ml-auto">
          <BuildIndicator status={buildStatus} onClick={() => setView('builds')} />
        </div>
      </nav>

      {state === 'loading' && (
        <div className="m-6 text-sm text-slate-500">Loading configuration…</div>
      )}

      {state === 'error' && (
        <div className="m-6 rounded bg-red-50 p-4 text-sm text-red-700">
          <p>Failed to load configuration: {error}</p>
          <p className="mt-1 text-red-600">Is the API server running on :8080?</p>
          <button
            className="mt-3 rounded border border-red-300 px-3 py-1 text-xs font-medium hover:bg-red-100"
            onClick={load}
          >
            Retry
          </button>
        </div>
      )}

      {state === 'ready' && (
        <>
          <div hidden={view !== 'basic'}>
            <BasicPage
              onBuildStarted={onBuildStarted}
              buildInProgress={isActiveStatus(buildStatus)}
            />
          </div>
          <div hidden={view !== 'builds'}>
            <BuildImagePage
              buildId={buildId}
              onRetry={onRetry}
              retrying={retrying}
              retryError={retryError}
              onStatusChange={onBuildStatusChange}
            />
          </div>
        </>
      )}
    </div>
  )
}

// One entry per non-idle build state. Typed as an exhaustive Record so adding a
// state to BuildStatus is a compile error here rather than a blank indicator.
const indicatorStyles: Record<
  Exclude<BuildStatus, 'idle'>,
  { color: string; pulse: boolean; label: string }
> = {
  'not-started': { color: 'bg-yellow-400', pulse: true, label: 'Compose starting' },
  running: { color: 'bg-yellow-400', pulse: true, label: 'Compose in progress' },
  cancelling: { color: 'bg-amber-500', pulse: true, label: 'Cancelling compose' },
  cancelled: { color: 'bg-slate-400', pulse: false, label: 'Compose cancelled' },
  success: { color: 'bg-green-400', pulse: false, label: 'Compose completed' },
  failed: { color: 'bg-red-500', pulse: false, label: 'Compose failed' },
}

function BuildIndicator({ status, onClick }: { status: BuildStatus; onClick: () => void }) {
  if (status === 'idle') return null
  const cfg = indicatorStyles[status]
  return (
    <button
      onClick={onClick}
      title={cfg.label}
      aria-label={cfg.label}
      className="flex items-center rounded p-1.5 hover:bg-white/10"
    >
      <span className="relative flex h-3 w-3">
        {cfg.pulse && (
          <span className={`absolute inline-flex h-full w-full animate-ping rounded-full ${cfg.color} opacity-75`} />
        )}
        <span className={`relative inline-flex h-3 w-3 rounded-full ${cfg.color}`} />
      </span>
    </button>
  )
}
