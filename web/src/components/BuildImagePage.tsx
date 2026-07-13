import { BuildView } from './BuildView'

interface BuildImagePageProps {
  buildId: string | null
  onRetry: () => Promise<void>
  retrying: boolean
}

// The "Build Image" panel. It shows the live build view once a build has been
// started from the Basic (or Advanced) tab, and an empty prompt otherwise.
export function BuildImagePage({ buildId, onRetry, retrying }: BuildImagePageProps) {
  return (
    <div className="mx-auto max-w-6xl p-6">
      <h1 className="mb-4 text-2xl font-bold text-[#00285a]">Build Image</h1>
      {buildId ? (
        <BuildView buildId={buildId} onRetry={onRetry} retrying={retrying} />
      ) : (
        <div className="rounded-md border border-dashed border-slate-300 p-8 text-center text-sm text-slate-500">
          No build started yet. Choose a configuration on the Basic tab and click
          <span className="font-semibold"> Build Image</span>.
        </div>
      )}
    </div>
  )
}
