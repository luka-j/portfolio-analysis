import SegmentedControl from '../SegmentedControl'
import DateRangePicker from '../DateRangePicker'
import { CURRENCIES, formatDate, getFromDate } from '../../utils/format'
import type { DailyValue } from '../../api'

const CURRENCY_OPTIONS = CURRENCIES.map(c => ({ label: c, value: c }))

const FX_METHOD_OPTIONS = [
  { label: 'Historical', value: 'historical' as const, tooltip: 'Uses the FX rate at the time each trade was executed. Reflects your true cost basis in the currency, accounting for currency movements over time.' },
  { label: 'Spot',       value: 'spot'       as const, tooltip: "Applies today's FX rate to all prices. Shows current market value converted at the current exchange rate, regardless of when trades were made." },
]

interface AnalysisControlsProps {
  currency: string
  setCurrency: (c: string) => void
  period: number
  setPeriod: (p: number) => void
  customFrom: string
  setCustomFrom: (f: string) => void
  customTo: string
  setCustomTo: (t: string) => void
  isPickerOpen: boolean
  setIsPickerOpen: (o: boolean) => void
  portfolioHistory: DailyValue[]
  acctModel: 'historical' | 'spot'
  setAcctModel: (m: 'historical' | 'spot') => void
  riskFreeRate: number
  riskFreeRateInput: string
  setRiskFreeRateInput: (v: string) => void
  handleRiskFreeRateBlur: () => void
  applyRiskFreeRate: (r: number) => void
}

export default function AnalysisControls({
  currency, setCurrency,
  period, setPeriod,
  customFrom, setCustomFrom,
  customTo, setCustomTo,
  isPickerOpen, setIsPickerOpen,
  portfolioHistory,
  acctModel, setAcctModel,
  riskFreeRate, riskFreeRateInput, setRiskFreeRateInput,
  handleRiskFreeRateBlur, applyRiskFreeRate,
}: AnalysisControlsProps) {
  const periodOptions = [
    { label: '1M',     value: 1  },
    { label: '3M',     value: 3  },
    { label: '1Y',     value: 12 },
    { label: 'All',    value: 0  },
    { label: period === -1 ? `${customFrom.substring(2).replace(/-/g, '/')} - ${customTo.substring(2).replace(/-/g, '/')}` : 'Custom', value: -1 },
  ]

  return (
    <div className="flex flex-wrap justify-center items-end gap-4 mb-20">
      <SegmentedControl label="Currency" options={CURRENCY_OPTIONS} value={currency} onChange={setCurrency} />
      
      <div className="relative">
        <SegmentedControl
          label="Time Period"
          options={periodOptions}
          value={period}
          onChange={p => {
            if (p === -1) {
              if (period !== -1) {
                setCustomFrom(getFromDate(period === 0 ? 12 : period))
                setCustomTo(formatDate(new Date()))
              }
              setIsPickerOpen(true)
            } else {
              setIsPickerOpen(false)
            }
            setPeriod(p)
          }}
        />
        {period === -1 && isPickerOpen && (
          <div className="absolute top-full left-1/2 -translate-x-1/2 mt-2 z-50">
            <DateRangePicker
              initialFrom={customFrom}
              initialTo={customTo}
              minDate={portfolioHistory[0]?.date}
              onApply={(f, t) => { setCustomFrom(f); setCustomTo(t); setIsPickerOpen(false) }}
              onCancel={() => setIsPickerOpen(false)}
            />
          </div>
        )}
      </div>

      <SegmentedControl label="FX Method" options={FX_METHOD_OPTIONS} value={acctModel} onChange={setAcctModel} />
      <div className="flex flex-col items-center gap-2">
        <div className="relative group/rfr cursor-default">
          <span className="text-[9px] font-black text-slate-500 uppercase tracking-[0.2em]">Risk-free rate</span>
          <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 w-60 max-w-[calc(100vw-2rem)] px-3 py-2.5 bg-panel border border-border-dim/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover/rfr:opacity-100 transition-opacity z-50 shadow-2xl">
            The annual return of a theoretically risk-free asset. Used as the baseline in Sharpe and Sortino ratio calculations — only returns above this threshold are treated as compensation for risk.
          </div>
        </div>
        <div className="flex items-center gap-1.5 bg-surface rounded-2xl p-1.5 border border-border-dim/50 shadow-xl shadow-black/20">
          <div className="relative flex items-center">
            <input
              type="number" value={riskFreeRateInput}
              onChange={e => setRiskFreeRateInput(e.target.value)}
              onBlur={handleRiskFreeRateBlur}
              onKeyDown={e => e.key === 'Enter' && e.currentTarget.blur()}
              step="0.1" min="0" max="20"
              className="w-20 px-3 py-2 pr-6 bg-transparent text-sm text-slate-200 text-right focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
            />
            <div className="absolute right-1.5 flex flex-col">
              <button
                type="button" tabIndex={-1}
                onClick={() => applyRiskFreeRate(Math.min(0.20, Math.round((riskFreeRate + 0.001) * 1000) / 1000))}
                className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
              >
                <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 4L4 1L7 4"/></svg>
              </button>
              <button
                type="button" tabIndex={-1}
                onClick={() => applyRiskFreeRate(Math.max(0, Math.round((riskFreeRate - 0.001) * 1000) / 1000))}
                className="flex items-center justify-center w-4 h-3.5 text-slate-500 hover:text-slate-300 transition-colors"
              >
                <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 1L4 4L7 1"/></svg>
              </button>
            </div>
          </div>
          <span className="text-slate-500 text-sm select-none pr-2">%</span>
        </div>
      </div>
    </div>
  )
}
