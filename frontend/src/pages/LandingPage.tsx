import { useState, useEffect, useCallback, useRef } from 'react'
import { XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Area, AreaChart } from 'recharts'
import NavBar from '../components/NavBar'
import {
  getPortfolioValue, getPortfolioHistory, getPortfolioStats, getPortfolioReturns,
  uploadFlexQuery, uploadEtradeBenefits, uploadEtradeSales,
  type PortfolioValueResponse, type DailyValue,
} from '../api'

const CURRENCIES = ['CZK', 'USD', 'EUR']
const PERIODS = [
  { label: '1M', months: 1 },
  { label: '3M', months: 3 },
  { label: '6M', months: 6 },
  { label: '1Y', months: 12 },
  { label: 'All', months: 0 },
]

function formatCurrency(value: number, currency: string): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency', currency, minimumFractionDigits: 0, maximumFractionDigits: 0,
  }).format(value)
}

function formatDate(d: Date): string {
  return d.toISOString().slice(0, 10)
}

function getFromDate(months: number): string {
  if (months === 0) return '2000-01-01'
  const d = new Date()
  d.setMonth(d.getMonth() - months)
  return formatDate(d)
}

export default function LandingPage() {
  const [currency, setCurrency] = useState('CZK')
  const [period, setPeriod] = useState(0)
  const [chartMode, setChartMode] = useState<'value' | 'twr' | 'mwr'>('value')
  const [portfolioValue, setPortfolioValue] = useState<PortfolioValueResponse | null>(null)
  const [history, setHistory] = useState<DailyValue[]>([])
  const [twrHistory, setTwrHistory] = useState<DailyValue[]>([]) // real TWR curve from backend
  const [stats, setStats] = useState<Record<string, unknown> | null>(null)
  const [loading, setLoading] = useState(true)
  const [chartLoading, setChartLoading] = useState(false)
  const [error, setError] = useState('')
  const [uploadMsg, setUploadMsg] = useState('')
  const [uploading, setUploading] = useState(false)

  // Ref to track the current load generation — used to ignore stale responses (fix 2.3).
  const loadGenRef = useRef(0)

  const loadData = useCallback(async () => {
    loadGenRef.current += 1
    const gen = loadGenRef.current
    setLoading(true)
    setError('')
    try {
      const [val, hist, st] = await Promise.all([
        getPortfolioValue(CURRENCIES.join(',')),
        getPortfolioHistory(getFromDate(period), formatDate(new Date()), currency),
        getPortfolioStats(getFromDate(period), formatDate(new Date()), currency),
      ])
      // Ignore if a newer call already started.
      if (gen !== loadGenRef.current) return
      setPortfolioValue(val)
      setHistory(hist.data)
      setStats(st.statistics)
    } catch (err) {
      if (gen !== loadGenRef.current) return
      setError(err instanceof Error ? err.message : 'Failed to load data')
    } finally {
      if (gen === loadGenRef.current) setLoading(false)
    }
  }, [currency, period])

  useEffect(() => { loadData() }, [loadData])

  // Reload chart (value history + real TWR curve) when currency/period/chartMode changes.
  // AbortController cancels any in-flight request when deps change (fix 2.3).
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

        // Always keep the TWR series fresh — it's cheap once the value history is cached.
        const twrHist = await getPortfolioReturns(from, to, currency, 'historical', controller.signal)
        if (cancelled) return
        setTwrHistory(twrHist.data)
      } catch {
        // Errors already surfaced by loadData; silently swallow here.
      } finally {
        if (!cancelled) setChartLoading(false)
      }
    }

    fetchChart()

    return () => {
      cancelled = true
      controller.abort()
    }
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

  // Build chart data based on mode.
  // - 'value': raw portfolio value
  // - 'twr': real cumulative TWR % from backend (fix 1.3)
  // - 'mwr': flat scalar from the stats endpoint shown as a reference (fix 1.3, partial)
  const chartData = (() => {
    if (chartMode === 'value') {
      return history.map(d => ({ date: d.date, value: d.value }))
    }
    if (chartMode === 'twr') {
      return twrHistory.map(d => ({ date: d.date, value: d.value }))
    }
    // MWR: the backend only returns a scalar MWR for the full period.
    // Render a flat reference line showing the MWR % alongside the simple growth curve
    // so the user can see how the time-weighted measure compares visually.
    const mwr = typeof stats?.mwr === 'number' ? (stats.mwr as number) * 100 : null
    return history.map(d => {
      const firstValue = history[0]?.value || 1
      const simpleGrowth = firstValue > 0 ? ((d.value / firstValue) - 1) * 100 : 0
      return { date: d.date, value: simpleGrowth, mwr: mwr ?? 0 }
    })
  })()

  const chartLabel = chartMode === 'value'
    ? `Portfolio Value (${currency})`
    : chartMode === 'twr' ? 'Cumulative TWR (%)' : 'Simple Growth vs MWR (%)'

  return (
    <div className="min-h-screen bg-[#0f1117] flex flex-col">
      <NavBar />
      <main className="flex-1 flex items-center justify-center py-8">
        <div className="max-w-7xl w-full px-6">
        {/* Header row */}
        <div className="flex items-start justify-between mb-8">
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Dashboard</h1>
            <p className="text-slate-400 text-sm mt-1">Your portfolio overview</p>
          </div>
          <div className="flex gap-3">
            <label className={`px-5 py-2.5 rounded-xl text-sm font-semibold cursor-pointer transition-all duration-200 shadow-lg shadow-indigo-500/20 ${uploading ? 'bg-slate-700 text-slate-400 cursor-not-allowed' : 'bg-indigo-500/15 text-indigo-400 hover:bg-indigo-500/25 border border-indigo-500/30'}`}>
              {uploading ? 'Uploading...' : '↑ FlexQuery'}
              <input type="file" accept=".xml" onChange={handleUpload} className="hidden" disabled={uploading} />
            </label>
            <label className={`px-5 py-2.5 rounded-xl text-sm font-semibold cursor-pointer transition-all duration-200 shadow-lg shadow-emerald-500/20 ${uploading ? 'bg-slate-700 text-slate-400 cursor-not-allowed' : 'bg-emerald-500/15 text-emerald-400 hover:bg-emerald-500/25 border border-emerald-500/30'}`}>
              {uploading ? 'Uploading...' : '↑ E*Trade Benefits'}
              <input type="file" accept=".xlsx" onChange={handleEtradeBenefitsUpload} className="hidden" disabled={uploading} />
            </label>
            <label className={`px-5 py-2.5 rounded-xl text-sm font-semibold cursor-pointer transition-all duration-200 shadow-lg shadow-amber-500/20 ${uploading ? 'bg-slate-700 text-slate-400 cursor-not-allowed' : 'bg-amber-500/15 text-amber-400 hover:bg-amber-500/25 border border-amber-500/30'}`}>
              {uploading ? 'Uploading...' : '↑ E*Trade Sales'}
              <input type="file" accept=".xlsx" onChange={handleEtradeSalesUpload} className="hidden" disabled={uploading} />
            </label>
          </div>
        </div>

        {uploadMsg && (
          <div className="mb-4 px-4 py-2 rounded-lg bg-emerald-500/10 text-emerald-400 text-sm border border-emerald-500/20">
            {uploadMsg}
          </div>
        )}

        {error && (
          <div className="mb-4 px-4 py-2 rounded-lg bg-red-500/10 text-red-400 text-sm border border-red-500/20">
            {error}
          </div>
        )}

        {/* Value cards */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-8">
          {CURRENCIES.map(cur => (
            <div key={cur}
              className={`bg-[#1a1d2e] rounded-2xl px-8 py-6 border transition-all duration-200 cursor-pointer ${
                cur === currency ? 'border-indigo-500/50 shadow-lg shadow-indigo-500/10' : 'border-[#2a2e42] hover:border-[#3a3f57]'
              }`}
              onClick={() => setCurrency(cur)}
            >
              <p className="text-xs text-slate-400 uppercase tracking-wider font-medium">{cur}</p>
              <p className="text-2xl font-bold mt-2 text-slate-100">
                {loading ? '—' : formatCurrency(portfolioValue?.values[cur] ?? 0, cur)}
              </p>
            </div>
          ))}
        </div>

        {/* Chart controls */}
        <div className="bg-[#1a1d2e] rounded-2xl border border-[#2a2e42] p-6 mb-8">
          <div className="flex items-center justify-between mb-6 flex-wrap gap-4">
            <div className="flex items-center gap-2">
              {(['value', 'twr', 'mwr'] as const).map(mode => (
                <button key={mode} onClick={() => setChartMode(mode)}
                  className={`px-4 py-2 rounded-xl text-xs font-semibold transition-all ${
                    chartMode === mode ? 'bg-indigo-500/20 text-indigo-400' : 'text-slate-400 hover:text-slate-200 hover:bg-white/5'
                  }`}
                >
                  {mode === 'value' ? 'Value' : mode.toUpperCase()}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-1">
              {PERIODS.map(p => (
                <button key={p.label} onClick={() => setPeriod(p.months)}
                  className={`px-4 py-2 rounded-xl text-xs font-semibold transition-all ${
                    period === p.months ? 'bg-white/10 text-slate-200' : 'text-slate-500 hover:text-slate-300'
                  }`}
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>

          {chartMode === 'twr' && (
            <p className="text-xs text-slate-500 mb-3">
              Cumulative Time-Weighted Return — adjusts for deposits &amp; withdrawals so capital movements don't distort the performance metric.
            </p>
          )}

          <div className="h-[400px]">
            {chartLoading || loading ? (
              <div className="h-full flex items-center justify-center text-slate-500">Loading chart...</div>
            ) : chartData.length === 0 ? (
              <div className="h-full flex items-center justify-center text-slate-500">No data — upload a FlexQuery file to get started</div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
                  <defs>
                    <linearGradient id="chartGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#6366f1" stopOpacity={0.3} />
                      <stop offset="100%" stopColor="#6366f1" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" />
                  <XAxis dataKey="date" tick={{ fontSize: 11, fill: '#64748b' }} tickLine={false} axisLine={false} />
                  <YAxis tick={{ fontSize: 11, fill: '#64748b' }} tickLine={false} axisLine={false}
                    tickFormatter={(v: number) => chartMode === 'value' ? `${(v / 1000).toFixed(0)}k` : `${v.toFixed(1)}%`}
                  />
                  <Tooltip
                    contentStyle={{ background: '#1a1d2e', border: '1px solid #2a2e42', borderRadius: '12px', fontSize: '13px', color: '#e2e8f0' }}
                    labelStyle={{ color: '#94a3b8', marginBottom: '4px' }}
                    formatter={(value) => [
                      chartMode === 'value' ? formatCurrency(Number(value), currency) : `${Number(value).toFixed(2)}%`,
                      chartLabel
                    ]}
                  />
                  <Area type="monotone" dataKey="value" stroke="#6366f1" strokeWidth={2} fill="url(#chartGrad)" dot={false} />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </div>
        </div>

        {/* Stats row */}
        {stats && (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            {Object.entries(stats).map(([key, val]) => {
              if (typeof val !== 'number') return null
              return (
                <div key={key} className="bg-[#1a1d2e] rounded-xl px-6 py-4 border border-[#2a2e42]">
                  <p className="text-xs text-slate-400 uppercase tracking-wider">{key}</p>
                  <p className={`text-lg font-semibold mt-1 ${val >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                    {(val * 100).toFixed(2)}%
                  </p>
                </div>
              )
            })}
          </div>
        )}
        </div>
      </main>
    </div>
  )
}
