import { useState } from 'react'

interface Props {
  symbol: string
  currentYahooSymbol?: string
  onConfirm: (yahooSymbol: string) => void
  onClose: () => void
}

export default function SymbolMappingModal({ symbol, currentYahooSymbol, onConfirm, onClose }: Props) {
  const [value, setValue] = useState(currentYahooSymbol || symbol)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (value.trim()) onConfirm(value.trim())
  }

  return (
    <div
      className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="bg-panel/95 border border-white/8 rounded-2xl p-6 w-full max-w-sm shadow-2xl backdrop-blur-xl ring-1 ring-white/5"
        onClick={e => e.stopPropagation()}
      >
        <h2 className="text-slate-100 font-semibold mb-1">Map Yahoo Finance Symbol</h2>
        <p className="text-slate-400 text-sm mb-4">
          Map <span className="text-indigo-400 font-mono">{symbol}</span> to a Yahoo Finance ticker
        </p>
        <form onSubmit={handleSubmit}>
          <input
            autoFocus
            className="w-full bg-bg border border-white/8 rounded-xl px-4 py-2.5 text-slate-100 font-mono text-sm focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 mb-4 transition-all"
            value={value}
            onChange={e => setValue(e.target.value)}
            placeholder="e.g. AAPL"
          />
          <div className="flex gap-3 justify-end">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-xl text-sm text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-all"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="px-4 py-2 rounded-xl text-sm font-semibold bg-indigo-500/20 text-indigo-400 hover:bg-indigo-500/30 border border-indigo-500/30 transition-all"
            >
              Save
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
