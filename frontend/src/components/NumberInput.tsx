interface Props {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  min?: number
  max?: number
  step?: number
}

function decimalPlaces(step: number): number {
  const s = step.toString()
  const dot = s.indexOf('.')
  return dot === -1 ? 0 : s.length - dot - 1
}

export default function NumberInput({ value, onChange, placeholder = '0', min, max, step = 1 }: Props) {
  const dp = decimalPlaces(step)

  const adjust = (dir: 1 | -1) => {
    const n = parseFloat(value) || 0
    const next = parseFloat((n + dir * step).toFixed(dp))
    if (min !== undefined && next < min) return
    if (max !== undefined && next > max) return
    onChange(next.toFixed(dp))
  }

  return (
    <div className="relative flex items-center bg-bg border border-border-dim rounded-xl focus-within:border-indigo-500/50 transition-colors">
      <input
        type="number"
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full px-4 py-2.5 pr-8 bg-transparent text-slate-100 text-sm focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
      />
      <div className="absolute right-2.5 flex flex-col gap-px">
        <button
          type="button"
          tabIndex={-1}
          onClick={() => adjust(1)}
          className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
        >
          <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M1 4L4 1L7 4"/>
          </svg>
        </button>
        <button
          type="button"
          tabIndex={-1}
          onClick={() => adjust(-1)}
          className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
        >
          <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M1 1L4 4L7 1"/>
          </svg>
        </button>
      </div>
    </div>
  )
}
