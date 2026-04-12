import { useState, useRef, useEffect, useCallback } from 'react'

export interface AutocompleteOption {
  value: string      // the symbol / selectable value
  label?: string     // secondary display text (e.g. security name)
}

interface Props {
  options: AutocompleteOption[]
  value: string
  onChange: (value: string) => void
  placeholder?: string
  autoFocus?: boolean
  /** When true, only options from the list produce a suggestion dropdown entry
   *  (free-text is still allowed but unmatched entries show a subtle warning).
   *  Default: false. */
  strictSuggestions?: boolean
}

export default function AutocompleteInput({
  options,
  value,
  onChange,
  placeholder = '',
  autoFocus = false,
}: Props) {
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  // Filter options: match value fragment against symbol or label, case-insensitive
  const query = value.trim().toUpperCase()
  const filtered: AutocompleteOption[] = query.length === 0
    ? options
    : options.filter(o =>
        o.value.toUpperCase().includes(query) ||
        (o.label?.toUpperCase().includes(query) ?? false)
      )

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

  const selectOption = useCallback((opt: AutocompleteOption) => {
    onChange(opt.value)
    setOpen(false)
    setActiveIndex(-1)
    inputRef.current?.focus()
  }, [onChange])

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        setOpen(true)
        return
      }
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIndex(i => Math.min(i + 1, filtered.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIndex(i => Math.max(i - 1, -1))
    } else if (e.key === 'Enter') {
      if (activeIndex >= 0 && filtered[activeIndex]) {
        e.preventDefault()
        selectOption(filtered[activeIndex])
      } else {
        setOpen(false)
      }
    } else if (e.key === 'Escape') {
      setOpen(false)
      setActiveIndex(-1)
    } else if (e.key === 'Tab') {
      setOpen(false)
    }
  }

  return (
    <div ref={containerRef} className="relative">
      <input
        ref={inputRef}
        autoFocus={autoFocus}
        autoComplete="off"
        spellCheck={false}
        className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 font-mono text-sm focus:outline-none focus:border-indigo-500/50 transition-colors"
        value={value}
        placeholder={placeholder}
        onChange={e => {
          onChange(e.target.value)
          setOpen(true)
          setActiveIndex(-1)
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={handleKeyDown}
      />

      {open && filtered.length > 0 && (
        <ul
          ref={listRef}
          className="absolute z-60 mt-1 w-full bg-surface border border-border-dim rounded-xl shadow-2xl overflow-y-auto"
          style={{ maxHeight: '14rem' }}
          onMouseDown={e => e.preventDefault()} // keep focus on input
        >
          {filtered.map((opt, i) => (
            <li
              key={opt.value}
              onMouseEnter={() => setActiveIndex(i)}
              onClick={() => selectOption(opt)}
              className={`flex items-baseline justify-between gap-3 px-4 py-2.5 cursor-pointer transition-colors ${
                i === activeIndex
                  ? 'bg-indigo-500/15 text-indigo-200'
                  : 'text-slate-200 hover:bg-white/5'
              }`}
            >
              <span className="font-mono text-sm font-semibold">{opt.value}</span>
              {opt.label && (
                <span className={`text-[11px] truncate max-w-[55%] ${
                  i === activeIndex ? 'text-indigo-400/70' : 'text-slate-500'
                }`}>
                  {opt.label}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
