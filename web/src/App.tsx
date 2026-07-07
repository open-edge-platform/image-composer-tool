import { useEffect, useState } from 'react'
import { api } from './api/client'
import { useStore } from './store'
import { BasicPage } from './components/BasicPage'

export default function App() {
  const setManifest = useStore((s) => s.setManifest)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .getManifest()
      .then(setManifest)
      .catch((e) => setError((e as Error).message))
  }, [setManifest])

  return (
    <div className="min-h-full">
      <nav className="flex items-center gap-6 bg-[#00285a] px-6 py-3 text-white">
        <span className="font-bold">Image Composer Tool</span>
        <span className="rounded bg-[#0071c5] px-3 py-1 text-sm">Basic</span>
      </nav>

      {error ? (
        <div className="m-6 rounded bg-red-50 p-4 text-sm text-red-700">
          Failed to load manifest: {error}. Is the API server running on :8080?
        </div>
      ) : (
        <BasicPage />
      )}
    </div>
  )
}
