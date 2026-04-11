import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Area,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { getSecurityChart, type SecurityChartPoint, type TradeEntry } from '../api'
import { usePersistentState } from '../utils/usePersistentState'
import Spinner from './Spinner'

// Trade types to show as dots on the chart (TRANSFER_IN / TRANSFER_OUT excluded).
const SHOW_ON_CHART = new Set(['BUY', 'SELL', 'ESPP_VEST', 'RSU_VEST'])
const BUY_SIDES = new Set(['BUY', 'ESPP_VEST', 'RSU_VEST'])

type Period = '3m' | '1y' | 'all' | 'custom'

// ---- Inline calendar (same logic as DatePicker but without its own trigger button) ----

const MONTH_NAMES = ['January','February','March','April','May','June','July','August','September','October','November','December']
const DAY_ABBRS = ['Mo','Tu','We','Th','Fr','Sa','Su']

function buildCalendarCells(year: number, month: number): (string | null)[] {
  const firstDay = (new Date(year, month, 1).getDay() + 6) % 7
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const cells: (string | null)[] = Array(firstDay).fill(null)
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push(`${year}-${String(month + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`)
  }
  while (cells.length % 7 !== 0) cells.push(null)
  return cells
}

interface InlineCalendarProps {
  value: string
  onChange: (date: string) => void
}

