import { useEffect, useRef } from 'react'
import type { CredentialInput, CredentialRequirement } from '../api/types'
import { CredentialsStep } from './CredentialsStep'

interface CredentialsDialogProps {
  open: boolean
  requirements: CredentialRequirement[]
  credentials: CredentialInput[]
  onChange: (credentials: CredentialInput[]) => void
  credentialsReady: boolean
  busy: boolean
  buildInProgress: boolean
  error: string | null
  onCancel: () => void
  onCompose: () => void
}

export function CredentialsDialog({
  open,
  requirements,
  credentials,
  onChange,
  credentialsReady,
  busy,
  buildInProgress,
  error,
  onCancel,
  onCompose,
}: CredentialsDialogProps) {
  const dialogRef = useRef<HTMLElement>(null)
  const onCancelRef = useRef(onCancel)
  onCancelRef.current = onCancel

  useEffect(() => {
    if (!open) return
    const trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const focusableSelector =
      'a[href], button:not([disabled]), input:not([disabled]):not([type="file"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
    const focusableElements = () =>
      Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])

    focusableElements()[0]?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onCancelRef.current()
        return
      }
      if (event.key !== 'Tab') return

      const focusable = focusableElements()
      if (focusable.length === 0) {
        event.preventDefault()
        dialogRef.current?.focus()
        return
      }

      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement
      if (event.shiftKey && (active === first || !dialogRef.current?.contains(active))) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && (active === last || !dialogRef.current?.contains(active))) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      trigger?.focus()
    }
  }, [open])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/45 p-4"
      onClick={(event) => {
        if (event.target === event.currentTarget) onCancel()
      }}
    >
      <section
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="credentials-dialog-title"
        tabIndex={-1}
        className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-lg bg-white p-6 shadow-xl"
      >
        <div className="mb-4 flex items-start justify-between gap-4">
          <div>
            <h2 id="credentials-dialog-title" className="text-lg font-semibold text-[#00285a]">
              {credentialsReady ? 'Credentials' : 'Credentials required'}
            </h2>
            <p className="mt-1 text-sm text-slate-600">
              {credentialsReady
                ? 'Review or update credentials for this image.'
                : 'Set a password or add an SSH public key before composing this image.'}
            </p>
          </div>
          <button
            type="button"
            aria-label="Close credentials dialog"
            className="rounded p-1 text-xl leading-none text-slate-500 hover:bg-slate-100 hover:text-slate-800"
            onClick={onCancel}
          >
            ×
          </button>
        </div>
        <CredentialsStep
          requirements={requirements}
          credentials={credentials}
          onChange={onChange}
          disabled={busy || buildInProgress}
          showRequiredHeading={false}
        />
        {error && (
          <div role="alert" className="mb-3 rounded bg-red-50 p-3 text-sm text-red-700">
            {error}
          </div>
        )}
        <div className="mt-5 flex justify-end gap-3">
          <button
            type="button"
            className="rounded-md border border-slate-300 bg-white px-4 py-2 font-semibold text-[#00285a] hover:bg-slate-50"
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            type="button"
            className="rounded-md bg-[#0071c5] px-4 py-2 font-semibold text-white hover:bg-[#00285a] disabled:cursor-not-allowed disabled:opacity-50"
            disabled={!credentialsReady || busy || buildInProgress}
            onClick={onCompose}
          >
            {busy ? 'Starting…' : 'Compose Image'}
          </button>
        </div>
      </section>
    </div>
  )
}