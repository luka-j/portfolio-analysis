import { useState, useEffect, useCallback } from 'react'
import HoverTooltip from './HoverTooltip'
import { SecurityPriceChart } from './SecurityPriceChart'
import { getPortfolioTrades, deleteTransaction, type TradeEntry } from '../api'
import { formatNumber } from '../utils/format'

interface Props {
  symbol: string
  exchange?: string
  isin?: string
  name?: string
  displayCurrency: string
  acctModel: 'historical' | 'spot'
  privacy: boolean
  onTradeDeleted: () => void
}

function entryMethodBadge(method: string | undefined) {
  if (!method) return <span className="text-slate-500">—</span>
  const styles: Record<string, string> = {
    flexquery:       'bg-slate-500/10 text-slate-500 border-slate-500/20',
    etrade_benefits: 'bg-slate-500/10 text-slate-500 border-slate-500/20',
    etrade_sales:    'bg-slate-500/10 text-slate-500 border-slate-500/20',
    manual:          'bg-indigo-500/10 text-indigo-400 border-indigo-500/20',
  }
  const labels: Record<string, string> = {
    flexquery:       'FlexQuery',
    etrade_benefits: 'E*Trade',
    etrade_sales:    'E*Trade',
    manual:          'Manual',
  }
  const cls = styles[method] ?? 'bg-slate-500/10 text-slate-500 border-slate-500/20'
  return (
    <span className={`px-2 py-1 rounded-xl text-[9px] font-black uppercase tracking-[0.1em] border ${cls}`}>
      {labels[method] ?? method}
    </span>
  )
}

