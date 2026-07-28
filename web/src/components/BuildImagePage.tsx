import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import { isActiveStatus, type BuildStatus, type HistoryItem } from '../api/types'
import { BuildView } from './BuildView'
import { HistorySidebar } from './HistorySidebar'

interface BuildImagePageProps {
  // The active (most recently started) build, owned by App. Null until the
  // first compose of the session.
  buildId: string | null
  onRetry: () => Promise<void>
  retrying: boolean
  // Why the last retry failed (e.g. a 409 while the previous build is still
  // tearing down), or null. Owned by App, which issues the start.
  retryError: string | null
  onStatusChange: (s: BuildStatus) => void
}

export function BuildImagePage({
  buildId,
  onRetry,
  retrying,
  retryError,
  onStatusChange,
}: BuildImagePageProps) {
  const [history, setHistory] = useState<HistoryItem[]>([])
  // Which build is shown on the right. Defaults to the active build; clicking a
  // history row overrides it.
  const [selectedId, setSelectedId] = useState<string | null>(buildId)

  const refresh = useCallback(() => {
    return api.listBuilds().then((builds) => {
      setHistory(builds)
      return builds
    }).catch(() => [] as HistoryItem[])
  }, [])

  // Load history on mount, and auto-select the newest build so the panel isn't
  // empty on first visit / after a refresh (history persists on the server).
  useEffect(() => {
    refresh().then((builds) => {
      setSelectedId((cur) => cur ?? (builds.length > 0 ? builds[0].id : null))
    })
  }, [refresh])

  // When a new compose starts (active buildId changes), select it and refresh.
  useEffect(() => {
    if (buildId) {
      setSelectedId(buildId)
      refresh()
    }
  }, [buildId, refresh])

  // Poll while any build is still in flight so the sidebar reflects live status.
  // 'cancelling' counts: the row's status changes again when teardown finishes.
  const anyActive = history.some((h) => isActiveStatus(h.status))
  useEffect(() => {
    if (!anyActive) return
    const t = setInterval(refresh, 3000)
    return () => clearInterval(t)
  }, [anyActive, refresh])

  // The active build's status drives the nav indicator; a past build's status
  // must not clobber it. Also refresh history whenever the active build reaches a
  // terminal state, so its sidebar row stops showing the in-flight status.
  const handleStatusChange = (s: BuildStatus) => {
    if (selectedId === buildId) onStatusChange(s)
    if (!isActiveStatus(s) && s !== 'idle') refresh()
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
              retryError={retryError}
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
