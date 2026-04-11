import { useState, useRef, useEffect, useCallback } from 'react'

interface Props {
  options: string[]
  value: string
  onChange: (value: string) => void
}

export default function SelectInput({ options, value, onChange }: Props) {
  const [open, setOpen]           = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)
  const listRef      = useRef<HTMLUListElement>(null)

  // Scroll active item into view
  useEffect(() => {
    if (activeIndex >= 0 && listRef.current) {
      const item = listRef.current.children[activeIndex] as HTMLElement | undefined
      item?.scrollIntoView({ block: 'nearest' })
    }
  }, [activeIndex])

  // Close on outside click
  useEffect(() => {
    function onMouseDown(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onMouseDown)
    return () => document.removeEventListener('mousedown', onMouseDown)
  }, [])

  const select = useCallback((opt: string) => {
    onChange(opt)
    setOpen(false)
    setActiveIndex(-1)
  }, [onChange])

  function handleKeyDown(e: React.KeyboardEvent) {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === ' ' || e.key === 'Enter') {
        e.preventDefault()
        setOpen(true)
        setActiveIndex(options.indexOf(value))
      }
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIndex(i => Math.min(i + 1, options.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIndex(i => Math.max(i - 1, 0))
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      if (activeIndex >= 0) select(options[activeIndex])
    } else if (e.key === 'Escape' || e.key === 'Tab') {
      setOpen(false)
      setActiveIndex(-1)
    }
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onKeyDown={handleKeyDown}
        onClick={() => {
          setOpen(o => !o)
          setActiveIndex(options.indexOf(value))
        }}
        className="w-full bg-[#0f1117] border border-[#2a2e42] rounded-xl px-4 py-2.5 text-slate-100 text-sm focus:outline-none focus:border-indigo-500/50 transition-colors flex items-center justify-between gap-2"
      >
        <span className="font-mono">{value}</span>
        <svg
          width="8" height="5" viewBox="0 0 8 5" fill="none"
          stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"
          className={`text-slate-500 shrink-0 transition-transform ${open ? 'rotate-180' : ''}`}
        >
          <path d="M1 1L4 4L7 1"/>
        </svg>
      </button>

      {open && (
        <ul
          ref={listRef}
          className="absolute z-60 mt-1 w-full bg-[#1a1d2e] border border-[#2a2e42] rounded-xl shadow-2xl overflow-y-auto"
          style={{ maxHeight: '14rem' }}
          onMouseDown={e => e.preventDefault()}
        >
          {options.map((opt, i) => (
            <li
              key={opt}
              onMouseEnter={() => setActiveIndex(i)}
              onClick={() => select(opt)}
              className={`px-4 py-2.5 cursor-pointer font-mono text-sm transition-colors ${
                i === activeIndex
                  ? 'bg-indigo-500/15 text-indigo-200'
                  : opt === value
                  ? 'text-slate-200'
                  : 'text-slate-400 hover:bg-white/5 hover:text-slate-200'
              }`}
            >
              {opt}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
