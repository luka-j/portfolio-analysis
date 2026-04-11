import ReactMarkdown from 'react-markdown'

const markdownComponents = {
  h1: ({ children }: { children?: React.ReactNode }) => <p className="font-bold text-white mt-5 mb-2">{children}</p>,
  h2: ({ children }: { children?: React.ReactNode }) => <p className="font-bold text-white mt-5 mb-2">{children}</p>,
  h3: ({ children }: { children?: React.ReactNode }) => <p className="font-semibold text-white/90 mt-5 mb-2">{children}</p>,
  p: ({ children }: { children?: React.ReactNode }) => <p className="mb-3 last:mb-0">{children}</p>,
  ul: ({ children }: { children?: React.ReactNode }) => <ul className="list-disc list-inside mb-3 space-y-2">{children}</ul>,
  ol: ({ children }: { children?: React.ReactNode }) => <ol className="list-decimal list-inside mb-3 space-y-2">{children}</ol>,
  li: ({ children }: { children?: React.ReactNode }) => <li className="ml-2">{children}</li>,
  strong: ({ children }: { children?: React.ReactNode }) => <strong className="text-white font-semibold">{children}</strong>,
  em: ({ children }: { children?: React.ReactNode }) => <em className="text-indigo-200">{children}</em>,
  code: ({ children }: { children?: React.ReactNode }) => <code className="bg-white/10 rounded px-1 text-xs font-mono">{children}</code>,
}

/** Renders an assistant message, collapsing <thinking> blocks into a disclosure. */
export default function AssistantMessage({ content }: { content: string }) {
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
              <div className="pl-3 mt-2 ml-1.5 border-l-2 border-indigo-500/20 text-slate-400 text-xs opacity-80 pb-2 cursor-auto">
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
