import { useState, useRef, useEffect } from 'react'
import NavBar from '../components/NavBar'
import HoverTooltip from '../components/HoverTooltip'
import WeightsModal, { type WeightRow } from '../components/WeightsModal'
import AssistantMessage from '../components/AssistantMessage'
import { useLocation } from 'react-router-dom'
import { postLLMChat, getPortfolioValue, type LLMChatRequest } from '../api'

interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  cached?: boolean
  originalRequest?: {
    message: string
    promptType: string
    displayMessage?: string
    extraParams?: Partial<LLMChatRequest>
  }
}

type ModelChoice = 'flash' | 'pro'


export default function LLMPage() {
  const location = useLocation()
  const locationState = location.state as { initialMessages?: ChatMessage[]; initialPrompt?: { promptType?: string; message?: string; displayMessage: string; extraParams?: Partial<LLMChatRequest> } } | null
  const initialMessages = locationState?.initialMessages ?? []
  const initialPrompt = locationState?.initialPrompt
  const [messages, setMessages] = useState<ChatMessage[]>(initialMessages)
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)

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
        setMessages(prev => [...prev, { role: 'assistant', content: res.response, cached: res.cached }])
      } else {
        setMessages(prev => {
          const newMessages = [...prev]
          newMessages[newMessages.length - 1] = { ...newMessages[newMessages.length - 1], content: res.response, cached: res.cached }
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
        setMessages(prev => [...prev, { role: 'assistant', content: res.response, cached: res.cached }])
      } else {
        setMessages(prev => {
          const newMessages = [...prev]
          newMessages[newMessages.length - 1] = { ...newMessages[newMessages.length - 1], content: res.response, cached: res.cached }
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
          <div className="flex flex-wrap gap-3 mt-2">
            <button
              onClick={() => handleSend('', 'general_analysis', 'Analyze my current portfolio given current market conditions. What am I effectively betting on?', undefined, true)}
              disabled={cannedDisabled}
              title={!includePortfolio ? 'Enable "Include portfolio" to use canned prompts' : undefined}
              className={`transition-colors text-sm font-medium ${cannedDisabled ? 'text-indigo-300/40 cursor-not-allowed' : 'text-indigo-300 hover:text-indigo-200'}`}
            >
              What Am I Betting On?
            </button>
            <button
              onClick={() => handleSend('', 'best_worst_scenarios', 'Analyze my current portfolio. What are the best and worst realistic scenarios?', undefined, true)}
              disabled={cannedDisabled}
              title={!includePortfolio ? 'Enable "Include portfolio" to use canned prompts' : undefined}
              className={`transition-colors text-sm font-medium ${cannedDisabled ? 'text-emerald-400/40 cursor-not-allowed' : 'text-emerald-400 hover:text-emerald-300'}`}
            >
              Best & Worst Scenarios
            </button>
            <button
              onClick={() => handleSend('', 'upcoming_events', 'What upcoming events (earnings, macroeconomic data, world events) might impact my portfolio?', undefined, true)}
              disabled={cannedDisabled}
              title={!includePortfolio ? 'Enable "Include portfolio" to use canned prompts' : undefined}
              className={`transition-colors text-sm font-medium ${cannedDisabled ? 'text-amber-400/40 cursor-not-allowed' : 'text-amber-400 hover:text-amber-300'}`}
            >
              Upcoming Events
            </button>
            <button
              onClick={() => handleSend('', 'add_or_trim', 'Which of my holdings should I add to, and which should I trim?', undefined, true)}
              disabled={cannedDisabled}
              title={!includePortfolio ? 'Enable "Include portfolio" to use canned prompts' : undefined}
              className={`transition-colors text-sm font-medium ${cannedDisabled ? 'text-rose-400/40 cursor-not-allowed' : 'text-rose-400 hover:text-rose-300'}`}
            >
              Add or Trim?
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
            <div key={i} className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'} group relative`}>
              <div className="relative max-w-[85%]">
                {msg.role === 'user' ? (
                  <div className="bg-indigo-500/8 rounded-2xl px-4 py-2.5 text-sm text-indigo-300 font-medium whitespace-pre-wrap leading-[1.8]">
                    {msg.content}
                  </div>
                ) : (
                  <div className="bg-white/[0.02] rounded-2xl px-5 py-4 text-sm leading-[1.8] text-indigo-100/90">
                    <AssistantMessage content={msg.content} />
                  </div>
                )}
                {msg.role === 'assistant' && msg.cached && i === messages.length - 1 && (
                  <button
                    onClick={() => handleRegenerate(i)}
                    className="absolute -right-8 top-1.5 p-1.5 text-indigo-400/40 hover:text-indigo-300 transition-colors opacity-0 group-hover:opacity-100 bg-surface rounded-md border border-indigo-500/10 shadow-sm"
                    title="Cached response. Click to force regenerate."
                  >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                      <polyline points="23 4 23 10 17 10" />
                      <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
                    </svg>
                  </button>
                )}
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
              <span className={`text-[11px] uppercase font-bold tracking-wide ${portfolioShared ? 'text-slate-500' : 'text-slate-500'}`}>Include portfolio</span>
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
