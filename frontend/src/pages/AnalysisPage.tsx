import { useState, useCallback, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend } from 'recharts'
import PageLayout from '../components/PageLayout'
import HoverTooltip from '../components/HoverTooltip'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import DateRangePicker from '../components/DateRangePicker'
import ErrorAlert from '../components/ErrorAlert'
import {
  getPortfolioStats, getPortfolioReturns, getMarketHistory, comparePortfolio, getStandaloneMetrics,
  type StatsResponse, type DailyValue, type BenchmarkResult, type StandaloneResult,
} from '../api'
import { formatDate, CURRENCIES, getFromDate } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'

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

const STAT_TOOLTIPS: Record<string, string> = {
  twr: 'Time-weighted return. Eliminates the effect of cash flows — best for evaluating portfolio manager skill.',
  mwr: 'Money-weighted return. Reflects your actual return including the timing and size of your deposits and withdrawals.',
}

const STANDALONE_TOOLTIPS: Record<string, string> = {
  sharpe:     'Excess return above the risk-free rate, divided by total volatility. Higher numbers mean better risk-adjusted performance.',
  vami:       'Value Added Monthly Index. Growth of a 1,000 investment — reflects compounded total return.',
  volatility: 'Annualized standard deviation of daily returns. Measures how much the portfolio fluctuates.',
  sortino:    'Like Sharpe, but only penalizes downside volatility below the risk-free rate, ignoring upside swings. Higher is better.',
  max_drawdown: 'Largest peak-to-trough decline over the period. Measures worst-case loss from a high point.',
}

const COMPARE_TOOLTIPS: Record<string, string> = {
  Security:      'The benchmark being compared against your portfolio.',
  Alpha:         'Annualized excess return over the benchmark after adjusting for market risk. Positive means outperformance.',
  Beta:          'Sensitivity to benchmark moves. Beta > 1 means your portfolio is more volatile than the benchmark.',
  Treynor:       'Return per unit of systematic (market) risk, using beta as the risk measure. Higher is better.',
  'Tracking Err':'Annualized deviation of your returns from the benchmark. Lower means closer tracking.',
  'Info Ratio':  'Active return divided by tracking error. Measures the consistency of outperformance.',
  Correlation:   'How closely your returns move with the benchmark. 1 = perfect alignment, 0 = no relationship.',
}



