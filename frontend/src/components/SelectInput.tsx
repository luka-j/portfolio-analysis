import { useState, useRef, useEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'

interface Props {
  options: readonly string[]
  // Optional display labels, aligned by index with `options`. When omitted the raw option value is shown.
  labels?: readonly string[]
  value: string
  onChange: (value: string) => void
}

interface DropdownPos {
  top: number
  left: number
  width: number
}

export default function SelectInput({ options, labels, value, onChange }: Props) {
  const labelFor = (opt: string) => {
    if (!labels) return opt
    const i = options.indexOf(opt)
    return i >= 0 && i < labels.length ? labels[i] : opt
  }
  const [open, setOpen]           = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const [pos, setPos]             = useState<DropdownPos | null>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const listRef   = useRef<HTMLUListElement>(null)

  function calcPos() {
    if (!buttonRef.current) return
    const r = buttonRef.current.getBoundingClientRect()
    setPos({ top: r.bottom + 4, left: r.left, width: r.width })
  }

  // Recalculate position on scroll/resize while open so the dropdown tracks the button.
  useEffect(() => {
    if (!open) return
    function update() { calcPos() }
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => {
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
    }
  }, [open])

  // Scroll active item into view
  useEffect(() => {
    if (activeIndex >= 0 && listRef.current) {
      const item = listRef.current.children[activeIndex] as HTMLElement | undefined
      item?.scrollIntoView({ block: 'nearest' })
    }
  }, [activeIndex])

  // Close on outside click — check both the trigger button and the portalled list
  useEffect(() => {
    function onMouseDown(e: MouseEvent) {
      const target = e.target as Node
      if (
        buttonRef.current && !buttonRef.current.contains(target) &&
        listRef.current   && !listRef.current.contains(target)
      ) {
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
        calcPos()
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
    <div className="relative">
      <button
        ref={buttonRef}
        type="button"
        onKeyDown={handleKeyDown}
        onClick={() => {
          if (open) {
            setOpen(false)
          } else {
            calcPos()
            setOpen(true)
            setActiveIndex(options.indexOf(value))
          }
        }}
        className="w-full bg-bg border border-border-dim rounded-xl px-4 py-2.5 text-slate-100 text-sm focus:outline-none focus:border-indigo-500/50 transition-colors flex items-center justify-between gap-2"
      >
        <span className="font-mono">{labelFor(value)}</span>
        <svg
          width="8" height="5" viewBox="0 0 8 5" fill="none"
          stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"
          className={`text-slate-500 shrink-0 transition-transform ${open ? 'rotate-180' : ''}`}
        >
          <path d="M1 1L4 4L7 1"/>
        </svg>
      </button>

      {open && pos && createPortal(
        <ul
          ref={listRef}
          className="bg-surface border border-border-dim rounded-xl shadow-2xl overflow-y-auto"
          style={{ position: 'fixed', top: pos.top, left: pos.left, width: pos.width, maxHeight: '14rem', zIndex: 9999 }}
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
              {labelFor(opt)}
            </li>
          ))}
        </ul>,
        document.body
      )}
    </div>
  )
}
