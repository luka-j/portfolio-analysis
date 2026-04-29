import HoverTooltip from '../HoverTooltip'
import Spinner from '../Spinner'
import type { StatsResponse, StandaloneResult } from '../../api'
import type { ChartMode } from '../../pages/hooks/types'

// eslint-disable-next-line react-refresh/only-export-components
export const STAT_TOOLTIPS: Record<string, string> = {
  twr: 'Time-weighted return. Eliminates the effect of cash flows — best for evaluating portfolio manager skill.',
  mwr: 'Money-weighted return. Reflects your actual return including the timing and size of your deposits and withdrawals.',
}

// eslint-disable-next-line react-refresh/only-export-components
export const STANDALONE_TOOLTIPS: Record<string, string> = {
  sharpe:     'Excess return above the risk-free rate, divided by total volatility. Higher numbers mean better risk-adjusted performance.',
  vami:       'Value Added Monthly Index. Growth of a 1,000 investment — reflects compounded total return.',
  volatility: 'Annualized standard deviation of daily returns. Measures how much the portfolio fluctuates.',
  sortino:    'Like Sharpe, but only penalizes downside volatility below the risk-free rate, ignoring upside swings. Higher is better.',
  max_drawdown: 'Largest peak-to-trough decline over the period. Measures worst-case loss from a high point.',
}

interface StatCardsProps {
  stats: StatsResponse
  compare: number | null
  compareStats: StatsResponse | null
  compareDataLoading: boolean
  activeLabel: string
  compareLabel: string | null
  handleStatCardClick: (mode: ChartMode) => void
  standaloneLoading: boolean
  portfolioStandalone: StandaloneResult | null
  compareStandalone: StandaloneResult | null
}

