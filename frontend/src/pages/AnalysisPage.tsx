import { useState, useCallback, useEffect } from 'react'
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend } from 'recharts'
import NavBar from '../components/NavBar'
import {
  getPortfolioStats, getPortfolioHistory, getMarketHistory, comparePortfolio,
  type StatsResponse, type DailyValue, type BenchmarkResult,
} from '../api'
import { formatDate, CURRENCIES_WITH_ORIGINAL } from '../utils/format'

const PERIODS = [
  { label: '1M', months: 1 },
  { label: '3M', months: 3 },
  { label: '6M', months: 6 },
  { label: '1Y', months: 12 },
  { label: 'Inception', months: 0 },
]
const COLORS = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#06b6d4', '#ec4899', '#8b5cf6']

function getFromDate(months: number): string {
  if (months === 0) return '2000-01-01'
  const d = new Date(); d.setMonth(d.getMonth() - months)
  return formatDate(d)
}

export default function AnalysisPage() {
  const [currency, setCurrency] = useState('CZK')
  const [period, setPeriod] = useState(0)
  const [acctModel, setAcctModel] = useState<'historical' | 'spot'>('historical')
  const [stats, setStats] = useState<StatsResponse | null>(null)
  const [portfolioHistory, setPortfolioHistory] = useState<DailyValue[]>([])
  const [benchmarkInput, setBenchmarkInput] = useState('SPY')
  const [benchmarkSymbols, setBenchmarkSymbols] = useState<string[]>([])
  const [benchmarkData, setBenchmarkData] = useState<Record<string, { date: string; close: number }[]>>({})
  const [compareResults, setCompareResults] = useState<BenchmarkResult[]>([])
  const [loading, setLoading] = useState(true)
  const [compareLoading, setCompareLoading] = useState(false)
  const [error, setError] = useState('')

  const from = getFromDate(period)
  const to = formatDate(new Date())

  // Load stats + portfolio history
  const loadData = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const [st, hist] = await Promise.all([
        getPortfolioStats(from, to, currency, acctModel),
        getPortfolioHistory(from, to, currency, acctModel),
      ])
      setStats(st)
      setPortfolioHistory(hist.data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [currency, period, acctModel])

  useEffect(() => { loadData() }, [loadData])

  // Handle benchmark comparison
  const handleCompare = async () => {
    const symbols = benchmarkInput.split(',').map(s => s.trim()).filter(Boolean)
    if (symbols.length === 0) return
    setBenchmarkSymbols(symbols)
    setCompareLoading(true)
    try {
      // Fetch benchmark price history for charting
      const histPromises = symbols.map(sym => getMarketHistory(sym, from, to))
      const histResults = await Promise.all(histPromises)
      const newData: Record<string, { date: string; close: number }[]> = {}
      histResults.forEach((res, i) => {
        newData[symbols[i]] = res.data.map(p => ({ date: p.date.slice(0, 10), close: p.close }))
      })
      setBenchmarkData(newData)

      // Fetch comparison metrics
      const comp = await comparePortfolio(symbols.join(','), currency, from, to, acctModel)
      setCompareResults(comp.benchmarks)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Comparison failed')
    } finally {
      setCompareLoading(false)
    }
  }

  // Build merged chart data for overlay: normalize to 100 at start
  const mergedChartData = (() => {
    if (portfolioHistory.length === 0) return []

    const firstPortVal = portfolioHistory[0]?.value || 1
    const dateMap: Record<string, Record<string, number>> = {}

    portfolioHistory.forEach(d => {
      if (!dateMap[d.date]) dateMap[d.date] = {}
      dateMap[d.date]['Portfolio'] = (d.value / firstPortVal) * 100
    })

    benchmarkSymbols.forEach(sym => {
      const data = benchmarkData[sym]
      if (!data || data.length === 0) return
      const first = data[0].close || 1
      data.forEach(d => {
        if (!dateMap[d.date]) dateMap[d.date] = {}
        dateMap[d.date][sym] = (d.close / first) * 100
      })
    })

    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  })()

  return (
    <div className="min-h-screen bg-[#0f1117] flex flex-col">
      <NavBar />
      <main className="flex-1 flex items-center justify-center py-8">
        <div className="max-w-7xl w-full px-6">
        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Analysis</h1>
            <p className="text-slate-400 text-sm mt-1">Performance statistics &amp; benchmark comparison</p>
          </div>
        </div>

        {/* Controls bar */}
        <div className="flex items-center gap-4 mb-8 flex-wrap">
          <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-xl p-1 border border-[#2a2e42]">
            {CURRENCIES_WITH_ORIGINAL.map(cur => (
              <button key={cur} onClick={() => setCurrency(cur)}
                className={`px-4 py-2 rounded-xl text-xs font-semibold transition-all ${
                  currency === cur ? 'bg-indigo-500/20 text-indigo-400' : 'text-slate-400 hover:text-slate-200'
                }`}
              >{cur}</button>
            ))}
          </div>
          <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-xl p-1 border border-[#2a2e42]">
            {PERIODS.map(p => (
              <button key={p.label} onClick={() => setPeriod(p.months)}
                className={`px-4 py-2 rounded-xl text-xs font-semibold transition-all ${
                  period === p.months ? 'bg-white/10 text-slate-200' : 'text-slate-500 hover:text-slate-300'
                }`}
              >{p.label}</button>
            ))}
          </div>
          <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-xl p-1 border border-[#2a2e42]">
            {(['historical', 'spot'] as const).map(m => (
              <button key={m} onClick={() => setAcctModel(m)}
                className={`px-4 py-2 rounded-xl text-xs font-semibold transition-all ${
                  acctModel === m ? 'bg-white/10 text-slate-200' : 'text-slate-500 hover:text-slate-300'
                }`}
              >{m.charAt(0).toUpperCase() + m.slice(1)}</button>
            ))}
          </div>
        </div>

        {error && (
          <div className="mb-4 px-4 py-2 rounded-lg bg-red-500/10 text-red-400 text-sm border border-red-500/20">{error}</div>
        )}

        {/* Row 1: Portfolio Stats */}
        <div className="mb-8">
          <h2 className="text-lg font-semibold text-slate-200 mb-4">Portfolio Statistics</h2>
          {loading ? (
            <div className="text-center py-8 text-slate-500">Loading statistics...</div>
          ) : stats ? (
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              {Object.entries(stats.statistics).map(([key, val]) => {
                const numVal = typeof val === 'number' ? val : null
                if (numVal === null) return null
                return (
                  <div key={key} className="bg-[#1a1d2e] rounded-xl px-7 py-5 border border-[#2a2e42]">
                    <p className="text-xs text-slate-400 uppercase tracking-wider font-medium">{key}</p>
                    <p className={`text-xl font-bold mt-2 ${numVal >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                      {(numVal * 100).toFixed(2)}%
                    </p>
                  </div>
                )
              })}
            </div>
          ) : (
            <p className="text-slate-500">No statistics available</p>
          )}
        </div>

        {/* Row 2: Benchmark Comparison */}
        <div className="bg-[#1a1d2e] rounded-2xl border border-[#2a2e42] p-6">
          <h2 className="text-lg font-semibold text-slate-200 mb-4">Benchmark Comparison</h2>

          {/* Input */}
          <div className="flex items-center gap-3 mb-6">
            <input
              type="text" value={benchmarkInput} onChange={e => setBenchmarkInput(e.target.value)}
              placeholder="Enter symbols, e.g. SPY, QQQ, VWCE.DE"
              className="flex-1 px-4 py-2.5 bg-[#0f1117] border border-[#2a2e42] rounded-xl text-sm text-slate-200 placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/50 focus:border-indigo-500 transition-all"
              onKeyDown={e => e.key === 'Enter' && handleCompare()}
            />
            <button
              onClick={handleCompare} disabled={compareLoading}
              className="px-6 py-2.5 bg-indigo-500 text-white text-sm font-semibold rounded-xl hover:bg-indigo-600 transition-all disabled:opacity-50 shadow-lg shadow-indigo-500/20"
            >
              {compareLoading ? 'Loading...' : 'Compare'}
            </button>
          </div>

          {/* Comparison chart: normalized to 100 */}
          {mergedChartData.length > 0 && (
            <div className="h-[350px] mb-6">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={mergedChartData} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" />
                  <XAxis dataKey="date" tick={{ fontSize: 11, fill: '#64748b' }} tickLine={false} axisLine={false} />
                  <YAxis tick={{ fontSize: 11, fill: '#64748b' }} tickLine={false} axisLine={false}
                    tickFormatter={(v: number) => `${v.toFixed(0)}`}
                  />
                  <Tooltip
                    contentStyle={{ background: '#1a1d2e', border: '1px solid #2a2e42', borderRadius: '12px', fontSize: '13px', color: '#e2e8f0' }}
                    labelStyle={{ color: '#94a3b8', marginBottom: '4px' }}
                    formatter={(value, name) => [`${Number(value).toFixed(2)}`, String(name)]}
                  />
                  <Legend wrapperStyle={{ fontSize: '12px', color: '#94a3b8' }} />
                  <Line type="monotone" dataKey="Portfolio" stroke={COLORS[0]} strokeWidth={2} dot={false} />
                  {benchmarkSymbols.map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} dot={false} />
                  ))}
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}

          {/* Comparison table */}
          {compareResults.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-[#2a2e42]">
                    <th className="text-left py-3 px-4 text-xs font-medium text-slate-400 uppercase">Benchmark</th>
                    <th className="text-right py-3 px-4 text-xs font-medium text-slate-400 uppercase">Alpha</th>
                    <th className="text-right py-3 px-4 text-xs font-medium text-slate-400 uppercase">Beta</th>
                    <th className="text-right py-3 px-4 text-xs font-medium text-slate-400 uppercase">Sharpe</th>
                    <th className="text-right py-3 px-4 text-xs font-medium text-slate-400 uppercase">Treynor</th>
                    <th className="text-right py-3 px-4 text-xs font-medium text-slate-400 uppercase">Tracking Err</th>
                    <th className="text-right py-3 px-4 text-xs font-medium text-slate-400 uppercase">Info Ratio</th>
                    <th className="text-right py-3 px-4 text-xs font-medium text-slate-400 uppercase">Correlation</th>
                  </tr>
                </thead>
                <tbody>
                  {compareResults.map(bm => (
                    <tr key={bm.symbol} className="border-b border-[#2a2e42]/50 hover:bg-[#232740] transition-colors">
                      <td className="py-3 px-4 font-medium text-slate-200">{bm.symbol}</td>
                      <td className={`py-3 px-4 text-right ${bm.alpha >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{(bm.alpha * 100).toFixed(2)}%</td>
                      <td className="py-3 px-4 text-right text-slate-300">{bm.beta.toFixed(3)}</td>
                      <td className={`py-3 px-4 text-right ${bm.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{bm.sharpe_ratio.toFixed(3)}</td>
                      <td className="py-3 px-4 text-right text-slate-300">{bm.treynor_ratio.toFixed(4)}</td>
                      <td className="py-3 px-4 text-right text-slate-300">{(bm.tracking_error * 100).toFixed(2)}%</td>
                      <td className="py-3 px-4 text-right text-slate-300">{bm.information_ratio.toFixed(3)}</td>
                      <td className="py-3 px-4 text-right text-slate-300">{bm.correlation.toFixed(3)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {benchmarkSymbols.length === 0 && (
            <p className="text-center text-slate-500 text-sm py-8">Enter benchmark symbols above to compare against your portfolio</p>
          )}
        </div>
        </div>
      </main>
    </div>
  )
}
