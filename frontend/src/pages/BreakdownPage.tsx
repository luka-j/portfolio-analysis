import { useState, useEffect, useCallback } from 'react'
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer } from 'recharts'
import PageLayout from '../components/PageLayout'
import SegmentedControl from '../components/SegmentedControl'
import Spinner from '../components/Spinner'
import { getPortfolioBreakdown, type BreakdownSection, type BreakdownEntry } from '../api'
import { CURRENCIES } from '../utils/format'
import { usePersistentState } from '../utils/usePersistentState'
import { usePrivacy } from '../utils/PrivacyContext'

const CURRENCY_OPTIONS = CURRENCIES.map(c => ({ label: c, value: c }))

const PALETTE = [
  '#6366f1', '#8b5cf6', '#a78bfa', '#c4b5fd',
  '#22d3ee', '#38bdf8', '#7dd3fc', '#bae6fd',
  '#34d399', '#4ade80', '#86efac', '#bbf7d0',
  '#fbbf24', '#fb923c', '#f87171', '#f472b6',
]

function sectionColor(idx: number): string {
  return PALETTE[idx % PALETTE.length]
}

interface SectionCardProps {
  section: BreakdownSection
  formatValue: (v: number) => string
  privacy: boolean
}

const CUSTOM_TOOLTIP_STYLE = {
  backgroundColor: 'rgba(26,29,46,0.98)',
  border: '1px solid rgba(99,102,241,0.3)',
  borderRadius: '20px',
  padding: '12px 20px',
  color: '#e2e8f0',
  fontSize: '12px',
  backdropFilter: 'blur(32px)',
  boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.5)',
}

function CustomTooltip({ active, payload }: { active?: boolean; payload?: Array<{ name: string; value: number; payload: BreakdownEntry }> }) {
  if (!active || !payload || payload.length === 0) return null
  const d = payload[0].payload
  return (
    <div style={CUSTOM_TOOLTIP_STYLE}>
      <p className="font-black text-slate-100 mb-1 uppercase tracking-widest">{d.label}</p>
      <p className="text-indigo-400 font-black text-sm">{d.percentage.toFixed(1)}%</p>
    </div>
  )
}

function SectionCard({ section, formatValue, privacy }: SectionCardProps) {
  const [activeIdx, setActiveIdx] = useState<number | null>(null)
  const pieData = section.entries.map((e) => ({ ...e, name: e.label }))

  return (
    <div className="py-2 w-full flex flex-col items-center">
      <div className="flex flex-col items-center mb-6 text-center">
        <h2 className="text-2xl font-semibold text-slate-100">{section.title}</h2>
        {section.note && (
          <p className="text-sm text-slate-500 mt-2">{section.note}</p>
        )}
      </div>
      
      <div className="flex flex-col lg:flex-row gap-12 items-center lg:items-start w-full">
        {/* Pie chart */}
        <div className="w-full md:w-72 h-72 shrink-0 relative">
          <ResponsiveContainer width="100%" height="100%">
            <PieChart>
              <Pie
                data={pieData}
                dataKey="value"
                nameKey="label"
                cx="50%"
                cy="50%"
                innerRadius="60%"
                outerRadius="95%"
                paddingAngle={4}
                onMouseEnter={(_, idx) => setActiveIdx(idx)}
                onMouseLeave={() => setActiveIdx(null)}
              >
                {pieData.map((_, idx) => (
                  <Cell
                    key={idx}
                    fill={sectionColor(idx)}
                    stroke="rgba(0,0,0,0.1)"
                    strokeWidth={1}
                    opacity={activeIdx === null || activeIdx === idx ? 1 : 0.25}
                    style={{ cursor: 'pointer', transition: 'all 300ms cubic-bezier(0.4, 0, 0.2, 1)' }}
                  />
                ))}
              </Pie>
              <Tooltip content={<CustomTooltip />} />
            </PieChart>
          </ResponsiveContainer>
          {/* Inner ring for depth */}
          <div className="absolute inset-0 m-auto w-32 h-32 rounded-full border border-white/3 pointer-events-none" />
        </div>

        {/* Table */}
        <div className="flex-1 w-full max-w-2xl">
          <div className={section.entries.length > 6 ? 'overflow-y-auto max-h-78' : ''}>
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-[#0f1117] z-10">
              <tr className="text-slate-500 text-xs font-semibold border-b border-[#2a2e42]/40">
                <th className="text-left py-4 pr-6 w-1/2">Category</th>
                <th className="text-right py-4 pr-6 w-32">Value</th>
                <th className="text-right py-4">Weight</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/4">
              {section.entries.map((e, idx) => (
                <tr
                  key={e.label}
                  className="hover:bg-white/2 transition-colors group"
                  onMouseEnter={() => setActiveIdx(idx)}
                  onMouseLeave={() => setActiveIdx(null)}
                >
                  <td className="py-4 pr-6">
                    <div className="flex items-center gap-3">
                      <span
                        className={`w-2.5 h-2.5 rounded-full shrink-0 transition-transform duration-300 ${activeIdx === idx ? 'scale-125' : 'group-hover:scale-110'}`}
                        style={{ backgroundColor: sectionColor(idx) }}
                      />
                      <span className="text-slate-200 text-sm group-hover:text-indigo-400 transition-colors truncate max-w-[320px]" title={e.label}>{e.label}</span>
                    </div>
                  </td>
                  <td className="py-4 pr-6 text-right text-slate-400 tabular-nums text-sm">
                    {privacy ? '—' : formatValue(e.value)}
                  </td>
                  <td className="py-4 text-right">
                    <div className="flex items-center justify-end gap-3">
                      <div className="w-20 h-1 bg-white/5 rounded-full overflow-hidden">
                        <div
                          className={`h-full rounded-full transition-all duration-700 ease-out ${activeIdx === idx ? 'opacity-100' : 'opacity-60'}`}
                          style={{ width: `${Math.min(e.percentage, 100)}%`, backgroundColor: sectionColor(idx) }}
                        />
                      </div>
                      <span className="text-slate-300 font-medium text-sm w-12 text-right tabular-nums">{e.percentage.toFixed(1)}%</span>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
  )
}

function formatCurrencyValue(value: number, currency: string): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency,
      minimumFractionDigits: 0,
      maximumFractionDigits: 0,
    }).format(value)
  } catch {
    return `${currency} ${value.toFixed(0)}`
  }
}

