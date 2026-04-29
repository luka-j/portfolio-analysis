import { useNavigate } from 'react-router-dom'
import React, { useRef, useEffect } from 'react'
import AutocompleteInput from '../AutocompleteInput'
import ErrorAlert from '../ErrorAlert'
import HoverTooltip from '../HoverTooltip'
import { STANDALONE_TOOLTIPS } from './StatCards'
import type { BenchmarkResult, StandaloneResult, DailyValue } from '../../api'

// eslint-disable-next-line react-refresh/only-export-components
export const COMPARE_TOOLTIPS: Record<string, string> = {
  Security:      'The benchmark being compared against your portfolio.',
  Alpha:         'Annualized excess return over the benchmark after adjusting for market risk. Positive means outperformance.',
  Beta:          'Sensitivity to benchmark moves. Beta > 1 means your portfolio is more volatile than the benchmark.',
  Treynor:       'Return per unit of systematic (market) risk, using beta as the risk measure. Higher is better.',
  'Tracking Err':'Annualized deviation of your returns from the benchmark. Lower means closer tracking.',
  'Info Ratio':  'Active return divided by tracking error. Measures the consistency of outperformance.',
  Correlation:   'How closely your returns move with the benchmark. 1 = perfect alignment, 0 = no relationship.',
}

interface BenchmarkPanelProps {
  marketSymbols: string[]
  scenarios: Array<{ id: number; name: string }>
  active: number | null
  benchmarkInput: string
  setBenchmarkInput: (v: string) => void
  handleCompare: (input: string) => Promise<boolean>
  compareLoading: boolean
  compareError: string | null
  benchmarkSymbols: string[]
  handleRemoveSymbol: (sym: string) => void
  addScenarioBenchmark: (id: number, name: string) => void
  removeScenarioBenchmark: (id: number) => void
  scenarioPickerOpen: boolean
  setScenarioPickerOpen: React.Dispatch<React.SetStateAction<boolean>>
  scenarioBenchmarks: Array<{ id: number; name: string; twr: DailyValue[]; mwr: DailyValue[] }>
  scenarioBenchmarkLoading: boolean
  standaloneResults: StandaloneResult[]
  standaloneRefreshing: boolean
  standaloneError: string
  compareResults: BenchmarkResult[]
  portfolioStandalone: StandaloneResult | null
  periodLabel: string
  effectiveFrom: string
  to: string
  currency: string
  acctModel: 'historical' | 'spot'
  riskFreeRate: number
  children?: React.ReactNode
}

