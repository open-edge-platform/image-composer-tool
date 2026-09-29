interface InputProps {
  label: string
  value: string
  placeholder?: string
  disabled?: boolean
  onChange: (value: string) => void
}

export function Input({ label, value, placeholder, disabled, onChange }: InputProps) {
  const id = `input-${label.toLowerCase().replace(/\s+/g, '-')}`
  return (
    <div className="mb-4">
      <label htmlFor={id} className="mb-1 block text-sm font-semibold text-[#00285a]">
        {label}
      </label>
      <input
        id={id}
        type="text"
        className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-[#00285a] disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-400 focus:border-[#0071c5] focus:outline-none focus:ring-1 focus:ring-[#0071c5]"
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  )
}
