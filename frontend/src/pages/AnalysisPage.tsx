import { useState, useCallback, useEffect } from 'react'
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend } from 'recharts'
import PageLayout from '../components/PageLayout'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import DateRangePicker from '../components/DateRangePicker'
import {
  getPortfolioStats, getPortfolioReturns, getMarketHistory, comparePortfolio,
  type StatsResponse, type DailyValue, type BenchmarkResult,
} from '../api'
import { formatDate, CURRENCIES } from '../utils/format'

const CURRENCY_OPTIONS = CURRENCIES.map(c => ({ label: c, value: c }))

const FX_METHOD_OPTIONS = [
  { label: 'Historical', value: 'historical' as const, tooltip: 'Uses the FX rate at the time each trade was executed. Reflects your true cost basis in the currency, accounting for currency movements over time.' },
  { label: 'Spot',       value: 'spot'       as const, tooltip: "Applies today's FX rate to all prices. Shows current market value converted at the current exchange rate, regardless of when trades were made." },
]

const PERIOD_OPTIONS = [
  { label: '1M',     value: 1  },
  { label: '3M',     value: 3  },
  { label: '6M',     value: 6  },
  { label: '1Y',     value: 12 },
  { label: 'All',    value: 0  },
  { label: 'Custom', value: -1 },
]

const COLORS = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#06b6d4', '#ec4899', '#8b5cf6']

function getFromDate(months: number): string {
  if (months === 0) return '2000-01-01'
  const d = new Date(); d.setMonth(d.getMonth() - months)
  return formatDate(d)
}


