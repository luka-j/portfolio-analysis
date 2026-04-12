import { useState, useEffect, useCallback } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import PageLayout from '../components/PageLayout'
import HoverTooltip from '../components/HoverTooltip'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import SymbolMappingModal from '../components/SymbolMappingModal'
import AddTransactionModal from '../components/AddTransactionModal'
import DateRangePicker from '../components/DateRangePicker'
import { TradeDetail } from '../components/TradeDetail'
import ErrorAlert from '../components/ErrorAlert'
import { getPortfolioValue, getPortfolioPriceHistory, updateSymbolMapping, type PositionValue, type SymbolPriceHistory } from '../api'
import { formatCurrency, formatNumber, formatDate } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'
import { usePrivacy } from '../utils/PrivacyContext'

const FX_METHOD_OPTIONS = [
  { label: 'Historical', value: 'historical' as const, tooltip: 'Uses the FX rate at the time each trade was executed. Reflects your true cost basis in the currency, accounting for currency movements over time.' },
  { label: 'Spot',       value: 'spot'       as const, tooltip: "Applies today's FX rate to all prices. Shows current market value converted at the current exchange rate, regardless of when trades were made." },
]

const CURRENCY_OPTIONS = [
  { label: 'CZK',      value: 'CZK' },
  { label: 'USD',      value: 'USD' },
  { label: 'EUR',      value: 'EUR' },
  { label: 'Original', value: 'Original', tooltip: 'Shows each position in its native trading currency without any conversion applied. Totals cannot be aggregated across currencies.', tooltipAlign: 'right' as const },
]

const PERIOD_OPTIONS = [
  { label: '1D', value: '1d' },
  { label: '1M', value: '1m' },
  { label: '1Y', value: '1y' },
  { label: 'Custom', value: 'custom' },
]

function getPeriodDates(period: string, customFrom: string, customTo: string): { from: string; to: string } {
  const today = formatDate(new Date())
  if (period === 'custom') return { from: customFrom, to: customTo }
  const d = new Date()
  if (period === '1d') { d.setDate(d.getDate() - 1) }
  else if (period === '1m') { d.setMonth(d.getMonth() - 1) }
  else if (period === '1y') { d.setFullYear(d.getFullYear() - 1) }
  return { from: formatDate(d), to: today }
}

