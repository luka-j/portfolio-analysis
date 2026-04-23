import { useState, useEffect, useCallback, useRef } from 'react'
import { XAxis, YAxis, Tooltip, ResponsiveContainer, Area, AreaChart } from 'recharts'
import ReactMarkdown from 'react-markdown'
import NavBar from '../components/NavBar'
import HoverTooltip from '../components/HoverTooltip'
import UploadResultModal from '../components/UploadResultModal'
import { useNavigate } from 'react-router-dom'
import {
  getPortfolioValueMulti, getPortfolioHistory, getPortfolioStats, getPortfolioReturns,
  uploadFlexQuery, uploadEtradeBenefits, uploadEtradeSales,
  getLLMSummary,
  type DailyValue, type ImportedTransaction, type ImportedCorporateAction,
} from '../api'
import { formatCurrencyCompact, formatDate, CURRENCIES, CURRENCY_SYMBOLS, getFromDate, RECHARTS_TOOLTIP_STYLE, RECHARTS_LABEL_STYLE, RECHARTS_ITEM_STYLE } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'
import { usePrivacy } from '../utils/PrivacyContext'
import { useScenario } from '../context/ScenarioContext'

const PERIODS = [
  { label: '1M', months: 1 },
  { label: '3M', months: 3 },
  { label: '1Y', months: 12 },
  { label: 'All', months: 0 },
]



