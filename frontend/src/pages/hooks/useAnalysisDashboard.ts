import { useState, useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  getAnalysisDashboard,
  getAnalysisHoldings,
  getStandaloneMetrics,
  type StatsResponse,
} from '../../api'
import type { AnalysisParams } from './types'
import { formatSymbolName } from './types'

export function useAnalysisDashboard(params: AnalysisParams) {
  const { currency, acctModel, from, to, effectiveFrom, active, riskFreeRate, scenarios } = params

  const [standaloneSymbols, setStandaloneSymbols] = useState('')

  const loadStandalone = useCallback((symbols = '') => {
    setStandaloneSymbols(symbols)
  }, [])

  // 1. Dashboard query
  const { data: dashboardData, isLoading: dashboardLoading, error: dashboardError } = useQuery({
    queryKey: ['analysisDashboard', from, to, currency, acctModel, riskFreeRate, active],
    queryFn: () => getAnalysisDashboard(from, to, currency, acctModel, riskFreeRate, undefined, active),
  })

  // 2. Standalone Metrics query
  const { data: standaloneData, isLoading: standaloneLoading, error: standaloneErrorMsg } = useQuery({
    queryKey: ['standaloneMetrics', standaloneSymbols, currency, effectiveFrom, to, acctModel, riskFreeRate, active],
    queryFn: () => getStandaloneMetrics(standaloneSymbols, currency, effectiveFrom, to, acctModel, riskFreeRate, active),
  })

  // 3. Holdings & Attribution query
  const { data: holdingsData, isLoading: holdingsLoading, error: holdingsErrorMsg } = useQuery({
    queryKey: ['analysisHoldings', effectiveFrom, to, currency, acctModel, riskFreeRate, active],
    queryFn: () => getAnalysisHoldings(effectiveFrom, to, currency, acctModel, riskFreeRate, undefined, active),
  })

  // Derived states
  const stats = useMemo<StatsResponse | null>(() => {
    if (!dashboardData) return null
    return {
      currency: dashboardData.currency,
      accounting_model: dashboardData.accounting_model,
      statistics: dashboardData.stats,
    }
  }, [dashboardData])

  const portfolioHistory = dashboardData?.twr_history ?? []
  const mwrHistory = dashboardData?.mwr_history ?? []

  const standaloneResults = useMemo(() => {
    if (!standaloneData) return []
    return standaloneData.results.map(r => ({
      ...r,
      symbol: formatSymbolName(r.symbol, scenarios),
    }))
  }, [standaloneData, scenarios])

  const attributionData = holdingsData?.attribution.positions ?? []
  const attributionTWR = holdingsData?.attribution.total_twr ?? 0
  const correlationData = useMemo(() => {
    if (!holdingsData) return { symbols: [], matrix: [] }
    return {
      symbols: holdingsData.correlations.symbols,
      matrix: holdingsData.correlations.matrix,
    }
  }, [holdingsData])

  const error = useMemo(() => {
    if (!dashboardError) return ''
    return dashboardError instanceof Error ? dashboardError.message : String(dashboardError)
  }, [dashboardError])

  const standaloneError = useMemo(() => {
    if (!standaloneErrorMsg) return ''
    return standaloneErrorMsg instanceof Error ? standaloneErrorMsg.message : String(standaloneErrorMsg)
  }, [standaloneErrorMsg])

  const holdingsError = useMemo(() => {
    if (!holdingsErrorMsg) return ''
    return holdingsErrorMsg instanceof Error ? holdingsErrorMsg.message : String(holdingsErrorMsg)
  }, [holdingsErrorMsg])

  return {
    stats,
    portfolioHistory,
    mwrHistory,
    loading: dashboardLoading,
    refreshing: false,
    error,
    standaloneResults,
    standaloneLoading,
    standaloneRefreshing: false,
    standaloneError,
    loadStandalone,
    attributionData,
    attributionTWR,
    correlationData,
    holdingsLoading,
    holdingsError,
  }
}
