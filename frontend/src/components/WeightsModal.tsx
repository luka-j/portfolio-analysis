/** WeightRow represents a position and its percentage weight in the portfolio. */
export interface WeightRow {
  symbol: string
  weight: number
}

/** WeightsModal lets the user inspect and adjust portfolio weights before sending to LLM. */
export default function WeightsModal({
  weights,
  weightsLoading,
  portfolioTotals,
  onWeightChange,
  onWeightStep,
  onRemove,
  onReset,
  onClose,
}: {
  weights: WeightRow[]
  weightsLoading: boolean
  portfolioTotals: { CZK: number; USD: number; EUR: number } | null
  onWeightChange: (idx: number, val: string) => void
  onWeightStep: (idx: number, delta: number) => void
  onRemove: (idx: number) => void
  onReset: () => void
  onClose: () => void
}) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={e => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="w-full max-w-sm mx-4 bg-panel/95 backdrop-blur-xl border border-white/8 rounded-2xl shadow-2xl flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-white/5">
          <div>
            <h3 className="text-sm font-semibold text-slate-100">Portfolio Weights</h3>
            <p className="text-xs text-slate-500 mt-0.5">Adjust to explore hypothetical scenarios</p>
            {portfolioTotals && portfolioTotals.USD > 0 && (
              <p className="text-xs text-slate-400 mt-1">
                1% ≈ ${Math.round(portfolioTotals.USD * 0.01).toLocaleString()} · €{Math.round(portfolioTotals.EUR * 0.01).toLocaleString()} · {Math.round(portfolioTotals.CZK * 0.01).toLocaleString()} Kč
              </p>
            )}
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={onReset}
              className="text-xs font-medium text-indigo-400/70 hover:text-indigo-400 transition-colors"
            >
              Reset
            </button>
            <button
              onClick={onClose}
              className="text-slate-500 hover:text-slate-300 transition-colors"
              aria-label="Close"
            >
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
                <path d="M1 1l12 12M13 1L1 13" />
              </svg>
            </button>
          </div>
        </div>

        {/* Table */}
        <div className="overflow-y-auto max-h-105">
          {weightsLoading ? (
            <p className="text-sm text-slate-500 text-center py-10">Loading…</p>
          ) : weights.length === 0 ? (
            <p className="text-sm text-slate-500 text-center py-10">No portfolio data. Upload portfolio data first.</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/5">
                  <th className="text-left px-6 py-3 text-xs font-semibold text-slate-500 uppercase tracking-widest">Symbol</th>
                  <th className="text-right px-6 py-3 text-xs font-semibold text-slate-500 uppercase tracking-widest">Weight</th>
                  <th className="w-8" />
                </tr>
              </thead>
              <tbody className="divide-y divide-white/3">
                {weights.map((row, idx) => (
                  <tr key={row.symbol} className="group hover:bg-white/2 transition-colors">
                    <td className="px-6 py-3 font-mono text-sm text-slate-300">{row.symbol}</td>
                    <td className="px-6 py-3 text-right">
                      <div className="inline-flex items-center gap-1.5">
                        <div className="relative flex items-center">
                          <input
                            type="number"
                            min={0}
                            max={100}
                            step={0.1}
                            value={row.weight}
                            onChange={e => onWeightChange(idx, e.target.value)}
                            className="w-20 px-3 py-1.5 pr-6 bg-surface border border-border-dim/60 rounded-xl text-sm text-slate-200 text-right focus:outline-none focus:ring-2 focus:ring-indigo-500/40 transition-all [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                          />
                          <div className="absolute right-1.5 flex flex-col">
                            <button
                              type="button"
                              tabIndex={-1}
                              onClick={() => onWeightStep(idx, 0.1)}
                              className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
                            >
                              <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 4L4 1L7 4"/></svg>
                            </button>
                            <button
                              type="button"
                              tabIndex={-1}
                              onClick={() => onWeightStep(idx, -0.1)}
                              className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
                            >
                              <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 1L4 4L7 1"/></svg>
                            </button>
                          </div>
                        </div>
                        <span className="text-xs text-slate-500">%</span>
                      </div>
                    </td>
                    <td className="pr-4 py-3 text-center">
                      <button
                        onClick={() => onRemove(idx)}
                        className="text-slate-500 hover:text-rose-400 transition-colors opacity-0 group-hover:opacity-100"
                        aria-label={`Remove ${row.symbol}`}
                      >
                        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
                          <path d="M1 1l8 8M9 1L1 9" />
                        </svg>
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* Footer */}
        <div className="px-6 py-3 border-t border-white/5">
          <p className="text-xs text-slate-500">
            Changes apply to the next freeform message only. Canned prompts always use live data.
          </p>
        </div>
      </div>
    </div>
  )
}
