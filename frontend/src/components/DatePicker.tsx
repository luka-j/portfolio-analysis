import { useState, useRef, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { formatDate } from '../utils/format'

const MONTH_NAMES = ['January','February','March','April','May','June','July','August','September','October','November','December']
const DAY_ABBRS = ['Mo','Tu','We','Th','Fr','Sa','Su']

function buildCells(year: number, month: number): (string | null)[] {
  const firstDaySun = new Date(year, month, 1).getDay()
  const firstDay = (firstDaySun + 6) % 7
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const cells: (string | null)[] = Array(firstDay).fill(null)
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push(`${year}-${String(month + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`)
  }
  while (cells.length % 7 !== 0) cells.push(null)
  return cells
}

interface Props {
  value: string        // YYYY-MM-DD
  onChange: (date: string) => void
}

interface DropdownPos {
  top: number
  left: number
}

export default function DatePicker({ value, onChange }: Props) {
  const [open, setOpen] = useState(false)
  const [pos, setPos]   = useState<DropdownPos | null>(null)
  const buttonRef   = useRef<HTMLButtonElement>(null)
  const calendarRef = useRef<HTMLDivElement>(null)
  const today = formatDate(new Date())

  const seed = value || today
  const [viewYear,  setViewYear]  = useState(() => parseInt(seed.slice(0, 4)))
  const [viewMonth, setViewMonth] = useState(() => parseInt(seed.slice(5, 7)) - 1)

  function calcPos() {
    if (!buttonRef.current) return
    const r = buttonRef.current.getBoundingClientRect()
    setPos({ top: r.bottom + 4, left: r.left })
  }

  // Track button position while open
  useEffect(() => {
    if (!open) return
    function update() { calcPos() }
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => {
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
    }
  }, [open])

  // Close on outside click
  useEffect(() => {
    function onMouseDown(e: MouseEvent) {
      const target = e.target as Node
      if (
        buttonRef.current   && !buttonRef.current.contains(target) &&
        calendarRef.current && !calendarRef.current.contains(target)
      ) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onMouseDown)
    return () => document.removeEventListener('mousedown', onMouseDown)
  }, [])

  const nav = (dir: -1 | 1) => {
    const m = viewMonth + dir
    if (m < 0)  { setViewYear(y => y - 1); setViewMonth(11) }
    else if (m > 11) { setViewYear(y => y + 1); setViewMonth(0) }
    else setViewMonth(m)
  }

  const cells = buildCells(viewYear, viewMonth)

  return (
    <div className="relative">
      <button
        ref={buttonRef}
        type="button"
        onClick={() => {
          if (open) { setOpen(false) } else { calcPos(); setOpen(true) }
        }}
        className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 text-sm focus:outline-none focus:border-indigo-500/50 transition-colors text-left flex items-center justify-between"
      >
        <span className="font-mono">{value || 'Select date'}</span>
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5" className="text-slate-500 shrink-0">
          <rect x="1" y="2" width="12" height="11" rx="2"/>
          <path d="M1 5h12M4 1v2M10 1v2"/>
        </svg>
      </button>

      {open && pos && createPortal(
        <div
          ref={calendarRef}
          className="bg-surface border border-border-dim/70 rounded-2xl px-5 py-4 shadow-2xl"
          style={{ position: 'fixed', top: pos.top, left: pos.left, zIndex: 9999 }}
          onMouseDown={e => e.preventDefault()}
        >
          <div className="flex items-center justify-between mb-3">
            <button
              type="button"
              onClick={() => nav(-1)}
              className="p-1.5 text-slate-500 hover:text-slate-200 hover:bg-white/5 rounded-lg transition-all leading-none text-base"
            >‹</button>
            <span className="text-[11px] font-black text-slate-300 uppercase tracking-widest">
              {MONTH_NAMES[viewMonth]} {viewYear}
            </span>
            <button
              type="button"
              onClick={() => nav(1)}
              className="p-1.5 text-slate-500 hover:text-slate-200 hover:bg-white/5 rounded-lg transition-all leading-none text-base"
            >›</button>
          </div>

          <div className="grid grid-cols-7 mb-0.5">
            {DAY_ABBRS.map(d => (
              <div key={d} className="text-center py-1 text-[9px] font-black text-slate-500 tracking-wider">{d}</div>
            ))}
          </div>

          <div className="grid grid-cols-7 w-48">
            {cells.map((ds, i) => {
              if (!ds) return <div key={i} className="h-8" />
              const future     = ds > today
              const isSelected = ds === value
              const isToday    = ds === today
              return (
                <button
                  key={ds}
                  type="button"
                  disabled={future}
                  onClick={() => { onChange(ds); setOpen(false) }}
                  className={[
                    'h-8 w-full text-[11px] font-semibold transition-all',
                    future    ? 'text-slate-500 cursor-default' : 'cursor-pointer',
                    isSelected ? 'bg-indigo-600 text-white shadow-md rounded-lg' : '',
                    !isSelected && !future ? 'text-slate-400 hover:bg-white/5 hover:text-slate-200 rounded-lg' : '',
                    isToday && !isSelected ? 'ring-1 ring-inset ring-indigo-500/40 rounded-lg' : '',
                  ].filter(Boolean).join(' ')}
                >
                  {parseInt(ds.slice(8))}
                </button>
              )
            })}
          </div>
        </div>,
        document.body
      )}
    </div>
  )
}
