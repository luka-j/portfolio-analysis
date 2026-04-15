import { useState, useRef, useEffect } from 'react'
import NavBar from '../components/NavBar'
import HoverTooltip from '../components/HoverTooltip'
import WeightsModal, { type WeightRow } from '../components/WeightsModal'
import AssistantMessage from '../components/AssistantMessage'
import { useLocation } from 'react-router-dom'
import { postLLMChat, getPortfolioValue, type LLMChatRequest, type LLMResponseSection } from '../api'

interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  cached?: boolean
  sections?: LLMResponseSection[]
  originalRequest?: {
    message: string
    promptType: string
    displayMessage?: string
    extraParams?: Partial<LLMChatRequest>
  }
}

type ModelChoice = 'flash' | 'pro'

const loadingLabelMap: Record<string, string> = {
  general_analysis: 'Analyzing portfolio positioning…',
  best_worst_scenarios: 'Stress-testing scenarios…',
  risk_metrics: 'Interpreting risk metrics…',
  benchmark_analysis: 'Comparing against benchmark…',
  upcoming_events: 'Scanning for upcoming events…',
  add_or_trim: 'Evaluating capital allocation…',
  long_market_summary: 'Pulling market data…',
  ticker_analysis: 'Researching ticker…',
}

const examplePrompts = [
  'How exposed am I to rising interest rates?',
  'Which of my holdings are most correlated?',
  'Explain the risk metrics for my top 5 positions.',
]

