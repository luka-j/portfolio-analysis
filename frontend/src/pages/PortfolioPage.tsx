import { useState, useEffect, useCallback } from 'react'
import PageLayout from '../components/PageLayout'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import SymbolMappingModal from '../components/SymbolMappingModal'
import { getPortfolioValue, getPortfolioTrades, updateSymbolMapping, type PositionValue, type TradeEntry } from '../api'
import { formatCurrency, formatNumber } from '../utils/format'

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

export default function PortfolioPage() {
  const [currency, setCurrency] = useState('CZK')
  const [acctModel, setAcctModel] = useState<'historical' | 'spot'>('historical')
  const [positions, setPositions] = useState<PositionValue[]>([])
  const [totalValue, setTotalValue] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [mappingTarget, setMappingTarget] = useState<{ symbol: string; yahooSymbol?: string; exchange?: string } | null>(null)

  const loadData = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const val = await getPortfolioValue(currency, acctModel)
      const sorted = [...val.positions].sort((a, b) => (b.value || 0) - (a.value || 0))
      setPositions(sorted)
      setTotalValue(val.value || 0)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [currency, acctModel])

  useEffect(() => { loadData() }, [loadData])

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

  return (
    <PageLayout>
      {mappingTarget && (
        <SymbolMappingModal
          symbol={mappingTarget.symbol}
          currentYahooSymbol={mappingTarget.yahooSymbol}
          onConfirm={handleMapConfirm}
          onClose={() => setMappingTarget(null)}
        />
      )}
          {/* Header centered */}
          <div className="w-full flex flex-col items-center mb-16 text-center">
            <h1 className="text-3xl font-semibold text-slate-100">Portfolio Holdings</h1>
            <p className="text-slate-500 text-sm mt-4">Active positions and unrealized performance</p>
          </div>

        {/* Controls — centered */}
        <div className="flex flex-wrap justify-center gap-4 mb-16">
          <SegmentedControl label="FX Method" options={FX_METHOD_OPTIONS} value={acctModel} onChange={setAcctModel} />
          <SegmentedControl label="Currency" options={CURRENCY_OPTIONS} value={currency} onChange={setCurrency} />
        </div>

        {error && (
          <div className="w-full mb-10 px-8 py-4 rounded-2xl bg-red-500/10 text-red-400 text-xs font-black uppercase tracking-widest border border-red-500/20 text-center">
            {error}
          </div>
        )}

        {loading ? (
          <Spinner label="Hydrating state…" className="py-24" />
        ) : positions.length === 0 ? (
          <div className="text-center py-24 text-slate-600 font-black uppercase tracking-[0.2em] text-[11px]">No holdings found. Synchronize your data.</div>
        ) : (
          <div className="w-full selection:bg-indigo-500/20">
            {/* Table header */}
            <div
              className="grid gap-4 px-8 py-5 text-xs font-semibold text-slate-500 border-b border-[#2a2e42]/40"
              style={{ gridTemplateColumns: 'repeat(16, minmax(0, 1fr))' }}
            >
              <div className="col-span-2">Symbol</div>
              <div className="col-span-1 text-right">Qty</div>
              <div className="col-span-2 text-right">Price</div>
              <div className="col-span-2 text-right">Value</div>
              <div className="col-span-2 text-right">Portfolio %</div>
              <div className="col-span-2 text-right">Unrealized</div>
              <div className="col-span-2 text-right">Realized</div>
              <div className="col-span-2 text-right">Comm.</div>
              <div className="col-span-1 text-right">
                <svg className="w-3 h-3 ml-auto opacity-30" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M19 9l-7 7-7-7" /></svg>
              </div>
            </div>

            {/* Rows */}
            <div className="divide-y divide-[#2a2e42]/40">
              {positions.map(pos => {
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

                return (
                  <div key={posKey}>
                    <div
                      className={`grid gap-4 px-8 py-6 items-center cursor-pointer transition-all duration-300 ${isExpanded ? 'bg-[#1a1d2e] ring-1 ring-white/5 shadow-2xl z-10' : 'hover:bg-white/2'}`}
                      style={{ display: 'grid', gridTemplateColumns: 'repeat(16, minmax(0, 1fr))' }}
                      onClick={() => toggleExpand(pos.symbol, pos.listing_exchange)}
                    >
                      {/* Symbol cell */}
                      <div className="col-span-2 flex items-center gap-4">
                        <div className="w-10 h-10 rounded-2xl bg-linear-to-br from-indigo-500/10 to-purple-500/10 flex items-center justify-center text-xs font-bold text-indigo-300 border border-white/5 shrink-0 shadow-lg ring-1 ring-white/5">
                          {pos.symbol.slice(0, 2)}
                        </div>
                        <div className="min-w-0">
                          <div className="font-semibold flex items-center gap-2 text-slate-100 text-sm tracking-tight">
                            {pos.symbol}
                            {pos.bond_duration != null && (
                              <div className="relative group">
                                <span className="text-amber-400 text-xs font-medium bg-amber-400/10 px-1.5 py-0.5 rounded-lg border border-amber-400/20 cursor-default">
                                  {pos.bond_duration.toFixed(1)}y
                                </span>
                                <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2.5 w-52 px-3 py-2.5 bg-[#12151f] border border-[#2a2e42]/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity z-50 shadow-2xl">
                                  Bond ETF effective duration — the weighted-average time (in years) until cash flows are received. Higher duration means greater sensitivity to interest rate changes.
                                </div>
                              </div>
                            )}
                            <button
                              onClick={(e) => handleMapSymbol(e, pos.symbol, pos.yahoo_symbol, pos.listing_exchange)}
                              className="text-slate-700 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
                            >
                              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                              </svg>
                            </button>
                          </div>
                          <div className="text-xs font-medium text-slate-500 flex items-center gap-1.5 mt-1 opacity-80">
                            <span>{pos.native_currency}</span>
                            {pos.listing_exchange && (
                              <>
                                <span className="w-1 h-1 rounded-full bg-slate-800" />
                                <span>{pos.listing_exchange}</span>
                              </>
                            )}
                          </div>
                        </div>
                      </div>

                      <div className="col-span-1 text-right text-sm text-slate-300 font-medium tabular-nums">{formatNumber(pos.quantity, 0)}</div>
                      <div className="col-span-2 text-right">
                        <div className="text-sm font-medium text-slate-300 tabular-nums">{formatNumber(pos.price || 0)} {currency === 'Original' ? pos.native_currency : currency}</div>
                        {(pos.cost_basis || 0) > 0 && (
                          <div className="text-xs text-slate-500 mt-0.5">@ {formatNumber(pos.cost_basis)}</div>
                        )}
                      </div>
                      <div className="col-span-2 text-right text-sm font-semibold text-slate-100 tabular-nums">
                        {formatCurrency(value, currency, pos.native_currency)}
                      </div>
                      <div className="col-span-2 text-right">
                        <div className="inline-flex items-center gap-3">
                          <div className="w-20 h-1 bg-white/5 rounded-full overflow-hidden">
                            <div className="h-full bg-indigo-500 rounded-full shadow-[0_0_12px_rgba(99,102,241,0.4)]" style={{ width: `${Math.min(pct, 100)}%` }} />
                          </div>
                          <span className="text-xs font-medium text-slate-400 tabular-nums w-10 text-right">{pct.toFixed(1)}%</span>
                        </div>
                      </div>
                      <div className="col-span-2 text-right">
                        {unrealizedGL !== null ? (
                          <div className="flex flex-col items-end">
                            <span className={`text-sm font-medium tabular-nums ${unrealizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                              {unrealizedGL >= 0 ? '+' : ''}{formatNumber(unrealizedGL)}
                            </span>
                            <div className={`text-xs mt-0.5 ${unrealizedPct! >= 0 ? 'text-emerald-400/50' : 'text-red-400/50'}`}>
                              {unrealizedPct! >= 0 ? '+' : ''}{unrealizedPct!.toFixed(2)}%
                            </div>
                          </div>
                        ) : <span className="text-slate-600 font-medium">unavailable</span>}
                      </div>
                      <div className="col-span-2 text-right text-sm">
                        {realizedGLNative !== 0 ? (
                          <span className={`font-medium tabular-nums ${realizedGLNative >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                            {realizedGLNative >= 0 ? '+' : ''}{formatNumber(realizedGLNative)}
                          </span>
                        ) : <span className="text-slate-600 opacity-40">—</span>}
                      </div>
                      <div className="col-span-2 text-right text-sm">
                        {commission !== 0 ? (
                          <span className="text-amber-500/80 font-medium tabular-nums">{formatNumber(commission)}</span>
                        ) : <span className="text-slate-600 opacity-40">—</span>}
                      </div>
                      <div className="col-span-1 text-right">
                        <span className={`text-slate-500 text-xs transition-transform duration-300 inline-block ${isExpanded ? 'rotate-180 text-indigo-400' : ''}`}>▼</span>
                      </div>
                    </div>

                    {isExpanded && (
                      <TradeDetail symbol={pos.symbol} exchange={pos.listing_exchange} displayCurrency={currency} />
                    )}
                  </div>
                )
              })}
            </div>

            {/* Total row — gradient bg, strong bold font */}
            <div
              className="grid gap-4 px-8 py-8 mt-12 border-t-2 border-[#2a2e42]/60 bg-linear-to-r from-indigo-500/4 to-purple-500/4 items-center rounded-3xl ring-1 ring-white/5 shadow-2xl"
              style={{ gridTemplateColumns: 'repeat(16, minmax(0, 1fr))' }}
            >
              <div className="col-span-5 flex items-center">
                <span className="text-xs font-black text-slate-100 tracking-[0.25em] uppercase">Aggregated Total</span>
              </div>
              <div className="col-span-2 text-right">
                <div className="text-xl font-black text-white tabular-nums tracking-tight">
                  {currency === 'Original' ? 'MIXED' : formatCurrency(totalValue, currency)}
                </div>
              </div>
              <div className="col-span-2 text-right">
                <span className="text-xs font-black text-slate-600 tracking-widest tabular-nums">100.0%</span>
              </div>
              <div className="col-span-2 text-right">
                {currency === 'Original' ? (
                  <span className="text-[10px] text-slate-700 uppercase font-black tracking-widest">N/A</span>
                ) : totals.hasUnrealized ? (
                  <span className={`text-sm font-black tabular-nums ${totals.unrealizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                    {totals.unrealizedGL >= 0 ? '+' : ''}{formatNumber(totals.unrealizedGL)}
                  </span>
                ) : <span className="text-slate-800 opacity-40">—</span>}
              </div>
              <div className="col-span-2 text-right">
                {totals.realizedGL !== 0 ? (
                  <span className={`text-sm font-black tabular-nums ${totals.realizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                    {totals.realizedGL >= 0 ? '+' : ''}{formatNumber(totals.realizedGL)}
                  </span>
                ) : <span className="text-slate-800 opacity-40">—</span>}
              </div>
              <div className="col-span-2 text-right">
                {totals.commission !== 0 ? (
                  <span className="text-sm font-black text-amber-500/80 tabular-nums">
                    {formatNumber(totals.commission)}
                  </span>
                ) : <span className="text-slate-800 opacity-40">—</span>}
              </div>
              <div className="col-span-1" />
            </div>
          </div>
        )}
    </PageLayout>
  )
}

function TradeDetail({ symbol, exchange, displayCurrency }: { symbol: string; exchange?: string; displayCurrency: string }) {
  const [trades, setTrades] = useState<TradeEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    getPortfolioTrades(symbol, displayCurrency, exchange || '')
      .then(res => { if (!cancelled) setTrades(res.trades || []) })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load trades') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [symbol, displayCurrency])

  const hasTaxCostBasis = trades.some(t => t.tax_cost_basis !== undefined && t.tax_cost_basis !== null)
  const colsClass = hasTaxCostBasis ? 'grid-cols-7' : 'grid-cols-6'

  return (
    <div className="px-10 py-8 bg-[#0f1117]">
      <div className="flex items-center gap-3 mb-5 px-5">
        <div className="w-1.5 h-1.5 rounded-full bg-indigo-500 animate-pulse" />
        <p className="text-[10px] font-black text-slate-500 uppercase tracking-[0.25em]">Transaction History — {symbol}</p>
      </div>
      <div className="bg-[#1a1d2e]/40 border border-white/5 rounded-3xl overflow-hidden shadow-2xl backdrop-blur-3xl ring-1 ring-white/5">
        <div className={`grid ${colsClass} gap-4 px-8 py-4 text-[9px] font-black text-slate-600 uppercase tracking-widest border-b border-white/5 bg-white/2`}>
          <div>Execution Date</div>
          <div>Mechanism</div>
          <div className="text-right">Quantity</div>
          <div className="text-right">Native Price</div>
          <div className="text-right">Converted</div>
          {hasTaxCostBasis && <div className="text-right">Tax Basis</div>}
          <div className="text-right">Charge</div>
        </div>

        {loading ? (
          <div className="px-5 py-12 text-center text-slate-700 text-[10px] font-black uppercase tracking-[0.25em] animate-pulse">Retrieving ledger…</div>
        ) : error ? (
          <div className="px-5 py-8 text-center text-red-400 text-[10px] font-black uppercase tracking-widest">{error}</div>
        ) : trades.length === 0 ? (
          <div className="px-5 py-12 text-center text-slate-800 text-[10px] font-black uppercase tracking-widest">No transaction records</div>
        ) : (
          <div className="divide-y divide-white/5">
            {trades.map((trade, i) => (
              <div key={i} className={`grid ${colsClass} gap-4 px-8 py-5 text-xs hover:bg-white/3 transition-colors items-center`}>
                <div className="text-slate-400 font-bold tabular-nums">{trade.date}</div>
                <div>
                  <span className={`px-4 py-1.5 rounded-2xl text-[9px] font-black uppercase tracking-[0.15em] border ${
                    trade.side === 'BUY' || trade.side === 'TRANSFER_IN' || trade.side === 'ESPP_VEST' || trade.side === 'RSU_VEST'
                      ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
                      : trade.side === 'SELL' || trade.side === 'TRANSFER_OUT'
                      ? 'bg-red-500/10 text-red-400 border-red-500/20'
                      : 'bg-slate-500/10 text-slate-500 border-slate-500/20'
                  }`}>
                    {trade.side}
                  </span>
                </div>
                <div className="text-right text-slate-200 font-black tabular-nums">{formatNumber(trade.quantity, 0)}</div>
                <div className="text-right text-slate-500 font-bold tabular-nums text-[11px]">{formatNumber(trade.price)}</div>
                <div className="text-right text-slate-200 font-black tabular-nums">{formatNumber(trade.converted_price)}</div>
                {hasTaxCostBasis && (
                  <div className="text-right text-slate-600 font-bold text-[11px] tabular-nums">
                    {trade.tax_cost_basis !== undefined && trade.tax_cost_basis !== null ? formatNumber(trade.tax_cost_basis) : '—'}
                  </div>
                )}
                <div className="text-right text-slate-600 font-bold tabular-nums text-[11px]">{formatNumber(Math.abs(trade.commission))}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
