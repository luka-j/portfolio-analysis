import { useState, useCallback, useMemo } from 'react'
import { XAxis, YAxis, Tooltip, ResponsiveContainer, Area, AreaChart } from 'recharts'
import ReactMarkdown from 'react-markdown'
import NavBar from '../components/NavBar'
import HoverTooltip from '../components/HoverTooltip'
import UploadResultModal from '../components/UploadResultModal'
import { useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  getPortfolioValueMulti, getPortfolioHistory, getPortfolioStats, getPortfolioReturns,
  uploadFlexQuery, uploadEtradeBenefits, uploadEtradeSales,
  getLLMSummary,
  type ImportedTransaction, type ImportedCorporateAction,
} from '../api'
import { formatCurrencyCompact, formatDate, CURRENCIES, CURRENCY_SYMBOLS, getFromDate, RECHARTS_TOOLTIP_STYLE, RECHARTS_LABEL_STYLE, RECHARTS_ITEM_STYLE } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'
import { usePrivacy } from '../utils/PrivacyContext'
import { useScenario } from '../context/ScenarioContext'
import { Skeleton } from '../components/Skeleton'

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
  
  const [uploadExpanded, setUploadExpanded] = useState(false)
  const [showUploadModal, setShowUploadModal] = useState(false)
  const [uploadModalTransactions, setUploadModalTransactions] = useState<ImportedTransaction[]>([])
  const [uploadModalCorporateActions, setUploadModalCorporateActions] = useState<ImportedCorporateAction[]>([])
  const [uploadModalError, setUploadModalError] = useState<string | null>(null)
  const [pendingFirstUpload, setPendingFirstUpload] = useState(false)

  const defaultPeriod = [0, 6].includes(new Date().getDay()) ? '1w' : '1d'
  const [llmPeriod, setLlmPeriod] = usePersistentState('landing_llmPeriod', defaultPeriod)

  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const cycleCurrency = () => setCurrency(c => {
    const idx = (CURRENCIES as readonly string[]).indexOf(c)
    return CURRENCIES[(idx + 1) % CURRENCIES.length]
  })

  // Queries
  const { data: valData, isLoading: valLoading, isFetching: valueRefreshing, error: valErrorObj } = useQuery({
    queryKey: ['portfolioValueMulti', active],
    queryFn: () => getPortfolioValueMulti(CURRENCIES as unknown as string[], 'historical', undefined, active),
    staleTime: 5 * 60 * 1000,
  })

  const portfolioValues = useMemo(() => {
    const next: Record<string, number> = {}
    if (!valData) return next
    for (const curr of CURRENCIES) {
      let sum = 0
      for (const pos of valData.positions ?? []) {
        const v = pos.values?.[curr]
        if (typeof v === 'number') sum += v
      }
      if (sum > 0) {
        next[curr] = sum
      }
    }
    if (Object.keys(next).length === 0) {
      next[currency] = valData.value
    }
    return next
  }, [valData, currency])

  const hasTransactions = valData ? valData.has_transactions : null
  const currValue = portfolioValues[currency] ?? 0
  const shouldShowLlm = hasTransactions === true && currValue > 0

  const fromDate = useMemo(() => getFromDate(period), [period])
  const toDate = useMemo(() => formatDate(new Date()), [period])

  const { data: statsData, isFetching: statsRefreshing } = useQuery({
    queryKey: ['portfolioStats', period, currency, active],
    queryFn: () => getPortfolioStats(fromDate, toDate, currency, 'historical', undefined, active),
    staleTime: 5 * 60 * 1000,
  })

  const stats = statsData?.statistics ?? null

  const { data: valueHistoryData, isLoading: valHistLoading, isFetching: valueHistFetching } = useQuery({
    queryKey: ['portfolioValueHistory', period, currency, active],
    queryFn: () => getPortfolioHistory(fromDate, toDate, currency, 'historical', undefined, active),
    staleTime: 5 * 60 * 1000,
    enabled: chartMode === 'value',
  })

  const { data: twrHistoryData, isLoading: twrHistLoading, isFetching: twrHistFetching } = useQuery({
    queryKey: ['portfolioTwrHistory', period, currency, active],
    queryFn: () => getPortfolioReturns(fromDate, toDate, currency, 'historical', 'twr', undefined, active),
    staleTime: 5 * 60 * 1000,
    enabled: chartMode === 'twr',
  })

  const { data: mwrHistoryData, isLoading: mwrHistLoading, isFetching: mwrHistFetching } = useQuery({
    queryKey: ['portfolioMwrHistory', period, currency, active],
    queryFn: () => getPortfolioReturns(fromDate, toDate, currency, 'historical', 'mwr', undefined, active),
    staleTime: 5 * 60 * 1000,
    enabled: chartMode === 'mwr',
  })

  const { data: llmSummaryData, isLoading: llmSummaryLoading, error: llmSummaryError } = useQuery({
    queryKey: ['llmSummary', llmPeriod, active],
    queryFn: () => getLLMSummary(llmPeriod, false, active),
    staleTime: 5 * 60 * 1000,
    enabled: shouldShowLlm,
  })

  const llmSummary = useMemo(() => {
    if (llmSummaryError) {
      if ((llmSummaryError as Error)?.message?.includes('GEMINI_API_KEY')) {
        return "Market summary unavailable. Please configure GEMINI_API_KEY."
      }
      return "Failed to generate market summary."
    }
    return llmSummaryData?.summary ?? ''
  }, [llmSummaryData, llmSummaryError])

  const llmAvailable = useMemo(() => {
    if (llmSummaryError) {
      if ((llmSummaryError as Error)?.message?.includes('GEMINI_API_KEY')) {
        return false
      }
      return true
    }
    if (llmSummaryData) return true
    return null
  }, [llmSummaryData, llmSummaryError])

  const [llmRefreshing, setLlmRefreshing] = useState(false)
  const handleLlmRefresh = async () => {
    setLlmRefreshing(true)
    try {
      const res = await getLLMSummary(llmPeriod, true, active)
      queryClient.setQueryData(['llmSummary', llmPeriod, active], res)
    } catch {
      // swallow
    } finally {
      setLlmRefreshing(false)
    }
  }

  const invalidateAll = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['portfolioValueMulti'] })
    queryClient.invalidateQueries({ queryKey: ['portfolioStats'] })
    queryClient.invalidateQueries({ queryKey: ['portfolioValueHistory'] })
    queryClient.invalidateQueries({ queryKey: ['portfolioTwrHistory'] })
    queryClient.invalidateQueries({ queryKey: ['portfolioMwrHistory'] })
    queryClient.invalidateQueries({ queryKey: ['llmSummary'] })
  }, [queryClient])

  // Mutations
  const uploadFlexMutation = useMutation({
    mutationFn: uploadFlexQuery,
    onSuccess: (res) => {
      setUploadModalTransactions(res.transactions ?? [])
      setUploadModalCorporateActions(res.corporate_actions ?? [])
      invalidateAll()
    },
    onError: (err) => {
      setUploadModalError(err instanceof Error ? err.message : 'Upload failed')
    }
  })

  const uploadEtradeBenefitsMutation = useMutation({
    mutationFn: uploadEtradeBenefits,
    onSuccess: (res) => {
      setUploadModalTransactions(res.transactions ?? [])
      setUploadModalCorporateActions([])
      invalidateAll()
    },
    onError: (err) => {
      setUploadModalError(err instanceof Error ? err.message : 'Upload failed')
    }
  })

  const uploadEtradeSalesMutation = useMutation({
    mutationFn: uploadEtradeSales,
    onSuccess: (res) => {
      setUploadModalTransactions(res.transactions ?? [])
      setUploadModalCorporateActions([])
      invalidateAll()
    },
    onError: (err) => {
      setUploadModalError(err instanceof Error ? err.message : 'Upload failed')
    }
  })

  const uploading = uploadFlexMutation.isPending || uploadEtradeBenefitsMutation.isPending || uploadEtradeSalesMutation.isPending

  const handleModalClose = useCallback(async () => {
    setShowUploadModal(false)
    setUploadModalTransactions([])
    setUploadModalCorporateActions([])
    setUploadModalError(null)
    if (pendingFirstUpload) {
      setPendingFirstUpload(false)
      navigate('/portfolio', { state: { firstUpload: true } })
    } else {
      invalidateAll()
    }
  }, [pendingFirstUpload, navigate, invalidateAll])

  const createUploadHandler = (mutation: any) =>
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0]
      if (!file) return
      setPendingFirstUpload(hasTransactions === false)
      setUploadModalTransactions([])
      setUploadModalCorporateActions([])
      setUploadModalError(null)
      setShowUploadModal(true)
      mutation.mutate(file, {
        onSettled: () => {
          e.target.value = ''
        }
      })
    }

  const handleUpload = createUploadHandler(uploadFlexMutation)
  const handleEtradeBenefitsUpload = createUploadHandler(uploadEtradeBenefitsMutation)
  const handleEtradeSalesUpload = createUploadHandler(uploadEtradeSalesMutation)

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

  const chartData = useMemo(() => {
    if (chartMode === 'value') {
      return valueHistoryData?.data.map(d => ({ date: d.date, value: d.value })) ?? []
    }
    if (chartMode === 'twr') {
      return twrHistoryData?.data.map(d => ({ date: d.date, value: d.value })) ?? []
    }
    if (chartMode === 'mwr') {
      return mwrHistoryData?.data.map(d => ({ date: d.date, value: d.value })) ?? []
    }
    return []
  }, [chartMode, valueHistoryData, twrHistoryData, mwrHistoryData])

  const chartLoading = chartMode === 'value' ? valHistLoading
    : chartMode === 'twr' ? twrHistLoading
    : mwrHistLoading

  const chartRefreshing = chartMode === 'value' ? !!valueHistoryData && valueHistFetching
    : chartMode === 'twr' ? !!twrHistoryData && twrHistFetching
    : !!mwrHistoryData && mwrHistFetching

  const loading = valLoading && hasTransactions === null
  const error = valErrorObj instanceof Error ? valErrorObj.message : ''

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
                   onClick={() => { if (!llmSummaryLoading && !llmRefreshing) handleLlmRefresh() }}
                   disabled={llmSummaryLoading || llmRefreshing}
                   className="w-5 h-5 flex items-center justify-center rounded-md text-indigo-300/40 hover:text-indigo-300 hover:bg-white/[0.07] transition-all duration-200 active:scale-90 disabled:opacity-30 disabled:cursor-not-allowed"
                   aria-label="Force refresh market summary"
                 >
                   <svg
                     xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24"
                     fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
                     className={llmSummaryLoading || llmRefreshing ? 'animate-spin' : ''}
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
              <Skeleton className="absolute inset-0 z-10 rounded-2xl" />
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
