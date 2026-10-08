interface InputProps {
  label: string
  value: string
  placeholder?: string
  disabled?: boolean
  // 'password' masks the value. Used for an account password typed into the
  // Credentials section, which should not be readable over the user's shoulder.
  type?: 'text' | 'password'
  // Hint read out with the field, for a value whose rules aren't obvious from
  // the label alone.
  hint?: string
  onChange: (value: string) => void
}

export function Input({ label, value, placeholder, disabled, type = 'text', hint, onChange }: InputProps) {
  const id = `input-${label.toLowerCase().replace(/\s+/g, '-')}`
  const hintId = hint ? `${id}-hint` : undefined
  return (
    <div className="mb-4">
      <label htmlFor={id} className="mb-1 block text-sm font-semibold text-[#00285a]">
        {label}
      </label>
      <input
        id={id}
        type={type}
        aria-describedby={hintId}
        className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-[#00285a] disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-400 focus:border-[#0071c5] focus:outline-none focus:ring-1 focus:ring-[#0071c5]"
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
      />
      {hint && (
        <p id={hintId} className="mt-1 text-xs text-slate-500">
          {hint}
        </p>
      )}
    </div>
  )
}