export default function PortfolioPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { privacy } = usePrivacy()
  const [showWelcome, setShowWelcome] = useState(() => !!(location.state as { firstUpload?: boolean } | null)?.firstUpload)
  const [globalCurrency, setGlobalCurrency] = usePersistentState<string>('app_currency', 'CZK')
  // 'Original' is a portfolio-only option — it doesn't propagate to other pages.
  const [localOriginal, setLocalOriginal] = useState(false)
  const currency = localOriginal ? 'Original' : globalCurrency
  const setCurrency = (v: string) => {
    if (v === 'Original') { setLocalOriginal(true) }
    else { setLocalOriginal(false); setGlobalCurrency(v) }
  }
  const [acctModel, setAcctModel] = usePersistentState<'historical' | 'spot'>('portfolio_acctModel', 'historical')
  const [period, setPeriod] = usePersistentState('portfolio_period', '1m')
  const defaultCustomFrom = (() => { const d = new Date(); d.setMonth(d.getMonth() - 1); return formatDate(d) })()
  const [customFrom, setCustomFrom] = usePersistentState('portfolio_customFrom', defaultCustomFrom)
  const [customTo, setCustomTo] = usePersistentState('portfolio_customTo', formatDate(new Date()))
  const [positions, setPositions] = useState<PositionValue[]>([])
  const [totalValue, setTotalValue] = useState(0)
  const [loading, setLoading] = useState(true)
  const [valueRefreshing, setValueRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [mappingTarget, setMappingTarget] = useState<{ symbol: string; yahooSymbol?: string; exchange?: string } | null>(null)
  const [sortCol, setSortCol] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<'desc' | 'asc' | null>(null)
  const [priceHistory, setPriceHistory] = useState<Record<string, SymbolPriceHistory>>({})
  const [phLoading, setPhLoading] = useState(false)
  const [showAddTransaction, setShowAddTransaction] = useState(false)

  const loadData = useCallback(async () => {
    setLoading(true)
    setValueRefreshing(false)
    setError('')

    let freshArrived = false

    // 1. Cached call — show positions immediately if there's data
    getPortfolioValue(currency, acctModel, true).then(val => {
      if (!freshArrived && (val.positions ?? []).length > 0) {
        const sorted = [...(val.positions ?? [])].sort((a, b) => (b.value || 0) - (a.value || 0))
        setPositions(sorted)
        setTotalValue(val.value || 0)
        setLoading(false)
        setValueRefreshing(true)
      }
    }).catch(() => {})

    // 2. Fresh call — always takes priority
    try {
      const val = await getPortfolioValue(currency, acctModel, false)
      freshArrived = true
      const sorted = [...(val.positions ?? [])].sort((a, b) => (b.value || 0) - (a.value || 0))
      setPositions(sorted)
      setTotalValue(val.value || 0)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
      setValueRefreshing(false)
    }
  }, [currency, acctModel])

  useEffect(() => { loadData() }, [loadData])

  useEffect(() => {
    const { from, to } = getPeriodDates(period, customFrom, customTo)
    setPhLoading(true)
    getPortfolioPriceHistory(from, to, currency, acctModel)
      .then(res => {
        const map: Record<string, SymbolPriceHistory> = {}
        for (const item of res.items) {
          const key = item.exchange ? `${item.symbol}@${item.exchange}` : item.symbol
          map[key] = item
        }
        setPriceHistory(map)
      })
      .catch(() => setPriceHistory({}))
      .finally(() => setPhLoading(false))
  }, [period, customFrom, customTo, currency, acctModel])

  const totals = positions.reduce(
    (acc, pos) => {
      const price = pos.price || 0
      const costBasis = pos.cost_basis || 0
      if (costBasis > 0) {
        acc.unrealizedGL += (price - costBasis) * pos.quantity
        acc.hasUnrealized = true
      }
      acc.realizedGL += pos.realized_gl || 0
      acc.commission += pos.commission || 0
      return acc
    },
    { unrealizedGL: 0, realizedGL: 0, commission: 0, hasUnrealized: false }
  )

  // Weighted-average price change % based on each position's share of total value.
  // Only positions that have priceHistory data contribute; the weight denominator is
  // re-normalized to the combined value of those positions (not the full portfolio).
  const weightedChangePct = (() => {
    if (totalValue <= 0) return null
    let weightedSum = 0
    let hasCoverage = false
    for (const pos of positions) {
      const posKey = pos.listing_exchange ? `${pos.symbol}@${pos.listing_exchange}` : pos.symbol
      const changePct = priceHistory[posKey]?.change_pct
      if (changePct == null) continue
      hasCoverage = true
      weightedSum += changePct * (pos.value || 0)
    }
    if (!hasCoverage) return null
    return weightedSum / totalValue
  })()

  const handleSparkle = (e: React.MouseEvent, pos: PositionValue) => {
    e.stopPropagation()
    const label = pos.name ? `${pos.symbol} (${pos.name})` : pos.symbol
    navigate('/llm', {
      state: {
        initialPrompt: {
          promptType: 'ticker_analysis',
          displayMessage: `Analyze recent market activity for ${label}`,
          extraParams: { symbol: pos.symbol },
        },
      },
    })
  }

  const handleMapSymbol = (e: React.MouseEvent, symbol: string, currentYahooSymbol?: string, exchange?: string) => {
    e.stopPropagation()
    setMappingTarget({ symbol, yahooSymbol: currentYahooSymbol, exchange })
  }

  const handleMapConfirm = async (yahooSymbol: string) => {
    if (!mappingTarget) return
    try {
      await updateSymbolMapping(mappingTarget.symbol, yahooSymbol, mappingTarget.exchange)
      setMappingTarget(null)
      await loadData()
    } catch {
      window.alert('Failed to map symbol')
    }
  }

  const toggleExpand = (symbol: string, exchange?: string) => {
    const key = exchange ? `${symbol}@${exchange}` : symbol
    setExpanded(prev => prev === key ? null : key)
  }

  const handleSort = (col: string) => {
    if (sortCol !== col) {
      setSortCol(col)
      setSortDir('desc')
    } else if (sortDir === 'desc') {
      setSortDir('asc')
    } else {
      setSortCol(null)
      setSortDir(null)
    }
  }

  const sortedPositions = [...positions].sort((a, b) => {
    if (!sortCol || !sortDir) return 0
    const mul = sortDir === 'desc' ? -1 : 1
    const posKeyA = a.listing_exchange ? `${a.symbol}@${a.listing_exchange}` : a.symbol
    const posKeyB = b.listing_exchange ? `${b.symbol}@${b.listing_exchange}` : b.symbol
    const getVal = (pos: PositionValue, posKey: string): number | string => {
      switch (sortCol) {
        case 'symbol': return pos.symbol
        case 'qty': return pos.quantity
        case 'price': return pos.price || 0
        case 'value': return pos.value || 0
        case 'pct': return pos.value || 0
        case 'change': return priceHistory[posKey]?.change_pct ?? -Infinity
        case 'avgprice': return priceHistory[posKey]?.avg_price ?? -Infinity
        case 'unrealized': return pos.cost_basis ? ((pos.price || 0) - pos.cost_basis) * pos.quantity : -Infinity
        case 'realized_comm': return pos.realized_gl || 0
        default: return 0
      }
    }
    const av = getVal(a, posKeyA), bv = getVal(b, posKeyB)
    if (typeof av === 'string' && typeof bv === 'string') return mul * av.localeCompare(bv)
    return mul * ((av as number) - (bv as number))
  })

  const SortIndicator = ({ col }: { col: string }) => {
    if (sortCol !== col) return <span className="ml-1 opacity-40 text-[9px] align-middle">↕</span>
    return <span className="ml-1 text-[9px] text-indigo-400 align-middle">{sortDir === 'desc' ? '↓' : '↑'}</span>
  }

  const { from: periodFrom, to: periodTo } = getPeriodDates(period, customFrom, customTo)

  const tableGridCols = 'minmax(0, 2.7fr) minmax(0, 0.8fr) repeat(4, minmax(0, 0.85fr)) repeat(2, minmax(0, 1.15fr)) repeat(6, minmax(0, 0.85fr)) minmax(0, 0.5fr)'

  return (
    <PageLayout maxWidth="max-w-[1400px]">
      {mappingTarget && (
        <SymbolMappingModal
          symbol={mappingTarget.symbol}
          currentYahooSymbol={mappingTarget.yahooSymbol}
          onConfirm={handleMapConfirm}
          onClose={() => setMappingTarget(null)}
        />
      )}
      {showWelcome && (
        <div className="mb-8 flex items-start gap-4 px-5 py-4 rounded-2xl bg-emerald-500/10 border border-emerald-500/25 shadow-lg shadow-emerald-500/5">
          <div className="mt-0.5 w-8 h-8 shrink-0 rounded-xl bg-emerald-500/15 flex items-center justify-center text-emerald-400">
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-semibold text-emerald-300">Portfolio data successfully uploaded!</p>
            <p className="mt-0.5 text-xs text-emerald-400/70 leading-relaxed">
              Use the{' '}
              <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md bg-white/5 border border-white/10 text-slate-300 align-middle">
                <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
              </span>{' '}
              pencil icon next to each position to adjust Yahoo! Finance symbol mappings.<br/>This ensures accurate price data and performance calculations.
            </p>
          </div>
          <button
            onClick={() => setShowWelcome(false)}
            className="shrink-0 text-emerald-400/40 hover:text-emerald-400 transition-colors"
            aria-label="Dismiss"
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          </button>
        </div>
      )}
          {/* Header centered */}
          <div className="w-full flex flex-col items-center mb-16 text-center">
            <div className="flex items-center justify-center gap-3">
              <h1 className="text-3xl font-semibold text-slate-100">Portfolio Holdings</h1>
              {valueRefreshing && (
                <span className="w-5 h-5 rounded-full border-2 border-indigo-400/25 border-t-indigo-300 animate-spin" />
              )}
            </div>
            <p className="text-slate-500 text-sm mt-4">Active positions and unrealized performance</p>
          </div>

        {/* Controls — centered */}
        <div className="flex flex-wrap justify-center gap-4 mb-6">
          <SegmentedControl label="FX Method" options={FX_METHOD_OPTIONS} value={acctModel} onChange={setAcctModel} />
          <SegmentedControl label="Currency" options={CURRENCY_OPTIONS} value={currency} onChange={setCurrency} />
          {/* Period shown here on desktop only; on mobile it moves below the table */}
          <div className="hidden md:block">
            <SegmentedControl label="Period" options={PERIOD_OPTIONS} value={period} onChange={setPeriod} />
          </div>
        </div>

        {period === 'custom' && (
          <div className="hidden md:flex justify-center mb-10">
            <DateRangePicker
              initialFrom={customFrom}
              initialTo={customTo}
              onApply={(f, t) => { setCustomFrom(f); setCustomTo(t) }}
            />
          </div>
        )}

        {period !== 'custom' && <div className="hidden md:block mb-10" />}

        {error && <ErrorAlert message={error} className="mb-10" />}

        {loading ? (
          <Spinner label="Loading…" className="py-24" />
        ) : positions.length === 0 ? (
          <div className="text-center py-24 text-slate-500 font-black uppercase tracking-[0.2em] text-[11px]">No holdings found. Upload your data first.</div>
        ) : (
          <div className="w-full selection:bg-indigo-500/20 overflow-x-auto">
          <div className="min-w-[960px]">
            {/* Table header */}
            <div
              className="grid gap-4 px-8 py-5 text-xs font-semibold text-slate-500 border-b border-border-dim/40"
              style={{ gridTemplateColumns: tableGridCols }}
            >
              {(['symbol', 'qty', 'price', 'value', 'pct', 'change', 'unrealized', 'realized_comm'] as const).map((col) => {
                const labels: Record<string, string> = { symbol: 'Symbol', qty: 'Qty', price: 'Curr. Mkt. Price', value: 'Value', pct: 'Portfolio %', change: 'Price Change %', unrealized: 'Unrealized', realized_comm: 'Realized / Comm.' }
                const spanClass: Record<string, string> = { symbol: 'col-span-1', qty: 'col-span-1', price: 'col-span-2', value: 'col-span-2', pct: 'col-span-2', change: 'col-span-2', unrealized: 'col-span-2', realized_comm: 'col-span-2' }
                const isRight = col !== 'symbol'
                return (
                  <div
                    key={col}
                    className={`${spanClass[col]} flex items-center ${isRight ? 'justify-end' : ''} cursor-pointer select-none hover:text-slate-300 transition-colors`}
                    onClick={() => handleSort(col)}
                  >
                    {col === 'change' ? (
                      <span className="flex items-center gap-1">
                        {labels[col]}
                        {phLoading && <span className="w-1.5 h-1.5 rounded-full bg-indigo-500/60 animate-pulse inline-block" />}
                        <SortIndicator col={col} />
                      </span>
                    ) : (
                      <>{labels[col]}<SortIndicator col={col} /></>
                    )}
                  </div>
                )
              })}
              <div className="col-span-1" />
            </div>

            {/* Rows */}
            <div className="divide-y divide-[#2a2e42]/40">
              {sortedPositions.map(pos => {
                const isPendingCash = pos.symbol === 'PENDING_CASH'
                const value = pos.value || 0
                const price = pos.price || 0
                const costBasis = pos.cost_basis || 0
                const realizedGLNative = pos.realized_gl || 0
                const commission = pos.commission || 0
                const pct = totalValue > 0 ? (value / totalValue) * 100 : 0
                const unrealizedGL = costBasis > 0 ? (price - costBasis) * pos.quantity : null
                const unrealizedPct = costBasis > 0 ? ((price - costBasis) / costBasis) * 100 : null
                const posKey = pos.listing_exchange ? `${pos.symbol}@${pos.listing_exchange}` : pos.symbol
                const isExpanded = expanded === posKey
                const ph = priceHistory[posKey]

                if (isPendingCash) {
                  return (
                    <div key="PENDING_CASH">
                      <div
                        className="grid gap-4 px-8 py-4 items-center hover:bg-white/2 transition-all duration-300"
                        style={{ display: 'grid', gridTemplateColumns: tableGridCols }}
                      >
                        {/* Symbol cell */}
                        <div className="col-span-1 flex items-center gap-4">
                          <div className="relative group w-8 h-8 shrink-0 rounded-xl bg-surface flex items-center justify-center border border-border-dim/50 shadow-sm cursor-help">
                            <span className="text-emerald-400 text-sm font-bold">$</span>
                            <HoverTooltip direction="down" align="left" className="w-max whitespace-nowrap">Cash</HoverTooltip>
                          </div>
                          <div className="min-w-0">
                            <div className="font-semibold flex items-center gap-2 text-slate-100 text-sm tracking-tight">
                              <div className="relative group">
                                <span className="cursor-default">Pending Cash</span>
                                <HoverTooltip className="w-72" direction="down">
                                  Cash from recent sales waiting to be reinvested. Purchases within 30 days use this cash instead of counting as new deposits. After 30 days without reinvestment, the cash is counted as a portfolio withdrawal.
                                </HoverTooltip>
                              </div>
                            </div>
                            <div className="text-xs font-medium text-slate-500 flex items-center gap-1.5 mt-1 opacity-80">
                              <span>{currency === 'Original' ? pos.native_currency : currency}</span>
                            </div>
                          </div>
                        </div>

                        <div className="col-span-1 text-right text-sm text-slate-500 tabular-nums">—</div>
                        <div className="col-span-2 text-right text-sm text-slate-500 tabular-nums">—</div>

                        <div className="col-span-2 text-right text-sm font-semibold text-slate-100 tabular-nums">
                          {privacy ? '—' : formatCurrency(value, currency, pos.native_currency)}
                        </div>
                        <div className="col-span-2 text-right">
                          <div className="inline-flex items-center gap-3">
                            <div className="w-20 h-1 bg-white/5 rounded-full overflow-hidden">
                              <div className="h-full bg-amber-500 rounded-full shadow-[0_0_12px_rgba(245,158,11,0.4)]" style={{ width: `${Math.min(pct, 100)}%` }} />
                            </div>
                            <span className="text-xs font-medium text-slate-400 tabular-nums w-10 text-right">{pct.toFixed(1)}%</span>
                          </div>
                        </div>
                        <div className="col-span-2 text-right text-slate-500 opacity-40">—</div>
                        <div className="col-span-2 text-right text-slate-500 opacity-40">—</div>
                        <div className="col-span-2 text-right text-slate-500 opacity-40">—</div>
                        <div className="col-span-1" />
                      </div>
                    </div>
                  )
                }

                return (
                  <div key={posKey}>
                    <div
                      className={`grid gap-4 px-8 py-4 items-center cursor-pointer transition-all duration-300 ${isExpanded ? 'bg-surface ring-1 ring-white/5 shadow-2xl z-10' : 'hover:bg-white/2'}`}
                      style={{ display: 'grid', gridTemplateColumns: tableGridCols }}
                      onClick={() => toggleExpand(pos.symbol, pos.listing_exchange)}
                    >
                      {/* Symbol cell */}
                      <div className="col-span-1 flex items-center gap-4">
                        {pos.price_status ? (
                          <div className="relative group w-8 h-8 shrink-0 rounded-xl bg-surface flex items-center justify-center border border-red-500/20 shadow-sm">
                            <svg className="w-4 h-4 text-red-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round">
                              <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                              <line x1="12" y1="9" x2="12" y2="13"/>
                              <line x1="12" y1="17" x2="12.01" y2="17"/>
                            </svg>
                            <HoverTooltip direction="down" align="left" className="w-60">
                              {pos.price_status === 'no_data' && 'No price data available from Yahoo Finance. The symbol may not be recognised — try updating the Yahoo symbol mapping.'}
                              {pos.price_status === 'stale' && 'Last available price is outdated. Yahoo Finance may have stopped tracking it, or there is some intermittent issue.'}
                              {pos.price_status === 'fetch_failed' && 'Failed to retrieve price data. This may be a temporary connection issue, or the symbol mapping may be incorrect.'}
                            </HoverTooltip>
                          </div>
                        ) : (
                          <div className="relative group hidden md:flex w-8 h-8 shrink-0 rounded-xl bg-surface items-center justify-center border border-border-dim/50 shadow-sm cursor-help">
                            <div className={`w-2 h-2 rounded-full ${
                              pos.asset_type === 'Bond ETF'  ? 'bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.8)]' :
                              pos.asset_type === 'ETF'       ? 'bg-teal-400 shadow-[0_0_8px_rgba(45,212,191,0.8)]' :
                              pos.asset_type === 'Commodity' ? 'bg-orange-400 shadow-[0_0_8px_rgba(251,146,60,0.8)]' :
                                                               'bg-indigo-400 shadow-[0_0_8px_rgba(129,140,248,0.8)]'
                            }`} />
                            <HoverTooltip direction="down" align="left" className="w-max whitespace-nowrap">
                              {pos.asset_type === 'Bond ETF' ? 'Bond ETF' : pos.asset_type === 'ETF' ? 'ETF' : pos.asset_type === 'Commodity' ? 'Commodity' : 'Stock'}
                            </HoverTooltip>
                          </div>
                        )}
                        <div className="min-w-0">
                          <div className="font-semibold flex items-center gap-2 text-slate-100 text-sm tracking-tight min-w-0">
                            <span className={pos.name ? 'relative group/name cursor-default' : undefined}>
                              {pos.symbol}
                              {pos.name && (
                                <span className="absolute bottom-full left-0 mb-2 w-max max-w-56 px-3 py-2 bg-panel border border-border-dim/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover/name:opacity-100 transition-opacity z-50 shadow-2xl whitespace-normal font-normal tracking-normal">
                                  {pos.name}
                                </span>
                              )}
                            </span>
                            {pos.bond_duration != null && (
                              <div className="relative group">
                                <span className="text-amber-400 text-xs font-medium bg-amber-400/10 px-1.5 py-0.5 rounded-lg border border-amber-400/20 cursor-default">
                                  {pos.bond_duration.toFixed(1)}y
                                </span>
                                <HoverTooltip className="w-52">
                                  Bond ETF effective duration — the weighted-average time (in years) until cash flows are received. Higher duration means greater sensitivity to interest rate changes.
                                </HoverTooltip>
                              </div>
                            )}
                            <div className="flex items-center gap-0.5 shrink-0">
                              <div className="relative group">
                                <button
                                  onClick={(e) => handleMapSymbol(e, pos.symbol, pos.yahoo_symbol, pos.listing_exchange)}
                                  className="text-slate-500 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
                                >
                                  <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                                  </svg>
                                </button>
                                <HoverTooltip direction="down" className="w-max whitespace-nowrap">Edit Yahoo! symbol</HoverTooltip>
                              </div>
                              <div className="relative group">
                                <button
                                  onClick={(e) => handleSparkle(e, pos)}
                                  className="text-slate-500 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
                                >
                                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                                    <path d="M12 2L13.5 8.5L20 10L13.5 11.5L12 18L10.5 11.5L4 10L10.5 8.5Z" />
                                    <path d="M19 1l.9 2.6 2.6.9-2.6.9L19 8.5l-.9-2.6L15.5 4l2.6-.9z" opacity=".6" />
                                    <path d="M5 17l.7 2.1L7.8 20l-2.1.9L5 23l-.7-2.1L2.2 20l2.1-.9z" opacity=".6" />
                                  </svg>
                                </button>
                                <HoverTooltip direction="down" className="w-max whitespace-nowrap">LLM market analysis</HoverTooltip>
                              </div>
                            </div>
                          </div>
                          <div className="text-xs font-medium text-slate-500 flex items-center gap-1.5 mt-1 opacity-80">
                            {pos.listing_exchange && (
                              <>
                                <span className="font-semibold tracking-wide text-slate-500">
                                  {pos.listing_exchange}
                                </span>
                                <span className="w-1 h-1 rounded-full bg-slate-800" />
                              </>
                            )}
                            <span>{pos.native_currency}</span>
                          </div>
                        </div>
                      </div>

                      <div className="col-span-1 text-right text-sm text-slate-300 font-medium tabular-nums">{privacy ? '—' : formatNumber(pos.quantity, 0)}</div>

                      {/* Price cell — always visible, cost basis labeled clearly */}
                      <div className="col-span-2 text-right">
                        <div className="text-sm font-medium text-slate-300 tabular-nums">{formatNumber(pos.price || 0)} {currency === 'Original' ? pos.native_currency : currency}</div>
                        {!privacy && (pos.cost_basis || 0) > 0 && (
                          <div className="text-[10px] text-slate-500 mt-0.5">avg. cost {formatNumber(pos.cost_basis)}</div>
                        )}
                      </div>

                      <div className="col-span-2 text-right text-sm font-semibold text-slate-100 tabular-nums">
                        {privacy ? '—' : formatCurrency(value, currency, pos.native_currency)}
                      </div>
                      <div className="col-span-2 text-right">
                        <div className="inline-flex items-center gap-3">
                          <div className="w-20 h-1 bg-white/5 rounded-full overflow-hidden">
                            <div className="h-full bg-indigo-500 rounded-full shadow-[0_0_12px_rgba(99,102,241,0.4)]" style={{ width: `${Math.min(pct, 100)}%` }} />
                          </div>
                          <span className="text-xs font-medium text-slate-400 tabular-nums w-10 text-right">{pct.toFixed(1)}%</span>
                        </div>
                      </div>

                      {/* Change % cell */}
                      <div className="col-span-2 text-right relative group">
                        {ph?.change_pct != null ? (
                          <>
                            <span className={`text-sm font-medium tabular-nums ${ph.change_pct >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                              {ph.change_pct >= 0 ? '+' : ''}{ph.change_pct.toFixed(2)}%
                            </span>
                            {ph.avg_price != null && (
                              <HoverTooltip className="w-28" align="right">
                                <div className="text-slate-500 mb-1">Avg. Market Price</div>
                                <div className="text-slate-200 font-bold">
                                  {formatNumber(ph.avg_price)} {currency === 'Original' ? pos.native_currency : currency}
                                </div>
                              </HoverTooltip>
                            )}
                          </>
                        ) : <span className="text-slate-500 opacity-40">—</span>}
                      </div>

<div className="col-span-2 text-right">
                        {privacy ? (
                          <span className="text-slate-500 font-medium">—</span>
                        ) : unrealizedGL !== null ? (
                          <div className="flex flex-col items-end">
                            <span className={`text-sm font-medium tabular-nums ${unrealizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                              {unrealizedGL >= 0 ? '+' : ''}{formatNumber(unrealizedGL)}
                            </span>
                            <div className={`text-xs mt-0.5 ${(unrealizedPct ?? 0) >= 0 ? 'text-emerald-400/50' : 'text-red-400/50'}`}>
                              {(unrealizedPct ?? 0) >= 0 ? '+' : ''}{(unrealizedPct ?? 0).toFixed(2)}%
                            </div>
                          </div>
                        ) : <span className="text-slate-500 font-medium opacity-40">—</span>}
                      </div>
                      <div className="col-span-2 text-right">
                        {privacy ? (
                          <span className="text-slate-500 opacity-40">—</span>
                        ) : (realizedGLNative !== 0 || commission !== 0) ? (
                          <div className="flex flex-col items-end">
                            {realizedGLNative !== 0 ? (
                              <span className={`text-sm font-medium tabular-nums ${realizedGLNative >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                                {realizedGLNative >= 0 ? '+' : ''}{formatNumber(realizedGLNative)}
                              </span>
                            ) : <span className="text-sm text-slate-500 opacity-40">—</span>}
                            {commission !== 0 && (
                              <div className="text-xs text-amber-500/80 font-medium tabular-nums mt-0.5">
                                {formatNumber(commission)} comm.
                              </div>
                            )}
                          </div>
                        ) : <span className="text-slate-500 opacity-40">—</span>}
                      </div>
                      <div className="col-span-1 text-right">
                        <span className={`text-slate-500 text-xs transition-transform duration-300 inline-block ${isExpanded ? 'rotate-180 text-indigo-400' : ''}`}>▼</span>
                      </div>
                    </div>

                    {isExpanded && (
                      <TradeDetail symbol={pos.symbol} exchange={pos.listing_exchange} isin={pos.isin} name={pos.name} displayCurrency={currency} acctModel={acctModel} privacy={privacy} onTradeDeleted={loadData} />
                    )}
                  </div>
                )
              })}
            </div>

            {/* Total row */}
            <div
              className="grid gap-4 px-8 py-8 mt-12 border-t-2 border-border-dim/60 bg-surface items-center rounded-3xl ring-1 ring-white/5 shadow-2xl"
              style={{ gridTemplateColumns: tableGridCols }}
            >
              <div className="col-span-4 flex items-center">
                <span className="text-xs font-black text-slate-100 tracking-[0.25em] uppercase">Aggregated Total</span>
              </div>
              <div className="col-span-2 text-right">
                <div className="text-xl font-black text-white tabular-nums tracking-tight">
                  {privacy ? '———' : currency === 'Original' ? 'MIXED' : formatCurrency(totalValue, currency)}
                </div>
              </div>
              <div className="col-span-2 text-right">
                <span className="text-xs font-black text-slate-500 tracking-widest tabular-nums">100.0%</span>
              </div>
              {/* Weighted change % */}
              <div className="col-span-2 text-right">
                {privacy ? (
                  <span className="text-slate-500 opacity-40">—</span>
                ) : weightedChangePct != null ? (
                  <span className={`text-sm font-black tabular-nums ${weightedChangePct >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                    {weightedChangePct >= 0 ? '+' : ''}{weightedChangePct.toFixed(2)}%
                  </span>
                ) : (
                  <span className="text-slate-500 opacity-40">—</span>
                )}
              </div>
              <div className="col-span-2 text-right">
                {privacy ? (
                  <span className="text-slate-500 opacity-40">—</span>
                ) : currency === 'Original' ? (
                  <span className="text-[10px] text-slate-500 uppercase font-black tracking-widest">N/A</span>
                ) : totals.hasUnrealized ? (
                  <span className={`text-sm font-black tabular-nums ${totals.unrealizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                    {totals.unrealizedGL >= 0 ? '+' : ''}{formatNumber(totals.unrealizedGL)}
                  </span>
                ) : <span className="text-slate-500 opacity-40">—</span>}
              </div>
              <div className="col-span-2 text-right">
                {privacy ? (
                  <span className="text-slate-500 opacity-40">—</span>
                ) : (totals.realizedGL !== 0 || totals.commission !== 0) ? (
                  <div className="flex flex-col items-end">
                    {totals.realizedGL !== 0 ? (
                      <span className={`text-sm font-black tabular-nums ${totals.realizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                        {totals.realizedGL >= 0 ? '+' : ''}{formatNumber(totals.realizedGL)}
                      </span>
                    ) : <span className="text-sm text-slate-500 opacity-40">—</span>}
                    {totals.commission !== 0 && (
                      <div className="text-xs font-black text-amber-500/80 tabular-nums mt-0.5">
                        {formatNumber(totals.commission)} comm.
                      </div>
                    )}
                  </div>
                ) : <span className="text-slate-500 opacity-40">—</span>}
              </div>
              <div className="col-span-1" />
            </div>

            {/* Period label */}
            <div className="text-center mt-4 text-[10px] text-slate-500 font-medium tracking-widest uppercase">
              Change % and Avg. Price over {periodFrom} — {periodTo}
            </div>
          </div>
          </div>
        )}

        {/* Mobile period selector — below the table */}
        <div className="md:hidden mt-8 flex flex-col items-center gap-4">
          <SegmentedControl label="Period" options={PERIOD_OPTIONS} value={period} onChange={setPeriod} />
          {period === 'custom' && (
            <DateRangePicker
              initialFrom={customFrom}
              initialTo={customTo}
              onApply={(f, t) => { setCustomFrom(f); setCustomTo(t) }}
            />
          )}
        </div>

        {/* Add Transaction FAB */}
        <button
          onClick={() => setShowAddTransaction(true)}
          className="fixed bottom-6 right-6 z-40 flex items-center gap-2 px-4 py-3 rounded-2xl bg-indigo-500/20 text-indigo-300 border border-indigo-500/30 hover:bg-indigo-500/30 hover:text-indigo-200 shadow-2xl backdrop-blur-sm transition-all text-sm font-semibold"
        >
          <span className="text-lg leading-none">+</span>
          Add Transaction
        </button>

        {showAddTransaction && (
          <AddTransactionModal
            positions={positions}
            onSuccess={loadData}
            onClose={() => setShowAddTransaction(false)}
          />
        )}
    </PageLayout>
  )
}
