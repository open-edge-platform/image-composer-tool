import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from './api/client'
import { useStore } from './store'
import { BasicPage } from './components/BasicPage'
import { BuildImagePage } from './components/BuildImagePage'

type LoadState = 'loading' | 'ready' | 'error'
type View = 'basic' | 'advanced' | 'builds'
type BuildStatus = 'idle' | 'running' | 'success' | 'failed'

export default function App() {
  const setManifest = useStore((s) => s.setManifest)
  const [state, setState] = useState<LoadState>('loading')
  const [error, setError] = useState<string | null>(null)

  const [view, setView] = useState<View>('basic')
  const [buildId, setBuildId] = useState<string | null>(null)
  const [retrying, setRetrying] = useState(false)
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

  const onBuildStarted = (id: string) => {
    setBuildId(id)
    setBuildStatus('running')
    setView('builds')
  }

  const onBuildStatusChange = (s: BuildStatus) => setBuildStatus(s)

  const onRetry = useCallback(async () => {
    setRetrying(true)
    setBuildStatus('running')
    try {
      const accepted = await api.startBuild(selectionRef.current)
      setBuildId(accepted.buildId)
    } finally {
      setRetrying(false)
    }
  }, [])

  const tabs: { id: View; label: string; enabled: boolean }[] = [
    { id: 'basic', label: 'Basic', enabled: true },
    { id: 'advanced', label: 'Advanced', enabled: false },
    { id: 'builds', label: 'Build Image', enabled: true },
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
              buildInProgress={buildStatus === 'running'}
            />
          </div>
          <div hidden={view !== 'builds'}>
            <BuildImagePage
              buildId={buildId}
              onRetry={onRetry}
              retrying={retrying}
              onStatusChange={onBuildStatusChange}
            />
          </div>
        </>
      )}
    </div>
  )
}

function BuildIndicator({ status, onClick }: { status: BuildStatus; onClick: () => void }) {
  if (status === 'idle') return null
  const cfg = {
    running: { color: 'bg-yellow-400', pulse: true,  label: 'Build in progress' },
    success: { color: 'bg-green-400',  pulse: false, label: 'Build completed' },
    failed:  { color: 'bg-red-500',    pulse: false, label: 'Build failed' },
  }[status]
  return (
    <button
      onClick={onClick}
      title={cfg.label}
      className="flex items-center gap-2 rounded px-2 py-1 text-xs text-white/80 hover:bg-white/10"
    >
      <span className="relative flex h-3 w-3">
        {cfg.pulse && (
          <span className={`absolute inline-flex h-full w-full animate-ping rounded-full ${cfg.color} opacity-75`} />
        )}
        <span className={`relative inline-flex h-3 w-3 rounded-full ${cfg.color}`} />
      </span>
      {cfg.label}
    </button>
  )
}
