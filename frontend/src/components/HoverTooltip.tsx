interface Props {
  children: React.ReactNode
  direction?: 'up' | 'down'
  align?: 'center' | 'left' | 'right' | 'none'
  className?: string
}

export default function HoverTooltip({ children, direction = 'up', align = 'center', className = '' }: Props) {
  const dirClass = direction === 'down' ? 'top-full mt-2.5' : 'bottom-full mb-2.5'
  const alignClass = align === 'right' ? 'right-0' : align === 'left' ? 'left-0' : align === 'center' ? 'left-1/2 -translate-x-1/2' : ''
  return (
    <div className={`absolute ${dirClass} ${alignClass} px-3 py-2.5 bg-panel border border-border-dim/80 rounded-xl text-[10px] text-slate-400 leading-relaxed pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity z-50 shadow-2xl ${className}`}>
      {children}
    </div>
  )
}
