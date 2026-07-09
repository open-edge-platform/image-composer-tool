import { useCallback, useEffect, useState } from 'react'
import { api } from './api/client'
import { useStore } from './store'
import { BasicPage } from './components/BasicPage'

type LoadState = 'loading' | 'ready' | 'error'

export default function App() {
  const setManifest = useStore((s) => s.setManifest)
  const [state, setState] = useState<LoadState>('loading')
  const [error, setError] = useState<string | null>(null)

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

  return (
    <div className="min-h-full">
      <nav className="flex items-center gap-6 bg-[#00285a] px-6 py-3 text-white">
        <span className="font-bold">Image Composer Tool</span>
        <span className="rounded bg-[#0071c5] px-3 py-1 text-sm">Basic</span>
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

      {state === 'ready' && <BasicPage />}
    </div>
  )
}
