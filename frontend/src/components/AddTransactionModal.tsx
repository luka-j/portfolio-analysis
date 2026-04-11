import { useState, useEffect } from 'react'
import { addTransaction, type AddTransactionRequest, type PositionValue } from '../api'
import AutocompleteInput, { type AutocompleteOption } from './AutocompleteInput'
import DatePicker from './DatePicker'
import HoverTooltip from './HoverTooltip'
import NumberInput from './NumberInput'
import SelectInput from './SelectInput'

type TxType = 'buy' | 'sell' | 'espp_vest' | 'rsu_vest'

interface Props {
  positions: PositionValue[]
  onSuccess: () => void
  onClose: () => void
}

const COMMON_CURRENCIES = ['USD', 'EUR', 'GBP', 'CZK', 'CHF', 'SEK', 'NOK', 'DKK', 'JPY', 'CAD', 'AUD']

export default function AddTransactionModal({ positions, onSuccess, onClose }: Props) {
  const [txType, setTxType] = useState<TxType>('buy')
  const [symbol, setSymbol] = useState('')
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10))
  const [quantity, setQuantity] = useState('')
  const [price, setPrice] = useState('')
  const [currency, setCurrency] = useState('USD')
  const [commission, setCommission] = useState('')
  const [taxCostBasis, setTaxCostBasis] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [duplicateId, setDuplicateId] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = '' // Restore on unmount
    }
  }, [])

  const positionSymbols = positions.map(p => p.symbol)
  const matchedPosition = positions.find(p => p.symbol === symbol.toUpperCase())

  // Autocomplete options built from current portfolio holdings (symbol + security name)
  const symbolOptions: AutocompleteOption[] = positions
    .filter(p => p.symbol !== 'PENDING_CASH')
    .map(p => ({ value: p.symbol, label: p.name }))

  const total = (parseFloat(quantity) || 0) * (parseFloat(price) || 0)


  async function submit(force = false) {
    setError('')
    const qty = parseFloat(quantity)
    const prc = parseFloat(price)

    if (!symbol.trim()) { setError('Symbol is required'); return }
    if (symbol.trim().toUpperCase() === 'PENDING_CASH') { setError('PENDING_CASH is not a valid symbol'); return }
    if (!date) { setError('Date is required'); return }
    if (!qty || qty <= 0) { setError('Quantity must be greater than 0'); return }
    if (!prc || prc <= 0) { setError('Price must be greater than 0'); return }
    if (txType === 'espp_vest' && taxCostBasis === '') { setError('Tax cost basis is required for ESPP vest'); return }

    const req: AddTransactionRequest = {
      transaction_type: txType,
      symbol: symbol.trim().toUpperCase(),
      currency,
      date,
      quantity: qty,
      price: prc,
      commission: parseFloat(commission) || 0,
      force,
    }
    if (txType === 'espp_vest') {
      req.tax_cost_basis = parseFloat(taxCostBasis) || 0
    }

    setSubmitting(true)
    try {
      const res = await addTransaction(req)
      if (res.status === 'duplicate' && !force) {
        setDuplicateId(res.id)
      } else {
        setSaved(true)
        setTimeout(() => {
          onSuccess()
          onClose()
        }, 800)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save transaction')
    } finally {
      setSubmitting(false)
    }
  }

  const tabs: { key: TxType; label: string }[] = [
    { key: 'buy', label: 'Buy' },
    { key: 'sell', label: 'Sell' },
    { key: 'espp_vest', label: 'ESPP Vest' },
    { key: 'rsu_vest', label: 'RSU Vest' },
  ]

  return (
    <div
      className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="bg-[#13151f]/95 border border-white/8 rounded-2xl p-6 w-full max-w-md shadow-2xl backdrop-blur-xl ring-1 ring-white/5 max-h-[90vh] overflow-y-auto"
        onClick={e => e.stopPropagation()}
      >
        <h2 className="text-slate-100 font-semibold mb-4">Add Transaction</h2>

        {/* Transaction type tabs */}
        <div className="flex gap-1 mb-5 bg-[#0f1117] rounded-xl p-1">
          {tabs.map(tab => (
            <button
              key={tab.key}
              type="button"
              onClick={() => { setTxType(tab.key); setDuplicateId(null); setError('') }}
              className={`flex-1 px-2 py-1.5 rounded-lg text-xs font-semibold transition-all ${
                txType === tab.key
                  ? 'bg-indigo-500/20 text-indigo-300 border border-indigo-500/30'
                  : 'text-slate-500 hover:text-slate-300'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        <div className="space-y-3">
          {/* Symbol */}
          <div>
            <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
              Symbol
            </label>
            <AutocompleteInput
              options={symbolOptions}
              value={symbol}
              onChange={v => { setSymbol(v); setDuplicateId(null) }}
              placeholder="e.g. AAPL"
              autoFocus
            />
            {matchedPosition?.name && (
              <p className="text-[11px] text-slate-500 mt-1 pl-1">{matchedPosition.name}</p>
            )}
            {txType === 'sell' && symbol && !positionSymbols.includes(symbol.toUpperCase()) && (
              <p className="text-[11px] text-amber-500/80 mt-1 pl-1">
                This symbol is not in your current portfolio
              </p>
            )}
          </div>

          {/* Date */}
          <div>
            <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
              Date
            </label>
            <DatePicker value={date} onChange={setDate} />
          </div>

          {/* Quantity + Price */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Quantity
              </label>
              <NumberInput value={quantity} onChange={setQuantity} placeholder="0" min={0} step={1} />
            </div>
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                {txType === 'espp_vest' ? (
                  <span className="relative group inline-flex items-center gap-1 cursor-default">
                    FMV (on purchase date)
                    <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" className="text-slate-600 shrink-0"><circle cx="5" cy="5" r="4"/><path d="M5 4.5v2M5 3h.01"/></svg>
                    <HoverTooltip align="left" className="w-56 font-normal normal-case tracking-normal">
                      Fair market value — the stock's closing price on the ESPP purchase date. Used to calculate taxable employment income (FMV minus your purchase price).
                    </HoverTooltip>
                  </span>
                ) : txType === 'rsu_vest' ? (
                  <span className="relative group inline-flex items-center gap-1 cursor-default">
                    FMV (on vest date)
                    <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" className="text-slate-600 shrink-0"><circle cx="5" cy="5" r="4"/><path d="M5 4.5v2M5 3h.01"/></svg>
                    <HoverTooltip align="left" className="w-56 font-normal normal-case tracking-normal">
                      Fair market value — the stock's closing price on the vest date. The full FMV is taxed as employment income; cost basis is set to zero.
                    </HoverTooltip>
                  </span>
                ) : (
                  <span>Price</span>
                )}
              </label>
              <NumberInput value={price} onChange={setPrice} placeholder="0.00" min={0} step={0.01} />
            </div>
          </div>

          {/* Currency */}
          <div>
            <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
              Currency
            </label>
            <SelectInput options={COMMON_CURRENCIES} value={currency} onChange={setCurrency} />
          </div>

          {/* Commission — only for buy/sell */}
          {(txType === 'buy' || txType === 'sell') && (
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Commission <span className="text-slate-700 normal-case font-normal">(optional)</span>
              </label>
              <NumberInput value={commission} onChange={setCommission} placeholder="0.00" min={0} step={0.01} />
            </div>
          )}

          {/* Tax cost basis — ESPP only */}
          {txType === 'espp_vest' && (
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                <span className="relative group inline-flex items-center gap-1 cursor-default">
                  Employee purchase price
                  <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" className="text-slate-600 shrink-0"><circle cx="5" cy="5" r="4"/><path d="M5 4.5v2M5 3h.01"/></svg>
                  <HoverTooltip align="left" className="w-60 font-normal normal-case tracking-normal">
                    The discounted price you actually paid (your tax cost basis). The spread between FMV and this price is taxed as employment income in the year of purchase.
                  </HoverTooltip>
                </span>
              </label>
              <NumberInput value={taxCostBasis} onChange={setTaxCostBasis} placeholder="0.00" min={0} step={0.01} />
            </div>
          )}

          {/* Total */}
          {total > 0 && (txType === 'buy' || txType === 'sell') && (
            <div className="bg-[#0f1117] border border-[#2a2e42]/50 rounded-xl px-4 py-2.5 flex justify-between items-center">
              <span className="text-[10px] font-black text-slate-600 uppercase tracking-widest">Total</span>
              <span className="text-slate-300 font-bold text-sm tabular-nums">
                {total.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {currency}
              </span>
            </div>
          )}
        </div>

        {/* Duplicate warning */}
        {duplicateId && (
          <div className="mt-4 bg-amber-500/10 border border-amber-500/30 rounded-xl px-4 py-3">
            <p className="text-amber-400 text-xs font-semibold mb-2">
              A transaction with these exact details already exists. Record it anyway?
            </p>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setDuplicateId(null)}
                className="px-3 py-1.5 rounded-lg text-xs text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-all"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => submit(true)}
                disabled={submitting}
                className="px-3 py-1.5 rounded-lg text-xs font-semibold bg-amber-500/20 text-amber-400 hover:bg-amber-500/30 border border-amber-500/30 transition-all disabled:opacity-50"
              >
                Insert Duplicate
              </button>
            </div>
          </div>
        )}

        {error && (
          <p className="mt-3 text-xs text-red-400">{error}</p>
        )}

        {saved && (
          <p className="mt-3 text-xs text-emerald-400 font-semibold">Saved.</p>
        )}

        {!duplicateId && (
          <div className="flex gap-3 justify-end mt-5">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-xl text-sm text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-all"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => submit(false)}
              disabled={submitting || saved}
              className="px-4 py-2 rounded-xl text-sm font-semibold bg-indigo-500/20 text-indigo-400 hover:bg-indigo-500/30 border border-indigo-500/30 transition-all disabled:opacity-50"
            >
              {submitting ? 'Saving…' : 'Save'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
