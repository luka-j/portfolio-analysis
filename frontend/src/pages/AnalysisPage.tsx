import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  LineChart, Line, AreaChart, Area,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts'
import PageLayout from '../components/PageLayout'
import HoverTooltip from '../components/HoverTooltip'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import DateRangePicker from '../components/DateRangePicker'
import ErrorAlert from '../components/ErrorAlert'
import CompareScenariosChip from '../components/CompareScenariosChip'
import StatCards from '../components/analysis/StatCards'
import BenchmarkPanel from '../components/analysis/BenchmarkPanel'
import HoldingsPanel from '../components/analysis/HoldingsPanel'
import { getMarketSymbols } from '../api'
import { formatDate, CURRENCIES, getFromDate, RECHARTS_TOOLTIP_STYLE, RECHARTS_LABEL_STYLE, RECHARTS_ITEM_STYLE } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'
import { useScenario } from '../context/ScenarioContext'

const CURRENCY_OPTIONS = CURRENCIES.map(c => ({ label: c, value: c }))

const FX_METHOD_OPTIONS = [
  { label: 'Historical', value: 'historical' as const, tooltip: 'Uses the FX rate at the time each trade was executed. Reflects your true cost basis in the currency, accounting for currency movements over time.' },
  { label: 'Spot',       value: 'spot'       as const, tooltip: "Applies today's FX rate to all prices. Shows current market value converted at the current exchange rate, regardless of when trades were made." },
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

const COLORS = ['#818cf8', '#34d399', '#fbbf24', '#f87171', '#22d3ee', '#f472b6', '#a78bfa']

import { useAnalysisData } from './hooks/useAnalysisData'
import { useBenchmarks } from './hooks/useBenchmarks'
import { useChartModeData } from './hooks/useChartModeData'
import { useCompareOverlay } from './hooks/useCompareOverlay'
import { useAnalysisChartData } from './hooks/useAnalysisChartData'
import type { ChartMode, HoldingsView } from './hooks/types'

function xTickFormatter(val: string) {
  return new Date(val).toLocaleString('default', { month: 'short', year: '2-digit' })
}

const AXIS_STYLE = { fontSize: 10, fill: '#475569' }
const AXIS_LABEL_STYLE = { fontSize: 10, fill: '#334155', fontWeight: 900 }

const COMPARE_COLOR = '#fbbf24' // amber — scenario-active accent

export default function AnalysisPage() {
  const navigate = useNavigate()
  const chartRef = useRef<HTMLDivElement>(null)

  const { active, compare, scenarios } = useScenario()
  const activeLabel = active === null ? 'Real' : (scenarios.find(s => s.id === active)?.name ?? `Scenario ${active}`)
  const compareLabel = compare === 0 ? 'Real'
    : compare !== null ? (scenarios.find(s => s.id === compare)?.name ?? `Scenario ${compare}`)
    : null

  const [currency, setCurrency]   = usePersistentState<string>('app_currency', 'CZK')
  const [period, setPeriod]       = usePersistentState('analysis_period', 0)
  const [acctModel, setAcctModel] = usePersistentState<'historical' | 'spot'>('analysis_acctModel', 'historical')
  const [customFrom, setCustomFrom] = useState(() => getFromDate(12))
  const [customTo,   setCustomTo]   = useState(() => formatDate(new Date()))
  const [isPickerOpen, setIsPickerOpen] = useState(false)

  const periodOptions = [
    { label: '1M',     value: 1  },
    { label: '3M',     value: 3  },
    { label: '1Y',     value: 12 },
    { label: 'All',    value: 0  },
    { label: period === -1 ? `${customFrom.substring(2).replace(/-/g, '/')} - ${customTo.substring(2).replace(/-/g, '/')}` : 'Custom', value: -1 },
  ]

  // Chart modes
  const [chartMode, setChartMode]       = usePersistentState<ChartMode>('analysis_chartMode', 'twr')
  const [rollingWindow, setRollingWindow] = usePersistentState('analysis_rollingWindow', 63)
  const [holdingsView, setHoldingsView] = usePersistentState<HoldingsView>('analysis_holdingsView', 'attribution')

  // Benchmark input and market symbols
  const [benchmarkInput, setBenchmarkInput]     = useState('SPY')
  const [marketSymbols, setMarketSymbols]       = useState<string[]>([])

  // Scenario Picker
  const [scenarioPickerOpen, setScenarioPickerOpen] = useState(false)
  const scenarioPickerRef = useRef<HTMLDivElement>(null)

  // Risk-free rate
  const [riskFreeRate, setRiskFreeRate]           = usePersistentState('analysis_riskFreeRate', 0.025)
  const [riskFreeRateInput, setRiskFreeRateInput] = usePersistentState('analysis_riskFreeRateInput', '2.50')

  const from = period === -1 ? customFrom : getFromDate(period)
  const to   = period === -1 ? customTo   : formatDate(new Date())

  // We need to resolve effectiveFrom first to pass to hooks. But portfolioHistory comes from useAnalysisData.
  // Wait, effectiveFrom depends on portfolioHistory. So we'll get it from useAnalysisData or handle it there.
  // The hooks compute effectiveFrom internally if we pass it, but to pass it we need portfolioHistory.
  // Actually, let's keep effectiveFrom as a let, and update it.
  
  // Actually, we can fetch market symbols
  useEffect(() => {
    getMarketSymbols().then(setMarketSymbols).catch(() => {})
  }, [])

  const analysisParams = { currency, acctModel, from, to, active, riskFreeRate, scenarios, effectiveFrom: from, compare }

  const analysisData = useAnalysisData(analysisParams)
  const { stats, portfolioHistory, mwrHistory, loading, refreshing, error, standaloneResults, standaloneLoading, standaloneRefreshing, standaloneError, loadStandalone, attributionData, attributionTWR, correlationData, holdingsLoading, holdingsError } = analysisData

  // Re-calculate effectiveFrom once we have portfolio history
  const effectiveFrom = period === 0 ? (portfolioHistory[0]?.date ?? from) : from
  const paramsWithEffectiveFrom = { ...analysisParams, effectiveFrom }

  // Update loadStandalone in analysisData to use effectiveFrom implicitly, but useBenchmarks needs it.
  const benchmarks = useBenchmarks({ ...paramsWithEffectiveFrom, loadStandalone })
  const { benchmarkSymbols, scenarioBenchmarks, cumulativeResults, compareResults, compareLoading, compareError, scenarioBenchmarkLoading, handleCompare, applyRiskFreeRate, handleRemoveSymbol, addScenarioBenchmark, removeScenarioBenchmark } = benchmarks

  const chartModeData = useChartModeData({ ...paramsWithEffectiveFrom, chartMode, rollingWindow, benchmarkSymbols, scenarioBenchmarks })
  const { drawdownResults, rollingSeries, chartModeLoading, chartModeError } = chartModeData

  const compareOverlay = useCompareOverlay({ ...paramsWithEffectiveFrom, compare })
  const { compareStats, compareTwrHistory, compareStandalone, compareDataLoading } = compareOverlay

  const chartData = useAnalysisChartData({
    portfolioHistory, mwrHistory, cumulativeResults, compareTwrHistory, compareLabel,
    scenarioBenchmarks, drawdownResults, rollingSeries, attributionData
  })
  const { mergedChartData, drawdownChartData, mwrChartData, rollingChartData, attrDisplay } = chartData

  // Close scenario picker on outside click
  useEffect(() => {
    if (!scenarioPickerOpen) return
    const handler = (e: MouseEvent) => {
      if (scenarioPickerRef.current && !scenarioPickerRef.current.contains(e.target as Node))
        setScenarioPickerOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [scenarioPickerOpen])

  const handleStatCardClick = (mode: ChartMode) => {
    setChartMode(mode)
    setTimeout(() => chartRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 50)
  }

  const handleRiskFreeRateBlur = () => {
    const parsed = parseFloat(riskFreeRateInput)
    const newRate = isNaN(parsed) ? riskFreeRate : Math.max(0, Math.min(20, parsed)) / 100
    setRiskFreeRateInput((newRate * 100).toFixed(2))
    if (newRate !== riskFreeRate) {
      setRiskFreeRate(newRate)
      applyRiskFreeRate(newRate)
    }
  }

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

  return (
    <PageLayout>
      {/* Header */}
      <div className="w-full flex flex-col items-center mb-16 text-center">
        <h1 className="text-3xl font-semibold text-slate-100">Performance Analysis</h1>
        <p className="text-slate-500 text-sm mt-4">Statistical attribution and benchmark alignment</p>
      </div>

      {/* Controls */}
      <div className="flex flex-wrap justify-center items-end gap-4 mb-20">
        <SegmentedControl label="Currency" options={CURRENCY_OPTIONS} value={currency} onChange={setCurrency} />
        
        <div className="relative">
          <SegmentedControl
            label="Time Period"
            options={periodOptions}
            value={period}
            onChange={p => {
              if (p === -1) {
                if (period !== -1) {
                  setCustomFrom(getFromDate(period === 0 ? 12 : period))
                  setCustomTo(formatDate(new Date()))
                }
                setIsPickerOpen(true)
              } else {
                setIsPickerOpen(false)
              }
              setPeriod(p)
            }}
          />
          {period === -1 && isPickerOpen && (
            <div className="absolute top-full left-1/2 -translate-x-1/2 mt-2 z-50">
              <DateRangePicker
                initialFrom={customFrom}
                initialTo={customTo}
                minDate={portfolioHistory[0]?.date}
                onApply={(f, t) => { setCustomFrom(f); setCustomTo(t); setIsPickerOpen(false) }}
                onCancel={() => setIsPickerOpen(false)}
              />
            </div>
          )}
        </div>

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

      {error && <ErrorAlert message={error} className="mb-10" />}

      {/* ── Section 1: Risk & Return Metrics ─────────────────────────────────── */}
      <div className="w-full mb-16 relative">
        {refreshing && (
          <div className="absolute top-0 right-4 w-4 h-4 rounded-full border-2 border-indigo-400/30 border-t-indigo-400 animate-spin" />
        )}
        <div className="flex items-center justify-center gap-3 mb-8">
          <h2 className="text-xl font-semibold text-slate-100">Risk &amp; Return Metrics</h2>
          <CompareScenariosChip onCompare={() => {}} />
          {stats && portfolioStandalone && (
            <div className="relative group">
              <button
                onClick={() => {
                  const isComparing = compare !== null && compareStats !== null
                  navigate('/llm', { state: { initialPrompt: isComparing
                    ? { promptType: 'risk_metrics_comparison', displayMessage: `Compare risk & return: ${activeLabel} vs ${compareLabel}`, extraParams: { scenario_id: active ?? 0, scenario_id_a: active ?? 0, scenario_id_b: compare, currency, from, to, accounting_model: acctModel, risk_free_rate: riskFreeRate } }
                    : { promptType: 'risk_metrics', displayMessage: `Analyze my portfolio's risk & return metrics for ${periodLabel}`, extraParams: { currency, from, to, accounting_model: acctModel, risk_free_rate: riskFreeRate } }
                  }})
                }}
                className="text-slate-500 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
              >
                <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                  <path d="M12 2L13.5 8.5L20 10L13.5 11.5L12 18L10.5 11.5L4 10L10.5 8.5Z" />
                  <path d="M19 1l.9 2.6 2.6.9-2.6.9L19 8.5l-.9-2.6L15.5 4l2.6-.9z" opacity=".6" />
                  <path d="M5 17l.7 2.1L7.8 20l-2.1.9L5 23l-.7-2.1L2.2 20l2.1-.9z" opacity=".6" />
                </svg>
              </button>
              <HoverTooltip direction="down" className="w-max whitespace-nowrap">
                {compare !== null && compareStats !== null ? `AI comparison: ${activeLabel} vs ${compareLabel}` : 'AI analysis of risk & return metrics'}
              </HoverTooltip>
            </div>
          )}
        </div>

        {loading ? (
          <Spinner label="Compiling statistics…" className="py-10" />
        ) : stats ? (
          <StatCards
            stats={stats}
            compare={compare}
            compareStats={compareStats}
            compareDataLoading={compareDataLoading}
            activeLabel={activeLabel}
            compareLabel={compareLabel}
            handleStatCardClick={handleStatCardClick}
            standaloneLoading={standaloneLoading}
            portfolioStandalone={portfolioStandalone}
            compareStandalone={compareStandalone}
          />
        ) : (
          <p className="text-slate-500 text-center text-sm py-10">Historical context required to generate statistics.</p>
        )}
      </div>

      {/* ── Section 2: Benchmarking ───────────────────────────────────────────── */}
      <BenchmarkPanel
        marketSymbols={marketSymbols}
        scenarios={scenarios}
        active={active}
        benchmarkInput={benchmarkInput}
        setBenchmarkInput={setBenchmarkInput}
        handleCompare={handleCompare}
        compareLoading={compareLoading}
        compareError={compareError}
        benchmarkSymbols={benchmarkSymbols}
        handleRemoveSymbol={handleRemoveSymbol}
        addScenarioBenchmark={addScenarioBenchmark}
        removeScenarioBenchmark={removeScenarioBenchmark}
        scenarioPickerOpen={scenarioPickerOpen}
        setScenarioPickerOpen={setScenarioPickerOpen}
        scenarioBenchmarks={scenarioBenchmarks}
        scenarioBenchmarkLoading={scenarioBenchmarkLoading}
        standaloneResults={standaloneResults}
        standaloneRefreshing={standaloneRefreshing}
        standaloneError={standaloneError}
        compareResults={compareResults}
        portfolioStandalone={portfolioStandalone}
        periodLabel={periodLabel}
        effectiveFrom={effectiveFrom}
        to={to}
        currency={currency}
        acctModel={acctModel}
        riskFreeRate={riskFreeRate}
      >

        {/* Chart mode controls */}
        <div ref={chartRef} className="flex flex-wrap justify-center items-center gap-4 mb-4">
          <SegmentedControl
            label="Chart Mode"
            options={CHART_MODE_OPTIONS.map(o => ({
              ...o,
              disabled: o.value === 'rolling_beta' && benchmarkSymbols.length === 0 && scenarioBenchmarks.length === 0,
              tooltip: o.value === 'rolling_beta' && benchmarkSymbols.length === 0 && scenarioBenchmarks.length === 0
                ? 'Add a benchmark symbol or scenario overlay first to enable rolling beta'
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
                  {compareLabel !== null && compareTwrHistory.length > 0 && (
                    <Line type="monotone" dataKey="Compare" name={compareLabel} stroke={COMPARE_COLOR} strokeWidth={2} strokeDasharray="4 2" dot={false} opacity={0.7} />
                  )}
                  {benchmarkSymbols.map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
                  ))}
                  {scenarioBenchmarks.map((sb, i) => (
                    <Line key={`[S] ${sb.name}`} type="monotone" dataKey={`[S] ${sb.name}`} name={sb.name} stroke={COLORS[(benchmarkSymbols.length + i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="8 3" dot={false} opacity={0.85} />
                  ))}
                </LineChart>
              ) : chartMode === 'mwr' ? (
                <LineChart data={mwrChartData} margin={{ top: 10, right: 20, left: 10, bottom: 36 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" vertical={false} opacity={0.3} />
                  <XAxis dataKey="date" tickFormatter={xTickFormatter} minTickGap={60} tick={AXIS_STYLE} axisLine={{ stroke: '#2a2e42' }} tickLine={false} label={{ value: 'Date', position: 'insideBottom', offset: -16, ...AXIS_LABEL_STYLE }} />
                  <YAxis domain={['auto', 'auto']} tickFormatter={val => `${Number(val).toFixed(0)}%`} tick={AXIS_STYLE} axisLine={false} tickLine={false} width={56} label={{ value: 'Return (%)', angle: -90, position: 'insideLeft', offset: 16, ...AXIS_LABEL_STYLE }} />
                  <Tooltip contentStyle={RECHARTS_TOOLTIP_STYLE} labelStyle={RECHARTS_LABEL_STYLE} itemStyle={RECHARTS_ITEM_STYLE} formatter={(value, name) => [`${Number(value).toFixed(2)}%`, String(name)]} />
                  <Legend wrapperStyle={{ fontSize: '10px', color: '#64748b', paddingTop: '30px', fontWeight: '900', textTransform: 'uppercase', letterSpacing: '0.15em' }} />
                  <Line type="monotone" dataKey="Portfolio" name="Portfolio (MWR)" stroke={COLORS[0]} strokeWidth={3} dot={false} animationDuration={1200} />
                  {benchmarkSymbols.map((sym, i) => (
                    <Line key={sym} type="monotone" dataKey={sym} name={`${sym} (TWR)`} stroke={COLORS[(i + 1) % COLORS.length]} strokeWidth={1.5} strokeDasharray="6 6" dot={false} />
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
                  {Object.keys(rollingSeries).map((sym, i) => (
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

      </BenchmarkPanel>

      {/* ── Section 3: Holdings Analysis ─────────────────────────────────────── */}
      <HoldingsPanel
        attributionData={attributionData}
        holdingsView={holdingsView}
        setHoldingsView={setHoldingsView}
        holdingsLoading={holdingsLoading}
        holdingsError={holdingsError}
        attrDisplay={attrDisplay}
        attributionTWR={attributionTWR}
        correlationData={correlationData}
        compare={compare}
        compareStats={compareStats}
        activeLabel={activeLabel}
        compareLabel={compareLabel}
        periodLabel={periodLabel}
        effectiveFrom={effectiveFrom}
        to={to}
        currency={currency}
        acctModel={acctModel}
        riskFreeRate={riskFreeRate}
        active={active}
      />
    </PageLayout>
  )
}