export default function AnalysisPage() {
  const [currency, setCurrency]   = useState('CZK')
  const [period, setPeriod]       = useState(0)
  const [acctModel, setAcctModel] = useState<'historical' | 'spot'>('historical')
  const [customFrom, setCustomFrom] = useState(() => getFromDate(12))
  const [customTo,   setCustomTo]   = useState(() => formatDate(new Date()))
  const [stats, setStats]           = useState<StatsResponse | null>(null)
  const [portfolioHistory, setPortfolioHistory] = useState<DailyValue[]>([])
  const [benchmarkInput, setBenchmarkInput]     = useState('SPY')
  const [benchmarkSymbols, setBenchmarkSymbols] = useState<string[]>([])
  const [benchmarkData, setBenchmarkData]       = useState<Record<string, { date: string; close: number }[]>>({})
  const [compareResults, setCompareResults]     = useState<BenchmarkResult[]>([])
  const [riskFreeRate, setRiskFreeRate]       = useState(0.025)
  const [riskFreeRateInput, setRiskFreeRateInput] = useState('2.50')
  const [loading, setLoading]           = useState(true)
  const [compareLoading, setCompareLoading] = useState(false)
  const [error, setError]               = useState('')
  const [compareError, setCompareError] = useState('')

  const from = period === -1 ? customFrom : getFromDate(period)
  const to   = period === -1 ? customTo   : formatDate(new Date())
  // For "All", getFromDate returns a dummy sentinel. Use the actual first data point
  // from the loaded portfolio history so benchmark calls start at portfolio inception.
  const effectiveFrom = period === 0 ? (portfolioHistory[0]?.date ?? from) : from

  const loadData = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const [st, hist] = await Promise.all([
        getPortfolioStats(from, to, currency, acctModel),
        getPortfolioReturns(from, to, currency, acctModel),
      ])
      setStats(st)
      setPortfolioHistory(hist.data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [currency, acctModel, from, to])

  useEffect(() => { loadData() }, [loadData])

  // Re-fetch benchmark data whenever the date range, currency, or model changes.
  // benchmarkSymbols and riskFreeRate are intentionally omitted: symbol additions are
  // handled by handleCompare, and rate changes are handled by applyRiskFreeRate.
  useEffect(() => {
    if (benchmarkSymbols.length === 0) return
    const refresh = async () => {
      setCompareLoading(true)
      setCompareError('')
      try {
        const [histResults, comp] = await Promise.all([
          Promise.all(benchmarkSymbols.map(sym => getMarketHistory(sym, effectiveFrom, to, currency, acctModel))),
          comparePortfolio(benchmarkSymbols.join(','), currency, effectiveFrom, to, acctModel, riskFreeRate),
        ])
        const newData: Record<string, { date: string; close: number }[]> = {}
        histResults.forEach((res, i) => {
          newData[benchmarkSymbols[i]] = res.data.map(p => ({ date: p.date.slice(0, 10), close: p.close }))
        })
        setBenchmarkData(newData)
        setCompareResults(comp.benchmarks)
      } catch (err) {
        setCompareError(err instanceof Error ? err.message : 'Comparison failed')
      } finally {
        setCompareLoading(false)
      }
    }
    refresh()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveFrom, to, currency, acctModel])

  const handleCompare = async () => {
    const inputSymbols = benchmarkInput.split(',').map(s => s.trim()).filter(Boolean)
    const newSymbols = inputSymbols.filter(s => !benchmarkSymbols.includes(s))
    if (newSymbols.length === 0) { setBenchmarkInput(''); return }
    setCompareLoading(true)
    setCompareError('')
    try {
      const histResults = await Promise.all(newSymbols.map(sym => getMarketHistory(sym, effectiveFrom, to, currency, acctModel)))
      const newData: Record<string, { date: string; close: number }[]> = {}
      histResults.forEach((res, i) => {
        newData[newSymbols[i]] = res.data.map(p => ({ date: p.date.slice(0, 10), close: p.close }))
      })
      setBenchmarkData(prev => ({ ...prev, ...newData }))
      const comp = await comparePortfolio(newSymbols.join(','), currency, effectiveFrom, to, acctModel, riskFreeRate)
      setBenchmarkSymbols(prev => [...prev, ...newSymbols])
      setCompareResults(prev => [...prev, ...comp.benchmarks])
      setBenchmarkInput('')
    } catch (err) {
      setCompareError(err instanceof Error ? err.message : 'Comparison failed')
    } finally {
      setCompareLoading(false)
    }
  }

  const applyRiskFreeRate = async (newRate: number) => {
    setRiskFreeRateInput((newRate * 100).toFixed(2))
    if (newRate === riskFreeRate) return
    setRiskFreeRate(newRate)
    if (benchmarkSymbols.length === 0) return
    setCompareLoading(true)
    setCompareError('')
    try {
      const comp = await comparePortfolio(benchmarkSymbols.join(','), currency, effectiveFrom, to, acctModel, newRate)
      setCompareResults(comp.benchmarks)
    } catch (err) {
      setCompareError(err instanceof Error ? err.message : 'Comparison failed')
    } finally {
      setCompareLoading(false)
    }
  }

  const handleRiskFreeRateBlur = () => {
    const parsed = parseFloat(riskFreeRateInput)
    applyRiskFreeRate(isNaN(parsed) ? riskFreeRate : Math.max(0, Math.min(20, parsed)) / 100)
  }

  const handleRemoveSymbol = (sym: string) => {
    setBenchmarkSymbols(prev => prev.filter(s => s !== sym))
    setBenchmarkData(prev => { const next = { ...prev }; delete next[sym]; return next })
    setCompareResults(prev => prev.filter(r => r.symbol !== sym))
  }

  const mergedChartData = (() => {
    if (portfolioHistory.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    portfolioHistory.forEach(d => {
      if (!dateMap[d.date]) dateMap[d.date] = {}
      dateMap[d.date]['Portfolio'] = d.value
    })
    benchmarkSymbols.forEach(sym => {
      const data = benchmarkData[sym]
      if (!data || data.length === 0) return
      const first = data[0].close || 1
      data.forEach(d => {
        if (!dateMap[d.date]) dateMap[d.date] = {}
        dateMap[d.date][sym] = (d.close / first - 1) * 100
      })
    })
    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  })()

  return (
    <PageLayout>
          {/* Header */}
          <div className="w-full flex flex-col items-center mb-16 text-center">
            <h1 className="text-3xl font-semibold text-slate-100">Performance Analysis</h1>
            <p className="text-slate-500 text-sm mt-4">Statistical attribution and benchmark alignment</p>
          </div>

          {/* Controls */}
          <div className={`flex flex-wrap justify-center gap-4 ${period === -1 ? 'mb-4' : 'mb-20'}`}>
            <SegmentedControl label="Currency" options={CURRENCY_OPTIONS} value={currency} onChange={setCurrency} />
            <SegmentedControl
              label="Time Period"
              options={PERIOD_OPTIONS}
              value={period}
              onChange={p => {
                if (p === -1 && period !== -1) {
                  setCustomFrom(getFromDate(period === 0 ? 12 : period))
                  setCustomTo(formatDate(new Date()))
                }
                setPeriod(p)
              }}
            />
            <SegmentedControl label="FX Method" options={FX_METHOD_OPTIONS} value={acctModel} onChange={setAcctModel} />
          </div>

          {/* Custom date picker */}
          {period === -1 && (
            <div className="mb-16">
              <DateRangePicker
                initialFrom={customFrom}
                initialTo={customTo}
                onApply={(f, t) => { setCustomFrom(f); setCustomTo(t) }}
              />
            </div>
          )}

          {error && (
            <div className="w-full mb-10 px-8 py-4 rounded-xl bg-red-500/10 text-red-400 text-sm font-medium border border-red-500/20 text-center">
              {error}
            </div>
          )}

          {/* Stats */}
          <div className="w-full mb-16">
            <h2 className="text-xl font-semibold text-slate-100 mb-8 text-center">Risk & Return Metrics</h2>
            {loading ? (
              <Spinner label="Compiling statistics…" className="py-10" />
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

          {/* Benchmarking */}
          <div className="w-full">
            <h2 className="text-xl font-semibold text-slate-100 mb-8 text-center">Benchmarking</h2>

            <div className="flex flex-col items-center gap-3 mb-14 w-full max-w-2xl mx-auto">
              <div className="flex flex-col sm:flex-row items-center w-full gap-4">
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
              <div className="flex items-center gap-3 self-start">
                <label className="text-xs font-semibold text-slate-500 whitespace-nowrap uppercase tracking-widest">Risk-free rate</label>
                <div className="flex items-center gap-1.5">
                  <div className="relative flex items-center">
                    <input
                      type="number" value={riskFreeRateInput}
                      onChange={e => setRiskFreeRateInput(e.target.value)}
                      onBlur={handleRiskFreeRateBlur}
                      onKeyDown={e => e.key === 'Enter' && e.currentTarget.blur()}
                      step="0.1" min="0" max="20"
                      className="w-20 px-3 py-2 pr-6 bg-[#1a1d2e] border border-[#2a2e42]/60 rounded-xl text-sm text-slate-200 text-right focus:outline-none focus:ring-2 focus:ring-indigo-500/40 transition-all [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                    />
                    <div className="absolute right-1.5 flex flex-col">
                      <button
                        type="button" tabIndex={-1}
                        onClick={() => applyRiskFreeRate(Math.min(0.20, Math.round((riskFreeRate + 0.001) * 1000) / 1000))}
                        className="flex items-center justify-center w-4 h-3.5 text-slate-600 hover:text-slate-300 transition-colors"
                      >
                        <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 4L4 1L7 4"/></svg>
                      </button>
                      <button
                        type="button" tabIndex={-1}
                        onClick={() => applyRiskFreeRate(Math.max(0, Math.round((riskFreeRate - 0.001) * 1000) / 1000))}
                        className="flex items-center justify-center w-4 h-3.5 text-slate-600 hover:text-slate-300 transition-colors"
                      >
                        <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 1L4 4L7 1"/></svg>
                      </button>
                    </div>
                  </div>
                  <span className="text-slate-500 text-sm select-none">%</span>
                </div>
              </div>
              {benchmarkSymbols.length > 0 && (
                <div className="flex flex-wrap gap-2 w-full">
                  {benchmarkSymbols.map(sym => (
                    <span key={sym} className="inline-flex items-center gap-2 px-3 py-1.5 bg-[#1a1d2e] border border-[#2a2e42]/60 rounded-lg text-sm font-medium text-slate-300">
                      {sym}
                      <button onClick={() => handleRemoveSymbol(sym)} className="text-slate-500 hover:text-red-400 transition-colors leading-none" aria-label={`Remove ${sym}`}>×</button>
                    </span>
                  ))}
                </div>
              )}
              {compareError && (
                <p className="w-full px-4 py-3 rounded-xl bg-red-500/10 text-red-400 text-sm border border-red-500/20">{compareError}</p>
              )}
            </div>

            {mergedChartData.length > 0 ? (
              <div className="h-100 mb-16 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={mergedChartData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" vertical={false} opacity={0.3} />
                    <XAxis
                      dataKey="date"
                      tickFormatter={val => new Date(val).toLocaleString('default', { month: 'short', year: '2-digit' })}
                      minTickGap={60}
                      tick={{ fontSize: 10, fill: '#475569' }}
                      axisLine={{ stroke: '#2a2e42' }}
                      tickLine={false}
                      label={{ value: 'Date', position: 'insideBottom', offset: -16, fontSize: 10, fill: '#334155', fontWeight: 900 }}
                    />
                    <YAxis
                      domain={['auto', 'auto']}
                      tickFormatter={val => `${Number(val).toFixed(0)}%`}
                      tick={{ fontSize: 10, fill: '#475569' }}
                      axisLine={false}
                      tickLine={false}
                      width={56}
                      label={{ value: 'Return (%)', angle: -90, position: 'insideLeft', offset: 16, fontSize: 10, fill: '#334155', fontWeight: 900 }}
                    />
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
                      <tr key={bm.symbol} className="hover:bg-white/2 transition-colors group">
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

    </PageLayout>
  )
}
