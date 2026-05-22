import { useState, useRef, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { getTransactions, type EnrichedTrade } from '../api'
import { formatCurrency, formatQuantity } from '../utils/format'
import { usePrivacy } from '../utils/PrivacyContext'
import { Skeleton } from './Skeleton'

interface RecentTransactionsPopoverProps {
  currency: string
  acctModel: string
}

export default function RecentTransactionsPopover({ currency, acctModel }: RecentTransactionsPopoverProps) {
  const [isOpen, setIsOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()
  const { privacy } = usePrivacy()

  // Close when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (popoverRef.current && !popoverRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside)
    }
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [isOpen])

  const { data, isLoading, error } = useQuery({
    queryKey: ['recentTransactions', currency, acctModel, isOpen],
    queryFn: () => getTransactions(currency, acctModel, 5, 0),
    enabled: isOpen, // Only fetch when open
  })

  // Format "time ago" relative to the date of the trade
  const formatTimeAgo = (dateStr: string) => {
    try {
      const date = new Date(dateStr)
      if (isNaN(date.getTime())) return dateStr

      const today = new Date()
      // Zero-out times to compare calendar days
      today.setHours(0, 0, 0, 0)
      date.setHours(0, 0, 0, 0)

      const diffTime = today.getTime() - date.getTime()
      const diffDays = Math.round(diffTime / (1000 * 60 * 60 * 24))

      if (diffDays === 0) return 'Today'
      if (diffDays === 1) return 'Yesterday'
      if (diffDays < 7) return `${diffDays} days ago`
      if (diffDays < 30) {
        const weeks = Math.floor(diffDays / 7)
        return `${weeks} week${weeks > 1 ? 's' : ''} ago`
      }
      const months = Math.floor(diffDays / 30)
      return `${months} month${months > 1 ? 's' : ''} ago`
    } catch {
      return dateStr
    }
  }

  // Get color/label for various sides
  const getSideBadge = (side: string) => {
    const s = side.toUpperCase()
    if (s.includes('BUY') || s.includes('VEST')) {
      return (
        <span className="px-2 py-0.5 text-[10px] font-bold rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
          {side}
        </span>
      )
    }
    if (s.includes('SELL')) {
      return (
        <span className="px-2 py-0.5 text-[10px] font-bold rounded bg-rose-500/10 text-rose-400 border border-rose-500/20">
          {side}
        </span>
      )
    }
    return (
      <span className="px-2 py-0.5 text-[10px] font-bold rounded bg-slate-500/10 text-slate-400 border border-slate-500/20">
        {side}
      </span>
    )
  }

  return (
    <div className="relative flex flex-col items-center gap-2" ref={popoverRef}>
      {/* Top Label */}
      <span className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em] select-none">
        Activity
      </span>

      {/* Button Wrapper with SegmentedControl style */}
      <div className="flex items-center bg-surface rounded-2xl p-1.5 border border-border-dim/50 shadow-xl shadow-black/20">
        <button
          onClick={() => setIsOpen(!isOpen)}
          className={`flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium transition-all duration-300 ${
            isOpen
              ? 'glass active text-indigo-300'
              : 'text-slate-500 hover:text-slate-300'
          }`}
          title="Recent Activity"
          aria-haspopup="true"
          aria-expanded={isOpen}
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
            strokeWidth="2.2"
            stroke="currentColor"
            className="w-4 h-4 transition-transform duration-200"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"
            />
          </svg>
          <span>History</span>
        </button>
      </div>

      {/* Popover Card */}
      {isOpen && (
        <div className="absolute right-0 top-full mt-2 w-88 md:w-96 rounded-2xl bg-panel border border-border-dim/60 shadow-2xl z-50 overflow-hidden animate-in fade-in slide-in-from-top-2 duration-200">
          {/* Header */}
          <div className="px-4 py-3 border-b border-border-dim/40 flex items-center justify-between bg-surface/50">
            <span className="text-xs font-bold text-slate-400 uppercase tracking-wider">Recent Activity</span>
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 border border-white/10 text-slate-500 tabular-nums">
              {acctModel === 'spot' ? 'Spot FX' : 'Hist. FX'}
            </span>
          </div>

          {/* Content */}
          <div className="p-2 max-h-96 overflow-y-auto divide-y divide-border-dim/30">
            {isLoading ? (
              <div className="p-3 space-y-3">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="flex items-center justify-between gap-3">
                    <div className="space-y-1.5 flex-1">
                      <Skeleton className="h-4 w-1/3" />
                      <Skeleton className="h-3 w-1/2" />
                    </div>
                    <Skeleton className="h-5 w-16" />
                  </div>
                ))}
              </div>
            ) : error ? (
              <div className="p-6 text-center text-xs text-rose-400">
                Failed to load recent activity.
              </div>
            ) : !data || data.trades.length === 0 ? (
              <div className="p-8 text-center text-xs text-slate-500">
                No recent transactions found.
              </div>
            ) : (
              data.trades.map((trade: EnrichedTrade) => {
                const tradeValue = trade.quantity * trade.price
                const isCashOrFx = ['CASH', 'FX', 'TRANSFER_IN', 'TRANSFER_OUT'].includes(trade.side.toUpperCase())

                return (
                  <div
                    key={trade.id}
                    className="p-3 hover:bg-surface/40 rounded-xl transition-all duration-200 flex items-start justify-between gap-3 group"
                  >
                    <div className="min-w-0">
                      {/* Top: Side Badge & Symbol */}
                      <div className="flex items-center gap-2 mb-1 flex-wrap">
                        {getSideBadge(trade.side)}
                        <span className="text-sm font-bold text-white group-hover:text-indigo-300 transition-colors">
                          {trade.symbol}
                        </span>
                      </div>

                      {/* Details */}
                      <div className="text-[11px] text-slate-400 space-y-0.5">
                        <div>
                          {privacy ? (
                            <span>*** shares @ ***</span>
                          ) : (
                            <span>
                              {formatQuantity(trade.quantity)} shares @{' '}
                              {currency === 'Original'
                                ? formatCurrency(trade.price, trade.native_currency)
                                : formatCurrency(trade.converted_price, currency)}
                            </span>
                          )}
                        </div>
                        <div className="text-slate-500 flex items-center gap-1.5">
                          <span>{formatTimeAgo(trade.date)}</span>
                          <span>•</span>
                          <span>{trade.date}</span>
                        </div>
                      </div>
                    </div>

                    {/* Value on the Right */}
                    <div className="text-right shrink-0">
                      <div className="text-xs font-semibold text-slate-200 tabular-nums">
                        {privacy ? (
                          '———'
                        ) : currency === 'Original' ? (
                          formatCurrency(tradeValue, trade.native_currency)
                        ) : (
                          formatCurrency(trade.quantity * trade.converted_price, currency)
                        )}
                      </div>

                      {/* Realized/Unrealized Gain display */}
                      {!privacy && !isCashOrFx && (
                        <div className="text-[10px] mt-0.5 tabular-nums">
                          {trade.realized_gain !== 0 && (
                            <span
                              className={trade.realized_gain >= 0 ? 'text-emerald-400' : 'text-red-400'}
                              title="Realized Gain (FIFO)"
                            >
                              {trade.realized_gain >= 0 ? '+' : ''}
                              {formatCurrency(trade.realized_gain, currency)} realized
                            </span>
                          )}
                          {trade.unrealized_gain !== 0 && (
                            <span
                              className={trade.unrealized_gain >= 0 ? 'text-emerald-400' : 'text-red-400'}
                              title="Unrealized Gain on remaining lot"
                            >
                              {trade.unrealized_gain >= 0 ? '+' : ''}
                              {formatCurrency(trade.unrealized_gain, currency)} unrealized
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                )
              })
            )}
          </div>

          {/* Footer Navigation */}
          <div className="px-3 py-2.5 border-t border-border-dim/40 bg-surface/50 flex justify-center">
            <button
              onClick={() => {
                setIsOpen(false)
                navigate('/transactions')
              }}
              className="text-xs font-semibold text-indigo-400 hover:text-indigo-300 flex items-center gap-1 group/btn py-1 px-3 rounded-lg hover:bg-indigo-500/10 transition-all"
            >
              <span>View all transactions</span>
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth="2.5"
                stroke="currentColor"
                className="w-3.5 h-3.5 transform transition-transform group-hover/btn:translate-x-0.5"
              >
                <path strokeLinecap="round" strokeLinejoin="round" d="M13.5 4.5 21 12m0 0-7.5 7.5M21 12H3" />
              </svg>
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
