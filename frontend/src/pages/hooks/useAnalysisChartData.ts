import { useMemo } from 'react'
import type { DailyValue, CumulativeSeriesResult, DrawdownResult, RollingPoint, AttributionResult } from '../../api'

interface AnalysisChartDataParams {
  portfolioHistory: DailyValue[]
  mwrHistory: DailyValue[]
  cumulativeResults: CumulativeSeriesResult[]
  compareTwrHistory: DailyValue[]
  compareLabel: string | null
  scenarioBenchmarks: Array<{ id: number; name: string; twr: DailyValue[]; mwr: DailyValue[] }>
  drawdownResults: DrawdownResult[]
  rollingSeries: Record<string, RollingPoint[]>
  attributionData: AttributionResult[]
}

export function useAnalysisChartData(params: AnalysisChartDataParams) {
  const {
    portfolioHistory,
    mwrHistory,
    cumulativeResults,
    compareTwrHistory,
    compareLabel,
    scenarioBenchmarks,
    drawdownResults,
    rollingSeries,
    attributionData,
  } = params

  const mergedChartData = useMemo(() => {
    if (portfolioHistory.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    portfolioHistory.forEach(d => {
      if (!dateMap[d.date]) dateMap[d.date] = {}
      dateMap[d.date]['Portfolio'] = d.value
    })
    cumulativeResults.forEach(r => {
      if (!r.series) return
      r.series.forEach(pt => {
        if (!dateMap[pt.date]) dateMap[pt.date] = {}
        dateMap[pt.date][r.symbol] = pt.value
      })
    })
    if (compareTwrHistory.length > 0 && compareLabel !== null) {
      compareTwrHistory.forEach(d => {
        if (!dateMap[d.date]) dateMap[d.date] = {}
        dateMap[d.date]['Compare'] = d.value
      })
    }
    scenarioBenchmarks.forEach(sb => {
      if (!sb.twr) return
      sb.twr.forEach(pt => {
        if (!dateMap[pt.date]) dateMap[pt.date] = {}
        dateMap[pt.date][`[S] ${sb.name}`] = pt.value
      })
    })
    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  }, [portfolioHistory, cumulativeResults, compareTwrHistory, compareLabel, scenarioBenchmarks])

  const drawdownChartData = useMemo(() => {
    if (drawdownResults.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    drawdownResults.forEach(r => {
      if (!r.series) return
      const key = r.symbol === 'Portfolio' ? 'Drawdown' : r.symbol
      r.series.forEach(pt => {
        if (!dateMap[pt.date]) dateMap[pt.date] = {}
        dateMap[pt.date][key] = +(pt.drawdown_pct * 100).toFixed(3)
      })
    })
    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  }, [drawdownResults])

  const mwrChartData = useMemo(() => {
    if (mwrHistory.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    mwrHistory.forEach(d => {
      if (!dateMap[d.date]) dateMap[d.date] = {}
      dateMap[d.date]['Portfolio'] = d.value
    })
    cumulativeResults.forEach(r => {
      if (!r.series) return
      r.series.forEach(pt => {
        if (!dateMap[pt.date]) dateMap[pt.date] = {}
        dateMap[pt.date][r.symbol] = pt.value
      })
    })
    scenarioBenchmarks.forEach(sb => {
      if (!sb.mwr) return
      sb.mwr.forEach(pt => {
        if (!dateMap[pt.date]) dateMap[pt.date] = {}
        dateMap[pt.date][`[S] ${sb.name}`] = pt.value
      })
    })
    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  }, [mwrHistory, cumulativeResults, scenarioBenchmarks])

  const rollingChartData = useMemo(() => {
    const keys = Object.keys(rollingSeries)
    if (keys.length === 0) return []
    const dateMap: Record<string, Record<string, number>> = {}
    keys.forEach(sym => {
      if (!rollingSeries[sym]) return
      rollingSeries[sym].forEach(pt => {
        if (!dateMap[pt.date]) dateMap[pt.date] = {}
        dateMap[pt.date][sym] = pt.value
      })
    })
    return Object.entries(dateMap)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }))
  }, [rollingSeries])

  const attrDisplay = useMemo(() => {
    const MAX_ATTR = 15
    if (attributionData.length === 0) return []
    const sorted = [...attributionData].sort((a, b) => b.contribution - a.contribution)
    if (sorted.length <= MAX_ATTR) return sorted
    const shown = sorted.slice(0, MAX_ATTR)
    const othersContrib = sorted.slice(MAX_ATTR).reduce((s, r) => s + r.contribution, 0)
    const othersWeight = sorted.slice(MAX_ATTR).reduce((s, r) => s + r.avg_weight, 0)
    shown.push({ symbol: 'Others', avg_weight: othersWeight, return: 0, contribution: othersContrib })
    return shown
  }, [attributionData])

  return {
    mergedChartData,
    drawdownChartData,
    mwrChartData,
    rollingChartData,
    attrDisplay,
  }
}
