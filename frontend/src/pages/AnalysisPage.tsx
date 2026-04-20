import { useState, useCallback, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  LineChart, Line, AreaChart, Area, BarChart, Bar, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts'
import PageLayout from '../components/PageLayout'
import HoverTooltip from '../components/HoverTooltip'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import DateRangePicker from '../components/DateRangePicker'
import ErrorAlert from '../components/ErrorAlert'
import CorrelationHeatmap from '../components/CorrelationHeatmap'
import {
  getPortfolioStats, getPortfolioReturns, getMarketHistory, comparePortfolio, getStandaloneMetrics,
  getDrawdownSeries, getRollingMetric, getAttribution, getCorrelations,
  type StatsResponse, type DailyValue, type BenchmarkResult, type StandaloneResult,
  type DrawdownPoint, type RollingPoint, type AttributionResult,
} from '../api'
import { formatDate, CURRENCIES, getFromDate, RECHARTS_TOOLTIP_STYLE, RECHARTS_LABEL_STYLE, RECHARTS_ITEM_STYLE } from '../utils/format'
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

const CHART_MODE_OPTIONS = [
  { label: 'TWR',                value: 'twr'                as const },
  { label: 'MWR',                value: 'mwr'                as const },
  { label: 'Rolling Sharpe',     value: 'rolling_sharpe'     as const },
  { label: 'Rolling Sortino',    value: 'rolling_sortino'    as const },
  { label: 'Rolling Volatility', value: 'rolling_volatility' as const },
  { label: 'Rolling Beta',       value: 'rolling_beta'       as const },
  { label: 'Drawdown',           value: 'drawdown'           as const },
]

const WINDOW_OPTIONS = [
  { label: '1M',  value: 21  },
  { label: '3M',  value: 63  },
  { label: '6M',  value: 126 },
]

const HOLDINGS_VIEW_OPTIONS = [
  { label: 'Attribution', value: 'attribution' as const },
  { label: 'Correlation', value: 'correlation' as const },
]

const COLORS = ['#818cf8', '#34d399', '#fbbf24', '#f87171', '#22d3ee', '#f472b6', '#a78bfa']

function priceReturns(prices: { date: string; close: number }[]): { date: string; ret: number }[] {
  return prices.slice(1).map((p, i) => ({
    date: p.date,
    ret: prices[i].close > 0 ? (p.close - prices[i].close) / prices[i].close : 0,
  }))
}

// todo move all this to backend and test properly
function rollingSharpeFromReturns(rets: { date: string; ret: number }[], window: number, rfr: number): { date: string; value: number }[] {
  const out: { date: string; value: number }[] = []
  for (let i = window - 1; i < rets.length; i++) {
    const slice = rets.slice(i - window + 1, i + 1).map(r => r.ret)
    const mean = slice.reduce((s, r) => s + r, 0) / window
    const variance = slice.reduce((s, r) => s + (r - mean) ** 2, 0) / window
    const std = Math.sqrt(variance)
    if (std < 1e-12) continue
    out.push({ date: rets[i].date, value: (mean - rfr / 252) / std * Math.sqrt(252) })
  }
  return out
}

function rollingSortinoFromReturns(rets: { date: string; ret: number }[], window: number, rfr: number): { date: string; value: number }[] {
  const out: { date: string; value: number }[] = []
  for (let i = window - 1; i < rets.length; i++) {
    const slice = rets.slice(i - window + 1, i + 1).map(r => r.ret)
    const dailyRfr = rfr / 252
    const mean = slice.reduce((s, r) => s + r, 0) / window
    const downsideVariance = slice.reduce((s, r) => {
      const excess = r - dailyRfr
      return s + (excess < 0 ? excess ** 2 : 0)
    }, 0) / window
    const downsideStd = Math.sqrt(downsideVariance)
    if (downsideStd < 1e-12) continue
    out.push({ date: rets[i].date, value: (mean - dailyRfr) / downsideStd * Math.sqrt(252) })
  }
  return out
}

function rollingVolFromReturns(rets: { date: string; ret: number }[], window: number): { date: string; value: number }[] {
  const out: { date: string; value: number }[] = []
  for (let i = window - 1; i < rets.length; i++) {
    const slice = rets.slice(i - window + 1, i + 1).map(r => r.ret)
    const mean = slice.reduce((s, r) => s + r, 0) / window
    const variance = slice.reduce((s, r) => s + (r - mean) ** 2, 0) / window
    out.push({ date: rets[i].date, value: Math.sqrt(variance * 252) })
  }
  return out
}

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

type ChartMode = 'twr' | 'mwr' | 'drawdown' | 'rolling_sharpe' | 'rolling_sortino' | 'rolling_volatility' | 'rolling_beta'
type HoldingsView = 'attribution' | 'correlation'

function xTickFormatter(val: string) {
  return new Date(val).toLocaleString('default', { month: 'short', year: '2-digit' })
}

const AXIS_STYLE = { fontSize: 10, fill: '#475569' }
const AXIS_LABEL_STYLE = { fontSize: 10, fill: '#334155', fontWeight: 900 }

export default function AnalysisPage() {
  const navigate = useNavigate()
  const chartRef = useRef<HTMLDivElement>(null)

  const [currency, setCurrency]   = usePersistentState<string>('app_currency', 'CZK')
  const [period, setPeriod]       = usePersistentState('analysis_period', 0)
  const [acctModel, setAcctModel] = usePersistentState<'historical' | 'spot'>('analysis_acctModel', 'historical')
  const [customFrom, setCustomFrom] = useState(() => getFromDate(12))
  const [customTo,   setCustomTo]   = useState(() => formatDate(new Date()))

  // Chart modes
  const [chartMode, setChartMode]       = usePersistentState<ChartMode>('analysis_chartMode', 'twr')
  const [rollingWindow, setRollingWindow] = usePersistentState('analysis_rollingWindow', 63)
  const [holdingsView, setHoldingsView] = usePersistentState<HoldingsView>('analysis_holdingsView', 'attribution')

  // Portfolio data
  const [stats, setStats]           = useState<StatsResponse | null>(null)
  const [portfolioHistory, setPortfolioHistory] = useState<DailyValue[]>([])
  const [mwrHistory, setMwrHistory]             = useState<DailyValue[]>([])

  // Benchmark
  const [benchmarkInput, setBenchmarkInput]     = useState('SPY')
  const [benchmarkSymbols, setBenchmarkSymbols] = useState<string[]>([])
  const [benchmarkData, setBenchmarkData]       = useState<Record<string, { date: string; close: number }[]>>({})
  const [compareResults, setCompareResults]     = useState<BenchmarkResult[]>([])
  const [standaloneResults, setStandaloneResults] = useState<StandaloneResult[]>([])

  // Chart-mode data
  const [drawdownSeries, setDrawdownSeries]     = useState<DrawdownPoint[]>([])
  const [rollingSeries, setRollingSeries]       = useState<Record<string, RollingPoint[]>>({}) // key: 'Portfolio' or bench sym
  const [chartModeLoading, setChartModeLoading] = useState(false)
  const [chartModeError, setChartModeError]     = useState('')

  // Holdings analysis data
  const [attributionData, setAttributionData]       = useState<AttributionResult[]>([])
  const [attributionTWR, setAttributionTWR]         = useState(0)
  const [correlationData, setCorrelationData]       = useState<{ symbols: string[]; matrix: number[][] }>({ symbols: [], matrix: [] })
  const [holdingsLoading, setHoldingsLoading]       = useState(false)
  const [holdingsError, setHoldingsError]           = useState('')

  // Risk-free rate
  const [riskFreeRate, setRiskFreeRate]           = usePersistentState('analysis_riskFreeRate', 0.025)
  const [riskFreeRateInput, setRiskFreeRateInput] = usePersistentState('analysis_riskFreeRateInput', '2.50')

  // Loading / error states
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
  const effectiveFrom = period === 0 ? (portfolioHistory[0]?.date ?? from) : from

  // ── Core data load (stats + cumulative returns) ─────────────────────────────
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

    Promise.all([
      getPortfolioStats(from, to, currency, acctModel, false).then(st => {
        freshStats = true
        if (gen === loadGenRef.current) setStats(st)
      }),
      getPortfolioReturns(from, to, currency, acctModel, 'twr', false).then(hist => {
        freshHist = true
        if (gen === loadGenRef.current) setPortfolioHistory(hist.data ?? [])
      }),
      getPortfolioReturns(from, to, currency, acctModel, 'mwr', false).then(hist => {
        if (gen === loadGenRef.current) setMwrHistory(hist.data ?? [])
      }).catch(() => {}),
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

  // ── Standalone metrics ───────────────────────────────────────────────────────
  const loadStandalone = useCallback(async (symbols = '') => {
    const gen = loadGenRef.current
    setStandaloneLoading(true)
    setStandaloneRefreshing(false)
    setStandaloneError('')

    let freshArrived = false

    getStandaloneMetrics(symbols, currency, effectiveFrom, to, acctModel, riskFreeRate, true).then(res => {
      if (gen === loadGenRef.current && !freshArrived && res.results.length > 0) {
        setStandaloneResults(res.results)
        setStandaloneLoading(false)
        setStandaloneRefreshing(true)
      }
    }).catch(() => {})

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

  // ── Chart-mode data ──────────────────────────────────────────────────────────
  const loadChartMode = useCallback(async (mode: ChartMode, window: number, benchSyms: string[]) => {
    if (mode === 'twr' || mode === 'mwr') {
      setChartModeLoading(false)
      setChartModeError('')
      return
    }
    setChartModeLoading(true)
    setChartModeError('')
    try {
      if (mode === 'drawdown') {
        const res = await getDrawdownSeries(effectiveFrom, to, currency, acctModel)
        setDrawdownSeries(res.series)
      } else {
        const metric = mode === 'rolling_sharpe' ? 'sharpe'
          : mode === 'rolling_volatility' ? 'volatility'
          : mode === 'rolling_sortino' ? 'sortino'
          : 'beta'
        const newSeries: Record<string, RollingPoint[]> = {}
        if (metric === 'beta') {
          await Promise.all(benchSyms.map(async sym => {
            try {
              const res = await getRollingMetric(metric, window, effectiveFrom, to, currency, acctModel, riskFreeRate, sym)
              const result = res.results[0]
              if (result && !result.error) newSeries[sym] = result.series
            } catch { /* skip bad symbols */ }
          }))
        } else {
          const res = await getRollingMetric(metric, window, effectiveFrom, to, currency, acctModel, riskFreeRate)
          const result = res.results[0]
          if (result && !result.error) newSeries['Portfolio'] = result.series
        }
        setRollingSeries(newSeries)
      }
    } catch (err) {
      setChartModeError(err instanceof Error ? err.message : 'Failed to load chart data')
    } finally {
      setChartModeLoading(false)
    }
  }, [effectiveFrom, to, currency, acctModel, riskFreeRate])

  useEffect(() => {
    loadChartMode(chartMode, rollingWindow, benchmarkSymbols)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadChartMode, chartMode, rollingWindow, benchmarkSymbols])

  // Auto-switch away from Rolling Beta when all benchmarks are removed
  useEffect(() => {
    if (benchmarkSymbols.length === 0 && chartMode === 'rolling_beta') {
      setChartMode('twr')
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [benchmarkSymbols])

  // ── Holdings analysis ────────────────────────────────────────────────────────
  const loadHoldings = useCallback(async () => {
    setHoldingsLoading(true)
    setHoldingsError('')
    try {
      const [attrRes, corrRes] = await Promise.all([
        getAttribution(effectiveFrom, to, currency, acctModel, riskFreeRate),
        getCorrelations(effectiveFrom, to, currency, acctModel),
      ])
      setAttributionData(attrRes.positions)
      setAttributionTWR(attrRes.total_twr)
      setCorrelationData({ symbols: corrRes.symbols, matrix: corrRes.matrix })
    } catch (err) {
      setHoldingsError(err instanceof Error ? err.message : 'Failed to load holdings data')
    } finally {
      setHoldingsLoading(false)
    }
  }, [effectiveFrom, to, currency, acctModel, riskFreeRate])

  useEffect(() => { loadHoldings() }, [loadHoldings])

  // ── Benchmark refresh on date/currency change ────────────────────────────────
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

  // ── Handlers ─────────────────────────────────────────────────────────────────
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

  const handleStatCardClick = (mode: ChartMode) => {
    setChartMode(mode)
    setTimeout(() => chartRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 50)
  }

  // ── Derived chart data ───────────────────────────────────────────────────────
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

  const drawdownChartData = (() => {
    if (drawdownSeries.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    drawdownSeries.forEach(pt => { dateMap[pt.date] = { Drawdown: +(pt.drawdown_pct * 100).toFixed(3) } })
    benchmarkSymbols.forEach(sym => {
      const prices = benchmarkData[sym]
      if (!prices || prices.length < 2) return
      let peak = 1, wealth = 1
      prices.slice(1).forEach((p, i) => {
        const ret = prices[i].close > 0 ? (p.close - prices[i].close) / prices[i].close : 0
        wealth *= (1 + ret)
        if (wealth > peak) peak = wealth
        const dd = +((wealth / peak - 1) * 100).toFixed(3)
        if (dateMap[p.date]) dateMap[p.date][sym] = dd
      })
    })
    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  })()

  const mwrChartData = (() => {
    if (mwrHistory.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    mwrHistory.forEach(d => {
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

  // Client-side benchmark rolling series for sharpe/sortino/vol modes
  const benchmarkRollingData: Record<string, { date: string; value: number }[]> = {}
  if (chartMode === 'rolling_sharpe' || chartMode === 'rolling_volatility' || chartMode === 'rolling_sortino') {
    benchmarkSymbols.forEach(sym => {
      const prices = benchmarkData[sym]
      if (!prices || prices.length < 2) return
      const rets = priceReturns(prices)
      benchmarkRollingData[sym] = chartMode === 'rolling_sharpe'
        ? rollingSharpeFromReturns(rets, rollingWindow, riskFreeRate)
        : chartMode === 'rolling_sortino'
        ? rollingSortinoFromReturns(rets, rollingWindow, riskFreeRate)
        : rollingVolFromReturns(rets, rollingWindow)
    })
  }
  const allRollingSeries = { ...rollingSeries, ...benchmarkRollingData }

  const rollingChartData = (() => {
    const keys = Object.keys(allRollingSeries)
    if (keys.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    keys.forEach(sym => {
      allRollingSeries[sym].forEach(pt => {
        if (!dateMap[pt.date]) dateMap[pt.date] = {}
        dateMap[pt.date][sym] = pt.value
      })
    })
    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  })()

  const portfolioStandalone = standaloneResults[0]?.symbol === 'Portfolio' ? standaloneResults[0] : null

  const periodLabel = period === 0 ? 'All time'
    : period === -1 ? `${customFrom} to ${customTo}`
    : period === 12 ? '1 year'
    : `${period} month${period !== 1 ? 's' : ''}`

  const isRollingMode = chartMode === 'rolling_sharpe' || chartMode === 'rolling_volatility' || chartMode === 'rolling_beta' || chartMode === 'rolling_sortino'
  const rollingMetricLabel = chartMode === 'rolling_sharpe' ? 'Sharpe Ratio'
    : chartMode === 'rolling_volatility' ? 'Volatility'
    : chartMode === 'rolling_sortino' ? 'Sortino Ratio'
    : 'Beta'

  // Attribution: top 15 + "Others"
  const MAX_ATTR = 15
  const attrDisplay = (() => {
    if (attributionData.length === 0) return []
    const sorted = [...attributionData].sort((a, b) => b.contribution - a.contribution)
    if (sorted.length <= MAX_ATTR) return sorted
    const shown = sorted.slice(0, MAX_ATTR)
    const othersContrib = sorted.slice(MAX_ATTR).reduce((s, r) => s + r.contribution, 0)
    const othersWeight = sorted.slice(MAX_ATTR).reduce((s, r) => s + r.avg_weight, 0)
    shown.push({ symbol: 'Others', avg_weight: othersWeight, return: 0, contribution: othersContrib })
    return shown
  })()

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
            <span className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em]">Risk-free rate</span>
            <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 w-60 max-w-[calc(100vw-2rem)] px-3 py-2.5 bg-panel border border-border-dim/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover/rfr:opacity-100 transition-opacity z-50 shadow-2xl">
              The annual return of a theoretically risk-free asset. Used as the baseline in Sharpe and Sortino ratio calculations — only returns above this threshold are treated as compensation for risk.
            </div>
          </div>
          <div className="flex items-center gap-1.5 bg-surface rounded-2xl p-1.5 border border-border-dim/50 shadow-xl shadow-black/20">
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
                  className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
                >
                  <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 4L4 1L7 4"/></svg>
                </button>
                <button
                  type="button" tabIndex={-1}
                  onClick={() => applyRiskFreeRate(Math.max(0, Math.round((riskFreeRate - 0.001) * 1000) / 1000))}
                  className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
                >
                  <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 1L4 4L7 1"/></svg>
                </button>
              </div>
            </div>
            <span className="text-slate-500 text-sm select-none pr-2">%</span>
          </div>
        </div>
      </div>

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

      {/* ── Section 1: Risk & Return Metrics ─────────────────────────────────── */}
      <div className="w-full mb-16 relative">
        {refreshing && (
          <div className="absolute top-0 right-4 w-4 h-4 rounded-full border-2 border-indigo-400/30 border-t-indigo-400 animate-spin" />
        )}
        <div className="flex items-center justify-center gap-3 mb-8">
          <h2 className="text-xl font-semibold text-slate-100">Risk &amp; Return Metrics</h2>
          {stats && portfolioStandalone && (
            <div className="relative group">
              <button
                onClick={() => navigate('/llm', { state: { initialPrompt: { promptType: 'risk_metrics', displayMessage: `Analyze my portfolio's risk & return metrics for ${periodLabel}`, extraParams: { currency, from, to, accounting_model: acctModel, risk_free_rate: riskFreeRate } } } })}
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
            {(() => {
              const entries = Object.entries(stats.statistics)
              const ordered = [
                ...entries.filter(([k]) => k === 'twr'),
                ...entries.filter(([k]) => k === 'mwr'),
                ...entries.filter(([k]) => k !== 'twr' && k !== 'mwr'),
              ]
              return ordered.map(([key, val]) => {
                const numVal = typeof val === 'number' ? val : null
                if (numVal === null) return null
                const tooltip = STAT_TOOLTIPS[key.toLowerCase()]
                if (key === 'twr') {
                  return (
                    <button
                      key={key}
                      onClick={() => handleStatCardClick('twr')}
                      className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all"
                    >
                      {tooltip && <HoverTooltip className="w-56">{tooltip}</HoverTooltip>}
                      <p className="text-sm font-medium text-slate-500 mb-2 uppercase">TWR</p>
                      <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%
                      </p>
                      <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
                    </button>
                  )
                }
                if (key === 'mwr') {
                  return (
                    <button
                      key={key}
                      onClick={() => handleStatCardClick('mwr')}
                      className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all"
                    >
                      {tooltip && <HoverTooltip className="w-56">{tooltip}</HoverTooltip>}
                      <p className="text-sm font-medium text-slate-500 mb-2 uppercase">MWR</p>
                      <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%
                      </p>
                      <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
                    </button>
                  )
                }
                return (
                  <div key={key} className={`relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 ${tooltip ? 'cursor-help' : ''}`}>
                    {tooltip && <HoverTooltip className="w-56">{tooltip}</HoverTooltip>}
                    <p className="text-sm font-medium text-slate-500 mb-2 capitalize">{key.replace(/_/g, ' ')}</p>
                    <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                      {numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%
                    </p>
                  </div>
                )
              })
            })()}

            {standaloneLoading && !portfolioStandalone && (
              <div className="col-span-2 md:col-span-4 flex justify-center py-4">
                <Spinner label="Computing risk metrics…" />
              </div>
            )}

            {portfolioStandalone && (
              <>
                {/* Sharpe — clickable, links to rolling_sharpe chart */}
                <button
                  onClick={() => handleStatCardClick('rolling_sharpe')}
                  className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all"
                >
                  <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sharpe}</HoverTooltip>
                  <p className="text-sm font-medium text-slate-500 mb-2">Sharpe Ratio</p>
                  <p className={`text-2xl font-semibold tabular-nums ${portfolioStandalone.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                    {portfolioStandalone.sharpe_ratio.toFixed(3)}
                  </p>
                  <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
                </button>

                <div className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-help">
                  <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.vami}</HoverTooltip>
                  <p className="text-sm font-medium text-slate-500 mb-2">VAMI</p>
                  <p className="text-2xl font-semibold tabular-nums text-slate-100">
                    {portfolioStandalone.vami.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}
                  </p>
                </div>

                {/* Volatility — clickable, links to rolling_volatility chart */}
                <button
                  onClick={() => handleStatCardClick('rolling_volatility')}
                  className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all"
                >
                  <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.volatility}</HoverTooltip>
                  <p className="text-sm font-medium text-slate-500 mb-2">Volatility</p>
                  <p className="text-2xl font-semibold tabular-nums text-slate-400">
                    {(portfolioStandalone.volatility * 100).toFixed(2)}%
                  </p>
                  <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
                </button>

                {/* Sortino — clickable, links to rolling_sortino chart */}
                <button
                  onClick={() => handleStatCardClick('rolling_sortino')}
                  className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all"
                >
                  <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sortino}</HoverTooltip>
                  <p className="text-sm font-medium text-slate-500 mb-2">Sortino Ratio</p>
                  <p className={`text-2xl font-semibold tabular-nums ${portfolioStandalone.sortino_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                    {portfolioStandalone.sortino_ratio.toFixed(3)}
                  </p>
                  <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
                </button>

                {/* Max Drawdown — clickable, links to drawdown chart */}
                <button
                  onClick={() => handleStatCardClick('drawdown')}
                  className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-rose-500/30 hover:bg-surface/60 transition-all"
                >
                  <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.max_drawdown}</HoverTooltip>
                  <p className="text-sm font-medium text-slate-500 mb-2">Max Drawdown</p>
                  <p className="text-2xl font-semibold tabular-nums text-rose-400">
                    -{(portfolioStandalone.max_drawdown * 100).toFixed(2)}%
                  </p>
                  <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
                </button>
              </>
            )}
          </div>
        ) : (
          <p className="text-slate-500 text-center text-sm py-10">Historical context required to generate statistics.</p>
        )}
      </div>

      {/* ── Section 2: Benchmarking ───────────────────────────────────────────── */}
      <div className="w-full mb-20">
        <h2 className="text-xl font-semibold text-slate-100 mb-8 text-center">Benchmarking</h2>

        <div className="flex flex-col items-center gap-3 mb-10 w-full max-w-2xl mx-auto">
          <div className="flex flex-col sm:flex-row items-center w-full gap-4">
            <input
              type="text" value={benchmarkInput} onChange={e => setBenchmarkInput(e.target.value)}
              placeholder="Symbols (SPY, QQQ, VWCE.DE)"
              className="w-full px-6 py-3 bg-surface border border-border-dim/60 rounded-xl text-sm text-slate-200 placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 transition-all"
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
                <span key={sym} className="inline-flex items-center gap-2 px-3 py-1.5 bg-surface border border-border-dim/60 rounded-lg text-sm font-medium text-slate-300">
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

        {/* Chart mode controls */}
        <div ref={chartRef} className="flex flex-wrap justify-center items-center gap-4 mb-4">
          <SegmentedControl
            label="Chart Mode"
            options={CHART_MODE_OPTIONS.map(o => ({
              ...o,
              disabled: o.value === 'rolling_beta' && benchmarkSymbols.length === 0,
              tooltip: o.value === 'rolling_beta' && benchmarkSymbols.length === 0
                ? 'Add a benchmark symbol first to enable rolling beta'
                : undefined,
            }))}
            value={chartMode}
            onChange={setChartMode}
          />
          {isRollingMode && (
            <SegmentedControl
              label="Window"
              options={WINDOW_OPTIONS}
              value={rollingWindow}
              onChange={setRollingWindow}
            />
          )}
        </div>

        {chartModeError && <ErrorAlert message={chartModeError} className="mb-4" />}

        {/* Chart */}
        {(chartMode === 'twr' ? mergedChartData.length > 0 : chartMode === 'mwr' ? mwrChartData.length > 0 : true) ? (
          <div className="h-100 mb-10 w-full relative">
            {chartModeLoading && (
              <div className="absolute inset-0 flex items-center justify-center bg-bg/60 z-10 rounded-2xl">
                <Spinner label="Loading…" />
              </div>
            )}
            <ResponsiveContainer width="100%" height="100%">
              {chartMode === 'twr' ? (
                <LineChart data={mergedChartData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tickFormatter={xTickFormatter} minTickGap={60} tick={AXIS_STYLE} axisLine={{ stroke: '#2a2e42' }} tickLine={false} label={{ value: 'Date', position: 'insideBottom', offset: -16, ...AXIS_LABEL_STYLE }} />
                  <YAxis domain={['auto', 'auto']} tickFormatter={val => `${Number(val).toFixed(0)}%`} tick={AXIS_STYLE} axisLine={false} tickLine={false} width={56} label={{ value: 'Return (%)', angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }} />
                  <Tooltip contentStyle={RECHARTS_TOOLTIP_STYLE} labelStyle={RECHARTS_LABEL_STYLE} itemStyle={RECHARTS_ITEM_STYLE} formatter={(value, name) => [`${Number(value).toFixed(2)}%`, String(name)]} />
                  <Legend wrapperStyle={{ fontSize: '10px', color: '#64748b', paddingTop: '30px', fontWeight: '900', textTransform: 'uppercase', letterSpacing: '0.15em' }} />
                  <Line type="monotone" dataKey="Portfolio" stroke={COLORS[0]} strokeWidth={3} dot={false} animationDuration={1200} />
                  {benchmarkSymbols.map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
                  ))}
                </LineChart>
              ) : chartMode === 'mwr' ? (
                <LineChart data={mwrChartData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tickFormatter={xTickFormatter} minTickGap={60} tick={AXIS_STYLE} axisLine={{ stroke: '#2a2e42' }} tickLine={false} label={{ value: 'Date', position: 'insideBottom', offset: -16, ...AXIS_LABEL_STYLE }} />
                  <YAxis domain={['auto', 'auto']} tickFormatter={val => `${Number(val).toFixed(0)}%`} tick={AXIS_STYLE} axisLine={false} tickLine={false} width={56} label={{ value: 'Return (%)', angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }} />
                  <Tooltip contentStyle={RECHARTS_TOOLTIP_STYLE} labelStyle={RECHARTS_LABEL_STYLE} itemStyle={RECHARTS_ITEM_STYLE} formatter={(value, name) => [`${Number(value).toFixed(2)}%`, String(name)]} />
                  <Legend wrapperStyle={{ fontSize: '10px', color: '#64748b', paddingTop: '30px', fontWeight: '900', textTransform: 'uppercase', letterSpacing: '0.15em' }} />
                  <Line type="monotone" dataKey="Portfolio" stroke={COLORS[0]} strokeWidth={3} dot={false} animationDuration={1200} />
                  {benchmarkSymbols.map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
                  ))}
                </LineChart>
              ) : chartMode === 'drawdown' ? (
                <AreaChart data={drawdownChartData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tickFormatter={xTickFormatter} minTickGap={60} tick={AXIS_STYLE} axisLine={{ stroke: '#2a2e42' }} tickLine={false} label={{ value: 'Date', position: 'insideBottom', offset: -16, ...AXIS_LABEL_STYLE }} />
                  <YAxis domain={['auto', 0]} tickFormatter={val => `${Number(val).toFixed(0)}%`} tick={AXIS_STYLE} axisLine={false} tickLine={false} width={56} label={{ value: 'Drawdown (%)', angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }} />
                  <Tooltip contentStyle={RECHARTS_TOOLTIP_STYLE} labelStyle={RECHARTS_LABEL_STYLE} itemStyle={RECHARTS_ITEM_STYLE} formatter={(value, name) => [`${Number(value).toFixed(2)}%`, String(name)]} />
                  {benchmarkSymbols.length > 0 && <Legend wrapperStyle={{ fontSize: '10px', color: '#64748b', paddingTop: '30px', fontWeight: '900', textTransform: 'uppercase', letterSpacing: '0.15em' }} />}
                  <Area type="monotone" dataKey="Drawdown" stroke="#f87171" strokeWidth={1.5} fill="#f87171" fillOpacity={0.15} dot={false} animationDuration={1000} />
                  {benchmarkSymbols.map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
                  ))}
                </AreaChart>
              ) : (
                <LineChart data={rollingChartData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tickFormatter={xTickFormatter} minTickGap={60} tick={AXIS_STYLE} axisLine={{ stroke: '#2a2e42' }} tickLine={false} label={{ value: 'Date', position: 'insideBottom', offset: -16, ...AXIS_LABEL_STYLE }} />
                  <YAxis domain={['auto', 'auto']} tick={AXIS_STYLE} axisLine={false} tickLine={false} width={56}
                    tickFormatter={val => chartMode === 'rolling_volatility' ? `${(Number(val) * 100).toFixed(0)}%` : Number(val).toFixed(2)}
                    label={{ value: rollingMetricLabel, angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }}
                  />
                  <Tooltip contentStyle={RECHARTS_TOOLTIP_STYLE} labelStyle={RECHARTS_LABEL_STYLE} itemStyle={RECHARTS_ITEM_STYLE}
                    formatter={(value, name) => [
                      chartMode === 'rolling_volatility' ? `${(Number(value) * 100).toFixed(2)}%` : Number(value).toFixed(3),
                      String(name)
                    ]}
                  />
                  <Legend wrapperStyle={{ fontSize: '10px', color: '#64748b', paddingTop: '30px', fontWeight: '900', textTransform: 'uppercase', letterSpacing: '0.15em' }} />
                  {Object.keys(allRollingSeries).map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} stroke={COLORS[i % COLORS.length]} strokeWidth={sym === 'Portfolio' ? 2.5 : 1.5} strokeDasharray={sym === 'Portfolio' ? undefined : '6 6'} dot={false} />
                  ))}
                </LineChart>
              )}
            </ResponsiveContainer>
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center py-20 gap-3 text-slate-500 opacity-60 mb-10">
            <svg className="w-10 h-10" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" /></svg>
            <p className="text-sm font-medium">Ready to compare — add a benchmark symbol above</p>
          </div>
        )}

        {/* Standalone metrics table */}
        {standaloneResults.length > 0 && (
          <div className="overflow-x-auto w-full mb-12 relative">
            {standaloneRefreshing && (
              <div className="absolute top-0 right-4 w-4 h-4 rounded-full border-2 border-indigo-400/30 border-t-indigo-400 animate-spin" />
            )}
            <p className="text-xs font-semibold text-slate-500 mb-4 text-center uppercase tracking-widest">Standalone Metrics</p>
            {standaloneError && <ErrorAlert message={standaloneError} className="mb-3" />}
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border-dim/60">
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
              <tbody className="divide-y divide-white/5">
                {standaloneResults.map(r => (
                  <tr key={r.symbol} className="hover:bg-white/2 transition-colors group">
                    <td className={`py-4 px-4 font-semibold uppercase ${r.symbol === 'Portfolio' ? 'text-indigo-400' : 'text-slate-100 group-hover:text-indigo-400 transition-colors'}`}>{r.symbol}</td>
                    {r.error ? (
                      <td colSpan={5} className="py-4 px-4 text-right text-red-400 text-xs">{r.error}</td>
                    ) : (
                      <>
                        <td className={`py-4 px-4 text-right font-medium tabular-nums ${r.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{r.sharpe_ratio.toFixed(3)}</td>
                        <td className="py-4 px-4 text-right text-slate-300 font-medium tabular-nums">{r.vami.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}</td>
                        <td className="py-4 px-4 text-right text-slate-400 font-medium tabular-nums">{(r.volatility * 100).toFixed(2)}%</td>
                        <td className={`py-4 px-4 text-right font-medium tabular-nums ${r.sortino_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{r.sortino_ratio.toFixed(3)}</td>
                        <td className="py-4 px-4 text-right font-medium tabular-nums text-rose-400">-{(r.max_drawdown * 100).toFixed(2)}%</td>
                      </>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* Benchmark comparison table */}
        {compareResults.length > 0 && (
          <div className="overflow-x-auto overflow-y-hidden w-full">
            <p className="text-xs font-semibold text-slate-500 mb-4 text-center uppercase tracking-widest">Benchmark Comparison</p>
            <table className="w-full min-w-160 text-sm">
              <thead>
                <tr className="border-b border-border-dim/60">
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
                  <th className="py-4 px-4 w-8 sticky right-0 bg-bg" />
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {compareResults.map(bm => (
                  <tr key={bm.symbol} className="hover:bg-white/2 transition-colors group">
                    <td className="py-4 px-4 font-semibold text-slate-100 group-hover:text-indigo-400 transition-colors uppercase">{bm.symbol}</td>
                    <td className={`py-4 px-4 text-right font-medium tabular-nums ${bm.alpha >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>{(bm.alpha * 100).toFixed(2)}%</td>
                    <td className="py-4 px-4 text-right text-slate-400 font-medium tabular-nums">{bm.beta.toFixed(3)}</td>
                    <td className="py-4 px-4 text-right text-slate-400 font-medium">{bm.treynor_ratio.toFixed(4)}</td>
                    <td className="py-4 px-4 text-right text-slate-400 font-medium">{(bm.tracking_error * 100).toFixed(2)}%</td>
                    <td className="py-4 px-4 text-right text-slate-300 font-medium tabular-nums">{bm.information_ratio.toFixed(3)}</td>
                    <td className="py-4 px-4 text-right text-slate-400 font-medium">{bm.correlation.toFixed(3)}</td>
                    <td className="py-4 px-4 text-right sticky right-0 bg-bg group-hover:bg-white/2 transition-colors">
                      {portfolioStandalone && !bm.error && (
                        <button
                          onClick={() => navigate('/llm', { state: { initialPrompt: { promptType: 'benchmark_analysis', displayMessage: `Analyze my portfolio vs ${bm.symbol} for ${periodLabel}`, extraParams: { benchmark_symbol: bm.symbol, currency, from: effectiveFrom, to, accounting_model: acctModel, risk_free_rate: riskFreeRate } } } })}
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

      {/* ── Section 3: Holdings Analysis ─────────────────────────────────────── */}
      <div className="w-full">
        <div className="flex items-center justify-center gap-3 mb-8">
          <h2 className="text-xl font-semibold text-slate-100">Holdings Analysis</h2>
          {attributionData.length > 0 && holdingsView === 'attribution' && (
            <div className="relative group">
              <button
                onClick={() => navigate('/llm', { state: { initialPrompt: { promptType: 'biggest_drag_on_performance', displayMessage: `Identify the biggest drag on performance in my portfolio for ${periodLabel}`, extraParams: { currency, from: effectiveFrom, to, accounting_model: acctModel, risk_free_rate: riskFreeRate } } } })}
                className="text-slate-500 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
              >
                <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                  <path d="M12 2L13.5 8.5L20 10L13.5 11.5L12 18L10.5 11.5L4 10L10.5 8.5Z" />
                  <path d="M19 1l.9 2.6 2.6.9-2.6.9L19 8.5l-.9-2.6L15.5 4l2.6-.9z" opacity=".6" />
                  <path d="M5 17l.7 2.1L7.8 20l-2.1.9L5 23l-.7-2.1L2.2 20l2.1-.9z" opacity=".6" />
                </svg>
              </button>
              <HoverTooltip direction="down" className="w-max whitespace-nowrap">AI attribution analysis</HoverTooltip>
            </div>
          )}
        </div>

        <div className="flex justify-center mb-10">
          <SegmentedControl
            label="View"
            options={HOLDINGS_VIEW_OPTIONS}
            value={holdingsView}
            onChange={setHoldingsView}
          />
        </div>

        {holdingsLoading ? (
          <Spinner label="Computing holdings data…" className="py-10" />
        ) : holdingsError ? (
          <ErrorAlert message={holdingsError} className="mb-6" />
        ) : holdingsView === 'attribution' ? (
          attributionData.length === 0 ? (
            <p className="text-slate-500 text-center text-sm py-10">No attribution data available for this period.</p>
          ) : (
            <div className="w-full">
              {/* Horizontal bar chart */}
              <div className="h-72 mb-8 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart
                    data={attrDisplay.map(r => ({ name: r.symbol.split('@')[0], value: +(r.contribution * 100).toFixed(3) }))}
                    layout="vertical"
                    margin={{ top: 4, right: 40, left: 60, bottom: 4 }}
                  >
                    <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" horizontal={false} opacity={0.3} />
                    <XAxis type="number" tickFormatter={v => `${Number(v).toFixed(1)}%`} tick={AXIS_STYLE} axisLine={false} tickLine={false} />
                    <YAxis type="category" dataKey="name" tick={{ ...AXIS_STYLE, fontSize: 9 }} axisLine={false} tickLine={false} width={56} />
                    <Tooltip contentStyle={RECHARTS_TOOLTIP_STYLE} labelStyle={RECHARTS_LABEL_STYLE} itemStyle={RECHARTS_ITEM_STYLE} formatter={(value) => [`${Number(value).toFixed(3)}%`, 'Contribution']} />
                    <Bar dataKey="value" radius={[0, 3, 3, 0]} isAnimationActive={false}>
                      {attrDisplay.map((r, i) => (
                        <Cell key={i} fill={r.contribution >= 0 ? '#34d399' : '#f87171'} fillOpacity={0.85} />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>

              {/* Attribution table */}
              <div className="overflow-x-auto w-full">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border-dim/60">
                      <th className="py-3 px-4 text-left text-xs font-semibold text-slate-500">Symbol</th>
                      <th className="py-3 px-4 text-right text-xs font-semibold text-slate-500">Avg Weight</th>
                      <th className="py-3 px-4 text-right text-xs font-semibold text-slate-500">Return</th>
                      <th className="py-3 px-4 text-right text-xs font-semibold text-slate-500">Contribution</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {attrDisplay.map(r => (
                      <tr key={r.symbol} className="hover:bg-white/2 transition-colors">
                        <td className="py-3 px-4 font-semibold text-slate-100 uppercase text-sm">{r.symbol.split('@')[0]}</td>
                        <td className="py-3 px-4 text-right text-slate-400 tabular-nums">{(r.avg_weight * 100).toFixed(1)}%</td>
                        <td className={`py-3 px-4 text-right font-medium tabular-nums ${r.return >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                          {r.return >= 0 ? '+' : ''}{(r.return * 100).toFixed(2)}%
                        </td>
                        <td className={`py-3 px-4 text-right font-semibold tabular-nums ${r.contribution >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                          {r.contribution >= 0 ? '+' : ''}{(r.contribution * 100).toFixed(3)}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                  <tfoot>
                    <tr className="border-t-2 border-border-dim/60">
                      <td className="py-3 px-4 font-black text-slate-100 text-xs uppercase tracking-widest">Portfolio TWR</td>
                      <td className="py-3 px-4 text-right text-slate-400 tabular-nums font-medium">100.0%</td>
                      <td className="py-3 px-4" />
                      <td className={`py-3 px-4 text-right font-black tabular-nums ${attributionTWR >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {attributionTWR >= 0 ? '+' : ''}{(attributionTWR * 100).toFixed(2)}%
                      </td>
                    </tr>
                  </tfoot>
                </table>
              </div>
            </div>
          )
        ) : (
          // Correlation view
          correlationData.symbols.length === 0 ? (
            <p className="text-slate-500 text-center text-sm py-10">Not enough data to compute correlations for this period.</p>
          ) : (
            <CorrelationHeatmap symbols={correlationData.symbols} matrix={correlationData.matrix} />
          )
        )}
      </div>
    </PageLayout>
  )
}
