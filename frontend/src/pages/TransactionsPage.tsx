import { useState, useEffect, useRef, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import PageLayout from '../components/PageLayout'
import SegmentedControl from '../components/SegmentedControl'
import ErrorAlert from '../components/ErrorAlert'
import { getTransactions, type EnrichedTrade } from '../api'
import { formatCurrency, formatQuantity } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'
import { usePrivacy } from '../utils/PrivacyContext'
import { Skeleton } from '../components/Skeleton'

const FX_METHOD_OPTIONS = [
  { label: 'Historical', value: 'historical' as const, tooltip: 'Uses the FX rate at the time each trade was executed. Reflects your true cost basis in the currency, accounting for currency movements over time.' },
  { label: 'Spot',       value: 'spot'       as const, tooltip: "Applies today's FX rate to all prices. Shows current market value converted at the current exchange rate, regardless of when trades were made." },
]

const CURRENCY_OPTIONS = [
  { label: 'CZK',      value: 'CZK' },
  { label: 'USD',      value: 'USD' },
  { label: 'EUR',      value: 'EUR' },
  { label: 'Original', value: 'Original', tooltip: 'Shows each transaction in its native currency without any conversion applied.', tooltipAlign: 'right' as const },
]

const SIDE_OPTIONS = [
  { label: 'All Types', value: 'all' },
  { label: 'BUY', value: 'BUY' },
  { label: 'SELL', value: 'SELL' },
  { label: 'ESPP VEST', value: 'ESPP_VEST' },
  { label: 'RSU VEST', value: 'RSU_VEST' },
]

const LIMIT = 30

export default function TransactionsPage() {
  const navigate = useNavigate()
  const { privacy } = usePrivacy()

  const [globalCurrency, setGlobalCurrency] = usePersistentState<string>('app_currency', 'CZK')
  const [localOriginal, setLocalOriginal] = useState(false)
  const currency = localOriginal ? 'Original' : globalCurrency
  const setCurrency = (v: string) => {
    if (v === 'Original') {
      setLocalOriginal(true)
    } else {
      setLocalOriginal(false)
      setGlobalCurrency(v)
    }
  }

  const [acctModel, setAcctModel] = usePersistentState<'historical' | 'spot'>('portfolio_acctModel', 'historical')
  
  // Search & Filter State
  const [searchSymbol, setSearchSymbol] = useState('')
  const [filterSide, setFilterSide] = useState('all')

  // Infinite Scroll & Pagination State
  const [trades, setTrades] = useState<EnrichedTrade[]>([])
  const [totalCount, setTotalCount] = useState(0)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')
  const [offset, setOffset] = useState(0)
  const [hasMore, setHasMore] = useState(true)

  const sentinelRef = useRef<HTMLDivElement>(null)
  const isResetting = useRef(false)

  const isOriginal = currency === 'Original'
  const reqCurrency = isOriginal ? globalCurrency : currency
  const reqAcctModel = isOriginal ? 'original' : acctModel

  // Reset list when currency, acctModel or filters change
  useEffect(() => {
    isResetting.current = true
    setTrades([])
    setOffset(0)
    setHasMore(true)
    setError('')
  }, [reqCurrency, reqAcctModel, filterSide])

  // Load initial page or handle offset change
  useEffect(() => {
    let active = true

    const fetchTrades = async (currentOffset: number, append = false) => {
      setIsLoading(true)
      try {
        const res = await getTransactions(reqCurrency, reqAcctModel, LIMIT, currentOffset)
        if (!active) return

        setTotalCount(res.total_count)
        
        const newTrades = res.trades || []
        
        // Filter trades locally for Search/Filter Type to ensure visual accuracy
        // since the general backend list serves all transactions.
        let filtered = newTrades
        if (filterSide !== 'all') {
          filtered = filtered.filter(t => t.side.toUpperCase() === filterSide.toUpperCase())
        }

        setTrades(prev => append ? [...prev, ...filtered] : filtered)
        setHasMore(newTrades.length === LIMIT)
      } catch (err) {
        if (active) {
          setError(err instanceof Error ? err.message : 'Failed to fetch transactions')
        }
      } finally {
        if (active) {
          setIsLoading(false)
          isResetting.current = false
        }
      }
    }

    fetchTrades(offset, offset > 0)

    return () => {
      active = false
    }
  }, [offset, reqCurrency, reqAcctModel, filterSide])

  // Intersection Observer for Infinite Scroll
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMore && !isLoading && !isResetting.current) {
          setOffset(prev => prev + LIMIT)
        }
      },
      { threshold: 0.1, rootMargin: '100px' }
    )

    observer.observe(sentinel)
    return () => {
      observer.unobserve(sentinel)
    }
  }, [hasMore, isLoading])

  // Apply symbol filter locally on loaded data
  const filteredTrades = useMemo(() => {
    if (!searchSymbol.trim()) return trades
    const term = searchSymbol.toLowerCase().trim()
    return trades.filter(t => t.symbol.toLowerCase().includes(term))
  }, [trades, searchSymbol])

  // Get color/label for various sides
  const getSideBadge = (side: string) => {
    const s = side.toUpperCase()
    if (s.includes('BUY') || s.includes('VEST')) {
      return (
        <span className="px-2.5 py-1 text-[11px] font-black rounded-lg bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 tracking-wider">
          {side}
        </span>
      )
    }
    if (s.includes('SELL')) {
      return (
        <span className="px-2.5 py-1 text-[11px] font-black rounded-lg bg-rose-500/10 text-rose-400 border border-rose-500/20 tracking-wider">
          {side}
        </span>
      )
    }
    return (
      <span className="px-2.5 py-1 text-[11px] font-black rounded-lg bg-slate-500/10 text-slate-400 border border-slate-500/20 tracking-wider">
        {side}
      </span>
    )
  }

  return (
    <PageLayout maxWidth="max-w-[1200px]">
      {/* Header section with back navigation */}
      <div className="w-full mb-10">
        <button
          onClick={() => navigate('/portfolio')}
          className="flex items-center gap-2 text-slate-500 hover:text-slate-300 text-xs font-semibold uppercase tracking-wider mb-6 group/back focus:outline-none"
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
            strokeWidth="2.5"
            stroke="currentColor"
            className="w-4 h-4 transform transition-transform group-hover/back:-translate-x-0.5"
          >
            <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
          </svg>
          <span>Back to Portfolio</span>
        </button>

        <div className="flex flex-col md:flex-row md:items-end md:justify-between gap-6">
          <div>
            <h1 className="text-3xl font-bold text-slate-100">Transaction History</h1>
            <p className="text-slate-500 text-sm mt-2">Comprehensive list of all uploads, purchases, sales, and vests</p>
          </div>
          
          {/* Controls */}
          <div className="flex flex-wrap items-center gap-3">
            <SegmentedControl label="FX Method" options={FX_METHOD_OPTIONS} value={acctModel} onChange={setAcctModel} />
            <SegmentedControl label="Currency" options={CURRENCY_OPTIONS} value={currency} onChange={setCurrency} />
          </div>
        </div>
      </div>

      {error && <ErrorAlert message={error} className="mb-8 w-full" />}

      {/* Filter and Search Bar */}
      <div className="w-full mb-6 flex flex-wrap gap-4 items-center justify-between px-8">
        <div className="flex items-center gap-3 flex-1 min-w-[240px]">
          <div className="relative flex-1">
            <input
              type="text"
              placeholder="Search symbol (e.g. AAPL)..."
              value={searchSymbol}
              onChange={(e) => setSearchSymbol(e.target.value)}
              className="w-full bg-surface border border-border-dim rounded-xl px-4 py-2.5 text-sm text-slate-200 placeholder-slate-500 focus:outline-none focus:border-indigo-500/50 transition-colors"
            />
            {searchSymbol && (
              <button
                onClick={() => setSearchSymbol('')}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-200"
              >
                ✕
              </button>
            )}
          </div>
        </div>

        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-slate-500">Filter Type:</span>
            <div className="flex gap-1.5 bg-surface p-1 rounded-xl border border-border-dim">
              {SIDE_OPTIONS.map(opt => (
                <button
                  key={opt.value}
                  onClick={() => setFilterSide(opt.value)}
                  className={`px-3 py-1.5 rounded-lg text-xs font-bold transition-all ${
                    filterSide === opt.value
                      ? 'bg-indigo-500/20 text-indigo-300'
                      : 'text-slate-400 hover:text-slate-200 hover:bg-surface-hover'
                  }`}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Transactions Table Container */}
      <div className="w-full selection:bg-indigo-500/20 overflow-x-auto">
        <div className="min-w-[960px]">
          {/* Table Header */}
          <div className="grid grid-cols-8 gap-4 px-8 py-5 text-xs font-bold text-slate-500 border-b border-border-dim/40 tracking-wider">
            <div className="col-span-1">Date</div>
            <div className="col-span-1">Side</div>
            <div className="col-span-1">Symbol</div>
            <div className="col-span-1 text-right">Quantity</div>
            <div className="col-span-1 text-right">Price</div>
            <div className="col-span-1 text-right">Value</div>
            <div className="col-span-1 text-right text-emerald-400/80">Realized Gain</div>
            <div className="col-span-1 text-right text-indigo-400/80">Unrealized Gain</div>
          </div>

          {/* Table Content */}
          <div className="divide-y divide-border-dim/30">
            {filteredTrades.length === 0 && !isLoading ? (
              <div className="text-center py-20 text-slate-500 font-bold uppercase tracking-widest text-[11px]">
                No transactions matched your criteria.
              </div>
            ) : (
              filteredTrades.map((trade, idx) => {
                const tradeValue = trade.quantity * trade.price
                const isCashOrFx = ['CASH', 'FX', 'TRANSFER_IN', 'TRANSFER_OUT'].includes(trade.side.toUpperCase())

                return (
                  <div
                    key={`${trade.id}-${idx}`}
                    className="grid grid-cols-8 gap-4 px-8 py-4.5 text-sm items-center hover:bg-surface-hover/30 transition-colors duration-150"
                  >
                    {/* Date */}
                    <div className="col-span-1 text-slate-300 font-mono tracking-tight text-sm">
                      {trade.date}
                    </div>

                    {/* Side */}
                    <div className="col-span-1 flex items-center">
                      {getSideBadge(trade.side)}
                    </div>

                    {/* Symbol */}
                    <div className="col-span-1 font-bold text-slate-100 flex items-center gap-1.5">
                      <span>{trade.symbol}</span>
                      {trade.listing_exchange && (
                        <span className="text-[10px] text-slate-500 font-normal px-1 py-0.2 rounded bg-white/5 border border-white/10 uppercase">
                          {trade.listing_exchange}
                        </span>
                      )}
                    </div>

                    {/* Quantity */}
                    <div className="col-span-1 text-right tabular-nums font-medium text-slate-200">
                      {privacy ? '———' : formatQuantity(trade.quantity)}
                    </div>

                    {/* Price */}
                    <div className="col-span-1 text-right tabular-nums text-slate-300">
                      {privacy ? (
                        '———'
                      ) : currency === 'Original' ? (
                        formatCurrency(trade.price, trade.native_currency)
                      ) : (
                        formatCurrency(trade.converted_price, currency)
                      )}
                    </div>

                    {/* Value */}
                    <div className="col-span-1 text-right tabular-nums font-semibold text-slate-100">
                      {privacy ? (
                        '———'
                      ) : currency === 'Original' ? (
                        formatCurrency(tradeValue, trade.native_currency)
                      ) : (
                        formatCurrency(trade.quantity * trade.converted_price, currency)
                      )}
                    </div>

                    {/* Realized Gain */}
                    <div className="col-span-1 text-right tabular-nums font-bold">
                      {privacy || isCashOrFx ? (
                        <span className="text-slate-500 opacity-40">—</span>
                      ) : trade.realized_gain !== 0 ? (
                        <span className={trade.realized_gain >= 0 ? 'text-emerald-400' : 'text-rose-400'}>
                          {trade.realized_gain >= 0 ? '+' : ''}
                          {formatCurrency(trade.realized_gain, currency)}
                        </span>
                      ) : (
                        <span className="text-slate-500 opacity-40">—</span>
                      )}
                    </div>

                    {/* Unrealized Gain */}
                    <div className="col-span-1 text-right tabular-nums font-bold">
                      {privacy || isCashOrFx ? (
                        <span className="text-slate-500 opacity-40">—</span>
                      ) : trade.unrealized_gain !== 0 ? (
                        <span className={trade.unrealized_gain >= 0 ? 'text-emerald-400' : 'text-rose-400'}>
                          {trade.unrealized_gain >= 0 ? '+' : ''}
                          {formatCurrency(trade.unrealized_gain, currency)}
                        </span>
                      ) : (
                        <span className="text-slate-500 opacity-40">—</span>
                      )}
                    </div>
                  </div>
                )
              })
            )}

            {/* Skeletons when fetching page */}
            {isLoading && (
              <div className="p-4 space-y-3.5">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="grid grid-cols-8 gap-4 px-4 items-center">
                    <Skeleton className="h-4 col-span-1" />
                    <Skeleton className="h-5 col-span-1 rounded-lg" />
                    <Skeleton className="h-4 col-span-1" />
                    <Skeleton className="h-4 col-span-1 text-right" />
                    <Skeleton className="h-4 col-span-1 text-right" />
                    <Skeleton className="h-4 col-span-1 text-right" />
                    <Skeleton className="h-4 col-span-1 text-right" />
                    <Skeleton className="h-4 col-span-1 text-right" />
                  </div>
                ))}
              </div>
            )}

            {/* Infinite scroll sentinel */}
            <div ref={sentinelRef} className="h-10 w-full flex items-center justify-center text-xs text-slate-500">
              {hasMore && !isLoading && 'Scroll to load more transactions...'}
              {!hasMore && filteredTrades.length > 0 && `Showing all ${totalCount} transactions.`}
            </div>
          </div>
        </div>
      </div>
    </PageLayout>
  )
}
