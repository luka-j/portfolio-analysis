import { useState, useEffect, useCallback } from 'react'
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer } from 'recharts'
import NavBar from '../components/NavBar'
import { getPortfolioBreakdown, type BreakdownSection, type BreakdownEntry } from '../api'
import { CURRENCIES } from '../utils/format'

// Curated colour palette — cycles for larger sections.
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
}

const CUSTOM_TOOLTIP_STYLE = {
  backgroundColor: '#1e2030',
  border: '1px solid #2a2e42',
  borderRadius: '8px',
  padding: '8px 12px',
  color: '#e2e8f0',
  fontSize: '13px',
}

function CustomTooltip({ active, payload }: { active?: boolean; payload?: Array<{ name: string; value: number; payload: BreakdownEntry }> }) {
  if (!active || !payload || payload.length === 0) return null
  const d = payload[0].payload
  return (
    <div style={CUSTOM_TOOLTIP_STYLE}>
      <p className="font-semibold text-slate-200 mb-0.5">{d.label}</p>
      <p className="text-indigo-300">{d.percentage.toFixed(1)}%</p>
    </div>
  )
}

function SectionCard({ section, formatValue }: SectionCardProps) {
  const [activeIdx, setActiveIdx] = useState<number | null>(null)

  const pieData = section.entries.map((e) => ({ ...e, name: e.label }))

  return (
    <div className="bg-[#1a1d2e]/80 border border-[#2a2e42] rounded-2xl p-8 backdrop-blur">
      <h2 className="text-lg font-bold text-slate-100 mb-3">{section.title}</h2>
      {section.note && (
        <p className="text-xs text-slate-500 mb-6 leading-snug">{section.note}</p>
      )}
      <div className="flex flex-col md:flex-row gap-8">
        {/* Pie chart */}
        <div className="w-full md:w-64 h-64 flex-shrink-0">
          <ResponsiveContainer width="100%" height="100%">
            <PieChart>
              <Pie
                data={pieData}
                dataKey="value"
                nameKey="label"
                cx="50%"
                cy="50%"
                innerRadius="55%"
                outerRadius="80%"
                paddingAngle={2}
                onMouseEnter={(_, idx) => setActiveIdx(idx)}
                onMouseLeave={() => setActiveIdx(null)}
              >
                {pieData.map((_, idx) => (
                  <Cell
                    key={idx}
                    fill={sectionColor(idx)}
                    opacity={activeIdx === null || activeIdx === idx ? 1 : 0.45}
                    style={{ cursor: 'pointer', transition: 'opacity 0.15s' }}
                  />
                ))}
              </Pie>
              <Tooltip content={<CustomTooltip />} />
            </PieChart>
          </ResponsiveContainer>
        </div>

        {/* Table */}
        <div className="flex-1 overflow-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-slate-500 text-xs uppercase tracking-wide">
                <th className="text-left pb-3 pr-6">Label</th>
                <th className="text-right pb-3 pr-6">Value</th>
                <th className="text-right pb-3">Share</th>
              </tr>
            </thead>
            <tbody>
              {section.entries.map((e, idx) => (
                <tr
                  key={e.label}
                  className="border-t border-[#2a2e42]/60 hover:bg-white/3 transition-colors"
                  onMouseEnter={() => setActiveIdx(idx)}
                  onMouseLeave={() => setActiveIdx(null)}
                >
                  <td className="py-2.5 pr-6">
                    <div className="flex items-center gap-2.5">
                      <span
                        className="w-2 h-2 rounded-full flex-shrink-0"
                        style={{ backgroundColor: sectionColor(idx) }}
                      />
                      <span className="text-slate-200" title={e.label}>
                        {e.label}
                      </span>
                    </div>
                  </td>
                  <td className="py-2.5 pr-6 text-right text-slate-400 font-mono whitespace-nowrap">
                    {formatValue(e.value)}
                  </td>
                  <td className="py-2.5 text-right">
                    <div className="flex items-center justify-end gap-2">
                      <div className="w-20 h-1.5 bg-white/10 rounded-full overflow-hidden">
                        <div
                          className="h-full rounded-full"
                          style={{
                            width: `${Math.min(e.percentage, 100)}%`,
                            backgroundColor: sectionColor(idx),
                          }}
                        />
                      </div>
                      <span className="text-slate-300 w-12 text-right tabular-nums">
                        {e.percentage.toFixed(1)}%
                      </span>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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
  const [currency, setCurrency] = useState('USD')
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
    <div className="min-h-screen bg-[#0d0f1a] text-slate-200 flex flex-col">
      <NavBar />

      <div className="flex-1 flex justify-center px-8 py-10">
      <div className="w-full max-w-6xl">
        {/* Header */}
        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between mb-10 gap-4">
          <div>
            <h1 className="text-2xl font-bold bg-gradient-to-r from-indigo-400 to-purple-400 bg-clip-text text-transparent">
              Portfolio Breakdown
            </h1>
            <p className="text-sm text-slate-500 mt-1">
              Drill into your holdings by type, asset, country, and sector — ETF weights sourced from Yahoo Finance.
            </p>
          </div>

          {/* Currency selector */}
          <div className="flex items-center gap-1 bg-[#1a1d2e] border border-[#2a2e42] rounded-xl p-1">
            {CURRENCIES.map((c) => (
              <button
                key={c}
                id={`breakdown-currency-${c}`}
                onClick={() => setCurrency(c)}
                className={`px-4 py-1.5 rounded-lg text-sm font-semibold transition-all duration-150 ${
                  currency === c
                    ? 'bg-indigo-500/25 text-indigo-300'
                    : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                {c}
              </button>
            ))}
          </div>
        </div>

        {/* States */}
        {loading && (
          <div className="flex items-center justify-center py-24 gap-3 text-slate-400">
            <div className="w-5 h-5 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin" />
            Loading breakdown…
          </div>
        )}

        {error && !loading && (
          <div className="bg-red-500/10 border border-red-500/30 rounded-xl px-5 py-4 text-red-400 text-sm">
            {error}
          </div>
        )}

        {!loading && !error && sections.length === 0 && (
          <div className="text-center text-slate-500 py-24">
            No portfolio data. Upload a FlexQuery file to get started.
          </div>
        )}

        {!loading && !error && sections.length > 0 && (
          <>
            {/* Data freshness note */}
            <div className="mb-6 text-xs text-slate-600">
              Fundamentals and ETF sector/country weights are fetched asynchronously in the background.
              Newly added holdings may show as "Unknown" until data has been retrieved.
            </div>
            <div className="flex flex-col gap-8">
              {sections.map((s) => (
                <SectionCard key={s.title} section={s} formatValue={fmt} />
              ))}
            </div>
          </>
        )}
      </div>
      </div>
    </div>
  )
}
