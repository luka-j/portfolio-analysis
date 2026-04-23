import { useState, useRef, useEffect } from 'react'
import { useScenario } from '../context/ScenarioContext'
import HoverTooltip from './HoverTooltip'

interface Props {
  disabled?: boolean
  onCompare: (targetId: number) => void
}

// CompareScenariosChip is a quick-action chip on the LLM page that lets the user pick a
// comparison target (Real portfolio or another scenario) and immediately kick off a
// scenarios/compare-llm run. It replaces the old navbar ComparePill.
export default function CompareScenariosChip({ disabled, onCompare }: Props) {
  const { active, compare, scenarios, setCompare } = useScenario()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const activeLabel = active === null ? 'Real' : (scenarios.find(s => s.id === active)?.name ?? `Scenario ${active}`)
  const compareScenario = compare !== null ? scenarios.find(s => s.id === compare) ?? null : null
  const compareLabel = compare === 0 ? 'Real' : compareScenario?.name ?? (compare !== null ? `Scenario ${compare}` : null)

  // Build option list: Real (if active isn't already Real) and each scenario except the active one.
  const options: { label: string; id: number }[] = []
  if (active !== null) options.push({ label: 'Real portfolio', id: 0 })
  for (const s of scenarios) {
    if (s.id !== active) options.push({ label: s.name || `Scenario ${s.id}`, id: s.id })
  }

  const hasTarget = compare !== null
  const chipLabel = hasTarget
    ? `⚡ Compare: ${activeLabel} vs. ${compareLabel}`
    : '⚡ Compare Scenarios'

  function handlePick(id: number) {
    setCompare(id)
    setOpen(false)
    onCompare(id)
  }

  function handleClear() {
    setCompare(null)
    setOpen(false)
  }

  return (
    <div className="relative group flex items-center" ref={ref}>
      <button
        onClick={() => setOpen(o => !o)}
        disabled={disabled || options.length === 0}
        className={`flex items-center gap-1 transition-all text-xs font-medium px-3 py-1.5 rounded-full border active:scale-95 disabled:opacity-40 disabled:cursor-not-allowed ${
          hasTarget
            ? 'text-amber-300 border-amber-500/25 bg-amber-500/8 hover:bg-amber-500/15 hover:border-amber-500/40'
            : 'text-amber-400/80 border-amber-500/20 bg-amber-500/5 hover:bg-amber-500/10 hover:border-amber-500/30 hover:text-amber-300'
        }`}
      >
        {chipLabel}
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      {!open && (
        <HoverTooltip direction="up" align="center" className="w-64">
          {options.length === 0
            ? 'Create a scenario to compare against the real portfolio.'
            : 'Pick a portfolio to compare against. Gemini Pro will generate a qualitative head-to-head on risk-adjusted return, drawdown, composition, and suitability.'}
        </HoverTooltip>
      )}

      {open && options.length > 0 && (
        <div className="absolute bottom-full mb-2 left-0 z-50 w-64 rounded-2xl bg-panel border border-white/8 shadow-2xl backdrop-blur-2xl overflow-hidden">
          <div className="px-4 py-2 text-[10px] uppercase tracking-wide text-slate-500 border-b border-white/5">
            Compare {activeLabel} vs…
          </div>
          {options.map(opt => (
            <button
              key={opt.id}
              onClick={() => handlePick(opt.id)}
              className={`w-full flex items-center gap-2 px-4 py-2.5 text-sm text-left transition-colors ${
                compare === opt.id ? 'text-amber-300 bg-amber-500/10' : 'text-slate-300 hover:bg-white/5'
              }`}
            >
              <span className="flex-1 truncate">{opt.label}</span>
              {compare === opt.id && (
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              )}
            </button>
          ))}
          {hasTarget && (
            <button
              onClick={handleClear}
              className="w-full flex items-center gap-2 px-4 py-2.5 text-sm text-slate-400 hover:text-white hover:bg-white/5 transition-colors border-t border-white/5"
            >
              Turn off comparison
            </button>
          )}
        </div>
      )}
    </div>
  )
}
