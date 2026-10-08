import { useRef } from 'react'
import { Input } from './Input'
import type { CredentialInput, CredentialRequirement } from '../api/types'

interface CredentialsStepProps {
  // What the resolved template reports. Entries already satisfied by the
  // template are not rendered — there is nothing to ask for.
  requirements: CredentialRequirement[]
  // Current entries, keyed by user. Held by the parent as transient component
  // state: a password must not reach the persisted store.
  credentials: CredentialInput[]
  onChange: (credentials: CredentialInput[]) => void
  disabled?: boolean
}

// unsatisfied returns the accounts that still need a login. An account the
// template already credentials is left alone: offering to override it would
// invite changing a curated image's login by accident.
function unsatisfied(requirements: CredentialRequirement[]): CredentialRequirement[] {
  return requirements.filter((r) => !r.satisfied)
}

// isSatisfiedBy reports whether an entry covers its account. Mirrors the
// backend rule (config.ValidateUserCredentials): either field is enough.
function isSatisfiedBy(cred: CredentialInput | undefined): boolean {
  if (!cred) return false
  return (cred.password ?? '').length > 0 || (cred.sshAuthorizedKey ?? '').length > 0
}

// credentialsComplete reports whether every account that needs a login has
// one. The caller gates its Build button on this so the user is stopped before
// a build rather than minutes into one.
export function credentialsComplete(
  requirements: CredentialRequirement[],
  credentials: CredentialInput[],
): boolean {
  return unsatisfied(requirements).every((r) =>
    isSatisfiedBy(credentials.find((c) => c.user === r.user)),
  )
}

// CredentialsStep collects a password and/or SSH public key for the accounts a
// template leaves without one.
//
// Rendered by both Basic and Advanced: the gap is a property of the template,
// not of which tab resolved it. Shown only when there is something to ask for,
// so curated templates that already carry a login are unaffected.
export function CredentialsStep({
  requirements,
  credentials,
  onChange,
  disabled,
}: CredentialsStepProps) {
  const needed = unsatisfied(requirements)
  if (needed.length === 0) return null

  const update = (user: string, patch: Partial<CredentialInput>) => {
    const existing = credentials.find((c) => c.user === user)
    const next: CredentialInput = { ...(existing ?? { user }), ...patch }
    // Drop an entry once both fields are empty, so an untouched account sends
    // nothing rather than an entry the backend would reject as empty.
    const rest = credentials.filter((c) => c.user !== user)
    onChange(isSatisfiedBy(next) ? [...rest, next] : rest)
  }

  return (
    <section className="mb-4 rounded-md border border-amber-300 bg-amber-50 p-4">
      <h3 className="mb-1 text-sm font-semibold text-[#00285a]">Credentials required</h3>
      <p className="mb-3 text-xs text-slate-600">
        This image creates an administrator account with no login of its own. Set a password or add
        an SSH public key so the account is not left with an empty password — either one is enough.
      </p>
      {needed.map((req) => (
        <CredentialFields
          key={req.user}
          req={req}
          cred={credentials.find((c) => c.user === req.user)}
          disabled={disabled}
          onUpdate={(patch) => update(req.user, patch)}
        />
      ))}
    </section>
  )
}

interface CredentialFieldsProps {
  req: CredentialRequirement
  cred: CredentialInput | undefined
  disabled?: boolean
  onUpdate: (patch: Partial<CredentialInput>) => void
}

function CredentialFields({ req, cred, disabled, onUpdate }: CredentialFieldsProps) {
  const fileInput = useRef<HTMLInputElement>(null)

  // Read the .pub file in the browser and keep only its text: the server has no
  // access to the user's filesystem, so unlike the CLI's --ssh-authorized-key
  // USER=FILE there is no path to hand over.
  const onPickFile = async (file: File | undefined) => {
    if (!file) return
    const text = await file.text()
    // A .pub file is one key, but tolerate the comment lines ssh-keygen and
    // hand-edited files carry; the backend accepts exactly one key line.
    const key = text
      .split('\n')
      .map((l) => l.trim())
      .find((l) => l.length > 0 && !l.startsWith('#'))
    if (key) onUpdate({ sshAuthorizedKey: key })
  }

  return (
    <div className="mb-2 rounded border border-amber-200 bg-white p-3">
      <p className="mb-2 text-sm font-semibold text-[#00285a]">
        {req.user}
        {req.sudo && <span className="ml-2 text-xs font-normal text-slate-500">administrator</span>}
      </p>
      <Input
        label="Password"
        type="password"
        value={cred?.password ?? ''}
        placeholder="Leave empty to use an SSH key instead"
        disabled={disabled}
        hint="Sent to the local build service over localhost, hashed before it is written, and never stored in build history."
        onChange={(v) => onUpdate({ password: v })}
      />
      <label
        htmlFor={`sshkey-${req.user}`}
        className="mb-1 block text-sm font-semibold text-[#00285a]"
      >
        SSH public key
      </label>
      <textarea
        id={`sshkey-${req.user}`}
        rows={3}
        className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-[#00285a] disabled:cursor-not-allowed disabled:bg-slate-100 focus:border-[#0071c5] focus:outline-none focus:ring-1 focus:ring-[#0071c5]"
        value={cred?.sshAuthorizedKey ?? ''}
        placeholder="ssh-ed25519 AAAA... user@host"
        disabled={disabled}
        onChange={(e) => onUpdate({ sshAuthorizedKey: e.target.value.trim() })}
      />
      <div className="mt-1 flex items-center gap-3">
        <button
          type="button"
          className="text-xs font-semibold text-[#0071c5] hover:underline disabled:cursor-not-allowed disabled:text-slate-400"
          disabled={disabled}
          onClick={() => fileInput.current?.click()}
        >
          Upload .pub file
        </button>
        <span className="text-xs text-slate-500">
          Paste the contents of a <code>.pub</code> file — never a private key.
        </span>
        <input
          ref={fileInput}
          type="file"
          accept=".pub,text/plain"
          className="hidden"
          onChange={(e) => {
            void onPickFile(e.target.files?.[0])
            // Clear so picking the same file again still fires a change event.
            e.target.value = ''
          }}
        />
      </div>
    </div>
  )
}
