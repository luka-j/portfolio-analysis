import { useQuery } from '@tanstack/react-query'
import {
  getPortfolioStats,
  getPortfolioReturns,
  getStandaloneMetrics,
} from '../../api'
import type { AnalysisParams } from './types'

export function useCompareOverlay(params: AnalysisParams) {
  const { compare, from, to, currency, acctModel, effectiveFrom, riskFreeRate } = params

  const isEnabled = compare !== null

  const { data, isLoading } = useQuery({
    queryKey: ['compareOverlay', compare, from, to, currency, acctModel, effectiveFrom, riskFreeRate],
    queryFn: async () => {
      const cid = compare !== null && compare > 0 ? compare : null
      const [st, hist, sa] = await Promise.all([
        getPortfolioStats(from, to, currency, acctModel, undefined, cid),
        getPortfolioReturns(from, to, currency, acctModel, 'twr', undefined, cid),
        getStandaloneMetrics('', currency, effectiveFrom, to, acctModel, riskFreeRate, cid),
      ])
      return {
        compareStats: st,
        compareTwrHistory: hist.data ?? [],
        compareStandalone: sa.results.find(r => r.symbol === 'Portfolio') ?? null,
      }
    },
    enabled: isEnabled,
  })

  return {
    compareStats: data?.compareStats ?? null,
    compareTwrHistory: data?.compareTwrHistory ?? [],
    compareStandalone: data?.compareStandalone ?? null,
    compareDataLoading: isEnabled ? isLoading : false,
  }
}