function InlineCalendar({ value, onChange }: InlineCalendarProps) {
  const today = new Date().toISOString().slice(0, 10)
  const seed = value || today
  const [viewYear,  setViewYear]  = useState(() => parseInt(seed.slice(0, 4)))
  const [viewMonth, setViewMonth] = useState(() => parseInt(seed.slice(5, 7)) - 1)

  const nav = (dir: -1 | 1) => {
    const m = viewMonth + dir
    if (m < 0)       { setViewYear(y => y - 1); setViewMonth(11) }
    else if (m > 11) { setViewYear(y => y + 1); setViewMonth(0)  }
    else             { setViewMonth(m) }
  }

  const cells = buildCalendarCells(viewYear, viewMonth)

  return (
    <div className="bg-[#0f1117] border border-[#2a2e42]/70 rounded-2xl px-5 py-4 shadow-2xl w-fit">
      <div className="flex items-center justify-between mb-3">
        <button type="button" onClick={() => nav(-1)}
          className="p-1.5 text-slate-500 hover:text-slate-200 hover:bg-white/5 rounded-lg transition-all leading-none text-base">‹</button>
        <span className="text-[11px] font-black text-slate-300 uppercase tracking-widest">
          {MONTH_NAMES[viewMonth]} {viewYear}
        </span>
        <button type="button" onClick={() => nav(1)}
          className="p-1.5 text-slate-500 hover:text-slate-200 hover:bg-white/5 rounded-lg transition-all leading-none text-base">›</button>
      </div>

      <div className="grid grid-cols-7 mb-0.5">
        {DAY_ABBRS.map(d => (
          <div key={d} className="text-center py-1 text-[9px] font-black text-slate-600 tracking-wider">{d}</div>
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
              onClick={() => onChange(ds)}
              className={[
                'h-8 w-full text-[11px] font-semibold transition-all',
                future     ? 'text-slate-800 cursor-default' : 'cursor-pointer',
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
    </div>
  )
}

interface EnrichedPoint extends SecurityChartPoint {
  tradeType: 'buy' | 'sell' | null
  tradeTotal: number | null
  tradeAction: string | null
}

interface TradeMeta {
  type: 'buy' | 'sell'
  total: number
  action: string
}

function periodToFrom(period: Exclude<Period, 'custom'>): string {
  const d = new Date()
  if (period === '3m') d.setMonth(d.getMonth() - 3)
  else if (period === '1y') d.setFullYear(d.getFullYear() - 1)
  else d.setFullYear(2000)
  return d.toISOString().slice(0, 10)
}

function todayStr(): string {
  return new Date().toISOString().slice(0, 10)
}

// ---- Custom tooltip ----

interface TooltipExtra {
  active?: boolean
  payload?: Array<{ dataKey?: string; value?: number; payload?: EnrichedPoint }>
  label?: string
  maDays: number
  maEnabled: boolean
  privacy: boolean
}

function ChartTooltip(props: TooltipExtra) {
  const { active, payload, label, maDays, maEnabled, privacy } = props
  if (!active || !payload || payload.length === 0) return null

  const pt = payload[0]?.payload as EnrichedPoint | undefined
  const close = payload.find(p => p.dataKey === 'close')?.value
  const ma = payload.find(p => p.dataKey === 'ma')?.value

  return (
    <div
      style={{
        background: 'rgba(26,29,46,0.98)',
        border: '1px solid rgba(99,102,241,0.3)',
        borderRadius: '24px',
        padding: '10px 14px',
        backdropFilter: 'blur(32px)',
        boxShadow: '0 25px 50px -12px rgba(0,0,0,0.5)',
        fontSize: 11,
        color: '#e2e8f0',
        minWidth: 140,
      }}
    >
      <div
        style={{
          color: '#6366f1',
          fontSize: 9,
          fontWeight: 900,
          textTransform: 'uppercase',
          letterSpacing: '0.25em',
          marginBottom: 6,
          opacity: 0.8,
        }}
      >
        {label}
      </div>
      {close != null && (
        <div style={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.1em' }}>
          Price:{' '}
          <span style={{ color: '#e2e8f0' }}>
            {privacy ? '———' : Number(close).toFixed(2)}
          </span>
        </div>
      )}
      {maEnabled && ma != null && (
        <div style={{ fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.1em', color: '#a78bfa' }}>
          MA({maDays}):{' '}
          <span>{privacy ? '———' : Number(ma).toFixed(2)}</span>
        </div>
      )}
      {pt?.tradeAction && pt.tradeTotal != null && (
        <>
          <div style={{ borderTop: '1px solid rgba(255,255,255,0.08)', margin: '8px 0 6px' }} />
          <div
            style={{
              fontWeight: 900,
              textTransform: 'uppercase',
              letterSpacing: '0.1em',
              color: pt.tradeType === 'buy' ? '#34d399' : '#f87171',
            }}
          >
            {pt.tradeAction.replace('_', ' ')}
            {!privacy && pt.tradeTotal > 0 && (
              <span style={{ color: '#94a3b8', marginLeft: 6 }}>
                · {pt.tradeTotal.toFixed(2)}
              </span>
            )}
          </div>
        </>
      )}
    </div>
  )
}

// ---- Trade dot renderer ----

interface DotProps {
  cx?: number
  cy?: number
  payload?: EnrichedPoint
}

function renderTradeDot(props: DotProps): React.ReactElement | null {
  const { cx, cy, payload } = props
  if (!payload?.tradeType || cx == null || cy == null) return null
  const fill = payload.tradeType === 'buy' ? '#34d399' : '#f87171'
  return (
    <circle
      key={`dot-${payload.date}`}
      cx={cx}
      cy={cy}
      r={4}
      fill={fill}
      stroke="rgba(15,17,23,0.8)"
      strokeWidth={1.5}
    />
  )
}

// ---- Main component ----

interface Props {
  symbol: string
  trades: TradeEntry[]
  privacy: boolean
  displayCurrency?: string   // e.g. 'CZK', 'USD', 'EUR', or 'Original'
  acctModel?: string         // 'historical' | 'spot'
}

export function SecurityPriceChart({ symbol, trades, privacy, displayCurrency, acctModel }: Props) {
  // period and customFrom are shared across all chart instances via localStorage.
  const [period, setPeriod] = usePersistentState<Period>('security-chart-period', '1y')
  const [customFrom, setCustomFrom] = usePersistentState('security-chart-custom-from', '')
  const [pickerOpen, setPickerOpen] = useState(false)
  const pickerRef = useRef<HTMLDivElement>(null)

  const [maDays, setMaDays] = useState(30)
  const [maDaysInput, setMaDaysInput] = useState('30')
  const [maEnabled, setMaEnabled] = useState(true)
  const [chartData, setChartData] = useState<SecurityChartPoint[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Close the picker when clicking outside it.
  useEffect(() => {
    function onMouseDown(e: MouseEvent) {
      if (pickerRef.current && !pickerRef.current.contains(e.target as Node)) {
        setPickerOpen(false)
      }
    }
    document.addEventListener('mousedown', onMouseDown)
    return () => document.removeEventListener('mousedown', onMouseDown)
  }, [])

  const from = period === 'custom' ? customFrom : periodToFrom(period)

  useEffect(() => {
    if (!from) return
    const controller = new AbortController()
    setLoading(true)
    setError('')
    const to = todayStr()
    getSecurityChart(symbol, from, to, maDays, controller.signal, displayCurrency, acctModel)
      .then(res => setChartData(res.data))
      .catch(err => {
        if (err.name !== 'AbortError') setError(err.message ?? 'Failed to load price data')
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, [symbol, from, maDays, displayCurrency, acctModel])

  function handleCustomClick() {
    if (period !== 'custom') {
      setPeriod('custom')
      setPickerOpen(true)
    } else {
      setPickerOpen(o => !o)
    }
  }

  function handleCustomDatePick(date: string) {
    setCustomFrom(date)
    setPickerOpen(false)
  }

  // Build a map of trade date → metadata, excluding TRANSFER_IN/TRANSFER_OUT.
  const tradeSideMap = useMemo(() => {
    const m = new Map<string, TradeMeta>()
    trades
      .filter(t => SHOW_ON_CHART.has(t.side))
      .forEach(t => {
        const isBuy = BUY_SIDES.has(t.side)
        const existing = m.get(t.date)
        m.set(t.date, {
          type: isBuy ? 'buy' : 'sell',
          total: (existing?.total ?? 0) + Math.abs(t.quantity * t.converted_price),
          action: t.side,
        })
      })
    return m
  }, [trades])

  // Resolve each trade date to the next available trading day in the chart data.
  // Trades on weekends, holidays, or outside regular market hours (after-hours fills)
  // may not have a matching candle; in those cases we forward the dot to the next date
  // that actually appears in the chart.
  const resolvedTradeMap = useMemo(() => {
    if (chartData.length === 0) return new Map<string, TradeMeta>()
    const chartDates = chartData.map(p => p.date) // already sorted ascending
    const chartDateSet = new Set(chartDates)
    const resolved = new Map<string, TradeMeta>()

    tradeSideMap.forEach((meta, tradeDate) => {
      let target = tradeDate
      if (!chartDateSet.has(tradeDate)) {
        // Find the next chart date that is >= the trade date.
        const next = chartDates.find(d => d >= tradeDate)
        if (!next) return // trade is beyond the chart window — skip
        target = next
      }
      // Accumulate in case multiple trade dates forward to the same trading day.
      const existing = resolved.get(target)
      resolved.set(target, {
        type: existing ? (existing.type === meta.type ? meta.type : 'buy') : meta.type,
        total: (existing?.total ?? 0) + meta.total,
        action: existing ? existing.action : meta.action,
      })
    })

    return resolved
  }, [chartData, tradeSideMap])

  // Merge trade info into chart points for the tooltip and dot renderer.
  const enrichedData = useMemo<EnrichedPoint[]>(
    () =>
      chartData.map(p => {
        const meta = resolvedTradeMap.get(p.date)
        return {
          ...p,
          tradeType: meta?.type ?? null,
          tradeTotal: meta?.total ?? null,
          tradeAction: meta?.action ?? null,
        }
      }),
    [chartData, resolvedTradeMap],
  )

  const gradientId = `secGrad-${symbol.replace(/[^a-zA-Z0-9]/g, '_')}`

  const commitMaDays = () => {
    const n = parseInt(maDaysInput)
    if (!isNaN(n) && n >= 2 && n <= 365) {
      setMaDays(n)
    } else {
      setMaDaysInput(String(maDays))
    }
  }

  return (
    <div className="px-2">
      <p className="text-[10px] font-black text-slate-500 uppercase tracking-[0.25em] mb-4">
        Price History
      </p>

      {/* Controls */}
      <div className="relative flex items-center justify-between mb-3 px-1">
        {/* Period selector */}
        <div className="flex gap-1 flex-wrap">
          {(['3m', '1y', 'all'] as const).map(p => (
            <button
              key={p}
              onClick={() => setPeriod(p)}
              className={`px-3 py-1 rounded-xl text-[9px] font-bold uppercase transition-all duration-200 ${
                period === p
                  ? 'bg-indigo-600 text-white ring-2 ring-indigo-500/20 shadow-lg shadow-indigo-600/20'
                  : 'text-slate-500 hover:text-slate-300 hover:bg-white/5 bg-[#1a1d2e]/40 border border-white/5'
              }`}
            >
              {p.toUpperCase()}
            </button>
          ))}
          <button
            onClick={handleCustomClick}
            className={`px-3 py-1 rounded-xl text-[9px] font-bold uppercase transition-all duration-200 ${
              period === 'custom'
                ? 'bg-indigo-600 text-white ring-2 ring-indigo-500/20 shadow-lg shadow-indigo-600/20'
                : 'text-slate-500 hover:text-slate-300 hover:bg-white/5 bg-[#1a1d2e]/40 border border-white/5'
            }`}
          >
            {period === 'custom' && customFrom ? customFrom : 'Custom'}
          </button>
        </div>

        {/* MA controls */}
        <div className="flex items-center gap-2">
          <button
            onClick={() => setMaEnabled(e => !e)}
            className={`text-[9px] font-bold uppercase px-2 py-1 rounded-lg border transition-all ${
              maEnabled
                ? 'border-violet-500/40 text-violet-400 bg-violet-500/10'
                : 'border-white/5 text-slate-600 bg-transparent'
            }`}
          >
            MA
          </button>
          {maEnabled && (
            <div className="flex items-center gap-1">
              <span className="text-[9px] text-slate-600 uppercase font-bold">Days</span>
              <input
                type="number"
                min={2}
                max={365}
                value={maDaysInput}
                onChange={e => setMaDaysInput(e.target.value)}
                onBlur={commitMaDays}
                onKeyDown={e => { if (e.key === 'Enter') commitMaDays() }}
                className="w-14 px-2 py-1 bg-[#0f1117] border border-[#2a2e42] rounded-lg text-slate-200 text-[11px] text-center focus:outline-none focus:border-indigo-500/50 [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
              />
            </div>
          )}
        </div>

        {/* Collapsible date picker — floats over the chart */}
        {period === 'custom' && pickerOpen && (
          <div ref={pickerRef} className="absolute top-full left-0 z-20 mt-1">
            <InlineCalendar value={customFrom} onChange={handleCustomDatePick} />
          </div>
        )}
      </div>

      {/* Chart area */}
      {loading ? (
        <Spinner className="h-80" />
      ) : error ? (
        <div className="h-80 flex items-center justify-center text-red-400 text-[10px] font-black uppercase tracking-widest">
          {error}
        </div>
      ) : enrichedData.length === 0 ? (
        <div className="h-80 flex items-center justify-center text-slate-700 text-[10px] font-black uppercase tracking-[0.3em]">
          No price data available
        </div>
      ) : (
        <ResponsiveContainer width="100%" height={320}>
          <ComposedChart data={enrichedData} margin={{ top: 4, right: 8, left: 8, bottom: 0 }}>
            <defs>
              <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#6366f1" stopOpacity={0.18} />
                <stop offset="100%" stopColor="#6366f1" stopOpacity={0} />
              </linearGradient>
            </defs>

            <XAxis
              dataKey="date"
              tick={{ fontSize: 9, fill: '#334155', fontWeight: 'bold' }}
              tickLine={false}
              axisLine={false}
              minTickGap={60}
              dy={6}
            />
            <YAxis
              domain={['auto', 'auto']}
              tick={{ fontSize: 9, fill: '#334155', fontWeight: 'bold' }}
              tickLine={false}
              axisLine={false}
              width={56}
              tickFormatter={val => (privacy ? '—' : Number(val).toFixed(2))}
            />

            <Tooltip
              content={
                <ChartTooltip
                  maDays={maDays}
                  maEnabled={maEnabled}
                  privacy={privacy}
                />
              }
            />

            {/* Price line with gradient area fill and trade dot markers */}
            <Area
              type="monotone"
              dataKey="close"
              stroke="#6366f1"
              strokeWidth={1.5}
              fill={`url(#${gradientId})`}
              dot={renderTradeDot as never}
              activeDot={{ r: 4, fill: '#6366f1', strokeWidth: 0 }}
              animationDuration={800}
            />

            {/* MA dotted line — only when enabled */}
            {maEnabled && (
              <Line
                type="monotone"
                dataKey="ma"
                stroke="#a78bfa"
                strokeWidth={1.2}
                strokeDasharray="4 4"
                dot={false}
                activeDot={false}
                connectNulls={false}
                animationDuration={800}
              />
            )}
          </ComposedChart>
        </ResponsiveContainer>
      )}
    </div>
  )
}