export default function BreakdownPage() {
  const { privacy } = usePrivacy()
  const [currency, setCurrency] = usePersistentState('breakdown_currency', 'USD')
  const [sections, setSections] = useState<BreakdownSection[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await getPortfolioBreakdown(currency)
      setSections(data.sections ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load breakdown')
    } finally {
      setLoading(false)
    }
  }, [currency])

  useEffect(() => { load() }, [load])

  const fmt = (v: number) => formatCurrencyValue(v, currency)

  return (
    <PageLayout maxWidth="max-w-6xl">
      <div className="w-full">
        {/* Header */}
        <div className="flex flex-col items-center mb-12 text-center">
          <h1 className="text-3xl font-semibold text-slate-100">Portfolio Breakdown</h1>
          <p className="text-sm text-slate-500 mt-4 leading-relaxed max-w-xl">
            Breakdown by asset class, geography, and sector based on current holdings.
          </p>
          <div className="mt-6">
            <SegmentedControl label="Currency" options={CURRENCY_OPTIONS} value={currency} onChange={setCurrency} />
          </div>
        </div>

        {loading && <Spinner label="Loading breakdowns…" className="py-40" />}

        {error && !loading && (
          <div className="bg-red-500/10 border border-red-500/20 rounded-3xl px-10 py-6 text-red-400 text-xs font-black uppercase tracking-widest text-center shadow-2xl">
            {error}
          </div>
        )}

        {!loading && !error && sections.length === 0 && (
          <div className="text-center text-slate-800 py-40 font-black uppercase tracking-[0.4em] text-[10px] opacity-40">
            Sync required to generate breakdown matrix
          </div>
        )}

        {!loading && !error && sections.length > 0 && (
          <div className="flex flex-col gap-12 animate-fade-in pb-20">
            {sections.map((s, i) => (
              <div key={s.title} className="w-full">
                <SectionCard section={s} formatValue={fmt} privacy={privacy} />
                {i < sections.length - 1 && (
                  <div className="mt-4 border-t border-[#2a2e42]/40 opacity-30" />
                )}
              </div>
            ))}
            <footer className="mt-12 text-[8px] font-black text-slate-800 uppercase tracking-[0.4em] text-center leading-loose max-w-2xl mx-auto opacity-30">
              Fundamental attribution weights are derived from aggregated security metadata.
              Values represent your current portfolio composition.
            </footer>
          </div>
        )}
      </div>
    </PageLayout>
  )
}
