import { useState, useRef, useEffect } from 'react'
import ReactMarkdown from 'react-markdown'
import NavBar from '../components/NavBar'
import HoverTooltip from '../components/HoverTooltip'
import { useLocation } from 'react-router-dom'
import { postLLMChat, getPortfolioValue } from '../api'

interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
}

interface WeightRow {
  symbol: string
  weight: number
}

type ModelChoice = 'flash' | 'pro'

function WeightsModal({
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
      <div className="w-full max-w-sm mx-4 bg-[#13151f] border border-white/8 rounded-2xl shadow-2xl flex flex-col overflow-hidden">
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
            <p className="text-sm text-slate-500 text-center py-10">No portfolio data. Upload a FlexQuery first.</p>
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
                            className="w-20 px-3 py-1.5 pr-6 bg-[#1a1d2e] border border-[#2a2e42]/60 rounded-xl text-sm text-slate-200 text-right focus:outline-none focus:ring-2 focus:ring-indigo-500/40 transition-all [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                          />
                          <div className="absolute right-1.5 flex flex-col">
                            <button
                              type="button"
                              tabIndex={-1}
                              onClick={() => onWeightStep(idx, 0.1)}
                              className="flex items-center justify-center w-4 h-3.5 text-slate-600 hover:text-slate-300 transition-colors"
                            >
                              <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 4L4 1L7 4"/></svg>
                            </button>
                            <button
                              type="button"
                              tabIndex={-1}
                              onClick={() => onWeightStep(idx, -0.1)}
                              className="flex items-center justify-center w-4 h-3.5 text-slate-600 hover:text-slate-300 transition-colors"
                            >
                              <svg width="8" height="5" viewBox="0 0 8 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M1 1L4 4L7 1"/></svg>
                            </button>
                          </div>
                        </div>
                        <span className="text-xs text-slate-600">%</span>
                      </div>
                    </td>
                    <td className="pr-4 py-3 text-center">
                      <button
                        onClick={() => onRemove(idx)}
                        className="text-slate-700 hover:text-rose-400 transition-colors opacity-0 group-hover:opacity-100"
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
          <p className="text-xs text-slate-600">
            Changes apply to the next freeform message only. Canned prompts always use live data.
          </p>
        </div>
      </div>
    </div>
  )
}

const markdownComponents = {
  h1: ({ children }: any) => <p className="font-bold text-white mt-3 mb-1">{children}</p>,
  h2: ({ children }: any) => <p className="font-bold text-white mt-3 mb-1">{children}</p>,
  h3: ({ children }: any) => <p className="font-semibold text-white/90 mt-2 mb-1">{children}</p>,
  p: ({ children }: any) => <p className="mb-2 last:mb-0">{children}</p>,
  ul: ({ children }: any) => <ul className="list-disc list-inside mb-2 space-y-0.5">{children}</ul>,
  ol: ({ children }: any) => <ol className="list-decimal list-inside mb-2 space-y-0.5">{children}</ol>,
  li: ({ children }: any) => <li className="ml-2">{children}</li>,
  strong: ({ children }: any) => <strong className="text-white font-semibold">{children}</strong>,
  em: ({ children }: any) => <em className="text-indigo-200">{children}</em>,
  code: ({ children }: any) => <code className="bg-white/10 rounded px-1 text-xs font-mono">{children}</code>,
}

function AssistantMessage({ content }: { content: string }) {
  const parts = content.split(/(<thinking>[\s\S]*?(?:<\/thinking>|$))/i)

  return (
    <>
      {parts.map((part, index) => {
        if (!part) return null
        if (part.toLowerCase().startsWith('<thinking>')) {
          const innerContent = part.replace(/^<thinking>/i, '').replace(/<\/thinking>$/i, '').trim()
          if (!innerContent) return null
          return (
            <details key={index} className="mb-3 group cursor-pointer">
              <summary className="text-xs text-indigo-400/60 hover:text-indigo-400 select-none mb-1 transition-colors outline-none flex items-center gap-1.5 font-medium list-none [&::-webkit-details-marker]:hidden">
                <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="opacity-70 group-open:rotate-90 transition-transform">
                  <path d="M4 2l4 4-4 4"/>
                </svg>
                AI Thinking Process
              </summary>
              <div className="pl-3 mt-2 ml-1.5 border-l-2 border-white/10 text-slate-400 text-xs opacity-80 pb-2 cursor-auto">
                <ReactMarkdown components={markdownComponents}>
                  {innerContent}
                </ReactMarkdown>
              </div>
            </details>
          )
        }

        return (
          <ReactMarkdown key={index} components={markdownComponents}>
            {part}
          </ReactMarkdown>
        )
      })}
    </>
  )
}

