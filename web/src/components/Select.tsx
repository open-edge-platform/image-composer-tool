import type { DropdownOption } from '../store'

interface SelectProps {
  label: string
  value: string
  options: DropdownOption[]
  placeholder: string
  disabled?: boolean
  onChange: (value: string) => void
}

export function Select({
  label,
  value,
  options,
  placeholder,
  disabled,
  onChange,
}: SelectProps) {
  const id = `select-${label.toLowerCase().replace(/\s+/g, '-')}`
  return (
    <div className="mb-4">
      <label htmlFor={id} className="mb-1 block text-sm font-semibold text-[#00285a]">
        {label}
      </label>
      <select
        id={id}
        className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-[#00285a] disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-400 focus:border-[#0071c5] focus:outline-none focus:ring-1 focus:ring-[#0071c5]"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="" disabled>
          {placeholder}
        </option>
        {options.map((o) => (
          <option key={o.id} value={o.id}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  )
}
