import { useState, useEffect, useCallback, useRef } from 'react'
import { XAxis, YAxis, Tooltip, ResponsiveContainer, Area, AreaChart } from 'recharts'
import NavBar from '../components/NavBar'
import {
  getPortfolioValue, getPortfolioHistory, getPortfolioStats, getPortfolioReturns,
  uploadFlexQuery, uploadEtradeBenefits, uploadEtradeSales,
  type DailyValue,
} from '../api'
import { formatCurrencyCompact, formatDate, CURRENCIES } from '../utils/format'

const PERIODS = [
  { label: '1M', months: 1 },
  { label: '3M', months: 3 },
  { label: '6M', months: 6 },
  { label: '1Y', months: 12 },
  { label: 'All', months: 0 },
]

const CURRENCY_SYMBOLS: Record<string, string> = {
  CZK: 'Kč',
  USD: '$',
  EUR: '€',
}

function getFromDate(months: number): string {
  if (months === 0) return '2000-01-01'
  const d = new Date()
  d.setMonth(d.getMonth() - months)
  return formatDate(d)
}

export default function LandingPage() {
  const [currencyIdx, setCurrencyIdx] = useState(0)
  const currency = CURRENCIES[currencyIdx]
  const [period, setPeriod] = useState(0)
  const [chartMode, setChartMode] = useState<'value' | 'twr' | 'mwr'>('value')
  const [portfolioValues, setPortfolioValues] = useState<Record<string, number>>({})
  const [history, setHistory] = useState<DailyValue[]>([])
  const [twrHistory, setTwrHistory] = useState<DailyValue[]>([])
  const [stats, setStats] = useState<Record<string, unknown> | null>(null)
  const [loading, setLoading] = useState(true)
  const [chartLoading, setChartLoading] = useState(false)
  const [error, setError] = useState('')
  const [uploadMsg, setUploadMsg] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadExpanded, setUploadExpanded] = useState(false)

  const loadGenRef = useRef(0)

  const cycleCurrency = () => setCurrencyIdx(i => (i + 1) % CURRENCIES.length)

  const loadData = useCallback(async () => {
    loadGenRef.current += 1
    const gen = loadGenRef.current
    setLoading(true)
    setError('')
    try {
      const [czk, usd, eur, st] = await Promise.all([
        getPortfolioValue('CZK'),
        getPortfolioValue('USD'),
        getPortfolioValue('EUR'),
        getPortfolioStats(getFromDate(period), formatDate(new Date()), currency),
      ])
      if (gen !== loadGenRef.current) return
      setPortfolioValues({ CZK: czk.value, USD: usd.value, EUR: eur.value })
      setStats(st.statistics)
    } catch (err) {
      if (gen !== loadGenRef.current) return
      setError(err instanceof Error ? err.message : 'Failed to load data')
    } finally {
      if (gen === loadGenRef.current) setLoading(false)
    }
  }, [currency, period])

  useEffect(() => { loadData() }, [loadData])

  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false
    const from = getFromDate(period)
    const to = formatDate(new Date())
    setChartLoading(true)
    const fetchChart = async () => {
      try {
        const hist = await getPortfolioHistory(from, to, currency, 'historical', controller.signal)
        if (cancelled) return
        setHistory(hist.data)
        const twrHist = await getPortfolioReturns(from, to, currency, 'historical', controller.signal)
        if (cancelled) return
        setTwrHistory(twrHist.data)
      } catch {
        // silently swallow
      } finally {
        if (!cancelled) setChartLoading(false)
      }
    }
    fetchChart()
    return () => { cancelled = true; controller.abort() }
  }, [currency, period])

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploading(true)
    setUploadMsg('')
    try {
      const res = await uploadFlexQuery(file)
      setUploadMsg(`Uploaded: ${res.positions_count} positions, ${res.trades_count} trades`)
      await loadData()
    } catch (err) {
      setUploadMsg(err instanceof Error ? err.message : 'Upload failed')
    } finally {
      setUploading(false)
      e.target.value = ''
    }
  }

  const handleEtradeBenefitsUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploading(true)
    setUploadMsg('')
    try {
      const res = await uploadEtradeBenefits(file)
      setUploadMsg(`Benefits Uploaded: ${res.parsed_count} parsed, ${res.saved_count} saved`)
      await loadData()
    } catch (err) {
      setUploadMsg(err instanceof Error ? err.message : 'Upload failed')
    } finally {
      setUploading(false)
      e.target.value = ''
    }
  }

  const handleEtradeSalesUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploading(true)
    setUploadMsg('')
    try {
      const res = await uploadEtradeSales(file)
      setUploadMsg(`Sales Uploaded: ${res.parsed_count} parsed, ${res.saved_count} saved`)
      await loadData()
    } catch (err) {
      setUploadMsg(err instanceof Error ? err.message : 'Upload failed')
    } finally {
      setUploading(false)
      e.target.value = ''
    }
  }

  const chartData = (() => {
    if (chartMode === 'value') {
      return history.map(d => ({ date: d.date, value: d.value }))
    }
    if (chartMode === 'twr') {
      return twrHistory.map(d => ({ date: d.date, value: d.value }))
    }
    const mwr = typeof stats?.mwr === 'number' ? (stats.mwr as number) * 100 : null
    return history.map(d => {
      const firstValue = history[0]?.value || 1
      const simpleGrowth = firstValue > 0 ? ((d.value / firstValue) - 1) * 100 : 0
      return { date: d.date, value: simpleGrowth, mwr: mwr ?? 0 }
    })
  })()

  const mwr = typeof stats?.mwr === 'number' ? (stats.mwr as number) * 100 : null
  const twr = typeof stats?.twr === 'number' ? (stats.twr as number) * 100 : null

  const currValue = portfolioValues[currency] ?? 0

  return (
    <div className="h-screen bg-[#0f1117] flex flex-col overflow-hidden">
      <NavBar />

      {/* Hero section centered */}
      <div className="z-10 w-full flex flex-col items-center gap-2 pointer-events-none">
        <h1 className="text-4xl md:text-6xl font-bold text-white tabular-nums tracking-tight [text-shadow:0_0_20px_rgba(255,255,255,0.05)] flex items-baseline gap-2">
          <button
            className="pointer-events-auto text-indigo-300/70 hover:text-indigo-300 px-1.5 py-0.5 rounded-lg hover:bg-white/[0.07] hover:backdrop-blur-sm transition-all duration-200 active:scale-95"
            onClick={cycleCurrency}
            title="Switch currency"
          >
            {CURRENCY_SYMBOLS[currency]}
          </button>
          {loading ? '—' : new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(currValue)}
        </h1>

        {/* TWR / MWR secondary indicators */}
        {(mwr !== null || twr !== null) && (
          <div className="flex items-center gap-8">
            {twr !== null && (
              <div className="flex flex-col items-center gap-0.5">
                <span className="text-xs text-slate-600">TWR</span>
                <span className={`text-base font-semibold tabular-nums ${twr >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                  {twr >= 0 ? '+' : ''}{twr.toFixed(2)}%
                </span>
              </div>
            )}
            {mwr !== null && (
              <div className="flex flex-col items-center gap-0.5">
                <span className="text-xs text-slate-600">MWR</span>
                <span className={`text-base font-semibold tabular-nums ${mwr >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                  {mwr >= 0 ? '+' : ''}{mwr.toFixed(2)}%
                </span>
              </div>
            )}
          </div>
        )}

        {/* Mode selector — simple pill toggles with proper padding */}
        <div className="pointer-events-auto flex items-center gap-1 mt-2 bg-[#1a1d2e] rounded-2xl p-1 border border-white/6">
          {(['value', 'twr', 'mwr'] as const).map(mode => (
            <button
              key={mode}
              onClick={() => setChartMode(mode)}
              className={`px-5 py-1.5 rounded-xl text-sm font-medium transition-all duration-200 ${
                chartMode === mode 
                  ? 'glass active text-indigo-300 shadow-lg' 
                  : 'text-slate-500 hover:text-slate-300'
              }`}
            >
              {mode === 'value' ? 'Value' : mode.toUpperCase()}
            </button>
          ))}
        </div>
      </div>

      {/* Main chart area with spacing from screen edge */}
      <div className="relative flex-1 mt-auto flex flex-col justify-end px-8 mb-6">
        
        {/* The chart itself — axes returned and labels added */}
        <div className="w-full h-[65%] min-h-[350px]">
          {chartLoading || loading ? (
            <div className="h-full flex items-center justify-center text-slate-800 font-black uppercase tracking-[0.3em] text-[10px] animate-pulse">Initializing history…</div>
          ) : chartData.length === 0 ? (
            <div className="h-full flex items-center justify-center text-slate-800 font-black uppercase tracking-[0.3em] text-[10px]">Matrix data unavailable</div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 0, right: 0, left: 10, bottom: 0 }}>
                <defs>
                  <linearGradient id="chartGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#6366f1" stopOpacity={0.15} />
                    <stop offset="100%" stopColor="#6366f1" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis 
                  dataKey="date" 
                  tick={{fontSize: 9, fill: '#334155', fontWeight: 'bold'}} 
                  tickLine={false} 
                  axisLine={false} 
                  interval={Math.floor(chartData.length / 6)}
                  dy={10}
                />
                <YAxis 
                  domain={['auto', 'auto']} 
                  tick={{fontSize: 9, fill: '#334155', fontWeight: 'bold'}} 
                  tickLine={false} 
                  axisLine={false} 
                  tickFormatter={(val) => chartMode === 'value' ? formatCurrencyCompact(val, currency) : `${val.toFixed(1)}%`}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: 'rgba(26,29,46,0.98)',
                    border: '1px solid rgba(99,102,241,0.3)',
                    borderRadius: '24px',
                    fontSize: '11px',
                    color: '#e2e8f0',
                    backdropFilter: 'blur(32px)',
                    boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.5)',
                  }}
                  itemStyle={{ fontWeight: 'black', textTransform: 'uppercase', letterSpacing: '0.1em' }}
                  labelStyle={{ color: '#6366f1', marginBottom: '6px', fontSize: '9px', textTransform: 'uppercase', letterSpacing: '0.25em', fontWeight: '900', opacity: 0.8 }}
                  formatter={(value) => [
                    chartMode === 'value'
                      ? formatCurrencyCompact(Number(value), currency)
                      : `${Number(value).toFixed(2)}%`,
                    chartMode.toUpperCase(),
                  ]}
                />
                <Area
                  type="monotone"
                  dataKey="value"
                  stroke="#6366f1"
                  strokeWidth={1.5}
                  fill="url(#chartGrad)"
                  dot={false}
                  animationDuration={1500}
                />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* Period vertical pills — middle-right */}
        <div className="absolute right-8 top-4 bottom-56 flex flex-col items-center justify-center gap-2 z-10">
          {PERIODS.map(p => (
            <button
              key={p.label}
              onClick={() => setPeriod(p.months)}
              className={`w-10 h-10 rounded-xl text-[9px] font-bold uppercase transition-all duration-200 flex items-center justify-center shadow-lg ${
                period === p.months
                  ? 'bg-indigo-600 text-white ring-2 ring-indigo-500/20 shadow-indigo-600/20'
                  : 'text-slate-500 hover:text-slate-300 hover:bg-white/[0.05] bg-[#1a1d2e]/40 border border-white/5'
              }`}
            >
              {p.label}
            </button>
          ))}
        </div>

        {/* Upload buttons — bottom right */}
        <div
          className="absolute bottom-4 right-8 flex flex-col items-end gap-2 z-20"
          onMouseEnter={() => setUploadExpanded(true)}
          onMouseLeave={() => setUploadExpanded(false)}
        >
          {/* Expanded options */}
          <div className="flex flex-col items-end gap-2">
            {([
              { label: 'IBKR FlexQuery',   accept: '.xml',  onChange: handleUpload,               labelCls: 'text-indigo-400',  btnCls: 'bg-indigo-500/10 text-indigo-400 border-indigo-500/20 hover:bg-indigo-600',  delay: '150ms' },
              { label: 'E*Trade Benefits', accept: '.xlsx', onChange: handleEtradeBenefitsUpload, labelCls: 'text-emerald-400', btnCls: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20 hover:bg-emerald-600', delay: '75ms'  },
              { label: 'E*Trade Sales',    accept: '.xlsx', onChange: handleEtradeSalesUpload,    labelCls: 'text-amber-400',   btnCls: 'bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-600',         delay: '0ms'   },
            ] as const).map(({ label, accept, onChange, labelCls, btnCls, delay }) => (
              <label
                key={label}
                className="flex items-center gap-3 cursor-pointer"
                style={{
                  opacity: uploadExpanded ? 1 : 0,
                  transform: uploadExpanded ? 'translateY(0) scale(1)' : 'translateY(6px) scale(0.97)',
                  transition: `opacity 200ms ease ${uploadExpanded ? delay : '0ms'}, transform 200ms ease ${uploadExpanded ? delay : '0ms'}`,
                  pointerEvents: uploadExpanded ? 'auto' : 'none',
                }}
              >
                <span className={`text-[9px] font-black uppercase tracking-[0.2em] whitespace-nowrap ${labelCls}`}>{label}</span>
                <div className={`w-10 h-10 rounded-xl flex items-center justify-center border hover:text-white transition-all shadow-lg active:scale-95 ${btnCls}`}>
                  <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                </div>
                <input type="file" accept={accept} onChange={onChange} className="hidden" disabled={uploading} />
              </label>
            ))}
          </div>

          {/* Trigger button */}
          <div className={`w-10 h-10 rounded-xl flex items-center justify-center cursor-default transition-all duration-200 shadow-lg ${uploadExpanded ? 'bg-indigo-600 text-white' : 'bg-indigo-500/10 text-indigo-400 border border-indigo-500/20'}`}>
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
          </div>
        </div>

        {/* Status messages — bottom left */}
        <div className="absolute bottom-4 left-8">
          {uploading && (
            <div className="flex items-center gap-4 text-slate-400 text-[10px] font-black uppercase tracking-[0.3em] bg-[#1a1d2e]/80 px-6 py-3 rounded-2xl border border-white/5 shadow-2xl backdrop-blur-3xl">
              <div className="w-3 h-3 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin" />
              Processing DataMatrix…
            </div>
          )}
          {uploadMsg && (
            <div className="px-6 py-3 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-[10px] font-black uppercase tracking-[0.2em] rounded-2xl animate-fade-in shadow-2xl shadow-emerald-500/10 backdrop-blur-3xl">
              {uploadMsg}
            </div>
          )}
          {error && (
            <div className="px-6 py-3 bg-red-500/10 border border-red-500/20 text-red-400 text-[10px] font-black uppercase tracking-[0.2em] rounded-2xl animate-fade-in shadow-2xl shadow-red-500/10 backdrop-blur-3xl">
              {error}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