export default function LLMPage() {
  const location = useLocation()
  const locationState = location.state as { initialMessages?: ChatMessage[]; initialPrompt?: { promptType?: string; message?: string; displayMessage: string; extraParams?: Partial<LLMChatRequest> } } | null
  const initialMessages = locationState?.initialMessages ?? []
  const initialPrompt = locationState?.initialPrompt
  const [messages, setMessages] = useState<ChatMessage[]>(initialMessages)
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [loadingLabel, setLoadingLabel] = useState('')

  // Settings
  const [model, setModel] = useState<ModelChoice>('flash')
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

  const autoSentRef = useRef(false)
  useEffect(() => {
    if (initialPrompt && !autoSentRef.current) {
      autoSentRef.current = true
      handleSend(initialPrompt.message ?? '', initialPrompt.promptType ?? 'freeform', initialPrompt.displayMessage, initialPrompt.extraParams)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
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

  const handleSend = async (message: string, promptType = 'freeform', displayMessage?: string, extraParams?: Partial<LLMChatRequest>, usePageModel = false) => {
    if (!message && promptType === 'freeform') return

    const isCanned = promptType !== 'freeform'
    const priorMessages = messages // capture before state update — becomes history
    if (isCanned || includePortfolio) setPortfolioShared(true)
    setMessages(prev => [...prev, { role: 'user', content: displayMessage || message, originalRequest: { message, promptType, displayMessage, extraParams } }])
    setInput('')
    setLoading(true)
    setLoadingLabel(loadingLabelMap[promptType] ?? 'Thinking…')

    try {
      const req: Parameters<typeof postLLMChat>[0] = {
        prompt_type: promptType,
        message: promptType === 'freeform' ? message : '',
        currency: 'USD',
        ...(usePageModel || !isCanned ? { model } : {}),
        ...extraParams,
      }
      if (!isCanned) {
        req.include_portfolio = includePortfolio
        if (includePortfolio && weights.length > 0) {
          req.override_portfolio_weights = weights.map(r => ({ symbol: r.symbol, weight: r.weight }))
        }
        if (priorMessages.length > 0) {
          req.history = priorMessages.map(m => ({ role: m.role, content: m.content }))
        }
      }

      let initialized = false;
      const res = await postLLMChat(req, (chunkText) => {
        if (!initialized) {
          setLoading(false)
          initialized = true
          setMessages(prev => [...prev, { role: 'assistant', content: chunkText, cached: false }])
        } else {
          setMessages(prev => {
            const newMessages = [...prev]
            newMessages[newMessages.length - 1] = { ...newMessages[newMessages.length - 1], content: chunkText }
            return newMessages
          })
        }
      })

      if (!initialized) {
        setLoading(false)
        setMessages(prev => [...prev, { role: 'assistant', content: res.response, cached: res.cached, sections: res.sections }])
      } else {
        setMessages(prev => {
          const newMessages = [...prev]
          newMessages[newMessages.length - 1] = { ...newMessages[newMessages.length - 1], content: res.response, cached: res.cached, sections: res.sections }
          return newMessages
        })
      }
    } catch (err) {
      setLoading(false)
      const error = err as Error
      const errMsg = error?.message?.includes('GEMINI_API_KEY')
        ? 'LLM features are currently unavailable. Please configure GEMINI_API_KEY.'
        : error?.message || 'Failed to generate response.'
      setMessages(prev => [...prev, { role: 'assistant', content: `**Error:** ${errMsg}` }])
    }
  }

  const handleRegenerate = async (idx: number) => {
    const userMsg = messages[idx - 1]
    if (!userMsg || !userMsg.originalRequest) return
    const { message, promptType, extraParams } = userMsg.originalRequest

    const isCanned = promptType !== 'freeform'
    const priorMessages = messages.slice(0, idx - 1)

    setMessages(prev => prev.slice(0, idx)) // Drop the assistant message
    setLoading(true)
    setLoadingLabel(loadingLabelMap[promptType] ?? 'Thinking…')

    try {
      const req: Parameters<typeof postLLMChat>[0] = {
        prompt_type: promptType,
        message: promptType === 'freeform' ? message : '',
        currency: 'USD',
        model,
        force_refresh: true, // Bypass cache
        ...extraParams,
      }
      if (!isCanned) {
        req.include_portfolio = includePortfolio
        if (includePortfolio && weights.length > 0) {
          req.override_portfolio_weights = weights.map(r => ({ symbol: r.symbol, weight: r.weight }))
        }
        if (priorMessages.length > 0) {
          req.history = priorMessages.map(m => ({ role: m.role, content: m.content }))
        }
      }

      let initialized = false;
      const res = await postLLMChat(req, (chunkText) => {
        if (!initialized) {
          setLoading(false)
          initialized = true
          setMessages(prev => [...prev, { role: 'assistant', content: chunkText, cached: false }])
        } else {
          setMessages(prev => {
            const newMessages = [...prev]
            newMessages[newMessages.length - 1] = { ...newMessages[newMessages.length - 1], content: chunkText }
            return newMessages
          })
        }
      })

      if (!initialized) {
        setLoading(false)
        setMessages(prev => [...prev, { role: 'assistant', content: res.response, cached: res.cached, sections: res.sections }])
      } else {
        setMessages(prev => {
          const newMessages = [...prev]
          newMessages[newMessages.length - 1] = { ...newMessages[newMessages.length - 1], content: res.response, cached: res.cached, sections: res.sections }
          return newMessages
        })
      }
    } catch (err) {
      setLoading(false)
      const error = err as Error
      const errMsg = error?.message?.includes('GEMINI_API_KEY')
        ? 'LLM features are currently unavailable. Please configure GEMINI_API_KEY.'
        : error?.message || 'Failed to regenerate response.'
      setMessages(prev => [...prev, { role: 'assistant', content: `**Error:** ${errMsg}` }])
    }
  }

  const cannedDisabled = loading || !includePortfolio

  return (
    <div className="min-h-screen md:h-screen bg-bg flex flex-col md:overflow-hidden">
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

      <div className="flex-1 max-w-4xl w-full mx-auto p-4 flex flex-col gap-4 md:overflow-hidden mb-6 min-h-0">

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

          {/* Quick-action chips (#1) */}
          <div className="flex flex-wrap gap-2 mt-1">
            {([
              {
                label: 'What Am I Betting On?', promptType: 'general_analysis',
                display: 'Analyze my current portfolio given current market conditions. What am I effectively betting on?',
                active: 'text-indigo-300 border-indigo-500/25 bg-indigo-500/8 hover:bg-indigo-500/15 hover:border-indigo-500/40',
                disabled: 'text-indigo-400/30 border-indigo-500/10',
              },
              {
                label: 'Best & Worst Scenarios', promptType: 'best_worst_scenarios',
                display: 'Analyze my current portfolio. What are the best and worst realistic scenarios?',
                active: 'text-emerald-300 border-emerald-500/25 bg-emerald-500/8 hover:bg-emerald-500/15 hover:border-emerald-500/40',
                disabled: 'text-emerald-400/30 border-emerald-500/10',
              },
              {
                label: 'Upcoming Events', promptType: 'upcoming_events',
                display: 'What upcoming events (earnings, macroeconomic data, world events) might impact my portfolio?',
                active: 'text-amber-300 border-amber-500/25 bg-amber-500/8 hover:bg-amber-500/15 hover:border-amber-500/40',
                disabled: 'text-amber-400/30 border-amber-500/10',
              },
              {
                label: 'Add or Trim?', promptType: 'add_or_trim',
                display: 'Which of my holdings should I add to, and which should I trim?',
                active: 'text-rose-300 border-rose-500/25 bg-rose-500/8 hover:bg-rose-500/15 hover:border-rose-500/40',
                disabled: 'text-rose-400/30 border-rose-500/10',
              },
            ] as const).map(({ label, promptType, display, active, disabled }) => (
              <button
                key={promptType}
                onClick={() => handleSend('', promptType, display, undefined, true)}
                disabled={cannedDisabled}
                title={!includePortfolio ? 'Enable "Include portfolio" to use canned prompts' : undefined}
                className={`transition-all text-xs font-medium px-3 py-1.5 rounded-full border ${cannedDisabled ? `${disabled} cursor-not-allowed` : `${active} active:scale-95`}`}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        {/* Chat Area */}
        <div className="flex-1 p-4 overflow-y-auto flex flex-col gap-6 scrollbar-thin scrollbar-track-transparent scrollbar-thumb-white/10">

          {/* Empty state (#7) */}
          {messages.length === 0 && !loading && (
            <div className="flex-1 flex flex-col items-center justify-center gap-4 text-center px-8">
              <p className="text-slate-500 text-sm">
                Select an analysis above, or ask anything about your portfolio below.
              </p>
              <div className="space-y-1.5 w-full max-w-xs">
                {examplePrompts.map(hint => (
                  <button
                    key={hint}
                    onClick={() => handleSend(hint)}
                    disabled={loading}
                    className="block w-full text-left px-3 py-2 rounded-lg border border-white/5 bg-white/2 text-slate-600 hover:bg-white/4 hover:text-slate-400 transition-colors text-xs"
                  >
                    "{hint}"
                  </button>
                ))}
              </div>
            </div>
          )}

          {messages.map((msg, i) => (
            <div key={i} className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}>
              <div className="relative max-w-[85%]">
                {msg.role === 'user' ? (
                  <div className="bg-indigo-500/8 rounded-2xl px-4 py-2.5 text-sm text-indigo-300 font-medium whitespace-pre-wrap leading-[1.8]">
                    {msg.content}
                  </div>
                ) : (
                  <div className="bg-white/2 rounded-2xl px-5 py-4 text-sm leading-[1.8] text-indigo-100/90">
                    <AssistantMessage content={msg.content} sections={msg.sections} />
                  </div>
                )}

                {/* Cached indicator (#6) — inline, always visible */}
                {msg.role === 'assistant' && msg.cached && (
                  <div className="flex items-center gap-2 mt-1.5 px-1">
                    <span className="text-[10px] text-slate-600">Cached</span>
                    {i === messages.length - 1 && (
                      <button
                        onClick={() => handleRegenerate(i)}
                        disabled={loading}
                        className="text-[10px] text-indigo-400/50 hover:text-indigo-400 disabled:opacity-30 transition-colors flex items-center gap-1"
                        title="Regenerate with fresh data"
                      >
                        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                          <polyline points="23 4 23 10 17 10" />
                          <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
                        </svg>
                        Refresh
                      </button>
                    )}
                  </div>
                )}
              </div>
            </div>
          ))}

          {/* Loading indicator with context label (#3) */}
          {loading && (
            <div className="flex justify-start">
              <div className="px-5 py-4 flex flex-col gap-2">
                <div className="flex items-center gap-2">
                  <div className="w-1.5 h-1.5 rounded-full bg-indigo-500/50 animate-bounce" style={{ animationDelay: '0ms' }} />
                  <div className="w-1.5 h-1.5 rounded-full bg-indigo-500 animate-bounce" style={{ animationDelay: '150ms' }} />
                  <div className="w-1.5 h-1.5 rounded-full bg-indigo-500 animate-bounce" style={{ animationDelay: '300ms' }} />
                </div>
                {loadingLabel && (
                  <p className="text-xs text-slate-500">{loadingLabel}</p>
                )}
              </div>
            </div>
          )}

          <div ref={endRef} />
        </div>

        {/* Input bar as contained box (#4) */}
        <form
          className="shrink-0 flex items-center gap-2 bg-white/2.5 border border-white/[0.07] rounded-xl px-3 py-1"
          onSubmit={e => { e.preventDefault(); handleSend(input) }}
        >
          <input
            type="text"
            value={input}
            onChange={e => setInput(e.target.value)}
            disabled={loading}
            placeholder="Ask a question about your portfolio…"
            className="flex-1 bg-transparent py-2.5 text-sm text-white focus:outline-none disabled:opacity-50 placeholder:text-slate-500"
          />
          <button
            type="submit"
            disabled={loading || !input.trim()}
            className="shrink-0 p-1.5 text-indigo-400 hover:text-indigo-300 disabled:opacity-30 transition-all active:scale-90"
            aria-label="Send"
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" />
            </svg>
          </button>
        </form>

        {/* Settings Panel */}
        <div className={`shrink-0 flex flex-col gap-2 -mt-2 ${loading ? 'opacity-50 pointer-events-none' : ''}`}>
          <div className="flex items-center gap-6">

            {/* Model selector as sliding pill toggle (#5) */}
            <div className="flex items-center gap-2">
              <span className="text-[11px] uppercase font-bold text-slate-500 tracking-wide">Model</span>
              <div className="relative flex text-xs rounded-full border border-white/8 bg-white/3 p-0.5">
                <div
                  className={`absolute inset-y-0.5 w-[calc(50%-2px)] rounded-full bg-indigo-500/20 border border-indigo-500/30 transition-transform duration-200 ${model === 'pro' ? 'translate-x-[calc(100%+4px)]' : 'translate-x-0.5'}`}
                />
                {(['flash', 'pro'] as const).map(m => (
                  <button
                    key={m}
                    onClick={() => setModel(m)}
                    title={m === 'flash' ? 'Faster responses' : 'More thorough analysis'}
                    className={`relative z-10 px-2.5 py-0.5 rounded-full capitalize transition-colors ${model === m ? 'text-indigo-300' : 'text-slate-500 hover:text-slate-300'}`}
                  >
                    {m}
                  </button>
                ))}
              </div>
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
              <span className="text-[11px] uppercase font-bold tracking-wide text-slate-500">Include portfolio</span>
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
          <p className="text-[10px] text-slate-500 leading-relaxed">
            Your portfolio weights (symbol, name, allocation %) are sent to the Gemini API by Google for analysis. No account IDs or personal details are included.
          </p>
        </div>

      </div>
    </div>
  )
}