export default function AnalysisPage() {
  const navigate = useNavigate()
  const [currency, setCurrency]   = usePersistentState<string>('app_currency', 'CZK')
  const [period, setPeriod]       = usePersistentState('analysis_period', 0)
  const [acctModel, setAcctModel] = usePersistentState<'historical' | 'spot'>('analysis_acctModel', 'historical')
  const [customFrom, setCustomFrom] = useState(() => getFromDate(12))
  const [customTo,   setCustomTo]   = useState(() => formatDate(new Date()))
  const [stats, setStats]           = useState<StatsResponse | null>(null)
  const [portfolioHistory, setPortfolioHistory] = useState<DailyValue[]>([])
  const [benchmarkInput, setBenchmarkInput]     = useState('SPY')
  const [benchmarkSymbols, setBenchmarkSymbols] = useState<string[]>([])
  const [benchmarkData, setBenchmarkData]       = useState<Record<string, { date: string; close: number }[]>>({})
  const [compareResults, setCompareResults]     = useState<BenchmarkResult[]>([])
  const [standaloneResults, setStandaloneResults] = useState<StandaloneResult[]>([])
  const [riskFreeRate, setRiskFreeRate]           = usePersistentState('analysis_riskFreeRate', 0.025)
  const [riskFreeRateInput, setRiskFreeRateInput] = usePersistentState('analysis_riskFreeRateInput', '2.50')
  const [loading, setLoading]           = useState(true)
  const [refreshing, setRefreshing]     = useState(false)
  const [compareLoading, setCompareLoading] = useState(false)
  const [standaloneLoading, setStandaloneLoading] = useState(false)
  const [standaloneRefreshing, setStandaloneRefreshing] = useState(false)
  const [error, setError]               = useState('')
  const [compareError, setCompareError] = useState('')
  const [standaloneError, setStandaloneError] = useState('')

  const loadGenRef = useRef(0)

  const from = period === -1 ? customFrom : getFromDate(period)
  const to   = period === -1 ? customTo   : formatDate(new Date())
  // For "All", getFromDate returns a dummy sentinel. Use the actual first data point
  // from the loaded portfolio history so benchmark calls start at portfolio inception.
  const effectiveFrom = period === 0 ? (portfolioHistory[0]?.date ?? from) : from

  const loadData = useCallback(async () => {
    loadGenRef.current += 1
    const gen = loadGenRef.current
    setLoading(true)
    setRefreshing(false)
    setError('')

    let freshStats = false
    let freshHist = false

    const checkCachedDone = () => {
      if (gen === loadGenRef.current && !freshStats && !freshHist) {
        setLoading(false)
        setRefreshing(true)
      }
    }

    // 1. Cached calls
    getPortfolioStats(from, to, currency, acctModel, true).then(st => {
      if (gen === loadGenRef.current && !freshStats && Object.keys(st.statistics).length > 0) {
        setStats(st)
        checkCachedDone()
      }
    }).catch(() => {})

    getPortfolioReturns(from, to, currency, acctModel, 'twr', true).then(hist => {
      if (gen === loadGenRef.current && !freshHist && hist.data.length > 0) {
        setPortfolioHistory(hist.data ?? [])
        checkCachedDone()
      }
    }).catch(() => {})

    // 2. Fresh calls
    Promise.all([
      getPortfolioStats(from, to, currency, acctModel, false).then(st => {
        freshStats = true
        if (gen === loadGenRef.current) setStats(st)
      }),
      getPortfolioReturns(from, to, currency, acctModel, 'twr', false).then(hist => {
        freshHist = true
        if (gen === loadGenRef.current) setPortfolioHistory(hist.data ?? [])
      })
    ]).catch(err => {
      if (gen === loadGenRef.current) setError(err instanceof Error ? err.message : 'Failed to load')
    }).finally(() => {
      if (gen === loadGenRef.current) {
        setLoading(false)
        setRefreshing(false)
      }
    })
  }, [currency, acctModel, from, to])

  useEffect(() => { loadData() }, [loadData])

  // Load standalone metrics for portfolio (and symbols if any are active).
  // Fires on initial load and whenever currency/date/model/riskFreeRate change.
  const loadStandalone = useCallback(async (symbols = '') => {
    const gen = loadGenRef.current
    setStandaloneLoading(true)
    setStandaloneRefreshing(false)
    setStandaloneError('')

    let freshArrived = false

    // 1. Cached
    getStandaloneMetrics(symbols, currency, effectiveFrom, to, acctModel, riskFreeRate, true).then(res => {
      if (gen === loadGenRef.current && !freshArrived && res.results.length > 0) {
        setStandaloneResults(res.results)
        setStandaloneLoading(false)
        setStandaloneRefreshing(true)
      }
    }).catch(() => {})

    // 2. Fresh
    getStandaloneMetrics(symbols, currency, effectiveFrom, to, acctModel, riskFreeRate, false).then(res => {
      if (gen === loadGenRef.current) {
        freshArrived = true
        setStandaloneResults(res.results)
      }
    }).catch(err => {
      if (gen === loadGenRef.current) setStandaloneError(err instanceof Error ? err.message : 'Standalone metrics failed')
    }).finally(() => {
      if (gen === loadGenRef.current) {
        setStandaloneLoading(false)
        setStandaloneRefreshing(false)
      }
    })
  }, [currency, effectiveFrom, to, acctModel, riskFreeRate])

  useEffect(() => {
    loadStandalone(benchmarkSymbols.join(','))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadStandalone])

  // Re-fetch benchmark data whenever the date range, currency, or model changes.
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
        loadStandalone(benchmarkSymbols.join(','))
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
      const allSymbols = [...benchmarkSymbols, ...newSymbols]
      const comp = await comparePortfolio(newSymbols.join(','), currency, effectiveFrom, to, acctModel, riskFreeRate)
      setBenchmarkSymbols(allSymbols)
      setCompareResults(prev => [...prev, ...comp.benchmarks])
      setBenchmarkInput('')
      loadStandalone(allSymbols.join(','))
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
      loadStandalone(benchmarkSymbols.join(','))
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
    const remaining = benchmarkSymbols.filter(s => s !== sym)
    setBenchmarkSymbols(remaining)
    setBenchmarkData(prev => { const next = { ...prev }; delete next[sym]; return next })
    setCompareResults(prev => prev.filter(r => r.symbol !== sym))
    loadStandalone(remaining.join(','))
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

  // Portfolio standalone metrics (always the first result when available).
  const portfolioStandalone = standaloneResults[0]?.symbol === 'Portfolio' ? standaloneResults[0] : null

  const periodLabel = period === 0 ? 'All time'
    : period === -1 ? `${customFrom} to ${customTo}`
    : period === 12 ? '1 year'
    : `${period} month${period !== 1 ? 's' : ''}`

  return (
    <PageLayout>
          {/* Header */}
          <div className="w-full flex flex-col items-center mb-16 text-center">
            <h1 className="text-3xl font-semibold text-slate-100">Performance Analysis</h1>
            <p className="text-slate-500 text-sm mt-4">Statistical attribution and benchmark alignment</p>
          </div>

          {/* Controls */}
          <div className={`flex flex-wrap justify-center items-end gap-4 ${period === -1 ? 'mb-4' : 'mb-20'}`}>
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
            <div className="flex flex-col items-center gap-2">
              <div className="relative group/rfr cursor-default">
                <span className="text-[9px] font-black text-slate-600 uppercase tracking-[0.2em]">Risk-free rate</span>
                <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 w-60 max-w-[calc(100vw-2rem)] px-3 py-2.5 bg-[#12151f] border border-[#2a2e42]/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover/rfr:opacity-100 transition-opacity z-50 shadow-2xl">
                  The annual return of a theoretically risk-free asset. Used as the baseline in Sharpe and Sortino ratio calculations — only returns above this threshold are treated as compensation for risk.
                </div>
              </div>
              <div className="flex items-center gap-1.5 bg-[#1a1d2e] rounded-2xl p-1.5 border border-[#2a2e42]/50 shadow-xl shadow-black/20">
                <div className="relative flex items-center">
                  <input
                    type="number" value={riskFreeRateInput}
                    onChange={e => setRiskFreeRateInput(e.target.value)}
                    onBlur={handleRiskFreeRateBlur}
                    onKeyDown={e => e.key === 'Enter' && e.currentTarget.blur()}
                    step="0.1" min="0" max="20"
                    className="w-20 px-3 py-2 pr-6 bg-transparent text-sm text-slate-200 text-right focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
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
                <span className="text-slate-500 text-sm select-none pr-2">%</span>
              </div>
            </div>
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

          {error && <ErrorAlert message={error} className="mb-10" />}

          {/* Stats */}
          <div className="w-full mb-16 relative">
            {refreshing && (
                <div className="absolute top-0 right-4 w-4 h-4 rounded-full border-2 border-indigo-400/30 border-t-indigo-400 animate-spin" />
            )}
            <div className="flex items-center justify-center gap-3 mb-8">
              <h2 className="text-xl font-semibold text-slate-100">Risk & Return Metrics</h2>
              {stats && portfolioStandalone && (
                <div className="relative group">
                  <button
                    onClick={() => {
                      navigate('/llm', {
                        state: {
                          initialPrompt: {
                            promptType: 'risk_metrics',
                            displayMessage: `Analyze my portfolio's risk & return metrics for ${periodLabel}`,
                            extraParams: {
                              currency,
                              from,
                              to,
                              accounting_model: acctModel,
                              risk_free_rate: riskFreeRate,
                            },
                          },
                        },
                      })
                    }}
                    className="text-slate-500 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
                  >
                    <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                      <path d="M12 2L13.5 8.5L20 10L13.5 11.5L12 18L10.5 11.5L4 10L10.5 8.5Z" />
                      <path d="M19 1l.9 2.6 2.6.9-2.6.9L19 8.5l-.9-2.6L15.5 4l2.6-.9z" opacity=".6" />
                      <path d="M5 17l.7 2.1L7.8 20l-2.1.9L5 23l-.7-2.1L2.2 20l2.1-.9z" opacity=".6" />
                    </svg>
                  </button>
                  <HoverTooltip direction="down" className="w-max whitespace-nowrap">AI analysis of risk &amp; return metrics</HoverTooltip>
                </div>
              )}
            </div>
            {loading ? (
              <Spinner label="Compiling statistics…" className="py-10" />
            ) : stats ? (
              <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
                {/* TWR / MWR and any registry-computed stats */}
                {Object.entries(stats.statistics).map(([key, val]) => {
                  const numVal = typeof val === 'number' ? val : null
                  if (numVal === null) return null
                  const tooltip = STAT_TOOLTIPS[key.toLowerCase()]
                  return (
                    <div key={key} className={`relative group bg-[#1a1d2e]/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 ${tooltip ? 'cursor-help' : ''}`}>
                      {tooltip && <HoverTooltip className="w-56">{tooltip}</HoverTooltip>}
                      <p className="text-sm font-medium text-slate-500 mb-2 capitalize">{key.replace(/_/g, ' ')}</p>
                      <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%
                      </p>
                    </div>
                  )
                })}
                {/* Standalone metrics for the portfolio */}
                {standaloneLoading && !portfolioStandalone && (
                  <div className="col-span-2 md:col-span-4 flex justify-center py-4">
                    <Spinner label="Computing risk metrics…" />
                  </div>
                )}
                {portfolioStandalone && (
                  <>
                    <div className="relative group bg-[#1a1d2e]/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-help">
                      <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sharpe}</HoverTooltip>
                      <p className="text-sm font-medium text-slate-500 mb-2">Sharpe Ratio</p>
                      <p className={`text-2xl font-semibold tabular-nums ${portfolioStandalone.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {portfolioStandalone.sharpe_ratio.toFixed(3)}
                      </p>
                    </div>
                    <div className="relative group bg-[#1a1d2e]/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-help">
                      <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.vami}</HoverTooltip>
                      <p className="text-sm font-medium text-slate-500 mb-2">VAMI</p>
                      <p className="text-2xl font-semibold tabular-nums text-slate-100">
                        {portfolioStandalone.vami.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}
                      </p>
                    </div>
                    <div className="relative group bg-[#1a1d2e]/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-help">
                      <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.volatility}</HoverTooltip>
                      <p className="text-sm font-medium text-slate-500 mb-2">Volatility</p>
                      <p className="text-2xl font-semibold tabular-nums text-slate-400">
                        {(portfolioStandalone.volatility * 100).toFixed(2)}%
                      </p>
                    </div>
                    <div className="relative group bg-[#1a1d2e]/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-help">
                      <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sortino}</HoverTooltip>
                      <p className="text-sm font-medium text-slate-500 mb-2">Sortino Ratio</p>
                      <p className={`text-2xl font-semibold tabular-nums ${portfolioStandalone.sortino_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {portfolioStandalone.sortino_ratio.toFixed(3)}
                      </p>
                    </div>
                    <div className="relative group bg-[#1a1d2e]/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-help">
                      <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.max_drawdown}</HoverTooltip>
                      <p className="text-sm font-medium text-slate-500 mb-2">Max Drawdown</p>
                      <p className="text-2xl font-semibold tabular-nums text-rose-400">
                        -{(portfolioStandalone.max_drawdown * 100).toFixed(2)}%
                      </p>
                    </div>
                  </>
                )}
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

            {/* Standalone metrics table — portfolio + benchmark symbols */}
            {standaloneResults.length > 0 && (
              <div className="overflow-x-auto w-full mb-12 relative">
                {standaloneRefreshing && (
                    <div className="absolute top-0 right-4 w-4 h-4 rounded-full border-2 border-indigo-400/30 border-t-indigo-400 animate-spin" />
                )}
                <p className="text-xs font-semibold text-slate-500 mb-4 text-center uppercase tracking-widest">Standalone Metrics</p>
                {standaloneError && <ErrorAlert message={standaloneError} className="mb-3" />}
                <table className="w-full min-w-160 text-sm">
                  <thead>
                    <tr className="border-b border-[#2a2e42]/60">
                      {(['Security', 'Sharpe', 'VAMI', 'Volatility', 'Sortino', 'Max DD'] as const).map(h => {
                        const tipKey = { Security: undefined, Sharpe: 'sharpe', VAMI: 'vami', Volatility: 'volatility', Sortino: 'sortino', 'Max DD': 'max_drawdown' }[h] as keyof typeof STANDALONE_TOOLTIPS | undefined
                        const tip = tipKey ? STANDALONE_TOOLTIPS[tipKey] : undefined
                        return (
                          <th key={h} className={`py-4 px-4 text-xs font-semibold text-slate-500 ${h === 'Security' ? 'text-left' : 'text-right'}`}>
                            {tip ? (
                              <span className={`relative group inline-flex ${h === 'Security' ? '' : 'justify-end'} cursor-help`}>
                                {h}
                                <HoverTooltip align={h === 'Security' ? 'left' : 'right'} direction="down" className="w-56">{tip}</HoverTooltip>
                              </span>
                            ) : h}
                          </th>
                        )
                      })}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[#2a2e42]/30">
                    {standaloneResults.map(r => (
                      <tr key={r.symbol} className="hover:bg-white/2 transition-colors group">
                        <td className={`py-4 px-4 font-semibold uppercase ${r.symbol === 'Portfolio' ? 'text-indigo-400' : 'text-slate-100 group-hover:text-indigo-400 transition-colors'}`}>
                          {r.symbol}
                        </td>
                        {r.error ? (
                          <td colSpan={5} className="py-4 px-4 text-right text-red-400 text-xs">{r.error}</td>
                        ) : (
                          <>
                            <td className={`py-4 px-4 text-right font-medium tabular-nums ${r.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                              {r.sharpe_ratio.toFixed(3)}
                            </td>
                            <td className="py-4 px-4 text-right text-slate-300 font-medium tabular-nums">
                              {r.vami.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}
                            </td>
                            <td className="py-4 px-4 text-right text-slate-400 font-medium tabular-nums">
                              {(r.volatility * 100).toFixed(2)}%
                            </td>
                            <td className={`py-4 px-4 text-right font-medium tabular-nums ${r.sortino_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                              {r.sortino_ratio.toFixed(3)}
                            </td>
                            <td className="py-4 px-4 text-right font-medium tabular-nums text-rose-400">
                              -{(r.max_drawdown * 100).toFixed(2)}%
                            </td>
                          </>
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {/* Alpha/beta benchmark comparison table */}
            {compareResults.length > 0 && (
              <div className="overflow-x-auto overflow-y-hidden w-full">
                <p className="text-xs font-semibold text-slate-500 mb-4 text-center uppercase tracking-widest">Benchmark Comparison</p>
                <table className="w-full min-w-160 text-sm">
                  <thead>
                    <tr className="border-b border-[#2a2e42]/60">
                      {(['Security', 'Alpha', 'Beta', 'Treynor', 'Tracking Err', 'Info Ratio', 'Correlation'] as const).map(h => {
                        const tip = COMPARE_TOOLTIPS[h]
                        return (
                          <th key={h} className={`py-4 px-4 text-xs font-semibold text-slate-500 ${h === 'Security' ? 'text-left' : 'text-right'}`}>
                            {tip ? (
                              <span className={`relative group inline-flex ${h === 'Security' ? '' : 'justify-end'} cursor-help`}>
                                {h}
                                <HoverTooltip align={h === 'Security' ? 'left' : 'right'} direction="down" className="w-56">{tip}</HoverTooltip>
                              </span>
                            ) : h}
                          </th>
                        )
                      })}
                      <th className="py-4 px-4 w-8 sticky right-0 bg-[#0f1117]" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[#2a2e42]/30">
                    {compareResults.map(bm => (
                      <tr key={bm.symbol} className="hover:bg-white/2 transition-colors group">
                        <td className="py-4 px-4 font-semibold text-slate-100 group-hover:text-indigo-400 transition-colors uppercase">{bm.symbol}</td>
                        <td className={`py-4 px-4 text-right font-medium tabular-nums ${bm.alpha >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{(bm.alpha * 100).toFixed(2)}%</td>
                        <td className="py-4 px-4 text-right text-slate-400 font-medium tabular-nums">{bm.beta.toFixed(3)}</td>
                        <td className="py-4 px-4 text-right text-slate-400 font-medium">{bm.treynor_ratio.toFixed(4)}</td>
                        <td className="py-4 px-4 text-right text-slate-400 font-medium">{(bm.tracking_error * 100).toFixed(2)}%</td>
                        <td className="py-4 px-4 text-right text-slate-300 font-medium tabular-nums">{bm.information_ratio.toFixed(3)}</td>
                        <td className="py-4 px-4 text-right text-slate-400 font-medium">{bm.correlation.toFixed(3)}</td>
                        <td className="py-4 px-4 text-right sticky right-0 bg-[#0f1117]">
                          {portfolioStandalone && !bm.error && (
                            <button
                              onClick={() => navigate('/llm', {
                                state: {
                                  initialPrompt: {
                                    promptType: 'benchmark_analysis',
                                    displayMessage: `Analyze my portfolio vs ${bm.symbol} for ${periodLabel}`,
                                    extraParams: {
                                      benchmark_symbol: bm.symbol,
                                      currency,
                                      from: effectiveFrom,
                                      to,
                                      accounting_model: acctModel,
                                      risk_free_rate: riskFreeRate,
                                    },
                                  },
                                },
                              })}
                              className="text-slate-500 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
                              title="AI benchmark analysis"
                            >
                              <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                                <path d="M12 2L13.5 8.5L20 10L13.5 11.5L12 18L10.5 11.5L4 10L10.5 8.5Z" />
                                <path d="M19 1l.9 2.6 2.6.9-2.6.9L19 8.5l-.9-2.6L15.5 4l2.6-.9z" opacity=".6" />
                                <path d="M5 17l.7 2.1L7.8 20l-2.1.9L5 23l-.7-2.1L2.2 20l2.1-.9z" opacity=".6" />
                              </svg>
                            </button>
                          )}
                        </td>
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
