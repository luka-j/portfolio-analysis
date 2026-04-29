import { useState, useEffect } from 'react'
import {
  getCumulativeSeries,
  comparePortfolio,
  type BenchmarkResult,
  type CumulativeSeriesResult,
  type DailyValue,
} from '../../api'
import type { AnalysisParams } from './types'
import { formatSymbolName } from './types'

interface BenchmarksParams extends AnalysisParams {
  loadStandalone: (symbols?: string) => void
}

export function useBenchmarks(params: BenchmarksParams) {
  const { effectiveFrom, to, currency, acctModel, riskFreeRate, active, scenarios, loadStandalone } = params

  const [benchmarkSymbols, setBenchmarkSymbols] = useState<string[]>([])
  const [cumulativeResults, setCumulativeResults] = useState<CumulativeSeriesResult[]>([])
  const [compareResults, setCompareResults] = useState<BenchmarkResult[]>([])

  const [scenarioBenchmarks, setScenarioBenchmarks] = useState<Array<{ id: number; name: string; twr: DailyValue[]; mwr: DailyValue[] }>>([])
  const [scenarioBenchmarkLoading, setScenarioBenchmarkLoading] = useState(false)

  const [compareLoading, setCompareLoading] = useState(false)
  const [compareError, setCompareError] = useState('')

  useEffect(() => {
    if (benchmarkSymbols.length === 0 && scenarioBenchmarks.length === 0) {
      loadStandalone('')
      return
    }
    const refresh = async () => {
      setCompareLoading(true)
      setCompareError('')
      try {
        const allSymsStr = [...benchmarkSymbols, ...scenarioBenchmarks.map(sb => `scenario:${sb.id}`)].join(',')
        const [cumRes, comp] = await Promise.all([
          benchmarkSymbols.length > 0 ? getCumulativeSeries(effectiveFrom, to, currency, acctModel, false, benchmarkSymbols.join(','), active) : Promise.resolve({results: []}),
          comparePortfolio(allSymsStr, currency, effectiveFrom, to, acctModel, riskFreeRate, active),
        ])
        if (benchmarkSymbols.length > 0) {
          setCumulativeResults(cumRes.results.filter(r => r.symbol !== 'Portfolio'))
        }
        setCompareResults(comp.benchmarks.map(b => ({ ...b, symbol: formatSymbolName(b.symbol, scenarios) })))
        loadStandalone(allSymsStr)
      } catch (err) {
        setCompareError(err instanceof Error ? err.message : 'Comparison failed')
      } finally {
        setCompareLoading(false)
      }
    }
    refresh()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveFrom, to, currency, acctModel])

  useEffect(() => {
    if (scenarioBenchmarks.length === 0) return
    const refresh = async () => {
      const updated = await Promise.all(scenarioBenchmarks.map(async sb => {
        try {
          const res = await getCumulativeSeries(effectiveFrom, to, currency, acctModel, false, undefined, sb.id)
          const twr = res.results.find(r => r.symbol === 'Portfolio')?.series ?? []
          const mwr = res.results.find(r => r.symbol === 'Portfolio-MWR')?.series ?? []
          return { ...sb, twr, mwr }
        } catch { return sb }
      }))
      setScenarioBenchmarks(updated)
    }
    refresh()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effectiveFrom, to, currency, acctModel])

  const handleCompare = async (input: string) => {
    const matchedScenario = scenarios.find(s => s.name === input && s.id !== active)
    if (matchedScenario) {
      addScenarioBenchmark(matchedScenario.id, matchedScenario.name)
      return true // returning true means we handled it
    }

    const inputSymbols = input.split(',').map(s => s.trim()).filter(Boolean)
    const newSymbols = inputSymbols.filter(s => !benchmarkSymbols.includes(s))
    if (newSymbols.length === 0) return true

    setCompareLoading(true)
    setCompareError('')
    try {
      const allSymbols = [...benchmarkSymbols, ...newSymbols]
      const allSymsStr = [...allSymbols, ...scenarioBenchmarks.map(sb => `scenario:${sb.id}`)].join(',')
      
      const [cumRes, comp] = await Promise.all([
        getCumulativeSeries(effectiveFrom, to, currency, acctModel, false, newSymbols.join(','), active),
        comparePortfolio(allSymsStr, currency, effectiveFrom, to, acctModel, riskFreeRate, active),
      ])
      setCumulativeResults(prev => {
        const merged = [...prev]
        for (const r of cumRes.results.filter(r => r.symbol !== 'Portfolio')) {
          if (!merged.find(m => m.symbol === r.symbol)) merged.push(r)
        }
        return merged
      })
      setBenchmarkSymbols(allSymbols)
      setCompareResults(comp.benchmarks.map(b => ({ ...b, symbol: formatSymbolName(b.symbol, scenarios) })))
      loadStandalone(allSymsStr)
      return true
    } catch (err) {
      setCompareError(err instanceof Error ? err.message : 'Comparison failed')
      return false
    } finally {
      setCompareLoading(false)
    }
  }

  const applyRiskFreeRate = async (newRate: number) => {
    const allSymsStr = [...benchmarkSymbols, ...scenarioBenchmarks.map(sb => `scenario:${sb.id}`)].join(',')
    if (!allSymsStr) return
    setCompareLoading(true)
    setCompareError('')
    try {
      const comp = await comparePortfolio(allSymsStr, currency, effectiveFrom, to, acctModel, newRate, active)
      setCompareResults(comp.benchmarks.map(b => ({ ...b, symbol: formatSymbolName(b.symbol, scenarios) })))
      loadStandalone(allSymsStr)
    } catch (err) {
      setCompareError(err instanceof Error ? err.message : 'Comparison failed')
    } finally {
      setCompareLoading(false)
    }
  }

  const handleRemoveSymbol = (sym: string) => {
    const remaining = benchmarkSymbols.filter(s => s !== sym)
    setBenchmarkSymbols(remaining)
    setCumulativeResults(prev => prev.filter(r => r.symbol !== sym))
    
    const allSymsStr = [...remaining, ...scenarioBenchmarks.map(sb => `scenario:${sb.id}`)].join(',')
    if (allSymsStr) {
      comparePortfolio(allSymsStr, currency, effectiveFrom, to, acctModel, riskFreeRate, active).then(comp => {
         setCompareResults(comp.benchmarks.map(b => ({ ...b, symbol: formatSymbolName(b.symbol, scenarios) })))
      })
    } else {
      setCompareResults([])
    }
    loadStandalone(allSymsStr)
  }

  const addScenarioBenchmark = async (id: number, name: string) => {
    if (scenarioBenchmarks.find(sb => sb.id === id)) return
    setScenarioBenchmarkLoading(true)
    try {
      const res = await getCumulativeSeries(effectiveFrom, to, currency, acctModel, false, undefined, id)
      const twr = res.results.find(r => r.symbol === 'Portfolio')?.series ?? []
      const mwr = res.results.find(r => r.symbol === 'Portfolio-MWR')?.series ?? []
      setScenarioBenchmarks(prev => [...prev, { id, name, twr, mwr }])
      
      const allSymsStr = [...benchmarkSymbols, ...scenarioBenchmarks.map(sb => `scenario:${sb.id}`), `scenario:${id}`].join(',')
      const comp = await comparePortfolio(allSymsStr, currency, effectiveFrom, to, acctModel, riskFreeRate, active)
      setCompareResults(comp.benchmarks.map(b => ({ ...b, symbol: formatSymbolName(b.symbol, scenarios) })))
      loadStandalone(allSymsStr)
    } catch { /* ignore */ }
    finally { setScenarioBenchmarkLoading(false) }
  }

  const removeScenarioBenchmark = (id: number) => {
    setScenarioBenchmarks(prev => prev.filter(sb => sb.id !== id))
    
    const allSymsStr = [...benchmarkSymbols, ...scenarioBenchmarks.filter(sb => sb.id !== id).map(sb => `scenario:${sb.id}`)].join(',')
    if (allSymsStr) {
      comparePortfolio(allSymsStr, currency, effectiveFrom, to, acctModel, riskFreeRate, active).then(comp => {
         setCompareResults(comp.benchmarks.map(b => ({ ...b, symbol: formatSymbolName(b.symbol, scenarios) })))
      })
    } else {
      setCompareResults([])
    }
    loadStandalone(allSymsStr)
  }

  return {
    benchmarkSymbols,
    scenarioBenchmarks,
    cumulativeResults,
    compareResults,
    compareLoading,
    compareError,
    scenarioBenchmarkLoading,
    handleCompare,
    applyRiskFreeRate,
    handleRemoveSymbol,
    addScenarioBenchmark,
    removeScenarioBenchmark,
  }
}
