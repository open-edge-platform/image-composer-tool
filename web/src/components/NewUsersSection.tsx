import { useRef } from 'react'
import type { CredentialRequirement, NewUserInput } from '../api/types'

// userNameRe mirrors the template schema's pattern for a user (and group) name,
// so a name the backend would reject is caught while it is being typed rather
// than when the build is started.
const userNameRe = /^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,31}$/

// newUserError returns why one entry cannot be sent yet, or null when it can.
// `taken` is every name already spoken for — the template's own accounts plus
// the other new entries — since the backend rejects a collision outright.
export function newUserError(user: NewUserInput, taken: string[]): string | null {
  if (!user.user) return 'Enter a user name.'
  if (!userNameRe.test(user.user)) {
    return 'Use letters, digits, dot, dash or underscore (max 32 characters).'
  }
  if (taken.includes(user.user)) return 'That user name is already taken.'
  if (!(user.password ?? '') && !(user.sshAuthorizedKey ?? '')) {
    return 'Set a password or add an SSH public key.'
  }
  const bad = (user.groups ?? []).find((g) => !userNameRe.test(g))
  if (bad) return `Group "${bad}" is not a valid group name.`
  if (user.startupScript && !user.startupScript.startsWith('/')) {
    return 'Startup script must be an absolute path.'
  }
  return null
}

// newUsersValid reports whether every added account can be sent. An empty list
// is valid — adding a user is optional.
export function newUsersValid(users: NewUserInput[], requirements: CredentialRequirement[]): boolean {
  return newUserIssues(users, requirements).length === 0
}

// newUserIssues describes what is still wrong with each added account, named
// so a caller can say why a build cannot start instead of only that it cannot.
export function newUserIssues(
  users: NewUserInput[],
  requirements: CredentialRequirement[],
): string[] {
  return users
    .map((u, i) => {
      const err = newUserError(u, takenNames(users, i, requirements))
      return err ? `New user ${u.user || `#${i + 1}`}: ${err}` : null
    })
    .filter((m): m is string => m !== null)
}

// takenNames lists the names the entry at `index` may not use: the template's
// own accounts and every other added entry.
function takenNames(
  users: NewUserInput[],
  index: number,
  requirements: CredentialRequirement[],
): string[] {
  return [
    ...requirements.map((r) => r.user),
    ...users.filter((_, i) => i !== index).map((u) => u.user),
  ].filter((n) => n.length > 0)
}

interface NewUsersSectionProps {
  // The template's existing accounts — the names a new one may not reuse.
  requirements: CredentialRequirement[]
  users: NewUserInput[]
  onChange: (users: NewUserInput[]) => void
  disabled?: boolean
}

// NewUsersSection lets the user add accounts the template does not declare,
// with the same options a template may set for one. Rendered under the
// credentials fields, since both end up in the same systemConfig.users block.
export function NewUsersSection({
  requirements,
  users,
  onChange,
  disabled,
}: NewUsersSectionProps) {
  const add = () => onChange([...users, { user: '', sudo: false }])
  const update = (index: number, patch: Partial<NewUserInput>) =>
    onChange(users.map((u, i) => (i === index ? { ...u, ...patch } : u)))
  const remove = (index: number) => onChange(users.filter((_, i) => i !== index))

  return (
    <section className="mb-4 rounded-md border border-slate-200 bg-white p-4">
      <h3 className="mb-1 text-sm font-semibold text-[#00285a]">Add a user</h3>
      <p className="mb-3 text-xs text-slate-600">
        Create accounts the template does not already define. Fields marked{' '}
        <span className="font-semibold text-red-600">*</span> are required; a password and an SSH
        public key count as one requirement, so either one on its own is enough.
      </p>
      {users.map((user, index) => (
        <NewUserFields
          key={index}
          index={index}
          user={user}
          error={newUserError(user, takenNames(users, index, requirements))}
          disabled={disabled}
          onUpdate={(patch) => update(index, patch)}
          onRemove={() => remove(index)}
        />
      ))}
      <button
        type="button"
        className="rounded-md border border-dashed border-[#0071c5] px-3 py-2 text-sm font-semibold text-[#0071c5] hover:bg-blue-50 disabled:cursor-not-allowed disabled:border-slate-300 disabled:text-slate-400"
        disabled={disabled}
        onClick={add}
      >
        + Add user
      </button>
    </section>
  )
}

interface NewUserFieldsProps {
  index: number
  user: NewUserInput
  error: string | null
  disabled?: boolean
  onUpdate: (patch: Partial<NewUserInput>) => void
  onRemove: () => void
}

const fieldClass =
  'w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-[#00285a] ' +
  'disabled:cursor-not-allowed disabled:bg-slate-100 focus:border-[#0071c5] focus:outline-none ' +
  'focus:ring-1 focus:ring-[#0071c5]'

