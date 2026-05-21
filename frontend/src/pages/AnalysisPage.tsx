import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'

import PageLayout from '../components/PageLayout'
import HoverTooltip from '../components/HoverTooltip'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import ErrorAlert from '../components/ErrorAlert'
import CompareScenariosChip from '../components/CompareScenariosChip'
import StatCards from '../components/analysis/StatCards'
import BenchmarkPanel from '../components/analysis/BenchmarkPanel'
import HoldingsPanel from '../components/analysis/HoldingsPanel'
import AnalysisControls from '../components/analysis/AnalysisControls'
import { getMarketSymbols } from '../api'
import { formatDate, getFromDate } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'
import { useScenario } from '../context/ScenarioContext'


const CHART_MODE_OPTIONS = [
  { label: 'TWR',                value: 'twr'                as const, tooltip: 'Time-Weighted Return. Measures compound rate of growth, eliminating the distorting effects of cash inflows and outflows. Best for comparing against benchmarks.' },
  { label: 'MWR',                value: 'mwr'                as const, tooltip: 'Money-Weighted Return (IRR). Measures the performance of your actual cash flows. Best for seeing how your timing of deposits/withdrawals affected your total return.' },
  { label: 'Rolling Sharpe',     value: 'rolling_sharpe'     as const, tooltip: 'Risk-adjusted return over a rolling window. Higher is better.' },
  { label: 'Rolling Sortino',    value: 'rolling_sortino'    as const, tooltip: 'Return adjusted for downside risk only over a rolling window.' },
  { label: 'Rolling Volatility', value: 'rolling_volatility' as const, tooltip: 'Standard deviation of returns over a rolling window. Measures price fluctuations.' },
  { label: 'Rolling Beta',       value: 'rolling_beta'       as const, tooltip: 'Measures sensitivity to benchmark movements over a rolling window. 1.0 means it moves exactly with the benchmark.' },
  { label: 'Drawdown',           value: 'drawdown'           as const, tooltip: 'Percentage decline from the highest peak to the current value.' },
]

const WINDOW_OPTIONS = [
  { label: '1M',  value: 21  },
  { label: '3M',  value: 63  },
  { label: '6M',  value: 126 },
]

