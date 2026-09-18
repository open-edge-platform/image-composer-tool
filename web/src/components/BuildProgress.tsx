// Visual stepper showing the current image-composition phase. Phases are derived
// server-side from the build log (best-effort) and delivered via SSE "phase"
// events; see internal/api/phases.go.

interface BuildProgressProps {
  // Current phase id (one of PHASES below).
  phase: string
  // Install-phase counter, when available (0/0 otherwise).
  install: { done: number; total: number }
  // Whether the build failed — the active step is shown in red.
  failed?: boolean
}

const PHASES: { id: string; label: string }[] = [
  { id: 'preparing', label: 'Preparing' },
  { id: 'packages', label: 'Resolving & downloading packages' },
  { id: 'installing', label: 'Installing packages' },
  { id: 'generating', label: 'Generating image' },
  { id: 'done', label: 'Done' },
]

export function BuildProgress({ phase, install, failed }: BuildProgressProps) {
  const current = Math.max(0, PHASES.findIndex((p) => p.id === phase))

  return (
    <div className="mb-4 rounded-lg border border-slate-200 bg-white p-4">
      <ol className="flex flex-wrap items-center gap-y-3">
        {PHASES.map((p, i) => {
          const done = i < current
          const active = i === current && phase !== 'done'
          const complete = phase === 'done' && i === PHASES.length - 1
          const isFailed = failed && i === current

          const circle = isFailed
            ? 'bg-red-500 text-white'
            : done || complete
              ? 'bg-green-500 text-white'
              : active
                ? 'bg-[#0071c5] text-white'
                : 'bg-slate-200 text-slate-500'

          return (
            <li key={p.id} className="flex items-center">
              <div className="flex items-center gap-2">
                <span
                  className={
                    'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold ' +
                    circle +
                    (active ? ' animate-pulse' : '')
                  }
                >
                  {isFailed ? '✕' : done || complete ? '✓' : i + 1}
                </span>
                <span
                  className={
                    'text-xs ' +
                    (active
                      ? 'font-semibold text-[#00285a]'
                      : done || complete
                        ? 'text-slate-600'
                        : 'text-slate-400')
                  }
                >
                  {p.label}
                  {/* Live counter during the install phase */}
                  {active && p.id === 'installing' && install.total > 0 && (
                    <span className="ml-1 font-normal text-slate-500">
                      ({install.done}/{install.total})
                    </span>
                  )}
                </span>
              </div>
              {i < PHASES.length - 1 && (
                <span
                  className={
                    'mx-2 hidden h-px w-6 sm:inline-block ' +
                    (done ? 'bg-green-400' : 'bg-slate-200')
                  }
                />
              )}
            </li>
          )
        })}
      </ol>
    </div>
  )
}
