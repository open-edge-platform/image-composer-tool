import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { HistoryItem } from '../api/types'
import { BuildView } from './BuildView'
import { HistorySidebar } from './HistorySidebar'

type BuildStatus = 'idle' | 'running' | 'success' | 'failed'

interface BuildImagePageProps {
  // The active (most recently started) build, owned by App. Null until the
  // first compose of the session.
  buildId: string | null
  onRetry: () => Promise<void>
  retrying: boolean
  onStatusChange: (s: BuildStatus) => void
}

export function BuildImagePage({ buildId, onRetry, retrying, onStatusChange }: BuildImagePageProps) {
  const [history, setHistory] = useState<HistoryItem[]>([])
  // Which build is shown on the right. Defaults to the active build; clicking a
  // history row overrides it.
  const [selectedId, setSelectedId] = useState<string | null>(buildId)

  const refresh = useCallback(() => {
    api.listBuilds().then(setHistory).catch(() => {})
  }, [])

  // When a new compose starts (active buildId changes), select it and refresh.
  useEffect(() => {
    if (buildId) setSelectedId(buildId)
    refresh()
  }, [buildId, refresh])

  // Poll while any build is still running so the sidebar reflects live status.
  const anyRunning = history.some((h) => h.status === 'running')
  const runningRef = useRef(anyRunning)
  runningRef.current = anyRunning
  useEffect(() => {
    if (!anyRunning) return
    const t = setInterval(refresh, 3000)
    return () => clearInterval(t)
  }, [anyRunning, refresh])

  // The active build's terminal status drives the nav indicator; a past build's
  // status must not clobber it. Also refresh history when the active build ends.
  const handleStatusChange = (s: BuildStatus) => {
    if (selectedId === buildId) onStatusChange(s)
    if (s === 'success' || s === 'failed') refresh()
  }

  return (
    <div className="mx-auto max-w-screen-2xl px-10 py-8">
      <h1 className="mb-4 text-2xl font-bold text-[#00285a]">Compose Image</h1>
      {selectedId ? (
        <div className="flex gap-4">
          <HistorySidebar items={history} selectedId={selectedId} onSelect={setSelectedId} />
          <div className="min-w-0 flex-1">
            <BuildView
              key={selectedId}
              buildId={selectedId}
              onRetry={onRetry}
              retrying={retrying}
              onStatusChange={handleStatusChange}
              isActive={selectedId === buildId}
            />
          </div>
        </div>
      ) : (
        <div className="rounded-md border border-dashed border-slate-300 p-8 text-center text-sm text-slate-500">
          No compose started yet. Choose a configuration on the Basic tab and click
          <span className="font-semibold"> Compose Image</span>.
        </div>
      )}
    </div>
  )
}