import { useAnalysisData } from './hooks/useAnalysisData'
import { useBenchmarks } from './hooks/useBenchmarks'
import { useChartModeData } from './hooks/useChartModeData'
import { useCompareOverlay } from './hooks/useCompareOverlay'
import { useAnalysisChartData } from './hooks/useAnalysisChartData'
import type { ChartMode } from './hooks/types'
import PerformanceChart from '../components/analysis/PerformanceChart'

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

  // Chart modes
  const [chartMode, setChartMode]       = usePersistentState<ChartMode>('analysis_chartMode', 'twr')
  const [rollingWindow, setRollingWindow] = usePersistentState('analysis_rollingWindow', 63)

  // Benchmark input and market symbols
  const [benchmarkInput, setBenchmarkInput]     = useState('SPY')
  const [marketSymbols, setMarketSymbols]       = useState<string[]>([])

  // Scroll-spy active section
  const [activeSection, setActiveSection] = useState<string>('section-risk')

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

  // Scroll-spy observer
  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach(entry => {
          if (entry.isIntersecting) {
            setActiveSection(entry.target.id)
          }
        })
      },
      { rootMargin: '-40% 0px -40% 0px' }
    )
    const sections = ['section-risk', 'section-benchmarking', 'section-attribution', 'section-correlation']
    sections.forEach(id => {
      const el = document.getElementById(id)
      if (el) observer.observe(el)
    })
    return () => observer.disconnect()
  }, [])

  const handleRiskFreeRateBlur = () => {
    const parsed = parseFloat(riskFreeRateInput)
    const newRate = isNaN(parsed) ? riskFreeRate : Math.max(0, Math.min(20, parsed)) / 100
    setRiskFreeRateInput((newRate * 100).toFixed(2))
    if (newRate !== riskFreeRate) {
      setRiskFreeRate(newRate)
      applyRiskFreeRate(newRate)
    }
  }

  const portfolioStandalone = standaloneResults.find(r => r.symbol === 'Portfolio') ?? null

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
      {/* Scroll-spy Navigation */}
      <div className="fixed top-1/2 right-6 -translate-y-1/2 flex flex-col gap-4 z-50 hidden xl:flex">
        {[
          { id: 'section-risk', label: 'Risk & Return Metrics' },
          { id: 'section-benchmarking', label: 'Benchmarking & Charts' },
          { id: 'section-attribution', label: 'Performance Attribution' },
          { id: 'section-correlation', label: 'Asset Correlation' }
        ].map(section => (
          <button
            key={section.id}
            onClick={() => {
              const el = document.getElementById(section.id)
              if (el) {
                const y = el.getBoundingClientRect().top + window.scrollY - 100
                window.scrollTo({ top: y, behavior: 'smooth' })
              }
            }}
            className={`w-2.5 h-2.5 rounded-full transition-all relative group ${activeSection === section.id ? 'bg-indigo-400 scale-125' : 'bg-slate-700 hover:bg-slate-500'}`}
          >
            <span className="absolute right-full mr-4 top-1/2 -translate-y-1/2 px-2.5 py-1.5 bg-surface border border-white/5 shadow-xl rounded-lg text-xs font-medium text-slate-300 opacity-0 group-hover:opacity-100 whitespace-nowrap pointer-events-none transition-opacity">
              {section.label}
            </span>
          </button>
        ))}
      </div>

      {/* Header */}
      <div className="w-full flex flex-col items-center mb-16 text-center">
        <h1 className="text-3xl font-semibold text-slate-100">Performance Analysis</h1>
        <p className="text-slate-500 text-sm mt-4">Statistical attribution and benchmark alignment</p>
      </div>

      {/* Controls */}
      <AnalysisControls
        currency={currency}
        setCurrency={setCurrency}
        period={period}
        setPeriod={setPeriod}
        customFrom={customFrom}
        setCustomFrom={setCustomFrom}
        customTo={customTo}
        setCustomTo={setCustomTo}
        isPickerOpen={isPickerOpen}
        setIsPickerOpen={setIsPickerOpen}
        portfolioHistory={portfolioHistory}
        acctModel={acctModel}
        setAcctModel={setAcctModel}
        riskFreeRate={riskFreeRate}
        riskFreeRateInput={riskFreeRateInput}
        setRiskFreeRateInput={setRiskFreeRateInput}
        handleRiskFreeRateBlur={handleRiskFreeRateBlur}
        applyRiskFreeRate={applyRiskFreeRate}
      />

      {error && <ErrorAlert message={error} className="mb-10" />}

      {/* ── Section 1: Risk & Return Metrics ─────────────────────────────────── */}
      <div id="section-risk" className="w-full mb-16 relative scroll-mt-24">
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
      <div id="section-benchmarking" className="scroll-mt-24">
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
            <PerformanceChart
              chartMode={chartMode}
              mergedChartData={mergedChartData}
              mwrChartData={mwrChartData}
              drawdownChartData={drawdownChartData}
              rollingChartData={rollingChartData}
              rollingMetricLabel={rollingMetricLabel}
              compareLabel={compareLabel}
              compareTwrHistory={compareTwrHistory}
              benchmarkSymbols={benchmarkSymbols}
              scenarioBenchmarks={scenarioBenchmarks}
              rollingSeries={rollingSeries}
            />
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center py-20 gap-3 text-slate-500 opacity-60 mb-10">
            <svg className="w-10 h-10" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" /></svg>
            <p className="text-sm font-medium">Ready to compare — add a benchmark symbol above</p>
          </div>
        )}

      </BenchmarkPanel>
      </div>

      {/* ── Section 3: Holdings Analysis ─────────────────────────────────────── */}
      <HoldingsPanel
        attributionData={attributionData}
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
