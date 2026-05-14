import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { LLMResponseSection } from '../api'

const markdownComponents = {
  h1: ({ children }: { children?: React.ReactNode }) => <p className="font-bold text-white mt-5 mb-2">{children}</p>,
  h2: ({ children }: { children?: React.ReactNode }) => <p className="font-bold text-white mt-5 mb-2">{children}</p>,
  h3: ({ children }: { children?: React.ReactNode }) => <h3 className="text-[11px] uppercase font-bold tracking-widest text-indigo-400/50 mt-6 mb-3">{children}</h3>,
  p: ({ children }: { children?: React.ReactNode }) => <p className="mb-3 last:mb-0 whitespace-pre-wrap">{children}</p>,
  ul: ({ children }: { children?: React.ReactNode }) => <ul className="list-disc list-inside mb-3 space-y-2">{children}</ul>,
  ol: ({ children }: { children?: React.ReactNode }) => <ol className="list-decimal list-inside mb-3 space-y-2">{children}</ol>,
  li: ({ children }: { children?: React.ReactNode }) => <li className="ml-2 whitespace-pre-wrap">{children}</li>,
  strong: ({ children }: { children?: React.ReactNode }) => <strong className="text-white font-semibold">{children}</strong>,
  em: ({ children }: { children?: React.ReactNode }) => <em className="text-indigo-200">{children}</em>,
  code: ({ children }: { children?: React.ReactNode }) => <code className="bg-white/10 rounded px-1 text-xs font-mono">{children}</code>,
  table: ({ children }: { children?: React.ReactNode }) => <div className="overflow-x-auto mb-4 border border-white/10 rounded-lg"><table className="w-full text-sm text-left">{children}</table></div>,
  thead: ({ children }: { children?: React.ReactNode }) => <thead className="text-xs uppercase bg-white/5 text-white/70 border-b border-white/10">{children}</thead>,
  tbody: ({ children }: { children?: React.ReactNode }) => <tbody className="divide-y divide-white/5">{children}</tbody>,
  tr: ({ children }: { children?: React.ReactNode }) => <tr className="hover:bg-white/[0.02] transition-colors">{children}</tr>,
  th: ({ children }: { children?: React.ReactNode }) => <th className="px-4 py-2.5 font-medium whitespace-nowrap">{children}</th>,
  td: ({ children }: { children?: React.ReactNode }) => <td className="px-4 py-2.5">{children}</td>,
}

function ThinkingDisclosure({ content }: { content: string }) {
  return (
    <details className="mb-3 group cursor-pointer">
      <summary className="text-xs text-indigo-400/60 hover:text-indigo-400 select-none mb-1 transition-colors outline-none flex items-center gap-1.5 font-medium list-none [&::-webkit-details-marker]:hidden">
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="opacity-70 group-open:rotate-90 transition-transform">
          <path d="M4 2l4 4-4 4"/>
        </svg>
        AI Thinking Process
      </summary>
      <div className="pl-3 mt-2 ml-1.5 border-l-2 border-indigo-500/20 text-slate-400 text-xs opacity-80 pb-2 cursor-auto">
        <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
          {content}
        </ReactMarkdown>
      </div>
    </details>
  )
}

function ConfidencePill({ score }: { score: number }) {
  const clamped = Math.max(1, Math.min(10, score))
  const color =
    clamped >= 8 ? 'bg-emerald-500/15 border-emerald-500/30 text-emerald-300' :
    clamped >= 5 ? 'bg-amber-500/15 border-amber-500/30 text-amber-300' :
                   'bg-rose-500/15 border-rose-500/30 text-rose-300'
  const label =
    clamped >= 8 ? 'High confidence' :
    clamped >= 5 ? 'Moderate confidence' : 'Low confidence'

  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full border text-[11px] font-semibold ${color}`}>
      <span className="opacity-70 text-[10px]">Confidence</span>
      {clamped}/10
      <span className="opacity-60">·</span>
      <span className="font-medium opacity-80">{label}</span>
    </span>
  )
}

interface AssistantMessageProps {
  content: string
  sections?: LLMResponseSection[]
  confidenceScore?: number
  missingDataContext?: string
}

/** Renders an assistant message. When structured sections are present (schema-backed prompts),
 * each section is rendered individually with its title. Otherwise falls back to parsing
 * the raw markdown string, collapsing any <thinking> blocks into a disclosure. */
export default function AssistantMessage({ content, sections, confidenceScore, missingDataContext }: AssistantMessageProps) {
  const confidenceFooter = (
    <>
      {confidenceScore != null && (
        <div className="mt-4 pt-3 border-t border-white/6 flex flex-col gap-2">
          <ConfidencePill score={confidenceScore} />
          {missingDataContext && missingDataContext.trim() !== '' && (
            <div className="mt-1 flex items-center gap-2.5 rounded-lg border border-amber-500/20 bg-amber-500/8 px-3 py-2.5">
              <span className="text-amber-400 shrink-0">⚠</span>
              <p className="text-xs text-amber-200/80 leading-relaxed">{missingDataContext}</p>
            </div>
          )}
        </div>
      )}
    </>
  )

  if (sections && sections.length > 0) {
    const thinkingSection = sections.find(s => s.key === 'thinking')
    const bodySections = sections.filter(s => s.key !== 'thinking')

    return (
      <>
        {thinkingSection && thinkingSection.content && (
          <ThinkingDisclosure content={thinkingSection.content} />
        )}
        {bodySections.map((section, idx) => (
          <div key={section.key} className={idx > 0 ? 'border-t border-white/6 pt-5 mt-1' : ''}>
            {section.title && (
              <p className="text-[11px] uppercase font-bold tracking-widest text-indigo-400/50 mb-3">
                {section.title}
              </p>
            )}
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
              {section.content}
            </ReactMarkdown>
          </div>
        ))}
        {confidenceFooter}
      </>
    )
  }

  // Fallback: parse raw markdown, splitting out <thinking> blocks.
  const parts = content.split(/(<thinking>[\s\S]*?(?:<\/thinking>|$))/i)

  return (
    <>
      {parts.map((part, index) => {
        if (!part) return null
        if (part.toLowerCase().startsWith('<thinking>')) {
          const innerContent = part.replace(/^<thinking>/i, '').replace(/<\/thinking>$/i, '').trim()
          if (!innerContent) return null
          return <ThinkingDisclosure key={index} content={innerContent} />
        }

        return (
          <ReactMarkdown key={index} remarkPlugins={[remarkGfm]} components={markdownComponents}>
            {part}
          </ReactMarkdown>
        )
      })}
      {confidenceFooter}
    </>
  )
}

