import { AVAILABLE_TOOLS } from '../constants/tools'

interface Props {
  enabledTools: string[]
  onToggle: (id: string) => void
  onToggleAll: (enabled: boolean) => void
  onClose: () => void
}

export default function ToolsModal({ enabledTools, onToggle, onToggleAll, onClose }: Props) {
  const allEnabled = enabledTools.length === AVAILABLE_TOOLS.length

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      {/* Click-away backdrop */}
      <div className="absolute inset-0" onClick={onClose} />

      <div className="relative w-full max-w-sm mx-4 bg-panel/95 backdrop-blur-xl border border-white/8 rounded-2xl shadow-2xl flex flex-col overflow-hidden">
        
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-white/5">
          <div>
            <h3 className="text-sm font-semibold text-slate-100">Agent Tools</h3>
            <p className="text-xs text-slate-500 mt-0.5">Select which tools the LLM can autonomously invoke.</p>
          </div>
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

        {/* Content */}
        <div className="overflow-y-auto max-h-105 px-6 py-4 space-y-4">
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs font-medium text-slate-400">
              {enabledTools.length} of {AVAILABLE_TOOLS.length} enabled
            </span>
            <button
              onClick={() => onToggleAll(!allEnabled)}
              className="text-xs font-semibold text-indigo-400 hover:text-indigo-300 transition-colors"
            >
              {allEnabled ? 'Disable All' : 'Enable All'}
            </button>
          </div>
          
          <div className="flex flex-col gap-3">
            {AVAILABLE_TOOLS.map(t => {
              const checked = enabledTools.includes(t.id)
              return (
                <label key={t.id} className="flex gap-4 items-start cursor-pointer group">
                  <div className="mt-0.5">
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => onToggle(t.id)}
                      className="accent-indigo-400/80 w-3.5 h-3.5 cursor-pointer"
                    />
                  </div>
                  <div className="flex flex-col">
                    <span className="text-sm font-medium text-slate-200 group-hover:text-slate-100 transition-colors">{t.label}</span>
                    <span className="text-xs text-slate-500 mt-0.5">{t.description}</span>
                  </div>
                </label>
              )
            })}
          </div>
        </div>

      </div>
    </div>
  )
}
