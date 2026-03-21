import { useState, useEffect, useCallback } from 'react'
import NavBar from '../components/NavBar'
import { getPortfolioValue, getPortfolioTrades, updateSymbolMapping, type PositionValue, type TradeEntry } from '../api'

const CURRENCIES = ['CZK', 'USD', 'EUR', 'Original']

function formatCurrency(value: number, currency: string, nativeCurrency?: string): string {
  const cur = currency === 'Original' ? (nativeCurrency || 'USD') : currency
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency', currency: cur, minimumFractionDigits: 2, maximumFractionDigits: 2,
    }).format(value)
  } catch (e) {
    return value.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + ' ' + cur
  }
}

function formatNumber(value: number, decimals = 2): string {
  return value.toLocaleString('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
}

export default function PortfolioPage() {
  const [currency, setCurrency] = useState('CZK')
  const [acctModel, setAcctModel] = useState<'historical' | 'spot'>('historical')
  const [positions, setPositions] = useState<PositionValue[]>([])
  const [totalValue, setTotalValue] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const val = await getPortfolioValue(CURRENCIES.join(','), acctModel)
      const sorted = val.positions.sort((a, b) => (b.values[currency] || 0) - (a.values[currency] || 0))
      setPositions(sorted)
      setTotalValue(val.values[currency] || 0)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [currency, acctModel])

  useEffect(() => { loadData() }, [loadData])

  // Derive portfolio-level totals from the positions array.
  const totals = positions.reduce(
    (acc, pos) => {
      const price = pos.prices[currency] || 0
      const costBasis = pos.cost_bases[currency] || 0
      if (costBasis > 0) {
        acc.unrealizedGL += (price - costBasis) * pos.quantity
        acc.hasUnrealized = true
      }
      acc.realizedGL += pos.realized_gls?.[currency] || 0
      acc.commission += pos.commissions?.[currency] || 0
      return acc
    },
    { unrealizedGL: 0, realizedGL: 0, commission: 0, hasUnrealized: false }
  )

  const handleMapSymbol = async (e: React.MouseEvent, symbol: string, currentYahooSymbol?: string, exchange?: string) => {
    e.stopPropagation()
    const mapped = window.prompt(`Map ${symbol} to Yahoo Finance ticker:`, currentYahooSymbol || symbol)
    if (mapped !== null) {
      try {
        await updateSymbolMapping(symbol, mapped, exchange)
        // Refetch to see updated mappings
        const data = await getPortfolioValue(CURRENCIES.join(','), acctModel)
        const sorted = data.positions.sort((a, b) => (b.values[currency] || 0) - (a.values[currency] || 0))
        setPositions(sorted)
        setTotalValue(data.values[currency] || 0)
      } catch (err) {
        window.alert('Failed to map symbol')
      }
    }
  }

  const toggleExpand = (symbol: string, exchange?: string) => {
    const key = exchange ? `${symbol}@${exchange}` : symbol
    setExpanded(prev => prev === key ? null : key)
  }

  return (
    <div className="min-h-screen bg-[#0f1117] flex flex-col">
      <NavBar />
      <main className="flex-1 flex items-center justify-center py-8">
        <div className="max-w-7xl w-full px-6">
        <div className="flex items-center justify-between mb-8">
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Portfolio Holdings</h1>
            <p className="text-slate-400 text-sm mt-1">Current positions and performance</p>
          </div>
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-xl p-1 border border-[#2a2e42]">
              {(['historical', 'spot'] as const).map(m => (
                <button key={m} onClick={() => setAcctModel(m)}
                  className={`px-4 py-2 rounded-xl text-xs font-semibold transition-all ${
                    acctModel === m ? 'bg-white/10 text-slate-200' : 'text-slate-500 hover:text-slate-300'
                  }`}
                >{m.charAt(0).toUpperCase() + m.slice(1)}</button>
              ))}
            </div>
            <div className="flex items-center gap-1 bg-[#1a1d2e] rounded-xl p-1 border border-[#2a2e42]">
              {CURRENCIES.map(cur => (
                <button key={cur} onClick={() => setCurrency(cur)}
                  className={`px-4 py-2 rounded-xl text-xs font-semibold transition-all ${
                    currency === cur ? 'bg-indigo-500/20 text-indigo-400' : 'text-slate-400 hover:text-slate-200'
                  }`}
                >
                  {cur}
                </button>
              ))}
            </div>
          </div>
        </div>

        {error && (
          <div className="mb-4 px-4 py-2 rounded-lg bg-red-500/10 text-red-400 text-sm border border-red-500/20">
            {error}
          </div>
        )}

        {loading ? (
          <div className="text-center py-20 text-slate-500">Loading portfolio...</div>
        ) : positions.length === 0 ? (
          <div className="text-center py-20 text-slate-500">No holdings found. Upload a FlexQuery file first.</div>
        ) : (
          <div className="bg-[#1a1d2e] rounded-2xl border border-[#2a2e42] overflow-hidden">
            {/* Table header */}
            <div className="grid gap-2 px-6 py-4 text-xs font-medium text-slate-400 uppercase tracking-wider border-b border-[#2a2e42] bg-[#161829]" style={{ gridTemplateColumns: 'repeat(16, minmax(0, 1fr))' }}>
              <div className="col-span-2">Symbol</div>
              <div className="col-span-1 text-right">Qty</div>
              <div className="col-span-2 text-right">Price</div>
              <div className="col-span-2 text-right">Current Value</div>
              <div className="col-span-2 text-right">% of Portfolio</div>
              <div className="col-span-2 text-right">Unrealized G/L</div>
              <div className="col-span-2 text-right">Realized G/L</div>
              <div className="col-span-2 text-right">Commission</div>
              <div className="col-span-1"></div>
            </div>

            {/* Rows */}
            {positions.map(pos => {
              const value = pos.values[currency] || 0
              const price = pos.prices[currency] || 0
              const costBasis = pos.cost_bases[currency] || 0
              const realizedGLNative = pos.realized_gls[currency] || 0
              const commission = pos.commissions?.[currency] || 0
              const pct = totalValue > 0 ? (value / totalValue) * 100 : 0
              const unrealizedGL = costBasis > 0
                ? (price - costBasis) * pos.quantity
                : null
              const unrealizedPct = costBasis > 0
                ? ((price - costBasis) / costBasis) * 100
                : null
              const posKey = pos.listing_exchange ? `${pos.symbol}@${pos.listing_exchange}` : pos.symbol
              const isExpanded = expanded === posKey

              return (
                <div key={posKey}>
                  <div
                    className={`gap-2 px-6 py-4 items-center cursor-pointer transition-colors hover:bg-[#232740] ${isExpanded ? 'bg-[#232740]' : ''}`}
                    style={{ display: 'grid', gridTemplateColumns: 'repeat(16, minmax(0, 1fr))' }}
                    onClick={() => toggleExpand(pos.symbol, pos.listing_exchange)}
                  >
                    <div className="col-span-2 flex items-center gap-3">
                      <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-indigo-500/20 to-purple-500/20 flex items-center justify-center text-xs font-bold text-indigo-400 border border-indigo-500/20">
                        {pos.symbol.slice(0, 2)}
                      </div>
                      <div>
                        <div className="font-medium flex items-center gap-2 text-slate-200 text-sm">
                          {pos.symbol}
                          <button
                            onClick={(e) => handleMapSymbol(e, pos.symbol, pos.yahoo_symbol, pos.listing_exchange)}
                            className="text-slate-500 hover:text-indigo-400 transition-colors p-0.5 rounded-lg hover:bg-indigo-500/10"
                            title="Map Yahoo Finance Symbol"
                          >
                            <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                            </svg>
                          </button>
                        </div>
                        <div className="text-xs text-slate-500 flex flex-col pt-0.5">
                          <span>{pos.native_currency}</span>
                          {pos.listing_exchange && (
                            <span className="text-slate-600 font-mono tracking-wide">{pos.listing_exchange}</span>
                          )}
                          {pos.yahoo_symbol && pos.yahoo_symbol !== pos.symbol && (
                            <span className="text-indigo-400/80">mapped: {pos.yahoo_symbol}</span>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className="col-span-1 text-right text-sm text-slate-300">{formatNumber(pos.quantity, 0)}</div>
                    <div className="col-span-2 text-right text-sm text-slate-300">
                      <div>{formatNumber(pos.prices[currency] || 0)} {currency === 'Original' ? pos.native_currency : currency}</div>
                      {(pos.cost_bases[currency] || 0) > 0 && (
                        <div className="text-xs text-slate-500">Cost: {formatNumber(pos.cost_bases[currency])} {currency === 'Original' ? pos.native_currency : ''}</div>
                      )}
                    </div>
                    <div className="col-span-2 text-right text-sm font-medium text-slate-200">
                      {formatCurrency(value, currency, pos.native_currency)}
                    </div>
                    <div className="col-span-2 text-right">
                      <div className="inline-flex items-center gap-2">
                        <div className="w-20 h-1.5 bg-[#2a2e42] rounded-full overflow-hidden">
                          <div className="h-full bg-indigo-500 rounded-full" style={{ width: `${Math.min(pct, 100)}%` }} />
                        </div>
                        <span className="text-sm text-slate-300">{pct.toFixed(1)}%</span>
                      </div>
                    </div>
                    <div className="col-span-2 text-right text-sm">
                      {unrealizedGL !== null ? (
                        <div>
                          <span className={unrealizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'}>
                            {unrealizedGL >= 0 ? '+' : ''}{formatNumber(unrealizedGL)} {currency === 'Original' ? pos.native_currency : currency}
                          </span>
                          <div className={`text-xs ${unrealizedPct! >= 0 ? 'text-emerald-400/70' : 'text-red-400/70'}`}>
                            {unrealizedPct! >= 0 ? '+' : ''}{unrealizedPct!.toFixed(2)}%
                          </div>
                        </div>
                      ) : (
                        <span className="text-slate-500">—</span>
                      )}
                    </div>
                    <div className="col-span-2 text-right text-sm">
                      {realizedGLNative !== 0 ? (
                        <span className={realizedGLNative >= 0 ? 'text-emerald-400' : 'text-red-400'}>
                          {realizedGLNative >= 0 ? '+' : ''}{formatNumber(realizedGLNative)} {currency === 'Original' ? pos.native_currency : currency}
                        </span>
                      ) : (
                        <span className="text-slate-500">—</span>
                      )}
                    </div>
                    <div className="col-span-2 text-right text-sm">
                      {commission !== 0 ? (
                        <span className="text-amber-400">
                          {formatNumber(commission)} {currency === 'Original' ? pos.native_currency : currency}
                        </span>
                      ) : (
                        <span className="text-slate-500">—</span>
                      )}
                    </div>
                    <div className="col-span-1 text-right">
                      <span className={`text-slate-400 text-xs transition-transform inline-block ${isExpanded ? 'rotate-180' : ''}`}>
                        ▼
                      </span>
                    </div>
                  </div>

                  {/* Expanded row — trade history */}
                  {isExpanded && (
                    <TradeDetail symbol={pos.symbol} exchange={pos.listing_exchange} nativeCurrency={pos.native_currency} displayCurrency={currency} />
                  )}
                </div>
              )
            })}

            {/* Total row */}
            <div
              className="gap-2 px-6 py-6 mx-4 mb-4 mt-3 rounded-xl border border-indigo-500/20 bg-gradient-to-r from-[#1a1d35] to-[#161829]"
              style={{ display: 'grid', gridTemplateColumns: 'repeat(16, minmax(0, 1fr))' }}
            >
              <div className="col-span-5 flex items-center">
                <span className="text-sm font-bold text-slate-200 tracking-wide uppercase">Portfolio Total</span>
              </div>
              <div className="col-span-2 text-right">
                <div className="text-base font-bold text-white">
                  {currency === 'Original' ? 'Mixed' : formatCurrency(totalValue, currency)}
                </div>
              </div>
              <div className="col-span-2 text-right">
                <span className="text-sm font-semibold text-slate-300">100%</span>
              </div>
              <div className="col-span-2 text-right">
                {currency === 'Original' ? (
                  <span className="text-sm text-slate-500">Mixed</span>
                ) : totals.hasUnrealized ? (
                  <div>
                    <span className={`text-sm font-semibold ${
                      totals.unrealizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'
                    }`}>
                      {totals.unrealizedGL >= 0 ? '+' : ''}{formatNumber(totals.unrealizedGL)} {currency}
                    </span>
                  </div>
                ) : (
                  <span className="text-slate-500 text-sm">—</span>
                )}
              </div>
              <div className="col-span-2 text-right">
                {totals.realizedGL !== 0 ? (
                  <span className={`text-sm font-semibold ${
                    totals.realizedGL >= 0 ? 'text-emerald-400' : 'text-red-400'
                  }`}>
                    {totals.realizedGL >= 0 ? '+' : ''}{formatNumber(totals.realizedGL)} {currency === 'Original' ? '' : currency}
                  </span>
                ) : (
                  <span className="text-slate-500 text-sm">—</span>
                )}
              </div>
              <div className="col-span-2 text-right">
                {totals.commission !== 0 ? (
                  <span className="text-sm font-semibold text-amber-400">
                    {formatNumber(totals.commission)} {currency === 'Original' ? '' : currency}
                  </span>
                ) : (
                  <span className="text-slate-500 text-sm">—</span>
                )}
              </div>
              <div className="col-span-1" />
            </div>
          </div>
        )}
        </div>
      </main>
    </div>
  )
}

function TradeDetail({ symbol, exchange, nativeCurrency, displayCurrency }: { symbol: string; exchange?: string; nativeCurrency: string; displayCurrency: string }) {
  const [trades, setTrades] = useState<TradeEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    getPortfolioTrades(symbol, displayCurrency, exchange || '')
      .then(res => {
        if (!cancelled) setTrades(res.trades || [])
      })
      .catch(err => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load trades')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [symbol, displayCurrency])

  return (
    <div className="px-6 py-4 bg-[#13152180] border-t border-[#2a2e42]">
      <p className="text-xs font-medium text-slate-400 uppercase tracking-wider mb-3">Trade History — {symbol}</p>
      <div className="bg-[#0f1117] rounded-xl border border-[#2a2e42] overflow-hidden">
        <div className="grid grid-cols-6 gap-2 px-4 py-2 text-xs font-medium text-slate-500 uppercase border-b border-[#2a2e42]">
          <div>Date</div>
          <div>Side</div>
          <div className="text-right">Quantity</div>
          <div className="text-right">Price ({nativeCurrency})</div>
          <div className="text-right">Price ({displayCurrency === 'Original' ? nativeCurrency : displayCurrency})</div>
          <div className="text-right">Commission</div>
        </div>

        {loading ? (
          <div className="px-4 py-6 text-center text-slate-500 text-sm">Loading trades...</div>
        ) : error ? (
          <div className="px-4 py-4 text-center text-red-400 text-sm">{error}</div>
        ) : trades.length === 0 ? (
          <div className="px-4 py-6 text-center text-slate-500 text-sm">No trades found for {symbol}.</div>
        ) : (
          trades.map((trade, i) => (
            <div key={i} className="grid grid-cols-6 gap-2 px-4 py-2.5 text-sm border-b border-[#2a2e42]/50 hover:bg-[#1a1d2e] transition-colors">
              <div className="text-slate-300">{trade.date}</div>
              <div>
                <span className={`px-2 py-0.5 rounded text-xs font-medium ${
                  trade.side === 'BUY' || trade.side === 'TRANSFER_IN'
                    ? 'bg-emerald-500/15 text-emerald-400'
                    : trade.side === 'SELL' || trade.side === 'TRANSFER_OUT'
                    ? 'bg-red-500/15 text-red-400'
                    : 'bg-slate-500/15 text-slate-400'
                }`}>
                  {trade.side}
                </span>
              </div>
              <div className="text-right text-slate-300">{formatNumber(trade.quantity, 0)}</div>
              <div className="text-right text-slate-300">{formatNumber(trade.price)}</div>
              <div className="text-right text-slate-300">{formatNumber(trade.converted_price)}</div>
              <div className="text-right text-slate-400">{formatNumber(Math.abs(trade.commission))}</div>
            </div>
          ))
        )}
      </div>
    </div>
  )
}
