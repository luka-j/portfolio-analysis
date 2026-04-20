import { useState, useRef, useEffect, useCallback } from 'react'
import { usePersistentState } from '../utils/usePersistentState'
import NavBar from '../components/NavBar'
import HoverTooltip from '../components/HoverTooltip'
import ToolsModal, { AVAILABLE_TOOLS } from '../components/ToolsModal'
import AssistantMessage from '../components/AssistantMessage'
import { useLocation } from 'react-router-dom'
import { postLLMChat, type LLMChatRequest, type LLMResponseSection, type LLMToolCallEvent } from '../api'

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
  const [activeToolCall, setActiveToolCall] = useState<LLMToolCallEvent | null>(null)
  const toolCallTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const toolCallShownAt = useRef<number>(0)

  // Settings
  const [model, setModel] = useState<ModelChoice>('flash')
  const defaultTools = AVAILABLE_TOOLS.map(t => t.id).filter(id => ![
    'get_fx_impact',
    'get_historical_performance_series',
    'get_open_positions_with_cost_basis',
    'get_tax_impact'
  ].includes(id))
  const [enabledTools, setEnabledTools] = usePersistentState<string[]>('llm_enabled_tools', defaultTools)
  const [toolsModalOpen, setToolsModalOpen] = useState(false)
  const [portfolioShared, setPortfolioShared] = useState(initialMessages.length > 0)

  const endRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const autoSentRef = useRef(false)

  // clearToolCall removes the active tool call banner.
  // If force is true, it clears it immediately. Otherwise it enforces a minimum 3-second display.
  const clearToolCall = useCallback((force = false) => {
    if (toolCallTimerRef.current) clearTimeout(toolCallTimerRef.current)
    if (force) {
      setActiveToolCall(null)
      return
    }
    const elapsed = Date.now() - toolCallShownAt.current
    const remaining = Math.max(0, 3000 - elapsed)
    toolCallTimerRef.current = setTimeout(() => {
      setActiveToolCall(null)
    }, remaining)
  }, [])

  // showToolCall sets the active tool call banner and records the show time.
  const showToolCall = useCallback((event: LLMToolCallEvent) => {
    if (toolCallTimerRef.current) clearTimeout(toolCallTimerRef.current)
    toolCallShownAt.current = Date.now()
    setActiveToolCall(event)
  }, [])
  useEffect(() => {
    if (initialPrompt && !autoSentRef.current) {
      autoSentRef.current = true
      handleSend(initialPrompt.message ?? '', initialPrompt.promptType ?? 'freeform', initialPrompt.displayMessage, initialPrompt.extraParams)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleToolToggle = (id: string) => setEnabledTools(prev => prev.includes(id) ? prev.filter(t => t !== id) : [...prev, id])
  const handleToolToggleAll = (enable: boolean) => setEnabledTools(enable ? AVAILABLE_TOOLS.map(t => t.id) : [])

  const handleSend = async (message: string, promptType = 'freeform', displayMessage?: string, extraParams?: Partial<LLMChatRequest>, usePageModel = false) => {
    if (!message && promptType === 'freeform') return

    const isCanned = promptType !== 'freeform'
    const priorMessages = messages // capture before state update — becomes history
    if (isCanned || enabledTools.length > 0) setPortfolioShared(true)
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
      req.enabled_tools = enabledTools
      if (!isCanned) {
        if (priorMessages.length > 0) {
          req.history = priorMessages.map(m => ({ role: m.role, content: m.content }))
        }
      }

      let initialized = false;
      const res = await postLLMChat(req, (chunkText) => {
        clearToolCall()
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
      }, showToolCall)

      clearToolCall(true)

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
      setActiveToolCall(null)
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
        req.enabled_tools = enabledTools
        if (priorMessages.length > 0) {
          req.history = priorMessages.map(m => ({ role: m.role, content: m.content }))
        }
      }

      let initialized = false;
      const res = await postLLMChat(req, (chunkText) => {
        clearToolCall()
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
      }, showToolCall)

      clearToolCall(true)

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
      setActiveToolCall(null)
      const error = err as Error
      const errMsg = error?.message?.includes('GEMINI_API_KEY')
        ? 'LLM features are currently unavailable. Please configure GEMINI_API_KEY.'
        : error?.message || 'Failed to regenerate response.'
      setMessages(prev => [...prev, { role: 'assistant', content: `**Error:** ${errMsg}` }])
    }
  }



  return (
    <div className="min-h-screen md:h-screen bg-bg flex flex-col md:overflow-hidden">
      <NavBar />

      {toolsModalOpen && (
        <ToolsModal
          enabledTools={enabledTools}
          onToggle={handleToolToggle}
          onToggleAll={handleToolToggleAll}
          onClose={() => setToolsModalOpen(false)}
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
                requiredTool: 'get_current_allocations'
              },
              {
                label: 'Add or Trim?', promptType: 'add_or_trim',
                display: 'Where should I add weight and where should I trim in my current portfolio?',
                active: 'text-violet-300 border-violet-500/25 bg-violet-500/8 hover:bg-violet-500/15 hover:border-violet-500/40',
                disabled: 'text-violet-400/30 border-violet-500/10',
                requiredTool: 'get_current_allocations'
              },
              {
                label: 'Geographic & Sector Bottlenecks', promptType: 'geographic_sector_bottlenecks',
                display: 'Analyze my Geographic & Sector Bottlenecks.',
                active: 'text-cyan-300 border-cyan-500/25 bg-cyan-500/8 hover:bg-cyan-500/15 hover:border-cyan-500/40',
                disabled: 'text-cyan-400/30 border-cyan-500/10',
                requiredTool: 'get_current_allocations'
              },
              {
                label: 'Biggest Drag on Performance', promptType: 'biggest_drag_on_performance',
                display: 'Identify the Biggest Drag on Performance in my portfolio.',
                active: 'text-rose-300 border-rose-500/25 bg-rose-500/8 hover:bg-rose-500/15 hover:border-rose-500/40',
                disabled: 'text-rose-400/30 border-rose-500/10',
                requiredTool: 'get_open_positions_with_cost_basis'
              },
              {
                label: 'Stress Test vs Market', promptType: 'stress_test_beta',
                display: 'Conduct a Stress Test vs. The Market (Beta Analysis).',
                active: 'text-emerald-300 border-emerald-500/25 bg-emerald-500/8 hover:bg-emerald-500/15 hover:border-emerald-500/40',
                disabled: 'text-emerald-400/30 border-emerald-500/10',
                requiredTool: 'get_benchmark_metrics'
              },
              {
                label: 'Upcoming Events', promptType: 'upcoming_events',
                display: 'What upcoming events (earnings, macroeconomic data, world events) might impact my portfolio?',
                active: 'text-amber-300 border-amber-500/25 bg-amber-500/8 hover:bg-amber-500/15 hover:border-amber-500/40',
                disabled: 'text-amber-400/30 border-amber-500/10',
                requiredTool: undefined
              },
            ] as const).map(({ label, promptType, display, active, disabled, requiredTool }) => {
              const toolMissing = requiredTool && !enabledTools.includes(requiredTool);
              const chipDisabled = loading || toolMissing;
              
              return (
                <div key={promptType} className="relative group flex items-center">
                  <button
                    onClick={() => handleSend('', promptType, display, undefined, true)}
                    disabled={chipDisabled}
                    className={`transition-all text-xs font-medium px-3 py-1.5 rounded-full border ${chipDisabled ? `${disabled} cursor-not-allowed` : `${active} active:scale-95`}`}
                  >
                    {label}
                  </button>
                  {toolMissing && (
                    <HoverTooltip direction="up" align="center" className="w-max whitespace-nowrap z-50">
                      Requires '{AVAILABLE_TOOLS.find(t => t.id === requiredTool)?.label || requiredTool}' tool
                    </HoverTooltip>
                  )}
                </div>
              );
            })}
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

          {/* Loading indicator with context label + tool call banner */}
          {(loading || activeToolCall) && (
            <div className="flex justify-start">
              <div className="px-5 py-4 flex flex-col gap-2">
                {activeToolCall ? (
                  // Tool execution state — shown for at least 3 seconds
                  <div className="flex items-center gap-2.5">
                    <svg className="w-3.5 h-3.5 text-indigo-400 animate-spin" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-20" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" />
                      <path className="opacity-80" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                    </svg>
                    <p className="text-xs text-indigo-400">{activeToolCall.label}…</p>
                  </div>
                ) : (
                  // Generic thinking state
                  <div className="flex items-center gap-2">
                    <div className="w-1.5 h-1.5 rounded-full bg-indigo-500/50 animate-bounce" style={{ animationDelay: '0ms' }} />
                    <div className="w-1.5 h-1.5 rounded-full bg-indigo-500 animate-bounce" style={{ animationDelay: '150ms' }} />
                    <div className="w-1.5 h-1.5 rounded-full bg-indigo-500 animate-bounce" style={{ animationDelay: '300ms' }} />
                  </div>
                )}
                {(loading && !activeToolCall && loadingLabel) && (
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

            {/* Tools Enabled Toggle Button */}
            <button
              type="button"
              onClick={() => setToolsModalOpen(true)}
              disabled={portfolioShared}
              className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full border transition-colors ${portfolioShared ? 'border-white/5 opacity-50 cursor-default grayscale' : 'border-indigo-500/30 bg-indigo-500/10 hover:bg-indigo-500/20'}`}
              title={portfolioShared ? "Tools cannot be selectively disabled after the conversation has started" : "Configure which tools the LLM can use"}
            >
              <div className="flex items-center gap-1.5 opacity-80">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
                </svg>
                <span className="text-[11px] font-bold tracking-wide">
                  {enabledTools.length === AVAILABLE_TOOLS.length ? 'All tools enabled' : `${enabledTools.length} tools enabled`}
                </span>
              </div>
            </button>

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