/** Expanded trade history panel, rendered inline below a portfolio row. */
export function TradeDetail({ symbol, exchange, isin, name, displayCurrency, acctModel, privacy, onTradeDeleted }: Props) {
  const [trades, setTrades] = useState<TradeEntry[]>([])
  const [resolvedDisplayCurrency, setResolvedDisplayCurrency] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)

  const fetchTrades = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const res = await getPortfolioTrades(symbol, displayCurrency, exchange || '')
      setTrades(res.trades || [])
      setResolvedDisplayCurrency(res.display_currency || displayCurrency)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load trades')
    } finally {
      setLoading(false)
    }
  }, [symbol, displayCurrency, exchange])

  useEffect(() => {
    let cancelled = false
    fetchTrades().catch(() => { if (!cancelled) setError('Failed to load trades') })
    return () => { cancelled = true }
  }, [fetchTrades])

  async function handleDelete(id: string) {
    setDeleting(true)
    try {
      await deleteTransaction(id)
      setPendingDeleteId(null)
      await fetchTrades()
      onTradeDeleted()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete')
    } finally {
      setDeleting(false)
    }
  }

  const hasTaxCostBasis = trades.some(t => t.tax_cost_basis !== undefined && t.tax_cost_basis !== null)
  const isOriginal = displayCurrency === 'Original'
  const colsClass = isOriginal
    ? hasTaxCostBasis ? 'grid-cols-8' : 'grid-cols-7'
    : hasTaxCostBasis ? 'grid-cols-9' : 'grid-cols-8'
  const nativeCurrency = trades[0]?.native_currency?.toLowerCase() ?? ''
  const dispCurrency = isOriginal ? '' : resolvedDisplayCurrency.toLowerCase()

  return (
    <div className="px-6 py-5 bg-bg">
      <div className="flex items-baseline gap-3 mb-4 px-3">
        {name && <span className="text-sm font-bold text-slate-300">{name}</span>}
        <span className="text-[10px] font-black text-slate-500 uppercase tracking-[0.2em]">{symbol}</span>
        {isin && (
          <span className="self-center text-[9px] font-bold text-slate-500 tracking-widest uppercase bg-white/3 border border-white/5 px-2.5 py-1 rounded-xl">{isin}</span>
        )}
      </div>
      <div className="mb-4">
        <SecurityPriceChart symbol={symbol} trades={trades} privacy={privacy} displayCurrency={displayCurrency} acctModel={acctModel} />
      </div>
      <p className="text-[10px] font-black text-slate-500 uppercase tracking-[0.25em] mb-2 px-3">Transaction History</p>
      <div className="bg-surface/40 border border-white/5 rounded-3xl overflow-hidden shadow-2xl backdrop-blur-3xl ring-1 ring-white/5">
        <div className={`flex items-center border-b border-white/5 bg-white/2`}>
          <div className={`grid ${colsClass} gap-4 flex-1 px-5 py-2.5 text-[9px] font-black text-slate-500 uppercase tracking-widest`}>
            <div>Date</div>
            <div>Action</div>
            <div>Source</div>
            <div className="text-right">Quantity</div>
            <div className="text-right">Price{nativeCurrency && ` (${nativeCurrency})`}</div>
            {!isOriginal && <div className="text-right">Conv. Price{dispCurrency && ` (${dispCurrency})`}</div>}
            <div className="text-right">Total{dispCurrency ? ` (${dispCurrency})` : nativeCurrency ? ` (${nativeCurrency})` : ''}</div>
            {hasTaxCostBasis && <div className="text-right">Tax Basis</div>}
            <div className="text-right">Commission</div>
          </div>
          <div className="w-7 shrink-0" />
        </div>

        {loading ? (
          <div className="px-5 py-12 text-center text-slate-500 text-xs animate-pulse">Loading trades…</div>
        ) : error ? (
          <div className="px-5 py-8 text-center text-red-400 text-xs">{error}</div>
        ) : trades.length === 0 ? (
          <div className="px-5 py-12 text-center text-slate-500 text-xs">No transaction records</div>
        ) : (
          <div className="divide-y divide-white/5">
            {trades.map((trade, i) => (
              <div key={trade.id || i} className={`flex items-center hover:bg-white/3 transition-colors`}>
                <div className={`grid ${colsClass} gap-4 flex-1 px-5 py-2 text-xs items-center`}>
                  <div className="text-slate-400 font-bold tabular-nums">{trade.date}</div>
                  <div>
                    <span className={`px-2.5 py-0.5 rounded-2xl text-[9px] font-black uppercase tracking-[0.15em] border ${
                      trade.side === 'BUY' || trade.side === 'TRANSFER_IN' || trade.side === 'ESPP_VEST' || trade.side === 'RSU_VEST'
                        ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
                        : trade.side === 'SELL' || trade.side === 'TRANSFER_OUT'
                        ? 'bg-red-500/10 text-red-400 border-red-500/20'
                        : 'bg-slate-500/10 text-slate-500 border-slate-500/20'
                    }`}>
                      {trade.side}
                    </span>
                  </div>
                  <div>{entryMethodBadge(trade.entry_method)}</div>
                  <div className="text-right text-slate-200 font-black tabular-nums">{privacy ? '—' : formatNumber(trade.quantity, 0)}</div>
                  <div className="text-right text-slate-500 font-bold tabular-nums text-[11px]">{privacy ? '—' : formatNumber(trade.price)}</div>
                  {!isOriginal && <div className="text-right text-slate-200 font-black tabular-nums">{privacy ? '—' : formatNumber(trade.converted_price)}</div>}
                  <div className="text-right text-slate-300 font-black tabular-nums">{privacy ? '—' : formatNumber(Math.abs(trade.quantity * (isOriginal ? trade.price : trade.converted_price)))}</div>
                  {hasTaxCostBasis && (
                    <div className="text-right text-slate-500 font-bold text-[11px] tabular-nums">
                      {privacy ? '—' : trade.tax_cost_basis !== undefined && trade.tax_cost_basis !== null ? formatNumber(trade.tax_cost_basis) : '—'}
                    </div>
                  )}
                  <div className="text-right text-slate-500 font-bold tabular-nums text-[11px]">{privacy ? '—' : formatNumber(Math.abs(trade.commission))}</div>
                </div>
                <div className="w-7 shrink-0 flex justify-center">
                  {trade.id ? (
                    <div className="relative group/del">
                      <button
                        onClick={() => setPendingDeleteId(trade.id)}
                        className="text-slate-500 hover:text-red-400 transition-colors p-0.5 rounded"
                      >
                        <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                          <path strokeLinecap="round" strokeLinejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                        </svg>
                      </button>
                      <HoverTooltip align="right" className="whitespace-nowrap">Delete transaction</HoverTooltip>
                    </div>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Delete confirmation dialog */}
      {pendingDeleteId && (
        <div
          className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50"
          onClick={() => setPendingDeleteId(null)}
        >
          <div
            className="bg-surface/95 backdrop-blur-xl border border-border-dim rounded-2xl p-6 w-full max-w-sm shadow-2xl"
            onClick={e => e.stopPropagation()}
          >
            <h3 className="text-slate-100 font-semibold mb-2">Delete transaction?</h3>
            <p className="text-slate-400 text-sm mb-5">This cannot be undone.</p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setPendingDeleteId(null)}
                className="px-4 py-2 rounded-xl text-sm text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-all"
              >
                Cancel
              </button>
              <button
                onClick={() => handleDelete(pendingDeleteId)}
                disabled={deleting}
                className="px-4 py-2 rounded-xl text-sm font-semibold bg-red-500/20 text-red-400 hover:bg-red-500/30 border border-red-500/30 transition-all disabled:opacity-50"
              >
                {deleting ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