export default function BenchmarkPanel({
  marketSymbols,
  scenarios,
  active,
  benchmarkInput,
  setBenchmarkInput,
  handleCompare,
  compareLoading,
  compareError,
  benchmarkSymbols,
  handleRemoveSymbol,
  addScenarioBenchmark,
  removeScenarioBenchmark,
  scenarioPickerOpen,
  setScenarioPickerOpen,
  scenarioBenchmarks,
  scenarioBenchmarkLoading,
  standaloneResults,
  standaloneRefreshing,
  standaloneError,
  compareResults,
  portfolioStandalone,
  periodLabel,
  effectiveFrom,
  to,
  currency,
  acctModel,
  riskFreeRate,
  children,
}: BenchmarkPanelProps) {
  const navigate = useNavigate()
  const scenarioPickerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!scenarioPickerOpen) return
    const handler = (e: MouseEvent) => {
      if (scenarioPickerRef.current && !scenarioPickerRef.current.contains(e.target as Node))
        setScenarioPickerOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [scenarioPickerOpen, setScenarioPickerOpen])

  return (
    <div className="w-full mb-20">
      <h2 className="text-xl font-semibold text-slate-100 mb-8 text-center">Benchmarking</h2>

      <div className="flex flex-col items-center gap-3 mb-10 w-full max-w-2xl mx-auto">
        <div className="flex flex-col sm:flex-row items-center w-full gap-4">
          <div className="w-full">
            <AutocompleteInput
              options={[
                ...marketSymbols.map(sym => ({ value: sym, label: 'Stock Ticker' })),
                ...scenarios.filter(s => s.id !== active).map(s => ({ value: s.name, label: 'Scenario' })),
              ]}
              value={benchmarkInput}
              onChange={setBenchmarkInput}
              onSelect={async opt => {
                if (await handleCompare(opt.value)) setBenchmarkInput('')
              }}
              onKeyDown={async e => {
                if (e.key === 'Enter' && !e.defaultPrevented) {
                  if (await handleCompare(benchmarkInput)) setBenchmarkInput('')
                }
              }}
              placeholder="Symbols (SPY, QQQ) or Scenario Name"
            />
          </div>
          <button
            onClick={async () => {
              if (await handleCompare(benchmarkInput)) setBenchmarkInput('')
            }} disabled={compareLoading}
            className="whitespace-nowrap px-8 py-3 bg-indigo-600 text-white text-sm font-medium rounded-xl hover:bg-indigo-500 transition-all disabled:opacity-50 shadow-lg"
          >
            {compareLoading ? 'Processing…' : 'Execute'}
          </button>
          {scenarios.length > 0 && (
            <div className="relative" ref={scenarioPickerRef}>
              <button
                onClick={() => setScenarioPickerOpen(p => !p)}
                disabled={scenarioBenchmarkLoading}
                className="whitespace-nowrap px-4 py-3 bg-surface border border-amber-500/30 text-amber-400 text-sm font-medium rounded-xl hover:bg-amber-500/10 transition-all disabled:opacity-50"
              >
                {scenarioBenchmarkLoading ? '…' : '+ Scenario'}
              </button>
              {scenarioPickerOpen && (
                <div className="absolute top-full mt-1 right-0 z-30 min-w-[160px] bg-panel border border-border-dim/80 rounded-xl shadow-2xl overflow-hidden">
                  {(() => {
                    const options: { id: number; name: string }[] = []
                    if (active !== null && !scenarioBenchmarks.find(sb => sb.id === 0)) {
                      options.push({ id: 0, name: 'Real Portfolio' })
                    }
                    for (const s of scenarios) {
                      if (s.id !== active && !scenarioBenchmarks.find(sb => sb.id === s.id)) {
                        options.push({ id: s.id, name: s.name || `Scenario ${s.id}` })
                      }
                    }

                    if (options.length === 0) return <div className="px-4 py-3 text-xs text-slate-500 whitespace-nowrap">No scenarios available</div>
                    return options.map(opt => (
                      <button
                        key={opt.id}
                        onClick={() => { addScenarioBenchmark(opt.id, opt.name); setScenarioPickerOpen(false) }}
                        className="w-full text-left px-4 py-2 text-sm text-slate-300 hover:bg-white/10 transition-colors"
                      >
                        {opt.name}
                      </button>
                    ))
                  })()}
                </div>
              )}
            </div>
          )}
        </div>
        {(benchmarkSymbols.length > 0 || scenarioBenchmarks.length > 0) && (
          <div className="flex flex-wrap gap-2 w-full">
            {benchmarkSymbols.map(sym => (
              <span key={sym} className="inline-flex items-center gap-2 px-3 py-1.5 bg-surface border border-border-dim/60 rounded-lg text-sm font-medium text-slate-300">
                {sym}
                <button onClick={() => handleRemoveSymbol(sym)} className="text-slate-500 hover:text-red-400 transition-colors leading-none" aria-label={`Remove ${sym}`}>×</button>
              </span>
            ))}
            {scenarioBenchmarks.map(sb => (
              <span key={sb.id} className="inline-flex items-center gap-2 px-3 py-1.5 bg-surface border border-amber-500/25 rounded-lg text-sm font-medium text-amber-400/80">
                {sb.name}
                <button onClick={() => removeScenarioBenchmark(sb.id)} className="text-amber-500/50 hover:text-red-400 transition-colors leading-none" aria-label={`Remove ${sb.name}`}>×</button>
              </span>
            ))}
          </div>
        )}
        {compareError && (
          <p className="w-full px-4 py-3 rounded-xl bg-red-500/10 text-red-400 text-sm border border-red-500/20">{compareError}</p>
        )}
        <p className="text-xs text-slate-500 text-center">
          Compare against tickers (e.g. SPY, QQQ) or select from your <span className="text-amber-500/80">Scenarios</span>
        </p>
      </div>

      {children}

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
  )
}
