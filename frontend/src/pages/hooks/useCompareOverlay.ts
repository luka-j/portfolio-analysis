import { useState, useCallback, useEffect } from 'react'
import {
  getPortfolioStats,
  getPortfolioReturns,
  getStandaloneMetrics,
  type StatsResponse,
  type DailyValue,
  type StandaloneResult,
} from '../../api'
import type { AnalysisParams } from './types'

export function useCompareOverlay(params: AnalysisParams) {
  const { compare, from, to, currency, acctModel, effectiveFrom, riskFreeRate } = params

  const [compareStats, setCompareStats] = useState<StatsResponse | null>(null)
  const [compareTwrHistory, setCompareTwrHistory] = useState<DailyValue[]>([])
  const [compareStandalone, setCompareStandalone] = useState<StandaloneResult | null>(null)
  const [compareDataLoading, setCompareDataLoading] = useState(false)

  const loadCompareData = useCallback(async () => {
    if (compare === null) {
      setCompareStats(null)
      setCompareTwrHistory([])
      setCompareStandalone(null)
      return
    }
    setCompareDataLoading(true)
    try {
      const cid = compare > 0 ? compare : null
      const [st, hist, sa] = await Promise.all([
        getPortfolioStats(from, to, currency, acctModel, false, undefined, cid),
        getPortfolioReturns(from, to, currency, acctModel, 'twr', false, undefined, cid),
        getStandaloneMetrics('', currency, effectiveFrom, to, acctModel, riskFreeRate, false, cid),
      ])
      setCompareStats(st)
      setCompareTwrHistory(hist.data ?? [])
      setCompareStandalone(sa.results.find(r => r.symbol === 'Portfolio') ?? null)
    } catch {
      // swallow
    } finally {
      setCompareDataLoading(false)
    }
  }, [compare, from, to, currency, acctModel, effectiveFrom, riskFreeRate])

  useEffect(() => { loadCompareData() }, [loadCompareData])

  return {
    compareStats,
    compareTwrHistory,
    compareStandalone,
    compareDataLoading,
  }
}
