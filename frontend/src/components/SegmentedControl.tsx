export interface SegmentOption<T> {
  label: string
  value: T
  tooltip?: string
  tooltipAlign?: 'center' | 'right'
}

interface Props<T extends string | number> {
  label: string
  options: readonly SegmentOption<T>[]
  value: T
  onChange: (value: T) => void
}

export default function SegmentedControl<T extends string | number>({
  label, options, value, onChange,
}: Props<T>) {
  return (
    <div className="flex flex-col items-center gap-2">
      <span className="text-[9px] font-black text-slate-600 uppercase tracking-[0.2em]">{label}</span>
      <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-2xl p-1.5 border border-[#2a2e42]/50 shadow-xl shadow-black/20">
        {options.map(opt => (
          <div key={String(opt.value)} className="relative group">
            <button
              onClick={() => onChange(opt.value)}
              className={`px-6 py-2 rounded-xl text-sm font-medium transition-all ${
                value === opt.value ? 'glass active text-indigo-300' : 'text-slate-500 hover:text-slate-300'
              }`}
            >
              {opt.label}
            </button>
            {opt.tooltip && (
              <div className={`absolute bottom-full mb-2.5 w-60 px-3 py-2.5 bg-[#12151f] border border-[#2a2e42]/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity z-50 shadow-2xl ${
                opt.tooltipAlign === 'right' ? 'right-0' : 'left-1/2 -translate-x-1/2'
              }`}>
                {opt.tooltip}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
