import { useMemo, useState } from 'react'
import { useStore, cascadingOptions } from '../store'
import { api } from '../api/client'
import type { ComposeResponse } from '../api/types'
import { Select } from './Select'
import { BuildView } from './BuildView'

export function BasicPage() {
  const manifest = useStore((s) => s.manifest)
  const selection = useStore((s) => s.selection)
  const setField = useStore((s) => s.setField)

  const [review, setReview] = useState<ComposeResponse | null>(null)
  const [reviewOpen, setReviewOpen] = useState(false)
  const [buildId, setBuildId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const opts = useMemo(
    () => (manifest ? cascadingOptions(manifest, selection) : null),
    [manifest, selection],
  )

  if (!manifest || !opts) return <div className="p-8">Loading…</div>

  const complete = !!opts.matched

  const onToggleReview = async () => {
    if (reviewOpen) {
      setReviewOpen(false)
      return
    }
    if (!complete) return
    try {
      setBusy(true)
      setError(null)
      setReview(await api.compose(selection))
      setReviewOpen(true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const onBuild = async () => {
    if (!complete) return
    try {
      setBusy(true)
      setError(null)
      const accepted = await api.startBuild(selection)
      setBuildId(accepted.buildId)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  // Changing any field invalidates a prior review.
  const setSel = (k: Parameters<typeof setField>[0], v: string) => {
    setField(k, v)
    setReviewOpen(false)
    setReview(null)
  }

  return (
    <div className="mx-auto max-w-2xl p-6">
      <h1 className="mb-1 text-2xl font-bold text-[#00285a]">Choose Image Configuration</h1>
      <p className="mb-5 text-sm text-slate-500">
        Select a targeted vertical, SKU, and platform. Pre-configured defaults are applied
        based on your selection.
      </p>

      <div className="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
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
        <Select
          label="Image Type"
          placeholder="-- Select Image Type --"
          value={selection.imageType}
          options={opts.imageTypes}
          disabled={!selection.os}
          onChange={(v) => setSel('imageType', v)}
        />

        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={reviewOpen}
            disabled={!complete}
            onChange={onToggleReview}
          />
          Review Image Configuration
        </label>

        {reviewOpen && review && (
          <div className="mt-3 rounded-md bg-slate-50 p-4">
            <table className="w-full text-sm">
              <tbody>
                {Object.entries({
                  Image: review.summary.imageName,
                  Vertical: review.summary.vertical,
                  SKU: review.summary.sku || '—',
                  Platform: review.summary.platform,
                  OS: review.summary.os,
                  'Image Type': review.summary.imageType.toUpperCase(),
                  Disk: review.summary.diskSize || '—',
                  Packages: `${review.summary.packageCount} packages`,
                }).map(([k, v]) => (
                  <tr key={k}>
                    <td className="py-1 pr-4 font-semibold">{k}</td>
                    <td className="py-1">{v}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {error && <div className="mt-3 rounded bg-red-50 p-3 text-sm text-red-700">{error}</div>}

      <div className="mt-6">
        <button
          className="rounded-md bg-[#0071c5] px-5 py-2.5 font-semibold text-white hover:bg-[#00285a] disabled:cursor-not-allowed disabled:opacity-50"
          disabled={!complete || busy}
          onClick={onBuild}
        >
          {busy ? 'Starting…' : 'Build Image'}
        </button>
        {!complete && (
          <span className="ml-3 text-sm text-slate-500">
            Complete all selections to build.
          </span>
        )}
      </div>

      {buildId && <BuildView buildId={buildId} />}
    </div>
  )
}
