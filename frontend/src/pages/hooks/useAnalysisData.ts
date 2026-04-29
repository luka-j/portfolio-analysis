import { useState, useCallback, useRef, useEffect } from 'react'
import {
  getPortfolioStats,
  getPortfolioReturns,
  getStandaloneMetrics,
  getAttribution,
  getCorrelations,
  type StatsResponse,
  type DailyValue,
  type StandaloneResult,
  type AttributionResult,
} from '../../api'
import type { AnalysisParams } from './types'
import { formatSymbolName } from './types'

export function useAnalysisData(params: AnalysisParams) {
  const { currency, acctModel, from, to, effectiveFrom, active, riskFreeRate, scenarios } = params

  const [stats, setStats] = useState<StatsResponse | null>(null)
  const [portfolioHistory, setPortfolioHistory] = useState<DailyValue[]>([])
  const [mwrHistory, setMwrHistory] = useState<DailyValue[]>([])

  const [standaloneResults, setStandaloneResults] = useState<StandaloneResult[]>([])
  const [attributionData, setAttributionData] = useState<AttributionResult[]>([])
  const [attributionTWR, setAttributionTWR] = useState(0)
  const [correlationData, setCorrelationData] = useState<{ symbols: string[]; matrix: number[][] }>({ symbols: [], matrix: [] })

  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')

  const [standaloneLoading, setStandaloneLoading] = useState(false)
  const [standaloneRefreshing, setStandaloneRefreshing] = useState(false)
  const [standaloneError, setStandaloneError] = useState('')

  const [holdingsLoading, setHoldingsLoading] = useState(false)
  const [holdingsError, setHoldingsError] = useState('')

  const loadGenRef = useRef(0)

  const loadData = useCallback(async () => {
    loadGenRef.current += 1
    const gen = loadGenRef.current
    setLoading(true)
    setRefreshing(false)
    setError('')

    let freshStats = false
    let freshHist = false

    const checkCachedDone = () => {
      if (gen === loadGenRef.current && !freshStats && !freshHist) {
        setLoading(false)
        setRefreshing(true)
      }
    }

    getPortfolioStats(from, to, currency, acctModel, true, undefined, active).then(st => {
      if (gen === loadGenRef.current && !freshStats && Object.keys(st.statistics).length > 0) {
        setStats(st)
        checkCachedDone()
      }
    }).catch(() => {})

    getPortfolioReturns(from, to, currency, acctModel, 'twr', true, undefined, active).then(hist => {
      if (gen === loadGenRef.current && !freshHist && hist.data.length > 0) {
        setPortfolioHistory(hist.data ?? [])
        checkCachedDone()
      }
    }).catch(() => {})

    Promise.all([
      getPortfolioStats(from, to, currency, acctModel, false, undefined, active).then(st => {
        freshStats = true
        if (gen === loadGenRef.current) setStats(st)
      }),
      getPortfolioReturns(from, to, currency, acctModel, 'twr', false, undefined, active).then(hist => {
        freshHist = true
        if (gen === loadGenRef.current) setPortfolioHistory(hist.data ?? [])
      }),
      getPortfolioReturns(from, to, currency, acctModel, 'mwr', false, undefined, active).then(hist => {
        if (gen === loadGenRef.current) setMwrHistory(hist.data ?? [])
      }).catch(() => {}),
    ]).catch(err => {
      if (gen === loadGenRef.current) setError(err instanceof Error ? err.message : 'Failed to load')
    }).finally(() => {
      if (gen === loadGenRef.current) {
        setLoading(false)
        setRefreshing(false)
      }
    })
  }, [currency, acctModel, from, to, active])

  useEffect(() => { loadData() }, [loadData])

  const loadStandalone = useCallback(async (symbols = '') => {
    const gen = loadGenRef.current
    setStandaloneLoading(true)
    setStandaloneRefreshing(false)
    setStandaloneError('')

    let freshArrived = false

    getStandaloneMetrics(symbols, currency, effectiveFrom, to, acctModel, riskFreeRate, true, active).then(res => {
      if (gen === loadGenRef.current && !freshArrived && res.results.length > 0) {
        setStandaloneResults(res.results.map(r => ({ ...r, symbol: formatSymbolName(r.symbol, scenarios) })))
        setStandaloneLoading(false)
        setStandaloneRefreshing(true)
      }
    }).catch(() => {})

    getStandaloneMetrics(symbols, currency, effectiveFrom, to, acctModel, riskFreeRate, false, active).then(res => {
      if (gen === loadGenRef.current) {
        freshArrived = true
        setStandaloneResults(res.results.map(r => ({ ...r, symbol: formatSymbolName(r.symbol, scenarios) })))
      }
    }).catch(err => {
      if (gen === loadGenRef.current) setStandaloneError(err instanceof Error ? err.message : 'Standalone metrics failed')
    }).finally(() => {
      if (gen === loadGenRef.current) {
        setStandaloneLoading(false)
        setStandaloneRefreshing(false)
      }
    })
  }, [currency, effectiveFrom, to, acctModel, riskFreeRate, active, scenarios])

  const loadHoldings = useCallback(async () => {
    setHoldingsLoading(true)
    setHoldingsError('')
    try {
      const [attrRes, corrRes] = await Promise.all([
        getAttribution(effectiveFrom, to, currency, acctModel, riskFreeRate, active),
        getCorrelations(effectiveFrom, to, currency, acctModel, active),
      ])
      setAttributionData(attrRes.positions)
      setAttributionTWR(attrRes.total_twr)
      setCorrelationData({ symbols: corrRes.symbols, matrix: corrRes.matrix })
    } catch (err) {
      setHoldingsError(err instanceof Error ? err.message : 'Failed to load holdings data')
    } finally {
      setHoldingsLoading(false)
    }
  }, [effectiveFrom, to, currency, acctModel, riskFreeRate, active])

  useEffect(() => { loadHoldings() }, [loadHoldings])

  return {
    stats,
    portfolioHistory,
    mwrHistory,
    loading,
    refreshing,
    error,
    standaloneResults,
    standaloneLoading,
    standaloneRefreshing,
    standaloneError,
    loadStandalone,
    attributionData,
    attributionTWR,
    correlationData,
    holdingsLoading,
    holdingsError,
  }
}
