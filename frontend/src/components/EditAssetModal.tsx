import { useState } from 'react'
import { updateAsset, type PositionValue } from '../api'
import SelectInput from './SelectInput'

const ASSET_TYPES = ['Stock', 'ETF', 'Bond ETF', 'Commodity', 'Unknown']

interface Props {
  position: PositionValue
  onSuccess: () => void
  onClose: () => void
}

export default function EditAssetModal({ position, onSuccess, onClose }: Props) {
  const [name, setName] = useState(position.name ?? '')
  const [assetType, setAssetType] = useState(position.asset_type ?? 'Unknown')
  const [country, setCountry] = useState('')
  const [sector, setSector] = useState('')
  const [yahooSymbol, setYahooSymbol] = useState(position.yahoo_symbol ?? '')
  const [listingExchange, setListingExchange] = useState(position.listing_exchange ?? '')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  const exchangeReadOnly = !!position.listing_exchange

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      await updateAsset(position.symbol, position.listing_exchange, {
        name: name || undefined,
        asset_type: assetType,
        country: country || undefined,
        sector: sector || undefined,
        yahoo_symbol: yahooSymbol || undefined,
        listing_exchange: !exchangeReadOnly && listingExchange ? listingExchange : undefined,
      })
      setSaved(true)
      setTimeout(() => {
        onSuccess()
        onClose()
      }, 700)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="bg-panel/95 border border-white/8 rounded-2xl p-6 w-full max-w-md shadow-2xl backdrop-blur-xl ring-1 ring-white/5 max-h-[90vh] overflow-y-auto"
        onClick={e => e.stopPropagation()}
      >
        <h2 className="text-slate-100 font-semibold mb-1">Edit Asset</h2>
        <p className="text-slate-400 text-sm mb-5">
          Editing metadata for{' '}
          <span className="text-indigo-400 font-mono">{position.symbol}</span>
        </p>

        <form onSubmit={handleSubmit}>
          <div className="space-y-3">

            {/* Symbol — read-only */}
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Symbol
              </label>
              <div className="w-full bg-bg/50 border border-border-dim rounded-xl px-4 py-2.5 text-slate-400 font-mono text-sm select-none">
                {position.symbol}
              </div>
            </div>

            {/* Exchange — read-only if already set, editable if empty */}
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Exchange{exchangeReadOnly && <span className="ml-1 font-normal normal-case tracking-normal text-slate-600">(set by broker, read-only)</span>}
              </label>
              {exchangeReadOnly ? (
                <div className="w-full bg-bg/50 border border-border-dim rounded-xl px-4 py-2.5 text-slate-400 font-mono text-sm select-none">
                  {position.listing_exchange}
                </div>
              ) : (
                <input
                  className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 font-mono text-sm focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 transition-all"
                  value={listingExchange}
                  onChange={e => setListingExchange(e.target.value)}
                  placeholder="e.g. XETRA"
                />
              )}
            </div>

            {/* Full name */}
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Full Name
              </label>
              <input
                className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 text-sm focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 transition-all"
                value={name}
                onChange={e => setName(e.target.value)}
                placeholder="e.g. Apple Inc."
              />
            </div>

            {/* Asset type */}
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Asset Type
              </label>
              <SelectInput options={ASSET_TYPES} value={assetType} onChange={setAssetType} />
            </div>

            {/* Country */}
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Country
              </label>
              <input
                className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 text-sm focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 transition-all"
                value={country}
                onChange={e => setCountry(e.target.value)}
                placeholder="e.g. United States"
              />
            </div>

            {/* Sector */}
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Sector
              </label>
              <input
                className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 text-sm focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 transition-all"
                value={sector}
                onChange={e => setSector(e.target.value)}
                placeholder="e.g. Technology"
              />
            </div>

            {/* Yahoo Symbol */}
            <div>
              <label className="text-[10px] font-black text-slate-500 uppercase tracking-widest block mb-1 pl-1">
                Yahoo Symbol
              </label>
              <input
                className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 font-mono text-sm focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 transition-all"
                value={yahooSymbol}
                onChange={e => setYahooSymbol(e.target.value)}
                placeholder={`e.g. ${position.symbol}.DE`}
              />
            </div>
          </div>

          {/* Info banner */}
          <div className="mt-4 bg-white/3 border border-white/6 rounded-xl px-4 py-3 text-[11px] text-slate-500 leading-relaxed">
            Once saved, the background data job will stop updating name, country, sector, and asset type for this asset.
            Yahoo Symbol and Exchange are never modified by the job.
          </div>

          {error && <p className="mt-3 text-xs text-red-400">{error}</p>}
          {saved && <p className="mt-3 text-xs text-emerald-400 font-semibold">Saved.</p>}

          <div className="flex gap-3 justify-end mt-5">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-xl text-sm text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-all"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting || saved}
              className="px-4 py-2 rounded-xl text-sm font-semibold bg-indigo-500/20 text-indigo-400 hover:bg-indigo-500/30 border border-indigo-500/30 transition-all disabled:opacity-50"
            >
              {submitting ? 'Saving…' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