export default function LandingPage() {
  const { privacy, togglePrivacy } = usePrivacy()
  const { active } = useScenario()
  const [currency, setCurrency] = usePersistentState<string>('app_currency', 'CZK')
  const [period, setPeriod] = usePersistentState('landing_period', 0)
  const [chartMode, setChartMode] = usePersistentState<'value' | 'twr' | 'mwr'>('landing_chartMode', 'value')
  const [portfolioValues, setPortfolioValues] = useState<Record<string, number>>({})
  const [hasTransactions, setHasTransactions] = useState<boolean | null>(null)
  const [history, setHistory] = useState<DailyValue[]>([])
  const [twrHistory, setTwrHistory] = useState<DailyValue[]>([])
  const [mwrHistory, setMwrHistory] = useState<DailyValue[]>([])
  const [stats, setStats] = useState<Record<string, number> | null>(null)
  const [loading, setLoading] = useState(true)
  const [chartLoading, setChartLoading] = useState(false)
  // valueRefreshing: cached value shown, fresh fetch still in flight
  const [valueRefreshing, setValueRefreshing] = useState(false)
  // chartRefreshing: cached chart data shown, fresh fetch still in flight
  const [chartRefreshing, setChartRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadExpanded, setUploadExpanded] = useState(false)
  // uploadCount increments after every successful upload so the LLM summary re-fetches.
  const [uploadCount, setUploadCount] = useState(0)
  const [showUploadModal, setShowUploadModal] = useState(false)
  const [uploadModalTransactions, setUploadModalTransactions] = useState<ImportedTransaction[]>([])
  const [uploadModalCorporateActions, setUploadModalCorporateActions] = useState<ImportedCorporateAction[]>([])
  const [uploadModalError, setUploadModalError] = useState<string | null>(null)
  const [pendingFirstUpload, setPendingFirstUpload] = useState(false)

  const defaultPeriod = [0, 6].includes(new Date().getDay()) ? '1w' : '1d'
  const [llmPeriod, setLlmPeriod] = usePersistentState('landing_llmPeriod', defaultPeriod)
  const [llmSummary, setLlmSummary] = useState('')
  const [llmSummaryLoading, setLlmSummaryLoading] = useState(false)
  const [llmAvailable, setLlmAvailable] = useState<boolean | null>(null)
  const [llmForceRefresh, setLlmForceRefresh] = useState(false)

  const loadGenRef = useRef(0)
  // statsRefreshing: cached stats shown, fresh fetch still in flight
  const [statsRefreshing, setStatsRefreshing] = useState(false)

  const navigate = useNavigate()

  const cycleCurrency = () => setCurrency(c => {
    const idx = (CURRENCIES as readonly string[]).indexOf(c)
    return CURRENCIES[(idx + 1) % CURRENCIES.length]
  })

  const digIntoThis = () => {
    const periodLabel = llmPeriod === '1d' ? 'past day' : llmPeriod === '1w' ? 'past week' : 'past month'
    navigate('/llm', {
      state: {
        initialMessages: [
          { role: 'user', content: `What happened in the market this ${periodLabel}?` },
          { role: 'assistant', content: llmSummary },
        ],
        initialPrompt: {
          promptType: 'long_market_summary',
          displayMessage: `Give me a detailed breakdown of the ${periodLabel}.`,
          extraParams: { period: llmPeriod },
        },
      },
    })
  }

  // applyMulti reads the primary value for the currently-selected currency
  // and also snapshots every per-currency scalar the backend computed in a
  // single pass (market data fetched once, FX conversions local + parallel).
  const applyMulti = useCallback((res: { value: number; has_transactions: boolean; positions?: { values?: Record<string, number> }[] }) => {
    const next: Record<string, number> = {}
    for (const curr of CURRENCIES) {
      let sum = 0
      for (const pos of res.positions ?? []) {
        const v = pos.values?.[curr]
        if (typeof v === 'number') sum += v
      }
      if (sum > 0) next[curr] = sum
    }
    if (Object.keys(next).length === 0) {
      // Fall back to the primary-currency scalar when positions are absent
      // (e.g. empty portfolio).
      next[currency] = res.value
    }
    setPortfolioValues(prev => ({ ...prev, ...next }))
    setHasTransactions(res.has_transactions)
  }, [currency])

  // Value loader: one multi-currency request for all CURRENCIES. Runs only when
  // currency changes (for the hero scalar) or after an upload. The period picker
  // does not affect this series, so switching 1M ↔ 1Y no longer re-fetches prices.
  useEffect(() => {
    loadGenRef.current += 1
    const gen = loadGenRef.current
    const controller = new AbortController()
    setLoading(true)
    setError('')
    let freshArrived = false

    // 1. Cached call — paint the hero instantly if anything is cached.
    getPortfolioValueMulti(CURRENCIES as unknown as string[], 'historical', true, controller.signal, active)
      .then(res => {
        if (gen === loadGenRef.current && !freshArrived && res.value > 0) {
          applyMulti(res)
          setLoading(false)
          setValueRefreshing(true)
        }
      })
      .catch(() => {})

    // 2. Fresh call — authoritative, clears the stale indicator.
    getPortfolioValueMulti(CURRENCIES as unknown as string[], 'historical', false, controller.signal, active)
      .then(res => {
        if (gen !== loadGenRef.current) return
        freshArrived = true
        applyMulti(res)
        setValueRefreshing(false)
        setLoading(false)
      })
      .catch(err => {
        if (gen !== loadGenRef.current) return
        if ((err as Error)?.name === 'AbortError') return
        setError(err instanceof Error ? err.message : 'Failed to load data')
        setLoading(false)
        setValueRefreshing(false)
      })

    return () => { controller.abort() }
  }, [currency, uploadCount, applyMulti, active])

  // Stats loader: depends on period (and currency / upload count). Split from
  // the value loader so period switches don't re-trigger the multi-currency
  // price pass. Uses the same cached-then-fresh pattern for instant feedback.
  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false
    let freshArrived = false
    const from = getFromDate(period)
    const to = formatDate(new Date())
    setStatsRefreshing(false)

    getPortfolioStats(from, to, currency, 'historical', true, controller.signal, active)
      .then(st => {
        if (cancelled || freshArrived) return
        if (st.statistics && Object.keys(st.statistics).length > 0) {
          setStats(st.statistics)
          setStatsRefreshing(true)
        }
      })
      .catch(() => {})

    getPortfolioStats(from, to, currency, 'historical', false, controller.signal, active)
      .then(st => {
        if (cancelled) return
        freshArrived = true
        setStats(st.statistics)
        setStatsRefreshing(false)
      })
      .catch(() => { if (!cancelled) setStatsRefreshing(false) })

    return () => { cancelled = true; controller.abort() }
  }, [currency, period, uploadCount, active])

  // Re-fetch triggered by explicit post-upload callback.
  const loadData = useCallback(() => {
    setUploadCount(c => c + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false
    let freshArrived = false

    const from = getFromDate(period)
    const to = formatDate(new Date())
    setChartLoading(true)
    setChartRefreshing(false)

    // Only fetch the series for the currently-displayed mode.
    // Previously-loaded data for other modes is retained in state.
    const fetchFn = chartMode === 'value'
      ? (cO: boolean, s?: AbortSignal) => getPortfolioHistory(from, to, currency, 'historical', cO, s, active)
      : (cO: boolean, s?: AbortSignal) => getPortfolioReturns(from, to, currency, 'historical', chartMode, cO, s, active)
    const setter = chartMode === 'value' ? setHistory : chartMode === 'twr' ? setTwrHistory : setMwrHistory

    // 1. Cached call — show immediately if non-empty, mark as stale
    fetchFn(true, controller.signal).then(res => {
      if (!cancelled && !freshArrived && res.data.length > 0) {
        setter(res.data)
        setChartLoading(false)
        setChartRefreshing(true)
      }
    }).catch(() => {})

    // 2. Fresh call — always overwrites cached, clears stale indicator
    fetchFn(false, controller.signal).then(res => {
      if (!cancelled) {
        freshArrived = true
        setter(res.data)
      }
    }).catch(() => {}).finally(() => {
      if (!cancelled) {
        setChartLoading(false)
        setChartRefreshing(false)
      }
    })

    return () => { cancelled = true; controller.abort() }
  }, [currency, period, chartMode, active])

  const currValue = portfolioValues[currency] ?? 0
  // Only show LLM when we know there are trades AND the portfolio has a non-zero value.
  const shouldShowLlm = hasTransactions === true && currValue > 0

  useEffect(() => {
    if (!shouldShowLlm) return
    let cancelled = false;
    const fetchSummary = async (forceRefresh: boolean) => {
      setLlmSummaryLoading(true)
      try {
        const res = await getLLMSummary(llmPeriod, forceRefresh, active)
        if (!cancelled) {
          setLlmSummary(res.summary)
          setLlmAvailable(true)
        }
      } catch (err) {
        const error = err as Error
        if (!cancelled) {
          if (error?.message?.includes('GEMINI_API_KEY')) {
            setLlmAvailable(false) // Hide entirely
            setLlmSummary("Market summary unavailable. Please configure GEMINI_API_KEY.")
          } else {
            setLlmAvailable(true) // Keep it shown but show error
            setLlmSummary("Failed to generate market summary.")
          }
        }
      } finally {
        if (!cancelled) {
          setLlmSummaryLoading(false)
          setLlmForceRefresh(false)
        }
      }
    }
    fetchSummary(llmForceRefresh)
    return () => { cancelled = true }
  }, [llmPeriod, uploadCount, llmForceRefresh, shouldShowLlm])

  const handleModalClose = useCallback(async () => {
    setShowUploadModal(false)
    setUploadModalTransactions([])
    setUploadModalCorporateActions([])
    setUploadModalError(null)
    setUploadCount(c => c + 1)
    if (pendingFirstUpload) {
      setPendingFirstUpload(false)
      navigate('/portfolio', { state: { firstUpload: true } })
    } else {
      await loadData()
    }
  }, [pendingFirstUpload, navigate, loadData])

  type UploadFn = (file: File) => Promise<{ transactions: ImportedTransaction[]; corporate_actions?: ImportedCorporateAction[] }>
  const createUploadHandler = (uploadFn: UploadFn) =>
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0]
      if (!file) return
      setPendingFirstUpload(hasTransactions === false)
      setUploadModalTransactions([])
      setUploadModalCorporateActions([])
      setUploadModalError(null)
      setShowUploadModal(true)
      setUploading(true)
      try {
        const res = await uploadFn(file)
        setUploadModalTransactions(res.transactions ?? [])
        setUploadModalCorporateActions(res.corporate_actions ?? [])
      } catch (err) {
        setUploadModalError(err instanceof Error ? err.message : 'Upload failed')
      } finally {
        setUploading(false)
        e.target.value = ''
      }
    }

  const handleUpload = createUploadHandler(uploadFlexQuery)
  const handleEtradeBenefitsUpload = createUploadHandler(uploadEtradeBenefits)
  const handleEtradeSalesUpload = createUploadHandler(uploadEtradeSales)


  const chartData = (() => {
    if (chartMode === 'value') {
      return history.map(d => ({ date: d.date, value: d.value }))
    }
    if (chartMode === 'twr') {
      return twrHistory.map(d => ({ date: d.date, value: d.value }))
    }
    if (chartMode === 'mwr') {
      return mwrHistory.map(d => ({ date: d.date, value: d.value }))
    }
    return []
  })()

  const mwr = typeof stats?.mwr === 'number' ? stats.mwr * 100 : null
  const twr = typeof stats?.twr === 'number' ? stats.twr * 100 : null

  return (
    <div className="min-h-screen md:h-screen bg-bg flex flex-col overflow-x-hidden md:overflow-hidden">
      <NavBar />

      {/* Hero section centered */}
      <div className="z-10 w-full flex flex-col items-center gap-2 pointer-events-none -mb-6">
        <div className="pointer-events-auto flex items-center gap-3">
          <h1 className="text-5xl md:text-6xl font-bold text-white tabular-nums tracking-tight [text-shadow:0_0_20px_rgba(255,255,255,0.05)] flex items-baseline gap-2">
            <button
              className="text-indigo-300/70 hover:text-indigo-300 px-1.5 py-0.5 rounded-lg hover:bg-white/[0.07] hover:backdrop-blur-sm transition-all duration-200 active:scale-95"
              onClick={cycleCurrency}
              title="Switch currency"
            >
              {CURRENCY_SYMBOLS[currency]}
            </button>
          {loading || hasTransactions === false ? '—' : privacy ? '———' : new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(currValue)}
          </h1>
          {(valueRefreshing || statsRefreshing) && (
            <span className="w-5 h-5 rounded-full border-2 border-indigo-400/25 border-t-indigo-300 animate-spin" />
          )}
          <div className="relative group">
            <button
              className={`p-1.5 rounded-lg transition-all duration-200 active:scale-95 ${privacy ? 'text-red-400/80 hover:text-red-400 hover:bg-red-500/10' : 'text-slate-500 hover:text-slate-400 hover:bg-white/[0.07]'}`}
              onClick={togglePrivacy}
            >
              {privacy ? (
                <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
                  <line x1="1" y1="1" x2="23" y2="23"/>
                </svg>
              ) : (
                <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
                  <circle cx="12" cy="12" r="3"/>
                </svg>
              )}
            </button>
            <HoverTooltip direction="down" className="w-36 text-center">
              {privacy ? 'Disable private mode' : 'Enable private mode'}
            </HoverTooltip>
          </div>
        </div>

        {/* TWR / MWR secondary indicators */}
        {(mwr !== null || twr !== null) && (
          <div className="flex items-center gap-8">
            {twr !== null && (
              <div className="flex flex-col items-center gap-0.5">
                <span className="text-[10px] md:text-xs text-slate-500">TWR</span>
                <span className={`text-sm md:text-base font-semibold tabular-nums ${twr >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                  {twr >= 0 ? '+' : ''}{twr.toFixed(2)}%
                </span>
              </div>
            )}
            {mwr !== null && (
              <div className="flex flex-col items-center gap-0.5">
                <span className="text-[10px] md:text-xs text-slate-500">MWR</span>
                <span className={`text-sm md:text-base font-semibold tabular-nums ${mwr >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                  {mwr >= 0 ? '+' : ''}{mwr.toFixed(2)}%
                </span>
              </div>
            )}
          </div>
        )}

        {/* LLM Market Summary Widget */}
        {llmAvailable !== false && shouldShowLlm && (
          <div className="pointer-events-auto mt-1 w-[95%] md:w-[80%] max-w-7xl px-2 py-1 flex flex-col items-center gap-1">
             <div className="flex items-center gap-2">
               <span className="text-[10px] uppercase font-bold text-indigo-300">What happened past:</span>
               <div className="flex gap-1">
                  {['1d', '1w', '1m'].map(p => {
                    const label = p === '1d' ? 'Day' : p === '1w' ? 'Week' : 'Month';
                    return (
                      <button
                        key={p}
                        onClick={() => setLlmPeriod(p)}
                        className={`text-[10px] uppercase font-bold px-2 py-0.5 rounded-md transition-all ${llmPeriod === p ? 'text-indigo-400' : 'text-indigo-300/50 hover:text-indigo-300'}`}
                      >
                        {label}
                      </button>
                    )
                  })}
               </div>
               <div className="relative group">
                 <button
                   id="llm-refresh-btn"
                   onClick={() => { if (!llmSummaryLoading) setLlmForceRefresh(true) }}
                   disabled={llmSummaryLoading}
                   className="w-5 h-5 flex items-center justify-center rounded-md text-indigo-300/40 hover:text-indigo-300 hover:bg-white/[0.07] transition-all duration-200 active:scale-90 disabled:opacity-30 disabled:cursor-not-allowed"
                   aria-label="Force refresh market summary"
                 >
                   <svg
                     xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24"
                     fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
                     className={llmSummaryLoading ? 'animate-spin' : ''}
                   >
                     <polyline points="23 4 23 10 17 10"/>
                     <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
                   </svg>
                 </button>
                 <HoverTooltip direction="down" className="w-32 text-center">Regenerate summary</HoverTooltip>
               </div>
             </div>
             <div className="text-xs text-indigo-100/90 text-left leading-relaxed font-medium min-h-7.5 w-full max-w-none px-4 mx-auto flex flex-col items-center justify-center mt-1">
               {llmSummaryLoading ? (
                 <span className="animate-pulse text-center">Analyzing latest market movements...</span>
               ) : (
                 <div className="w-full flex flex-col items-center">
                   {llmSummary ? (
                     <div className="flex flex-col gap-1.5 w-full">
                       <ReactMarkdown
                         components={{
                           p: ({ children }) => <p className="mb-1 last:mb-0 text-center mx-auto max-w-2xl text-slate-300">{children}</p>,
                           ul: ({ children }) => <ul className="grid grid-cols-1 md:grid-cols-2 gap-3 list-none justify-center w-full items-stretch">{children}</ul>,
                           li: ({ children }) => (
                             <li className="bg-white/3 hover:bg-white/5 transition-colors border border-white/5 px-4 py-2 rounded-2xl text-left shadow-md leading-relaxed w-full backdrop-blur-sm text-slate-200">
                               {children}
                             </li>
                           ),
                           strong: ({ children }) => <strong className="text-white font-bold tracking-wide">{children}</strong>,
                         }}
                       >
                         {llmSummary}
                       </ReactMarkdown>
                       {llmSummary !== "Failed to generate market summary." && (
                         <div className="text-center">
                           <button
                             onClick={digIntoThis}
                             className="inline text-[10px] uppercase font-black tracking-widest text-emerald-500/80 hover:text-emerald-400 transition-colors shadow-sm"
                           >
                             Dig into this →
                           </button>
                         </div>
                       )}
                     </div>
                   ) : <span className="text-center w-full">No market summary available.</span>}
                 </div>
               )}
             </div>
          </div>
        )}

        {/* Mode selector — hidden when no trades */}
        {hasTransactions !== false && (
          <div className="pointer-events-auto flex items-center gap-1 mt-4 bg-surface rounded-2xl p-1 border border-white/6">
            {(['value', 'twr', 'mwr'] as const).map(mode => (
              <button
                key={mode}
                onClick={() => setChartMode(mode)}
                className={`px-5 py-1.5 rounded-xl text-sm font-medium transition-all duration-200 ${
                  chartMode === mode
                    ? 'glass active text-indigo-300 shadow-lg'
                    : 'text-slate-500 hover:text-slate-300'
                }`}
              >
                {mode === 'value' ? 'Value' : mode.toUpperCase()}
              </button>
            ))}
          </div>
        )}
      </div>

      {hasTransactions === false ? (
        /* ── Empty state: no trades uploaded yet ── */
        <div className="relative flex-1 flex flex-col items-center justify-center gap-8 px-4 md:px-8 mb-6">
          <p className="text-slate-500 text-[10px] font-black uppercase tracking-[0.3em]">Upload your portfolio data to get started</p>
          <div className="flex flex-col sm:flex-row gap-4 w-full max-w-2xl">
            {([
              { label: 'IBKR FlexQuery',   desc: 'Interactive Brokers XML report', accept: '.xml',  onChange: handleUpload,               cardCls: 'border-indigo-500/20 bg-indigo-500/5 hover:bg-indigo-500/10',  iconCls: 'bg-indigo-500/10 text-indigo-400 group-hover:bg-indigo-500/20',  titleCls: 'text-indigo-400'  },
              { label: 'E*Trade Benefits', desc: 'RSU & ESPP benefit history',      accept: '.xlsx', onChange: handleEtradeBenefitsUpload, cardCls: 'border-emerald-500/20 bg-emerald-500/5 hover:bg-emerald-500/10', iconCls: 'bg-emerald-500/10 text-emerald-400 group-hover:bg-emerald-500/20', titleCls: 'text-emerald-400' },
              { label: 'E*Trade Sales',    desc: 'Gains & losses report',           accept: '.xlsx', onChange: handleEtradeSalesUpload,    cardCls: 'border-amber-500/20 bg-amber-500/5 hover:bg-amber-500/10',     iconCls: 'bg-amber-500/10 text-amber-400 group-hover:bg-amber-500/20',     titleCls: 'text-amber-400'   },
            ] as const).map(({ label, desc, accept, onChange, cardCls, iconCls, titleCls }) => (
              <label
                key={label}
                className={`flex-1 flex flex-col items-center gap-4 p-6 rounded-2xl border cursor-pointer transition-all duration-200 group active:scale-[0.98] ${cardCls}`}
              >
                <div className={`w-12 h-12 rounded-xl flex items-center justify-center transition-colors ${iconCls}`}>
                  <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                </div>
                <div className="flex flex-col items-center gap-1 text-center">
                  <span className={`text-xs font-black uppercase tracking-[0.15em] ${titleCls}`}>{label}</span>
                  <span className="text-[10px] text-slate-500 font-medium">{desc}</span>
                </div>
                <input type="file" accept={accept} onChange={onChange} className="hidden" disabled={uploading} />
              </label>
            ))}
          </div>

          {/* Status messages */}
          <div className="static md:absolute md:bottom-4 md:left-8">
            {uploading && (
              <div className="flex items-center gap-4 text-slate-400 text-[10px] font-black uppercase tracking-[0.3em] bg-surface/80 px-6 py-3 rounded-2xl border border-white/5 shadow-2xl backdrop-blur-3xl">
                <div className="w-3 h-3 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin" />
                Processing upload…
              </div>
            )}
            {error && (
              <div className="px-6 py-3 bg-red-500/10 border border-red-500/20 text-red-400 text-[10px] font-black uppercase tracking-[0.2em] rounded-2xl animate-fade-in shadow-2xl shadow-red-500/10 backdrop-blur-3xl">
                {error}
              </div>
            )}
          </div>
        </div>
      ) : (
        /* ── Normal chart area ── */
        <div className="relative flex-1 mt-auto flex flex-col justify-end pl-2 pr-2 md:pl-8 md:pr-24 mb-6 min-h-80 md:min-h-0">

          {/* The chart itself — axes returned and labels added */}
          <div className="relative w-full h-80 md:h-[85%] md:min-h-87.5 [@media(max-aspect-ratio:18/10)]:md:h-[90%]">
            {chartRefreshing && (
              <div className="absolute top-2 right-2 z-10 w-3.5 h-3.5 rounded-full border border-indigo-400/30 border-t-indigo-300/60 animate-spin opacity-50" />
            )}
            {chartLoading ? (
              <div className="h-full flex items-center justify-center text-slate-500 font-black uppercase tracking-[0.3em] text-[10px] animate-pulse">Loading chart…</div>
            ) : chartData.length === 0 ? (
              <div className="h-full flex items-center justify-center text-slate-500 font-black uppercase tracking-[0.3em] text-[10px]">No data available</div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ top: 0, right: 0, left: 10, bottom: 0 }}>
                  <defs>
                    <linearGradient id="chartGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#6366f1" stopOpacity={0.15} />
                      <stop offset="100%" stopColor="#6366f1" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <XAxis
                    dataKey="date"
                    tick={{fontSize: 9, fill: '#334155', fontWeight: 'bold'}}
                    tickLine={false}
                    axisLine={false}
                    interval={Math.floor(chartData.length / 6)}
                    dy={10}
                  />
                  <YAxis
                    domain={['auto', 'auto']}
                    tick={{fontSize: 9, fill: '#334155', fontWeight: 'bold'}}
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={(val) => privacy && chartMode === 'value' ? '—' : chartMode === 'value' ? formatCurrencyCompact(val, currency) : `${val.toFixed(1)}%`}
                  />
                  <Tooltip
                    contentStyle={RECHARTS_TOOLTIP_STYLE}
                    labelStyle={RECHARTS_LABEL_STYLE}
                    itemStyle={{ ...RECHARTS_ITEM_STYLE, fontWeight: 'bold', textTransform: 'uppercase', letterSpacing: '0.1em' }}
                    formatter={(value) => [
                      privacy && chartMode === 'value'
                        ? '———'
                        : chartMode === 'value'
                          ? formatCurrencyCompact(Number(value), currency)
                          : `${Number(value).toFixed(2)}%`,
                      chartMode.toUpperCase(),
                    ]}
                  />
                  <Area
                    type="monotone"
                    dataKey="value"
                    stroke="#6366f1"
                    strokeWidth={1.5}
                    fill="url(#chartGrad)"
                    dot={false}
                    animationDuration={1500}
                  />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </div>

          {/* Period vertical pills — middle-right (desktop) */}
          <div className="hidden md:flex absolute right-8 bottom-44 flex-col items-center gap-2 z-10">
            {PERIODS.map(p => (
              <button
                key={p.label}
                onClick={() => setPeriod(p.months)}
                className={`w-10 h-10 rounded-xl text-[9px] font-bold uppercase transition-all duration-200 flex items-center justify-center shadow-lg ${
                  period === p.months
                    ? 'bg-indigo-600 text-white ring-2 ring-indigo-500/20 shadow-indigo-600/20'
                    : 'text-slate-500 hover:text-slate-300 hover:bg-white/5 bg-surface/40 border border-white/5'
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>

          {/* Period horizontal pills — below chart (mobile) */}
          <div className="md:hidden flex justify-center gap-2 mt-3">
            {PERIODS.map(p => (
              <button
                key={p.label}
                onClick={() => setPeriod(p.months)}
                className={`px-4 py-2 rounded-xl text-[9px] font-bold uppercase transition-all duration-200 shadow-lg ${
                  period === p.months
                    ? 'bg-indigo-600 text-white ring-2 ring-indigo-500/20 shadow-indigo-600/20'
                    : 'text-slate-500 hover:text-slate-300 hover:bg-white/5 bg-surface/40 border border-white/5'
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>

          {/* Upload buttons — bottom right */}
          {uploadExpanded && (
            <div
              className="fixed inset-0 z-10 md:hidden"
              onClick={() => setUploadExpanded(false)}
            />
          )}
          <div
            className={`absolute bottom-4 right-8 flex flex-col items-end gap-2 z-20 ${uploadExpanded ? 'pointer-events-auto' : 'pointer-events-none'}`}
            onMouseLeave={() => setUploadExpanded(false)}
          >
            {/* Expanded options */}
            <div
              className="flex flex-col items-end gap-2 px-3 py-2 rounded-2xl transition-all duration-200"
              style={{
                background: uploadExpanded ? 'rgba(15,17,23,0.7)' : 'transparent',
                backdropFilter: uploadExpanded ? 'blur(20px)' : 'none',
                boxShadow: uploadExpanded ? '0 8px 32px rgba(0,0,0,0.4)' : 'none',
                border: uploadExpanded ? '1px solid rgba(255,255,255,0.06)' : '1px solid transparent',
              }}
            >
              {([
                { label: 'IBKR FlexQuery',   accept: '.xml',  onChange: handleUpload,               labelCls: 'text-indigo-400',  btnCls: 'bg-indigo-500/10 text-indigo-400 border-indigo-500/20 hover:bg-indigo-600',  delay: '150ms' },
                { label: 'E*Trade Benefits', accept: '.xlsx', onChange: handleEtradeBenefitsUpload, labelCls: 'text-emerald-400', btnCls: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20 hover:bg-emerald-600', delay: '75ms'  },
                { label: 'E*Trade Sales',    accept: '.xlsx', onChange: handleEtradeSalesUpload,    labelCls: 'text-amber-400',   btnCls: 'bg-amber-500/10 text-amber-400 border-amber-500/20 hover:bg-amber-600',         delay: '0ms'   },
              ] as const).map(({ label, accept, onChange, labelCls, btnCls, delay }) => (
                <label
                  key={label}
                  className="flex items-center gap-3 cursor-pointer"
                  onClick={() => setUploadExpanded(false)}
                  style={{
                    opacity: uploadExpanded ? 1 : 0,
                    transform: uploadExpanded ? 'translateY(0) scale(1)' : 'translateY(6px) scale(0.97)',
                    transition: `opacity 200ms ease ${uploadExpanded ? delay : '0ms'}, transform 200ms ease ${uploadExpanded ? delay : '0ms'}`,
                    pointerEvents: uploadExpanded ? 'auto' : 'none',
                  }}
                >
                  <span className={`text-[9px] font-black uppercase tracking-[0.2em] whitespace-nowrap ${labelCls}`}>{label}</span>
                  <div className={`w-10 h-10 rounded-xl flex items-center justify-center border hover:text-white transition-all shadow-lg active:scale-95 ${btnCls}`}>
                    <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                  </div>
                  <input type="file" accept={accept} onChange={onChange} className="hidden" disabled={uploading} />
                </label>
              ))}
            </div>

            {/* Trigger button */}
            <button
              onMouseEnter={() => setUploadExpanded(true)}
              onClick={() => setUploadExpanded(o => !o)}
              className={`w-10 h-10 rounded-xl flex items-center justify-center transition-all duration-200 shadow-lg pointer-events-auto ${uploadExpanded ? 'bg-indigo-600 text-white' : 'bg-indigo-500/10 text-indigo-400 border border-indigo-500/20'}`}
            >
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
            </button>
          </div>

          {/* General error — bottom left */}
          {error && (
            <div className="absolute bottom-4 left-8">
              <div className="px-6 py-3 bg-red-500/10 border border-red-500/20 text-red-400 text-[10px] font-black uppercase tracking-[0.2em] rounded-2xl animate-fade-in shadow-2xl shadow-red-500/10 backdrop-blur-3xl">
                {error}
              </div>
            </div>
          )}
        </div>
      )}

      <UploadResultModal
        open={showUploadModal}
        uploading={uploading}
        error={uploadModalError}
        transactions={uploadModalTransactions}
        corporateActions={uploadModalCorporateActions}
        onClose={handleModalClose}
      />
    </div>
  )
}