export default function LLMPage() {
  const location = useLocation()
  const initialMessages = (location.state as { initialMessages?: ChatMessage[] } | null)?.initialMessages ?? []
  const [messages, setMessages] = useState<ChatMessage[]>(initialMessages)
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)

  // Settings
  const [model, setModel] = useState<ModelChoice>('pro')
  const [includePortfolio, setIncludePortfolio] = useState(true)
  const [weights, setWeights] = useState<WeightRow[]>([])
  const [liveWeights, setLiveWeights] = useState<WeightRow[]>([])
  const [weightsLoading, setWeightsLoading] = useState(false)
  const [weightsOpen, setWeightsOpen] = useState(false)
  const [portfolioTotals, setPortfolioTotals] = useState<{ CZK: number; USD: number; EUR: number } | null>(null)
  const [portfolioShared, setPortfolioShared] = useState(initialMessages.length > 0)

  const endRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // Fetch live portfolio weights on mount
  useEffect(() => {
    setWeightsLoading(true)
    Promise.all([
      getPortfolioValue('USD', 'spot'),
      getPortfolioValue('CZK', 'spot'),
      getPortfolioValue('EUR', 'spot'),
    ])
      .then(([usd, czk, eur]) => {
        if (usd.value === 0) return
        const rows: WeightRow[] = usd.positions
          .filter(p => p.value > 0)
          .map(p => ({ symbol: p.symbol, weight: parseFloat(((p.value / usd.value) * 100).toFixed(1)) }))
          .sort((a, b) => b.weight - a.weight)
        setLiveWeights(rows)
        setWeights(rows)
        setPortfolioTotals({ USD: usd.value, CZK: czk.value, EUR: eur.value })
      })
      .catch(() => {})
      .finally(() => setWeightsLoading(false))
  }, [])

  const resetWeights = () => setWeights(liveWeights.map(r => ({ ...r })))

  const handleWeightChange = (idx: number, val: string) => {
    const n = parseFloat(val)
    if (isNaN(n)) return
    setWeights(prev => prev.map((r, i) => i === idx ? { ...r, weight: n } : r))
  }

  const handleWeightStep = (idx: number, delta: number) => {
    setWeights(prev => prev.map((r, i) =>
      i === idx ? { ...r, weight: Math.max(0, Math.round((r.weight + delta) * 10) / 10) } : r
    ))
  }

  const removeWeight = (idx: number) => setWeights(prev => prev.filter((_, i) => i !== idx))

  const isModified = weights.some((r, i) => liveWeights[i]?.symbol !== r.symbol || liveWeights[i]?.weight !== r.weight)
    || weights.length !== liveWeights.length

  const handleSend = async (message: string, promptType = 'freeform', displayMessage?: string) => {
    if (!message && promptType === 'freeform') return

    const isCanned = promptType !== 'freeform'
    const priorMessages = messages // capture before state update — becomes history
    if (isCanned || includePortfolio) setPortfolioShared(true)
    setMessages(prev => [...prev, { role: 'user', content: displayMessage || message }])
    setInput('')
    setLoading(true)

    try {
      const req: Parameters<typeof postLLMChat>[0] = {
        prompt_type: promptType,
        message: promptType === 'freeform' ? message : '',
        currency: 'USD',
        model,
      }
      if (!isCanned) {
        req.include_portfolio = includePortfolio
        if (includePortfolio && weights.length > 0) {
          req.custom_weights = weights.map(r => ({ symbol: r.symbol, weight: r.weight }))
        }
        if (priorMessages.length > 0) {
          req.history = priorMessages.map(m => ({ role: m.role, content: m.content }))
        }
      }

      const res = await postLLMChat(req)
      setMessages(prev => [...prev, { role: 'assistant', content: res.response }])
    } catch (err: any) {
      const errMsg = err?.message?.includes('GEMINI_API_KEY')
        ? 'LLM features are currently unavailable. Please configure GEMINI_API_KEY.'
        : err?.message || 'Failed to generate response.'
      setMessages(prev => [...prev, { role: 'assistant', content: `**Error:** ${errMsg}` }])
    } finally {
      setLoading(false)
    }
  }

  const cannedDisabled = loading || !includePortfolio

  return (
    <div className="h-screen bg-[#0f1117] flex flex-col overflow-hidden">
      <NavBar />

      {weightsOpen && (
        <WeightsModal
          weights={weights}
          weightsLoading={weightsLoading}
          portfolioTotals={portfolioTotals}
          onWeightChange={handleWeightChange}
          onWeightStep={handleWeightStep}
          onRemove={removeWeight}
          onReset={resetWeights}
          onClose={() => setWeightsOpen(false)}
        />
      )}

      <div className="flex-1 max-w-4xl w-full mx-auto p-4 flex flex-col gap-4 overflow-hidden mb-6">

        {/* Header */}
        <div className="flex flex-col gap-2 shrink-0">
          <div className="flex items-center justify-between">
            <h2 className="text-xl font-bold text-white tracking-tight">AI Portfolio Insights</h2>
            {messages.length > 0 && (
              <div className="relative group flex items-center">
                <button
                  onClick={() => { setMessages([]); setPortfolioShared(false) }}
                  disabled={loading}
                  className="text-slate-500 hover:text-slate-300 disabled:opacity-40 transition-colors"
                  aria-label="New chat"
                >
                  <svg width="15" height="15" viewBox="0 0 15 15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M7.5 1.5h-5a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-5" />
                    <path d="M11 1l-5 5v2h2l5-5-2-2Z" />
                  </svg>
                </button>
                <HoverTooltip direction="down" align="right" className="w-max whitespace-nowrap">
                  New chat
                </HoverTooltip>
              </div>
            )}
          </div>
          <p className="text-sm text-slate-400">Ask a question or select an analysis mode to get insights based on your portfolio positions.</p>
          <div className="flex flex-wrap gap-3 mt-2">
            <button
              onClick={() => handleSend('', 'general_analysis', 'Analyze my current portfolio given current market conditions. What am I effectively betting on?')}
              disabled={cannedDisabled}
              title={!includePortfolio ? 'Enable "Include portfolio" to use canned prompts' : undefined}
              className={`transition-colors text-sm font-medium ${cannedDisabled ? 'text-indigo-300/40 cursor-not-allowed' : 'text-indigo-300 hover:text-indigo-200'}`}
            >
              General Analysis
            </button>
            <button
              onClick={() => handleSend('', 'best_worst_scenarios', 'Analyze my current portfolio. What are the best and worst realistic scenarios?')}
              disabled={cannedDisabled}
              title={!includePortfolio ? 'Enable "Include portfolio" to use canned prompts' : undefined}
              className={`transition-colors text-sm font-medium ${cannedDisabled ? 'text-emerald-400/40 cursor-not-allowed' : 'text-emerald-400 hover:text-emerald-300'}`}
            >
              Scenario Analysis
            </button>
          </div>
        </div>

        {/* Chat Area */}
        <div className="flex-1 p-4 overflow-y-auto flex flex-col gap-6 scrollbar-thin scrollbar-track-transparent scrollbar-thumb-white/10">
          {messages.length === 0 && !loading && (
            <div className="flex-1 flex items-center justify-center text-slate-500 text-sm">
              Your insights will appear here
            </div>
          )}

          {messages.map((msg, i) => (
            <div key={i} className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}>
              <div className={`max-w-[85%] px-5 py-3 text-sm leading-relaxed
                ${msg.role === 'user' ? 'text-indigo-300 font-medium whitespace-pre-wrap' : 'text-indigo-100/90'}`}>
                {msg.role === 'assistant'
                  ? <AssistantMessage content={msg.content} />
                  : msg.content
                }
              </div>
            </div>
          ))}

          {loading && (
            <div className="flex justify-start">
              <div className="relative group px-5 py-4 flex items-center gap-2 cursor-default">
                <div className="w-2 h-2 rounded-full bg-indigo-500/50 animate-bounce" style={{ animationDelay: '0ms' }} />
                <div className="w-2 h-2 rounded-full bg-indigo-500 animate-bounce" style={{ animationDelay: '150ms' }} />
                <div className="w-2 h-2 rounded-full bg-indigo-500 animate-bounce" style={{ animationDelay: '300ms' }} />
                <HoverTooltip align="none" className="left-5 w-max whitespace-nowrap">
                  Model is thinking. This may take over a minute.
                </HoverTooltip>
              </div>
            </div>
          )}

          <div ref={endRef} />
        </div>

        {/* Input */}
        <form
          className="shrink-0 flex items-center gap-2"
          onSubmit={e => { e.preventDefault(); handleSend(input) }}
        >
          <input
            type="text"
            value={input}
            onChange={e => setInput(e.target.value)}
            disabled={loading}
            placeholder="Ask a question about your portfolio..."
            className="flex-1 bg-transparent border-b border-indigo-500/30 px-4 py-3 text-sm text-white focus:outline-none focus:border-indigo-400 transition-colors disabled:opacity-50"
          />
          <button
            type="submit"
            disabled={loading || !input.trim()}
            className="text-indigo-400 hover:text-indigo-300 disabled:opacity-50 transition-colors font-medium text-sm select-none flex items-center justify-center pt-2"
          >
            Send
          </button>
        </form>

        {/* Settings Panel */}
        <div className={`shrink-0 flex flex-col gap-2 -mt-2 ${loading ? 'opacity-50 pointer-events-none' : ''}`}>

          <div className="flex items-center gap-6">
            {/* Model toggle */}
            <div className="flex items-center gap-2">
              <span className="text-[11px] uppercase font-bold text-slate-500 tracking-wide">Model</span>
              <button
                onClick={() => setModel('flash')}
                title="Faster responses, lower quality"
                className={`text-xs px-2 py-0.5 rounded transition-all ${model === 'flash' ? 'text-indigo-300 font-semibold' : 'text-slate-500 hover:text-slate-300'}`}
              >
                Flash
              </button>
              <button
                onClick={() => setModel('pro')}
                title="Slower but more thorough analysis"
                className={`text-xs px-2 py-0.5 rounded transition-all ${model === 'pro' ? 'text-indigo-300 font-semibold' : 'text-slate-500 hover:text-slate-300'}`}
              >
                Pro
              </button>
            </div>

            {/* Include portfolio */}
            <label
              className={`relative group flex items-center gap-2 select-none ${portfolioShared ? 'cursor-default' : 'cursor-pointer'}`}
            >
              <input
                type="checkbox"
                checked={portfolioShared || includePortfolio}
                disabled={portfolioShared}
                onChange={e => !portfolioShared && setIncludePortfolio(e.target.checked)}
                className="accent-indigo-400 w-3 h-3 disabled:opacity-60"
              />
              <span className={`text-[11px] uppercase font-bold tracking-wide ${portfolioShared ? 'text-slate-600' : 'text-slate-500'}`}>Include portfolio</span>
              {portfolioShared && (
                <HoverTooltip className="w-60">
                  Portfolio data has already been shared in this conversation and cannot be removed from context
                </HoverTooltip>
              )}
            </label>

            {/* Adjust weights link */}
            {includePortfolio && (
              <button
                onClick={() => setWeightsOpen(true)}
                className="flex items-center gap-1 text-[11px] text-indigo-400/60 hover:text-indigo-400 transition-colors"
              >
                Adjust weights
                {isModified && <span className="w-1.5 h-1.5 rounded-full bg-indigo-400 ml-0.5" />}
              </button>
            )}
          </div>

          {/* Disclosure */}
          <p className="text-[10px] text-slate-600 leading-relaxed">
            Your portfolio weights (symbol, name, allocation %) are sent to the Gemini API by Google for analysis. No account IDs or personal details are included.
          </p>
        </div>

      </div>
    </div>
  )
}
