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
  { label: 'All', months: 0 },
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

  const handleCompare = async () => {
    const symbols = benchmarkInput.split(',').map(s => s.trim()).filter(Boolean)
    if (symbols.length === 0) return
    setBenchmarkSymbols(symbols)
    setCompareLoading(true)
    try {
      const histPromises = symbols.map(sym => getMarketHistory(sym, from, to))
      const histResults = await Promise.all(histPromises)
      const newData: Record<string, { date: string; close: number }[]> = {}
      histResults.forEach((res, i) => {
        newData[symbols[i]] = res.data.map(p => ({ date: p.date.slice(0, 10), close: p.close }))
      })
      setBenchmarkData(newData)
      const comp = await comparePortfolio(symbols.join(','), currency, from, to, acctModel)
      setCompareResults(comp.benchmarks)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Comparison failed')
    } finally {
      setCompareLoading(false)
    }
  }

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
      <div className="w-full flex-1 flex justify-center">
        <main className="py-10 px-12 max-w-7xl w-full flex flex-col items-center">
          
          {/* Header centered */}
          <div className="w-full flex flex-col items-center mb-16 text-center">
            <h1 className="text-3xl font-semibold text-slate-100">Performance Analysis</h1>
          <p className="text-slate-500 text-sm mt-4">Statistical attribution and benchmark alignment</p>
        </div>

        {/* Controls — centered, rounded-2xl */}
        <div className="flex flex-wrap justify-center gap-4 mb-20">
          <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-2xl p-1.5 border border-[#2a2e42]/50 shadow-xl shadow-black/30">
            {CURRENCIES_WITH_ORIGINAL.map(cur => (
              <button key={cur} onClick={() => setCurrency(cur)}
                className={`px-6 py-2 rounded-xl text-sm font-medium transition-all ${
                  currency === cur ? 'glass active text-indigo-300' : 'text-slate-500 hover:text-slate-300'
                }`}
              >{cur}</button>
            ))}
          </div>
          <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-2xl p-1.5 border border-[#2a2e42]/50 shadow-xl shadow-black/30">
            {PERIODS.map(p => (
              <button key={p.label} onClick={() => setPeriod(p.months)}
                className={`px-6 py-2 rounded-xl text-sm font-medium transition-all ${
                  period === p.months ? 'glass active text-indigo-300' : 'text-slate-500 hover:text-slate-300'
                }`}
              >{p.label}</button>
            ))}
          </div>
          <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-2xl p-1.5 border border-[#2a2e42]/50 shadow-xl shadow-black/30">
            {(['historical', 'spot'] as const).map(m => (
              <button key={m} onClick={() => setAcctModel(m)}
                className={`px-6 py-2 rounded-xl text-sm font-medium capitalize transition-all ${
                  acctModel === m ? 'glass active text-indigo-300' : 'text-slate-500 hover:text-slate-300'
                }`}
              >{m}</button>
            ))}
          </div>
        </div>

        {error && (
          <div className="w-full mb-10 px-8 py-4 rounded-xl bg-red-500/10 text-red-400 text-sm font-medium border border-red-500/20 text-center">
            {error}
          </div>
        )}

        {/* Stats Section */}
        <div className="w-full mb-16">
          <h2 className="text-xl font-semibold text-slate-100 mb-8 text-center">Risk & Return Metrics</h2>
          {loading ? (
            <div className="flex flex-col items-center justify-center py-10 gap-3 text-slate-500">
              <div className="w-5 h-5 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin" />
              <span className="text-sm">Compiling statistics…</span>
            </div>
          ) : stats ? (
            <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
              {Object.entries(stats.statistics).map(([key, val]) => {
                const numVal = typeof val === 'number' ? val : null
                if (numVal === null) return null
                return (
                  <div key={key} className="bg-[#1a1d2e]/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5">
                    <p className="text-sm font-medium text-slate-500 mb-2 capitalize">{key.replace(/_/g, ' ')}</p>
                    <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                      {numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%
                    </p>
                  </div>
                )
              })}
            </div>
          ) : (
            <p className="text-slate-500 text-center text-sm py-10">Historical context required to generate statistics.</p>
          )}
        </div>

        {/* Comparison section */}
        <div className="w-full">
          <h2 className="text-xl font-semibold text-slate-100 mb-8 text-center">Benchmarking</h2>

          <div className="flex flex-col sm:flex-row items-center justify-center gap-4 mb-14 w-full max-w-2xl mx-auto">
            <input
              type="text" value={benchmarkInput} onChange={e => setBenchmarkInput(e.target.value)}
              placeholder="Symbols (SPY, QQQ, VWCE.DE)"
              className="w-full px-6 py-3 bg-[#1a1d2e] border border-[#2a2e42]/60 rounded-xl text-sm text-slate-200 placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 transition-all"
              onKeyDown={e => e.key === 'Enter' && handleCompare()}
            />
            <button
              onClick={handleCompare} disabled={compareLoading}
              className="whitespace-nowrap px-8 py-3 bg-indigo-600 text-white text-sm font-medium rounded-xl hover:bg-indigo-500 transition-all disabled:opacity-50 shadow-lg"
            >
              {compareLoading ? 'Processing…' : 'Execute'}
            </button>
          </div>

          {mergedChartData.length > 0 ? (
            <div className="h-[400px] mb-16 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={mergedChartData} margin={{ top: 10, right: 10, left: 10, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" hide />
                  <YAxis domain={['auto', 'auto']} hide />
                  <Tooltip
                    contentStyle={{ background: 'rgba(26,29,46,0.95)', border: '1px solid rgba(99,102,241,0.25)', borderRadius: '18px', fontSize: '12px', color: '#e2e8f0', backdropFilter: 'blur(24px)' }}
                    labelStyle={{ color: '#6366f1', marginBottom: '8px', fontSize: '10px', fontWeight: '900', textTransform: 'uppercase', letterSpacing: '0.2em' }}
                    formatter={(value, name) => [`${Number(value).toFixed(2)}%`, String(name)]}
                  />
                  <Legend wrapperStyle={{ fontSize: '10px', color: '#64748b', paddingTop: '30px', fontWeight: '900', textTransform: 'uppercase', letterSpacing: '0.15em' }} />
                  <Line type="monotone" dataKey="Portfolio" stroke={COLORS[0]} strokeWidth={3} dot={false} animationDuration={1200} />
                  {benchmarkSymbols.map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
                  ))}
                </LineChart>
              </ResponsiveContainer>
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center py-20 gap-3 text-slate-600 opacity-60">
              <svg className="w-10 h-10" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" /></svg>
              <p className="text-sm font-medium">Ready to compare</p>
            </div>
          )}

          {compareResults.length > 0 && (
            <div className="overflow-x-auto w-full">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-[#2a2e42]/60">
                    {['Security','Alpha','Beta','Sharpe','Treynor','Tracking Err','Info Ratio','Correlation'].map(h => (
                      <th key={h} className={`py-4 px-4 text-xs font-semibold text-slate-500 ${h === 'Security' ? 'text-left' : 'text-right'}`}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#2a2e42]/30">
                  {compareResults.map(bm => (
                    <tr key={bm.symbol} className="hover:bg-white/[0.02] transition-colors group">
                      <td className="py-4 px-4 font-semibold text-slate-100 group-hover:text-indigo-400 transition-colors uppercase">{bm.symbol}</td>
                      <td className={`py-4 px-4 text-right font-medium tabular-nums ${bm.alpha >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{(bm.alpha * 100).toFixed(2)}%</td>
                      <td className="py-4 px-4 text-right text-slate-400 font-medium tabular-nums">{bm.beta.toFixed(3)}</td>
                      <td className={`py-4 px-4 text-right font-medium tabular-nums ${bm.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{bm.sharpe_ratio.toFixed(3)}</td>
                      <td className="py-4 px-4 text-right text-slate-400 font-medium">{bm.treynor_ratio.toFixed(4)}</td>
                      <td className="py-4 px-4 text-right text-slate-400 font-medium">{(bm.tracking_error * 100).toFixed(2)}%</td>
                      <td className="py-4 px-4 text-right text-slate-300 font-medium tabular-nums">{bm.information_ratio.toFixed(3)}</td>
                      <td className="py-4 px-4 text-right text-slate-400 font-medium">{bm.correlation.toFixed(3)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

      </main>
      </div>
    </div>
  )
}