export default function StatCards({
  stats,
  compare,
  compareStats,
  compareDataLoading,
  activeLabel,
  compareLabel,
  handleStatCardClick,
  standaloneLoading,
  portfolioStandalone,
  compareStandalone,
}: StatCardsProps) {
  if (compare !== null && (compareStats !== null || compareDataLoading)) {
    return (
      <div className="flex flex-col lg:flex-row gap-6">
        <div className="flex-1 min-w-0">
          <p className="text-[9px] font-black text-slate-400 uppercase tracking-[0.2em] mb-4 text-center">{activeLabel}</p>
          <div className="grid grid-cols-2 gap-3">
            {(() => {
              const twrVal = typeof stats.statistics['twr'] === 'number' ? stats.statistics['twr'] as number : null
              const mwrVal = typeof stats.statistics['mwr'] === 'number' ? stats.statistics['mwr'] as number : null
              return (<>
                {twrVal !== null && (
                  <button onClick={() => handleStatCardClick('twr')} className="relative group bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
                    <HoverTooltip className="w-56">{STAT_TOOLTIPS.twr}</HoverTooltip>
                    <p className="text-xs font-medium text-slate-500 mb-1 uppercase">TWR</p>
                    <p className={`text-xl font-semibold tabular-nums ${twrVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{twrVal >= 0 ? '+' : ''}{(twrVal * 100).toFixed(2)}%</p>
                    <p className="text-[8px] text-slate-600 mt-1 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
                  </button>
                )}
                {mwrVal !== null && (
                  <button onClick={() => handleStatCardClick('mwr')} className="relative group bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
                    <HoverTooltip className="w-56">{STAT_TOOLTIPS.mwr}</HoverTooltip>
                    <p className="text-xs font-medium text-slate-500 mb-1 uppercase">MWR</p>
                    <p className={`text-xl font-semibold tabular-nums ${mwrVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{mwrVal >= 0 ? '+' : ''}{(mwrVal * 100).toFixed(2)}%</p>
                    <p className="text-[8px] text-slate-600 mt-1 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
                  </button>
                )}
              </>)
            })()}
            {standaloneLoading && !portfolioStandalone && (
              <div className="col-span-2 flex justify-center py-2"><Spinner label="Computing…" /></div>
            )}
            {portfolioStandalone && (<>
              <button onClick={() => handleStatCardClick('rolling_sharpe')} className="relative group bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
                <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sharpe}</HoverTooltip>
                <p className="text-xs font-medium text-slate-500 mb-1">Sharpe</p>
                <p className={`text-xl font-semibold tabular-nums ${portfolioStandalone.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{portfolioStandalone.sharpe_ratio.toFixed(3)}</p>
                <p className="text-[8px] text-slate-600 mt-1 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
              </button>
              <div className="relative group bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-white/5 cursor-help">
                <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.vami}</HoverTooltip>
                <p className="text-xs font-medium text-slate-500 mb-1">VAMI</p>
                <p className="text-xl font-semibold tabular-nums text-slate-100">{portfolioStandalone.vami.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}</p>
              </div>
              <button onClick={() => handleStatCardClick('rolling_volatility')} className="relative group bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
                <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.volatility}</HoverTooltip>
                <p className="text-xs font-medium text-slate-500 mb-1">Volatility</p>
                <p className="text-xl font-semibold tabular-nums text-slate-400">{(portfolioStandalone.volatility * 100).toFixed(2)}%</p>
                <p className="text-[8px] text-slate-600 mt-1 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
              </button>
              <button onClick={() => handleStatCardClick('rolling_sortino')} className="relative group bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
                <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sortino}</HoverTooltip>
                <p className="text-xs font-medium text-slate-500 mb-1">Sortino</p>
                <p className={`text-xl font-semibold tabular-nums ${portfolioStandalone.sortino_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{portfolioStandalone.sortino_ratio.toFixed(3)}</p>
                <p className="text-[8px] text-slate-600 mt-1 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
              </button>
              <button onClick={() => handleStatCardClick('drawdown')} className="relative group bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-rose-500/30 hover:bg-surface/60 transition-all">
                <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.max_drawdown}</HoverTooltip>
                <p className="text-xs font-medium text-slate-500 mb-1">Max DD</p>
                <p className="text-xl font-semibold tabular-nums text-rose-400">-{(portfolioStandalone.max_drawdown * 100).toFixed(2)}%</p>
                <p className="text-[8px] text-slate-600 mt-1 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
              </button>
            </>)}
          </div>
        </div>

        <div className="hidden lg:block w-px bg-border-dim/30 self-stretch" />
        <div className="block lg:hidden h-px bg-border-dim/30 w-full" />

        <div className="flex-1 min-w-0">
          <p className="text-[9px] font-black text-amber-400/70 uppercase tracking-[0.2em] mb-4 text-center">{compareLabel}</p>
          {compareDataLoading ? (
            <div className="flex justify-center py-10"><Spinner label="Loading…" /></div>
          ) : compareStats ? (
            <div className="grid grid-cols-2 gap-3">
              {(() => {
                const twrVal = typeof compareStats.statistics['twr'] === 'number' ? compareStats.statistics['twr'] as number : null
                const mwrVal = typeof compareStats.statistics['mwr'] === 'number' ? compareStats.statistics['mwr'] as number : null
                return (<>
                  {twrVal !== null && (
                    <div className="bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-amber-500/10">
                      <p className="text-xs font-medium text-slate-500 mb-1 uppercase">TWR</p>
                      <p className={`text-xl font-semibold tabular-nums ${twrVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{twrVal >= 0 ? '+' : ''}{(twrVal * 100).toFixed(2)}%</p>
                    </div>
                  )}
                  {mwrVal !== null && (
                    <div className="bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-amber-500/10">
                      <p className="text-xs font-medium text-slate-500 mb-1 uppercase">MWR</p>
                      <p className={`text-xl font-semibold tabular-nums ${mwrVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{mwrVal >= 0 ? '+' : ''}{(mwrVal * 100).toFixed(2)}%</p>
                    </div>
                  )}
                </>)
              })()}
              {compareStandalone && (<>
                <div className="bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-amber-500/10">
                  <p className="text-xs font-medium text-slate-500 mb-1">Sharpe</p>
                  <p className={`text-xl font-semibold tabular-nums ${compareStandalone.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{compareStandalone.sharpe_ratio.toFixed(3)}</p>
                </div>
                <div className="bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-amber-500/10">
                  <p className="text-xs font-medium text-slate-500 mb-1">VAMI</p>
                  <p className="text-xl font-semibold tabular-nums text-slate-100">{compareStandalone.vami.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}</p>
                </div>
                <div className="bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-amber-500/10">
                  <p className="text-xs font-medium text-slate-500 mb-1">Volatility</p>
                  <p className="text-xl font-semibold tabular-nums text-slate-400">{(compareStandalone.volatility * 100).toFixed(2)}%</p>
                </div>
                <div className="bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-amber-500/10">
                  <p className="text-xs font-medium text-slate-500 mb-1">Sortino</p>
                  <p className={`text-xl font-semibold tabular-nums ${compareStandalone.sortino_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{compareStandalone.sortino_ratio.toFixed(3)}</p>
                </div>
                <div className="bg-surface/40 rounded-2xl px-4 py-5 flex flex-col items-center text-center border border-amber-500/10">
                  <p className="text-xs font-medium text-slate-500 mb-1">Max DD</p>
                  <p className="text-xl font-semibold tabular-nums text-rose-400">-{(compareStandalone.max_drawdown * 100).toFixed(2)}%</p>
                </div>
              </>)}
            </div>
          ) : null}
        </div>
      </div>
    )
  }

  return (
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
            const compareVal = compareStats ? compareStats.statistics['twr'] : null
            const delta = compareVal !== null && compareVal !== undefined ? numVal - (compareVal as number) : null
            return (
              <button key={key} onClick={() => handleStatCardClick('twr')} className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
                {tooltip && <HoverTooltip className="w-56">{tooltip}</HoverTooltip>}
                <p className="text-sm font-medium text-slate-500 mb-2 uppercase">TWR</p>
                <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%</p>
                {delta !== null && <p className={`text-xs tabular-nums mt-1 ${delta >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{delta >= 0 ? '+' : ''}{(delta * 100).toFixed(2)}% vs {compareLabel}</p>}
                <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
              </button>
            )
          }
          if (key === 'mwr') {
            const compareVal = compareStats ? compareStats.statistics['mwr'] : null
            const delta = compareVal !== null && compareVal !== undefined ? numVal - (compareVal as number) : null
            return (
              <button key={key} onClick={() => handleStatCardClick('mwr')} className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
                {tooltip && <HoverTooltip className="w-56">{tooltip}</HoverTooltip>}
                <p className="text-sm font-medium text-slate-500 mb-2 uppercase">MWR</p>
                <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%</p>
                {delta !== null && <p className={`text-xs tabular-nums mt-1 ${delta >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{delta >= 0 ? '+' : ''}{(delta * 100).toFixed(2)}% vs {compareLabel}</p>}
                <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
              </button>
            )
          }
          return (
            <div key={key} className={`relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 ${tooltip ? 'cursor-help' : ''}`}>
              {tooltip && <HoverTooltip className="w-56">{tooltip}</HoverTooltip>}
              <p className="text-sm font-medium text-slate-500 mb-2 capitalize">{key.replace(/_/g, ' ')}</p>
              <p className={`text-2xl font-semibold tabular-nums ${numVal >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{numVal >= 0 ? '+' : ''}{(numVal * 100).toFixed(2)}%</p>
            </div>
          )
        })
      })()}
      {standaloneLoading && !portfolioStandalone && (
        <div className="col-span-2 md:col-span-4 flex justify-center py-4"><Spinner label="Computing risk metrics…" /></div>
      )}
      {portfolioStandalone && (
        <>
          <button onClick={() => handleStatCardClick('rolling_sharpe')} className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
            <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sharpe}</HoverTooltip>
            <p className="text-sm font-medium text-slate-500 mb-2">Sharpe Ratio</p>
            <p className={`text-2xl font-semibold tabular-nums ${portfolioStandalone.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{portfolioStandalone.sharpe_ratio.toFixed(3)}</p>
            {compareStandalone !== null && <p className={`text-xs tabular-nums mt-1 ${portfolioStandalone.sharpe_ratio - compareStandalone.sharpe_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{portfolioStandalone.sharpe_ratio - compareStandalone.sharpe_ratio >= 0 ? '+' : ''}{(portfolioStandalone.sharpe_ratio - compareStandalone.sharpe_ratio).toFixed(3)} vs {compareLabel}</p>}
            <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
          </button>
          <div className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-help">
            <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.vami}</HoverTooltip>
            <p className="text-sm font-medium text-slate-500 mb-2">VAMI</p>
            <p className="text-2xl font-semibold tabular-nums text-slate-100">{portfolioStandalone.vami.toLocaleString(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}</p>
          </div>
          <button onClick={() => handleStatCardClick('rolling_volatility')} className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
            <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.volatility}</HoverTooltip>
            <p className="text-sm font-medium text-slate-500 mb-2">Volatility</p>
            <p className="text-2xl font-semibold tabular-nums text-slate-400">{(portfolioStandalone.volatility * 100).toFixed(2)}%</p>
            <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
          </button>
          <button onClick={() => handleStatCardClick('rolling_sortino')} className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-indigo-500/30 hover:bg-surface/60 transition-all">
            <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.sortino}</HoverTooltip>
            <p className="text-sm font-medium text-slate-500 mb-2">Sortino Ratio</p>
            <p className={`text-2xl font-semibold tabular-nums ${portfolioStandalone.sortino_ratio >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>{portfolioStandalone.sortino_ratio.toFixed(3)}</p>
            <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View rolling →</p>
          </button>
          <button onClick={() => handleStatCardClick('drawdown')} className="relative group bg-surface/40 rounded-3xl px-8 py-8 flex flex-col items-center text-center border border-white/5 cursor-pointer hover:border-rose-500/30 hover:bg-surface/60 transition-all">
            <HoverTooltip className="w-56">{STANDALONE_TOOLTIPS.max_drawdown}</HoverTooltip>
            <p className="text-sm font-medium text-slate-500 mb-2">Max Drawdown</p>
            <p className="text-2xl font-semibold tabular-nums text-rose-400">-{(portfolioStandalone.max_drawdown * 100).toFixed(2)}%</p>
            <p className="text-[9px] text-slate-600 mt-2 uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity">View chart →</p>
          </button>
        </>
      )}
    </div>
  )
}