function NewUserFields({ index, user, error, disabled, onUpdate, onRemove }: NewUserFieldsProps) {
  const fileInput = useRef<HTMLInputElement>(null)
  // Ids are index-based: several of these forms are on screen at once, so a
  // label-derived id would collide between them.
  const id = (field: string) => `new-user-${index}-${field}`

  // Same browser-side read as the credentials fields: the server has no access
  // to the user's filesystem, so only the key text can be handed over.
  const onPickFile = async (file: File | undefined) => {
    if (!file) return
    const text = await file.text()
    const key = text
      .split('\n')
      .map((l) => l.trim())
      .find((l) => l.length > 0 && !l.startsWith('#'))
    if (key) onUpdate({ sshAuthorizedKey: key })
  }

  return (
    <div className="mb-3 rounded border border-slate-200 bg-slate-50 p-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <label htmlFor={id('name')} className="text-sm font-semibold text-[#00285a]">
          User name<span className="text-red-600"> *</span>
        </label>
        <button
          type="button"
          className="text-xs font-semibold text-slate-600 hover:text-red-700 hover:underline"
          disabled={disabled}
          onClick={onRemove}
        >
          Remove
        </button>
      </div>
      <input
        id={id('name')}
        className={fieldClass}
        value={user.user}
        placeholder="e.g. operator"
        disabled={disabled}
        onChange={(e) => onUpdate({ user: e.target.value.trim() })}
      />

      {/* One of the two satisfies the account, so the mark sits on the pair:
          an asterisk on both would read as "fill in both". */}
      <p className="mb-2 mt-3 text-sm font-semibold text-[#00285a]">
        Set one of the following<span className="text-red-600"> *</span>
      </p>
      <label htmlFor={id('password')} className="mb-1 block text-sm font-semibold text-[#00285a]">
        Password
      </label>
      <input
        id={id('password')}
        type="password"
        className={fieldClass}
        value={user.password ?? ''}
        placeholder="Leave empty to use an SSH key instead"
        disabled={disabled}
        onChange={(e) => onUpdate({ password: e.target.value })}
      />

      <label htmlFor={id('sshkey')} className="mb-1 mt-3 block text-sm font-semibold text-[#00285a]">
        SSH public key
      </label>
      <textarea
        id={id('sshkey')}
        rows={3}
        className={fieldClass + ' font-mono text-xs'}
        value={user.sshAuthorizedKey ?? ''}
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
            e.target.value = ''
          }}
        />
      </div>

      <label className="mt-3 flex items-center gap-2 text-sm text-[#00285a]">
        <input
          type="checkbox"
          className="h-4 w-4"
          checked={user.sudo ?? false}
          disabled={disabled}
          onChange={(e) => onUpdate({ sudo: e.target.checked })}
        />
        Administrator (sudo)
      </label>

      <label htmlFor={id('groups')} className="mb-1 mt-3 block text-sm font-semibold text-[#00285a]">
        Groups
      </label>
      <input
        id={id('groups')}
        className={fieldClass}
        // Stored as a list but edited as text, so a half-typed "docker, " does
        // not become an empty group while it is being typed.
        value={(user.groups ?? []).join(', ')}
        placeholder="e.g. docker, dialout"
        disabled={disabled}
        onChange={(e) =>
          onUpdate({
            groups: e.target.value
              .split(',')
              .map((g) => g.trim())
              .filter((g) => g.length > 0),
          })
        }
      />

      <div className="mt-3 grid grid-cols-2 gap-3">
        <div>
          <label htmlFor={id('shell')} className="mb-1 block text-sm font-semibold text-[#00285a]">
            Login shell
          </label>
          <input
            id={id('shell')}
            className={fieldClass}
            value={user.shell ?? ''}
            placeholder="/bin/bash"
            disabled={disabled}
            onChange={(e) => onUpdate({ shell: e.target.value.trim() })}
          />
        </div>
        <div>
          <label htmlFor={id('home')} className="mb-1 block text-sm font-semibold text-[#00285a]">
            Home directory
          </label>
          <input
            id={id('home')}
            className={fieldClass}
            value={user.home ?? ''}
            placeholder="/home/operator"
            disabled={disabled}
            onChange={(e) => onUpdate({ home: e.target.value.trim() })}
          />
        </div>
        <div>
          <label htmlFor={id('startup')} className="mb-1 block text-sm font-semibold text-[#00285a]">
            Startup script
          </label>
          <input
            id={id('startup')}
            className={fieldClass}
            value={user.startupScript ?? ''}
            placeholder="/usr/local/bin/kiosk.sh"
            disabled={disabled}
            onChange={(e) => onUpdate({ startupScript: e.target.value.trim() })}
          />
        </div>
        <div>
          <label htmlFor={id('maxage')} className="mb-1 block text-sm font-semibold text-[#00285a]">
            Password max age (days)
          </label>
          <input
            id={id('maxage')}
            type="number"
            min={0}
            className={fieldClass}
            value={user.passwordMaxAge ?? ''}
            placeholder="0 = no limit"
            disabled={disabled}
            onChange={(e) =>
              onUpdate({
                passwordMaxAge: e.target.value === '' ? undefined : Number(e.target.value),
              })
            }
          />
        </div>
      </div>

      {error && <p className="mt-2 text-xs font-semibold text-red-700">{error}</p>}
    </div>
  )
}
