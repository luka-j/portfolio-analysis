import { useNavigate } from 'react-router-dom'
import { BarChart, Bar, Cell, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts'
import SegmentedControl from '../SegmentedControl'
import Spinner from '../Spinner'
import ErrorAlert from '../ErrorAlert'
import CorrelationHeatmap from '../CorrelationHeatmap'
import HoverTooltip from '../HoverTooltip'
import { RECHARTS_TOOLTIP_STYLE, RECHARTS_LABEL_STYLE, RECHARTS_ITEM_STYLE } from '../../utils/format'
import type { AttributionResult, StatsResponse } from '../../api'
import type { HoldingsView } from '../../pages/hooks/types'

const AXIS_STYLE = { fontSize: 10, fill: '#475569' }

const HOLDINGS_VIEW_OPTIONS = [
  { label: 'Attribution', value: 'attribution' as const },
  { label: 'Correlation', value: 'correlation' as const },
]

interface HoldingsPanelProps {
  attributionData: AttributionResult[]
  holdingsView: HoldingsView
  setHoldingsView: (v: HoldingsView) => void
  holdingsLoading: boolean
  holdingsError: string
  attrDisplay: AttributionResult[]
  attributionTWR: number
  correlationData: { symbols: string[]; matrix: number[][] }
  compare: number | null
  compareStats: StatsResponse | null
  activeLabel: string
  compareLabel: string | null
  periodLabel: string
  effectiveFrom: string
  to: string
  currency: string
  acctModel: 'historical' | 'spot'
  riskFreeRate: number
  active: number | null
}

export default function HoldingsPanel({
  attributionData,
  holdingsView,
  setHoldingsView,
  holdingsLoading,
  holdingsError,
  attrDisplay,
  attributionTWR,
  correlationData,
  compare,
  compareStats,
  activeLabel,
  compareLabel,
  periodLabel,
  effectiveFrom,
  to,
  currency,
  acctModel,
  riskFreeRate,
  active,
}: HoldingsPanelProps) {
  const navigate = useNavigate()

  return (
    <div className="w-full">
      <div className="flex items-center justify-center gap-3 mb-8">
        <h2 className="text-xl font-semibold text-slate-100">Holdings Analysis</h2>
        {attributionData.length > 0 && holdingsView === 'attribution' && (
          <div className="relative group">
            <button
              onClick={() => {
                const isComparing = compare !== null && compareStats !== null
                navigate('/llm', { state: { initialPrompt: isComparing
                  ? { promptType: 'holdings_comparison', displayMessage: `Compare holdings: ${activeLabel} vs ${compareLabel}`, extraParams: { scenario_id: active ?? 0, scenario_id_a: active ?? 0, scenario_id_b: compare, currency, from: effectiveFrom, to, accounting_model: acctModel, risk_free_rate: riskFreeRate } }
                  : { promptType: 'biggest_drag_on_performance', displayMessage: `Identify the biggest drag on performance in my portfolio for ${periodLabel}`, extraParams: { currency, from: effectiveFrom, to, accounting_model: acctModel, risk_free_rate: riskFreeRate } }
                }})
              }}
              className="text-slate-500 hover:text-indigo-400 transition-colors p-1 rounded-xl hover:bg-white/5"
            >
              <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M12 2L13.5 8.5L20 10L13.5 11.5L12 18L10.5 11.5L4 10L10.5 8.5Z" />
                <path d="M19 1l.9 2.6 2.6.9-2.6.9L19 8.5l-.9-2.6L15.5 4l2.6-.9z" opacity=".6" />
                <path d="M5 17l.7 2.1L7.8 20l-2.1.9L5 23l-.7-2.1L2.2 20l2.1-.9z" opacity=".6" />
              </svg>
            </button>
            <HoverTooltip direction="down" className="w-max whitespace-nowrap">
              {compare !== null && compareStats !== null ? `AI comparison: ${activeLabel} vs ${compareLabel}` : 'AI attribution analysis'}
            </HoverTooltip>
          </div>
        )}
      </div>

      <div className="flex justify-center mb-10">
        <SegmentedControl
          label="View"
          options={HOLDINGS_VIEW_OPTIONS}
          value={holdingsView}
          onChange={setHoldingsView}
        />
      </div>

      {holdingsLoading ? (
        <Spinner label="Computing holdings data…" className="py-10" />
      ) : holdingsError ? (
        <ErrorAlert message={holdingsError} className="mb-6" />
      ) : holdingsView === 'attribution' ? (
        attributionData.length === 0 ? (
          <p className="text-slate-500 text-center text-sm py-10">No attribution data available for this period.</p>
        ) : (
          <div className="w-full">
            <div className="h-72 mb-8 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart
                  data={attrDisplay.map(r => ({ name: r.symbol.split('@')[0], value: +(r.contribution * 100).toFixed(3) }))}
                  layout="vertical"
                  margin={{ top: 4, right: 40, left: 60, bottom: 4 }}
                >
                  <CartesianGrid strokeDasharray="3 3" stroke="#2a2e42" horizontal={false} opacity={0.3} />
                  <XAxis type="number" tickFormatter={v => `${Number(v).toFixed(1)}%`} tick={AXIS_STYLE} axisLine={false} tickLine={false} />
                  <YAxis type="category" dataKey="name" tick={{ ...AXIS_STYLE, fontSize: 9 }} axisLine={false} tickLine={false} width={56} interval={0} />
                  <Tooltip contentStyle={RECHARTS_TOOLTIP_STYLE} labelStyle={RECHARTS_LABEL_STYLE} itemStyle={RECHARTS_ITEM_STYLE} formatter={(value) => [`${Number(value).toFixed(3)}%`, 'Contribution']} />
                  <Bar dataKey="value" radius={[0, 3, 3, 0]} isAnimationActive={false}>
                    {attrDisplay.map((r, i) => (
                      <Cell key={i} fill={r.contribution >= 0 ? '#34d399' : '#f87171'} fillOpacity={0.85} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>

            <div className="overflow-x-auto w-full">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border-dim/60">
                    <th className="py-3 px-4 text-left text-xs font-semibold text-slate-500">Symbol</th>
                    <th className="py-3 px-4 text-right text-xs font-semibold text-slate-500">Avg Weight</th>
                    <th className="py-3 px-4 text-right text-xs font-semibold text-slate-500">Return</th>
                    <th className="py-3 px-4 text-right text-xs font-semibold text-slate-500">Contribution</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {attrDisplay.map(r => (
                    <tr key={r.symbol} className="hover:bg-white/2 transition-colors">
                      <td className="py-3 px-4 font-semibold text-slate-100 uppercase text-sm">{r.symbol.split('@')[0]}</td>
                      <td className="py-3 px-4 text-right text-slate-400 tabular-nums">{(r.avg_weight * 100).toFixed(1)}%</td>
                      <td className={`py-3 px-4 text-right font-medium tabular-nums ${r.return >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {r.return >= 0 ? '+' : ''}{(r.return * 100).toFixed(2)}%
                      </td>
                      <td className={`py-3 px-4 text-right font-semibold tabular-nums ${r.contribution >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                        {r.contribution >= 0 ? '+' : ''}{(r.contribution * 100).toFixed(3)}%
                      </td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr className="border-t-2 border-border-dim/60">
                    <td className="py-3 px-4 font-black text-slate-100 text-xs uppercase tracking-widest">Portfolio TWR</td>
                    <td className="py-3 px-4 text-right text-slate-400 tabular-nums font-medium">100.0%</td>
                    <td className="py-3 px-4" />
                    <td className={`py-3 px-4 text-right font-black tabular-nums ${attributionTWR >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                      {attributionTWR >= 0 ? '+' : ''}{(attributionTWR * 100).toFixed(2)}%
                    </td>
                  </tr>
                </tfoot>
              </table>
            </div>
          </div>
        )
      ) : (
        correlationData.symbols.length === 0 ? (
          <p className="text-slate-500 text-center text-sm py-10">Not enough data to compute correlations for this period.</p>
        ) : (
          <CorrelationHeatmap symbols={correlationData.symbols} matrix={correlationData.matrix} />
        )
      )}
    </div>
  )
}
