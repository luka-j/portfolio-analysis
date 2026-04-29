import { useState, useCallback, useEffect } from 'react'
import {
  getDrawdownSeries,
  getRollingMetric,
  type DrawdownResult,
  type RollingPoint,
} from '../../api'
import type { AnalysisParams, ChartMode } from './types'
import { formatSymbolName } from './types'

interface ChartModeDataParams extends AnalysisParams {
  chartMode: ChartMode
  rollingWindow: number
  benchmarkSymbols: string[]
  scenarioBenchmarks: Array<{ id: number; name: string }>
}

export function useChartModeData(params: ChartModeDataParams) {
  const {
    effectiveFrom, to, currency, acctModel, riskFreeRate, active, scenarios,
    chartMode, rollingWindow, benchmarkSymbols, scenarioBenchmarks
  } = params

  const [drawdownResults, setDrawdownResults] = useState<DrawdownResult[]>([])
  const [rollingSeries, setRollingSeries] = useState<Record<string, RollingPoint[]>>({})
  const [chartModeLoading, setChartModeLoading] = useState(false)
  const [chartModeError, setChartModeError] = useState('')

  const loadChartMode = useCallback(async (mode: ChartMode, window: number, benchSyms: string[], sBenches: typeof scenarioBenchmarks) => {
    if (mode === 'twr' || mode === 'mwr') {
      setChartModeLoading(false)
      setChartModeError('')
      return
    }
    setChartModeLoading(true)
    setChartModeError('')
    try {
      if (mode === 'drawdown') {
        const symParam = benchSyms.length > 0 ? benchSyms.join(',') : undefined
        const res = await getDrawdownSeries(effectiveFrom, to, currency, acctModel, false, symParam, active)
        const baseResults = res.results ?? []
        
        const sbRes = await Promise.all(sBenches.map(async sb => {
          try {
             const r = await getDrawdownSeries(effectiveFrom, to, currency, acctModel, false, undefined, sb.id)
             const pRes = (r.results ?? []).find(x => x.symbol === 'Portfolio')
             if (pRes) return { ...pRes, symbol: `[S] ${sb.name}` }
          } catch { /* ignore */ }
          return null
        }))
        setDrawdownResults([...baseResults, ...sbRes.filter(Boolean) as DrawdownResult[]])
      } else {
        const metric = mode === 'rolling_sharpe' ? 'sharpe'
          : mode === 'rolling_volatility' ? 'volatility'
          : mode === 'rolling_sortino' ? 'sortino'
          : 'beta'
        const newSeries: Record<string, RollingPoint[]> = {}
        if (metric === 'beta') {
          const allBenchSyms = [...benchSyms, ...sBenches.map(sb => `scenario:${sb.id}`)]
          await Promise.all(allBenchSyms.map(async sym => {
            const symName = formatSymbolName(sym, scenarios)
            try {
              const res = await getRollingMetric(metric, window, effectiveFrom, to, currency, acctModel, riskFreeRate, sym, undefined, active)
              const result = res.results[0]
              if (result && !result.error) newSeries[symName] = result.series
            } catch { /* skip bad symbols */ }
            
            await Promise.all(sBenches.map(async sb => {
              if (sym === `scenario:${sb.id}`) return // skip self comparison
              try {
                const r = await getRollingMetric(metric, window, effectiveFrom, to, currency, acctModel, riskFreeRate, sym, undefined, sb.id)
                const result = r.results[0]
                if (result && !result.error) newSeries[`[S] ${sb.name} (${symName})`] = result.series
              } catch { /* ignore */ }
            }))
          }))
        } else {
          const symParam = benchSyms.length > 0 ? benchSyms.join(',') : undefined
          const res = await getRollingMetric(metric, window, effectiveFrom, to, currency, acctModel, riskFreeRate, undefined, symParam, active)
          for (const result of res.results) {
            if (!result.error) newSeries[result.symbol] = result.series
          }
          await Promise.all(sBenches.map(async sb => {
             try {
               const r = await getRollingMetric(metric, window, effectiveFrom, to, currency, acctModel, riskFreeRate, undefined, undefined, sb.id)
               const pRes = r.results.find(x => x.symbol === 'Portfolio')
               if (pRes && !pRes.error) newSeries[`[S] ${sb.name}`] = pRes.series
             } catch { /* ignore */ }
          }))
        }
        setRollingSeries(newSeries)
      }
    } catch (err) {
      setChartModeError(err instanceof Error ? err.message : 'Failed to load chart data')
    } finally {
      setChartModeLoading(false)
    }
  }, [effectiveFrom, to, currency, acctModel, riskFreeRate, active, scenarios])

  useEffect(() => {
    loadChartMode(chartMode, rollingWindow, benchmarkSymbols, scenarioBenchmarks)
  }, [loadChartMode, chartMode, rollingWindow, benchmarkSymbols, scenarioBenchmarks])

  return {
    drawdownResults,
    rollingSeries,
    chartModeLoading,
    chartModeError,
  }
}
