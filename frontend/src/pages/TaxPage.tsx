import { useEffect, useState, useCallback } from 'react'
import { Navigate } from 'react-router-dom'
import PageLayout from '../components/PageLayout'
import Spinner from '../components/Spinner'
import SegmentedControl from '../components/SegmentedControl'
import ErrorAlert from '../components/ErrorAlert'
import { getTaxReport } from '../api'
import type { TaxReportResponse, TaxTransaction } from '../api'
import { escapeCSVField } from '../utils/format'
import { usePrivacy } from '../utils/PrivacyContext'

type FxMethod = 'historical' | 'universal'

const FX_OPTIONS = [
  { label: 'Historical', value: 'historical' as FxMethod },
  { label: 'Universal', value: 'universal' as FxMethod },
] as const

export default function TaxPage() {
  const { privacy } = usePrivacy()

  const [year, setYear] = useState<number>(new Date().getFullYear() - 1)
  const [fxMethod, setFxMethod] = useState<FxMethod>('historical')
  const [currencies, setCurrencies] = useState<string[]>([])
  const [rateInputs, setRateInputs] = useState<Record<string, string>>({})
  const [report, setReport] = useState<TaxReportResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // Extract unique currencies from a report's transactions
  const extractCurrencies = (data: TaxReportResponse): string[] => {
    const all = [...data.employment_income.transactions, ...data.investment_income.transactions]
    return [...new Set(all.map(t => t.currency))].sort()
  }

  // Fetch historical report to discover currencies when switching to universal mode
  useEffect(() => {
    if (fxMethod !== 'universal') return
    let cancelled = false
    async function fetchCurrencies() {
      setLoading(true)
      setError('')
      setReport(null)
      try {
        const data = await getTaxReport(year)
        if (!cancelled) {
          const found = extractCurrencies(data)
          setCurrencies(found)
          // Keep any already-entered rate inputs; reset only for new currencies
          setRateInputs(prev => {
            const next: Record<string, string> = {}
            for (const c of found) next[c] = prev[c] ?? ''
            return next
          })
        }
      } catch (err: unknown) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetchCurrencies()
    return () => { cancelled = true }
  }, [year, fxMethod])

  // Fetch historical report automatically
  useEffect(() => {
    if (fxMethod !== 'historical') return
    let cancelled = false
    async function fetchReport() {
      setLoading(true)
      setError('')
      try {
        const data = await getTaxReport(year)
        if (!cancelled) setReport(data)
      } catch (err: unknown) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetchReport()
    return () => { cancelled = true }
  }, [year, fxMethod])

  const allRatesFilled = currencies.length > 0 && currencies.every(c => {
    const v = parseFloat(rateInputs[c] ?? '')
    return isFinite(v) && v > 0
  })

  const fetchUniversalReport = useCallback(async () => {
    const rates: Record<string, number> = {}
    for (const c of currencies) rates[c] = parseFloat(rateInputs[c])
    setLoading(true)
    setError('')
    try {
      const data = await getTaxReport(year, rates)
      setReport(data)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [year, currencies, rateInputs])

  const handleFxMethodChange = (v: FxMethod) => {
    setReport(null)
    setError('')
    setCurrencies([])
    setFxMethod(v)
  }

  const exportToCSV = (transactions: TaxTransaction[], filename: string, isInvestment: boolean) => {
    if (!transactions || transactions.length === 0) return

    let headers, rows
    if (isInvestment) {
      headers = ['Symbol', 'Sale Date', 'Buy Date', 'Quantity', 'Sell Price', 'Currency', 'Buy Rate', 'Sell Rate', 'Buy Commission', 'Sell Commission', 'Cost CZK', 'Benefit CZK', 'Net Profit CZK']
      rows = transactions.map(t => [
        t.symbol, t.date, t.buy_date || '', t.quantity.toString(), t.native_price.toFixed(4), t.currency,
        t.buy_rate ? t.buy_rate.toFixed(4) : '', t.exchange_rate.toFixed(4),
        t.buy_commission ? t.buy_commission.toFixed(4) : '0', t.sell_commission ? t.sell_commission.toFixed(4) : '0',
        t.cost_czk.toFixed(2), t.benefit_czk.toFixed(2), (t.benefit_czk - t.cost_czk).toFixed(2)
      ])
    } else {
      headers = ['Type', 'Symbol', 'Date', 'Quantity', 'Native Price', 'Currency', 'Exchange Rate', 'Cost CZK', 'Benefit CZK', 'Net Profit CZK']
      rows = transactions.map(t => [
        t.type, t.symbol, t.date, t.quantity.toString(), t.native_price.toFixed(4), t.currency,
        t.exchange_rate.toFixed(4), t.cost_czk.toFixed(2), t.benefit_czk.toFixed(2), (t.benefit_czk - t.cost_czk).toFixed(2)
      ])
    }

    const csvContent = [headers.map(escapeCSVField).join(','), ...rows.map(e => e.map(escapeCSVField).join(','))].join('\n')
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.setAttribute('download', filename); document.body.appendChild(link); link.click(); document.body.removeChild(link)
  }

  const years = Array.from({ length: 10 }, (_, i) => new Date().getFullYear() - i)

  if (privacy) return <Navigate to="/" replace />

  return (
    <PageLayout mainClassName="pt-8">

        <div className="flex flex-col items-center mb-8 text-center">
          <h1 className="text-3xl font-semibold text-slate-100">Tax Report</h1>
          <p className="text-sm text-slate-500 mt-4 max-w-xl">
            Income and capital gains for Czech tax purposes, based on FIFO reconciliation.
          </p>

          <div className="flex items-end gap-6 mt-6 flex-wrap justify-center">
            <div className="flex items-center gap-3 bg-[#1a1d2e] border border-[#2a2e42]/60 rounded-2xl p-1.5 shadow-xl ring-1 ring-white/5">
              <label className="pl-4 text-sm text-slate-500">Year</label>
              <select
                value={year}
                onChange={(e) => setYear(Number(e.target.value))}
                className="bg-transparent text-slate-100 font-medium text-sm py-2 pr-5 pl-1 focus:outline-none cursor-pointer hover:text-indigo-400 transition-colors"
              >
                {years.map(y => <option key={y} value={y} className="bg-[#1a1d2e]">{y}</option>)}
              </select>
            </div>

            <SegmentedControl
              label="FX Method"
              options={FX_OPTIONS}
              value={fxMethod}
              onChange={handleFxMethodChange}
            />
          </div>
        </div>

        {/* Universal FX rate input table */}
        {fxMethod === 'universal' && !loading && !error && currencies.length > 0 && !report && (
          <div className="flex flex-col items-center mb-8 w-full max-w-sm mx-auto">
            <p className="text-xs text-slate-500 mb-4 text-center">
              Enter a single CZK exchange rate for each currency used in the {year} report.
            </p>
            <div className="w-full bg-[#1a1d2e]/40 border border-white/5 rounded-2xl overflow-hidden mb-4">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-slate-600 text-[9px] font-black uppercase tracking-[0.2em] border-b border-[#2a2e42]/40">
                    <th className="text-left py-2.5 px-4">Currency</th>
                    <th className="text-right py-2.5 px-4">Rate (1 unit → CZK)</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/4">
                  {currencies.map(c => (
                    <tr key={c}>
                      <td className="py-2.5 px-4 font-bold text-slate-300 uppercase tracking-tight">{c}</td>
                      <td className="py-2.5 px-4 text-right">
                        <div className="inline-flex items-center bg-[#1a1d2e] rounded-2xl p-1.5 border border-[#2a2e42]/50 shadow-xl shadow-black/20">
                          <input
                            type="number"
                            min="0"
                            step="any"
                            placeholder="e.g. 23.50"
                            value={rateInputs[c] ?? ''}
                            onChange={e => setRateInputs(prev => ({ ...prev, [c]: e.target.value }))}
                            onKeyDown={e => {
                              const allowed = ['0','1','2','3','4','5','6','7','8','9','.', ',','Backspace','Delete','ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Tab']
                              if (!allowed.includes(e.key) && !e.ctrlKey && !e.metaKey) e.preventDefault()
                            }}
                            className="w-28 px-3 py-1 bg-transparent text-sm text-slate-200 text-right tabular-nums focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                          />
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <button
              disabled={!allRatesFilled}
              onClick={fetchUniversalReport}
              className="px-8 py-2.5 rounded-xl text-sm font-semibold transition-all disabled:opacity-30 disabled:cursor-not-allowed glass text-indigo-300 hover:text-indigo-200"
            >
              Generate Report
            </button>
          </div>
        )}

        {loading ? (
          <Spinner label="Calculating tax report…" className="py-40" />
        ) : error ? (
          <ErrorAlert message={error} className="mb-8" />
        ) : report ? (
          <div className="flex flex-col gap-16 w-full max-w-6xl pb-12">

            {/* Section 1: Employment Income */}
            <section className="flex flex-col items-center">
              <div className="flex flex-col items-center mb-6 text-center w-full">
                <h2 className="text-2xl font-semibold text-slate-100">Employment Income</h2>
                <p className="text-sm text-slate-500 mt-2">Section 6 — ESPP and RSU Vests</p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6 w-full">
                <div className="bg-[#1a1d2e]/40 rounded-2xl px-6 py-4 flex flex-col items-center text-center border border-white/5">
                  <p className="text-xs text-slate-500 mb-2">Total Cost</p>
                  <p className="text-xl font-semibold tabular-nums text-rose-400">{report.employment_income.total_cost_czk.toLocaleString('cs-CZ')} CZK</p>
                </div>
                <div className="bg-[#1a1d2e]/40 rounded-2xl px-6 py-4 flex flex-col items-center text-center border border-white/5">
                  <p className="text-xs text-slate-500 mb-2">Total Benefit</p>
                  <p className="text-xl font-semibold tabular-nums text-emerald-400">{report.employment_income.total_benefit_czk.toLocaleString('cs-CZ')} CZK</p>
                </div>
                <div className="bg-[#1a1d2e]/40 rounded-2xl px-6 py-4 flex flex-col items-center text-center border border-white/5">
                  <p className="text-xs text-slate-500 mb-2">Net Taxable</p>
                  <p className="text-xl font-semibold tabular-nums text-white">{(report.employment_income.total_benefit_czk - report.employment_income.total_cost_czk).toLocaleString('cs-CZ')} CZK</p>
                </div>
              </div>

              <div className="flex justify-end w-full mb-3">
                <button
                  onClick={() => exportToCSV(report.employment_income.transactions, `employment_${year}.csv`, false)}
                  className="glass px-5 py-2 rounded-xl text-sm text-indigo-400 hover:text-indigo-300 transition-all"
                >
                  Export CSV
                </button>
              </div>

              <div className="overflow-x-auto w-full">
                <table className="w-full min-w-160 text-sm">
                  <thead>
                    <tr className="text-slate-600 text-[9px] font-black uppercase tracking-[0.2em] border-b border-[#2a2e42]/40">
                      <th className="text-left py-2.5 px-4">Event</th>
                      <th className="text-left py-2.5 px-4">Security</th>
                      <th className="text-left py-2.5 px-4">Date</th>
                      <th className="text-right py-2.5 px-4">Qty</th>
                      <th className="text-right py-2.5 px-4">FMV</th>
                      <th className="text-right py-2.5 px-4">Cost Basis</th>
                      <th className="text-right py-2.5 px-4">Income (CZK)</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/4">
                    {report.employment_income.transactions?.map((tx, idx) => {
                      const costBasisPerShare = tx.cost_czk > 0.001 ? tx.cost_czk / (tx.quantity * tx.exchange_rate) : 0
                      return (
                      <tr key={idx} className="hover:bg-white/2 transition-colors group">
                        <td className="py-2.5 px-4">
                          <span className={`px-3 py-1 rounded-xl text-[9px] font-black uppercase tracking-wider border ${tx.type === 'ESPP_VEST' ? 'bg-indigo-500/10 text-indigo-400 border-indigo-500/20' : 'bg-fuchsia-500/10 text-fuchsia-400 border-fuchsia-500/20'}`}>{tx.type}</span>
                        </td>
                        <td className="py-2.5 px-4 text-slate-200 font-bold group-hover:text-white transition-colors uppercase tracking-tight">{tx.symbol}</td>
                        <td className="py-2.5 px-4 text-slate-500 text-xs tabular-nums">{tx.date}</td>
                        <td className="py-2.5 px-4 text-slate-400 font-bold text-right tabular-nums">{tx.quantity}</td>
                        <td className="py-2.5 px-4 text-right">
                          <div className="text-xs font-black text-slate-300 tabular-nums">{tx.native_price.toFixed(2)} {tx.currency}</div>
                          <div className="text-[9px] font-black text-slate-700 uppercase tracking-widest mt-0.5">@ {tx.exchange_rate.toFixed(3)}</div>
                        </td>
                        <td className="py-2.5 px-4 text-right">
                          {costBasisPerShare > 0.001
                            ? <>
                                <div className="text-xs font-black text-slate-400 tabular-nums">{costBasisPerShare.toFixed(2)} {tx.currency}</div>
                                <div className="text-[9px] font-black text-slate-700 uppercase tracking-widest mt-0.5">@ {tx.exchange_rate.toFixed(3)}</div>
                              </>
                            : <div className="text-xs font-black text-slate-700">—</div>
                          }
                        </td>
                        <td className="py-2.5 px-4 text-right font-black tabular-nums text-emerald-400">
                          +{tx.benefit_czk.toLocaleString('cs-CZ', {maximumFractionDigits: 0})}
                        </td>
                      </tr>
                    )})}
                  </tbody>
                </table>
              </div>
            </section>

            {/* Section 2: Investment Income */}
            <section className="flex flex-col items-center">
              <div className="flex flex-col items-center mb-6 text-center w-full">
                <h2 className="text-2xl font-semibold text-slate-100">Investment Income</h2>
                <p className="text-sm text-slate-500 mt-2">Section 10 — Realised sales, paired via FIFO</p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6 w-full">
                <div className="bg-[#1a1d2e]/40 rounded-2xl px-6 py-4 flex flex-col items-center text-center border border-white/5">
                  <p className="text-xs text-slate-500 mb-2">Cost Basis</p>
                  <p className="text-xl font-semibold tabular-nums text-rose-400">{report.investment_income.total_cost_czk.toLocaleString('cs-CZ')} CZK</p>
                </div>
                <div className="bg-[#1a1d2e]/40 rounded-2xl px-6 py-4 flex flex-col items-center text-center border border-white/5">
                  <p className="text-xs text-slate-500 mb-2">Proceeds</p>
                  <p className="text-xl font-semibold tabular-nums text-emerald-400">{report.investment_income.total_benefit_czk.toLocaleString('cs-CZ')} CZK</p>
                </div>
                <div className="bg-[#1a1d2e]/40 rounded-2xl px-6 py-4 flex flex-col items-center text-center border border-white/5">
                  <p className="text-xs text-slate-500 mb-2">Net Gain</p>
                  <p className="text-xl font-semibold tabular-nums text-white">{(report.investment_income.total_benefit_czk - report.investment_income.total_cost_czk).toLocaleString('cs-CZ')} CZK</p>
                </div>
              </div>

              <div className="flex justify-end w-full mb-3">
                <button
                  onClick={() => exportToCSV(report.investment_income.transactions, `investment_${year}.csv`, true)}
                  className="glass px-5 py-2 rounded-xl text-sm text-emerald-400 hover:text-emerald-300 transition-all"
                >
                  Export CSV
                </button>
              </div>

              <div className="overflow-x-auto w-full">
                <table className="w-full min-w-3xl text-sm">
                  <thead>
                    <tr className="text-slate-600 text-[9px] font-black uppercase tracking-[0.2em] border-b border-[#2a2e42]/40">
                      <th className="text-left py-2.5 px-4">Security</th>
                      <th className="text-left py-2.5 px-4">Buy Date</th>
                      <th className="text-left py-2.5 px-4">Sell Date</th>
                      <th className="text-right py-2.5 px-4">Qty</th>
                      <th className="text-right py-2.5 px-4">Buy Price</th>
                      <th className="text-right py-2.5 px-4">Sell Price</th>
                      <th className="text-right py-2.5 px-4">Commissions</th>
                      <th className="text-right py-2.5 px-4">Delta (CZK)</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/4">
                    {report.investment_income.transactions?.map((tx: TaxTransaction, idx: number) => {
                      const buyPriceNative = tx.buy_rate ? tx.cost_czk / (tx.quantity * tx.buy_rate) : 0
                      const delta = tx.benefit_czk - tx.cost_czk
                      return (
                      <tr key={idx} className="hover:bg-white/2 transition-colors group">
                        <td className="py-2.5 px-4 text-slate-200 font-bold group-hover:text-white transition-colors uppercase tracking-tight">{tx.symbol}</td>
                        <td className="py-2.5 px-4 text-slate-500 text-xs tabular-nums">{tx.buy_date ?? '—'}</td>
                        <td className="py-2.5 px-4 text-slate-500 text-xs tabular-nums">{tx.date}</td>
                        <td className="py-2.5 px-4 text-slate-400 font-bold text-right tabular-nums">{tx.quantity.toLocaleString('cs-CZ')}</td>
                        <td className="py-2.5 px-4 text-right">
                          <div className="text-xs font-black text-slate-400 tabular-nums">{buyPriceNative.toFixed(2)} {tx.currency}</div>
                          <div className="text-[9px] font-black text-slate-700 uppercase tracking-widest mt-0.5">@ {tx.buy_rate?.toFixed(3) ?? '—'}</div>
                        </td>
                        <td className="py-2.5 px-4 text-right">
                          <div className="text-xs font-black text-slate-300 tabular-nums">{tx.native_price.toFixed(2)} {tx.currency}</div>
                          <div className="text-[9px] font-black text-slate-700 uppercase tracking-widest mt-0.5">@ {tx.exchange_rate.toFixed(3)}</div>
                        </td>
                        <td className="py-2.5 px-4 text-right">
                          {(tx.buy_commission ?? 0) > 0.0001
                            ? <div className="text-xs font-black text-slate-500 tabular-nums">−{tx.buy_commission!.toFixed(2)} {tx.currency}</div>
                            : <div className="text-xs font-black text-slate-700">—</div>
                          }
                          {(tx.sell_commission ?? 0) > 0.0001
                            ? <div className="text-xs font-black text-slate-500 tabular-nums mt-0.5">−{tx.sell_commission!.toFixed(2)} {tx.currency}</div>
                            : null
                          }
                        </td>
                        <td className={`py-2.5 px-4 text-right font-black tabular-nums ${delta >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                          {delta > 0 ? '+' : ''}{delta.toLocaleString('cs-CZ', {maximumFractionDigits: 0})}
                        </td>
                      </tr>
                    )})}
                  </tbody>
                </table>
              </div>
            </section>
          </div>
        ) : null}
    </PageLayout>
  )
}
