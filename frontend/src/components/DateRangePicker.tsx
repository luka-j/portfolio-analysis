import { useState } from 'react'
import { formatDate } from '../utils/format'

const MONTH_NAMES = ['January','February','March','April','May','June','July','August','September','October','November','December']
const DAY_ABBRS = ['Mo','Tu','We','Th','Fr','Sa','Su']

function buildCells(year: number, month: number): (string | null)[] {
  const firstDaySun = new Date(year, month, 1).getDay()
  const firstDay = (firstDaySun + 6) % 7  // shift so Monday = 0
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const cells: (string | null)[] = Array(firstDay).fill(null)
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push(`${year}-${String(month + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`)
  }
  while (cells.length % 7 !== 0) cells.push(null)
  return cells
}

function MonthGrid({
  year, month, from, to, hover, stage, today,
  onClick, onEnter, onLeave,
}: {
  year: number; month: number
  from: string; to: string
  hover: string | null; stage: 'from' | 'to'; today: string
  onClick: (d: string) => void
  onEnter: (d: string) => void
  onLeave: () => void
}) {
  const effectiveFrom = stage === 'to' && hover && hover < from ? hover : from
  const effectiveTo   = stage === 'to' && hover ? (hover >= from ? hover : from) : to
  const cells = buildCells(year, month)

  return (
    <div className="flex flex-col gap-1 w-48">
      <div className="text-center text-[11px] font-black text-slate-300 uppercase tracking-widest mb-1">
        {MONTH_NAMES[month]} {year}
      </div>
      <div className="grid grid-cols-7 mb-0.5">
        {DAY_ABBRS.map(d => (
          <div key={d} className="text-center py-1 text-[9px] font-black text-slate-600 tracking-wider">{d}</div>
        ))}
      </div>
      <div className="grid grid-cols-7">
        {cells.map((ds, i) => {
          if (!ds) return <div key={i} className="h-8" />
          const future  = ds > today
          const isFrom  = ds === effectiveFrom
          const isTo    = ds === effectiveTo
          const inRange = ds > effectiveFrom && ds < effectiveTo
          const isToday = ds === today
          return (
            <button
              key={ds}
              disabled={future}
              onClick={() => onClick(ds)}
              onMouseEnter={() => onEnter(ds)}
              onMouseLeave={onLeave}
              className={[
                'h-8 w-full text-[11px] font-semibold transition-all',
                future ? 'text-slate-800 cursor-default' : 'cursor-pointer',
                isFrom || isTo ? 'bg-indigo-600 text-white shadow-md rounded-lg relative z-10' : '',
                inRange ? 'bg-indigo-500/15 text-indigo-300' : '',
                !isFrom && !isTo && !inRange && !future ? 'text-slate-400 hover:bg-white/5 hover:text-slate-200 rounded-lg' : '',
                isToday && !isFrom && !isTo ? 'ring-1 ring-inset ring-indigo-500/40 rounded-lg' : '',
              ].filter(Boolean).join(' ')}
            >
              {parseInt(ds.slice(8))}
            </button>
          )
        })}
      </div>
    </div>
  )
}

export default function DateRangePicker({ initialFrom, initialTo, onApply }: {
  initialFrom: string
  initialTo: string
  onApply: (from: string, to: string) => void
}) {
  const today = formatDate(new Date())
  const [tmpFrom, setTmpFrom] = useState(initialFrom)
  const [tmpTo,   setTmpTo]   = useState(initialTo)
  const [stage, setStage]     = useState<'from' | 'to'>('from')
  const [hover, setHover]     = useState<string | null>(null)
  const start = new Date(initialFrom)
  const [viewYear,  setViewYear]  = useState(start.getFullYear())
  const [viewMonth, setViewMonth] = useState(start.getMonth())

  const rYear  = viewMonth === 11 ? viewYear + 1 : viewYear
  const rMonth = viewMonth === 11 ? 0 : viewMonth + 1

  const nav = (dir: -1 | 1) => {
    const m = viewMonth + dir
    if (m < 0)  { setViewYear(y => y - 1); setViewMonth(11) }
    else if (m > 11) { setViewYear(y => y + 1); setViewMonth(0) }
    else setViewMonth(m)
  }

  const handleClick = (ds: string) => {
    if (stage === 'from') {
      setTmpFrom(ds)
      if (ds > tmpTo) setTmpTo(ds)
      setStage('to')
    } else {
      const [a, b] = ds < tmpFrom ? [ds, tmpFrom] : [tmpFrom, ds]
      setTmpFrom(a); setTmpTo(b)
      setStage('from')
    }
  }

  const shared = { from: tmpFrom, to: tmpTo, hover, stage, today, onClick: handleClick, onEnter: setHover, onLeave: () => setHover(null) }

  return (
    <div className="bg-[#1a1d2e] border border-[#2a2e42]/70 rounded-3xl px-8 py-6 shadow-2xl flex flex-col items-center gap-4">
      <div className="flex items-center gap-3 text-[10px] font-black uppercase tracking-widest">
        <span className={`px-3 py-1.5 rounded-xl border transition-colors ${stage === 'from' ? 'border-indigo-500/60 bg-indigo-500/10 text-indigo-300' : 'border-[#2a2e42]/60 text-slate-400'}`}>
          {tmpFrom}
        </span>
        <span className="text-slate-700">→</span>
        <span className={`px-3 py-1.5 rounded-xl border transition-colors ${stage === 'to' ? 'border-indigo-500/60 bg-indigo-500/10 text-indigo-300' : 'border-[#2a2e42]/60 text-slate-400'}`}>
          {tmpTo}
        </span>
      </div>
      <p className="text-[9px] font-black text-slate-700 uppercase tracking-widest -mt-1">
        {stage === 'from' ? 'Select start date' : 'Select end date'}
      </p>
      <div className="flex items-start gap-2">
        <button onClick={() => nav(-1)} className="mt-7 p-2 text-slate-500 hover:text-slate-200 hover:bg-white/5 rounded-xl transition-all text-lg leading-none">‹</button>
        <div className="flex gap-8">
          <MonthGrid year={viewYear}  month={viewMonth} {...shared} />
          <MonthGrid year={rYear}     month={rMonth}    {...shared} />
        </div>
        <button onClick={() => nav(1)} className="mt-7 p-2 text-slate-500 hover:text-slate-200 hover:bg-white/5 rounded-xl transition-all text-lg leading-none">›</button>
      </div>
      <button
        onClick={() => onApply(tmpFrom, tmpTo)}
        className="px-8 py-2 bg-indigo-600 hover:bg-indigo-500 text-white text-[10px] font-black uppercase tracking-widest rounded-xl transition-all shadow-lg shadow-indigo-500/20"
      >
        Apply Range
      </button>
    </div>
  )
}
