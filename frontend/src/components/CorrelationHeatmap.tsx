import HoverTooltip from './HoverTooltip'

interface Props {
  symbols: string[]
  matrix: number[][]
}

function corrColor(value: number): string {
  // Diverging: blue (-1) → white (0) → red (+1)
  const abs = Math.abs(value)
  const r = value > 0 ? Math.round(55 + 200 * abs) : Math.round(55 + (1 - abs) * 200)
  const g = Math.round(55 + (1 - abs) * 160)
  const b = value < 0 ? Math.round(55 + 200 * abs) : Math.round(55 + (1 - abs) * 200)
  return `rgb(${r},${g},${b})`
}

export default function CorrelationHeatmap({ symbols, matrix }: Props) {
  const n = symbols.length
  if (n === 0) return null

  const cellSize = Math.max(44, Math.min(72, Math.floor(560 / n)))
  const labelWidth = 56

  return (
    <div className="overflow-x-auto w-full">
      <div style={{ minWidth: labelWidth + n * cellSize, marginLeft: 'auto', marginRight: 'auto', width: 'fit-content' }}>
        {/* Column labels (rotated) */}
        <div className="flex" style={{ paddingLeft: labelWidth }}>
          {symbols.map(sym => (
            <div
              key={sym}
              style={{ width: cellSize, minWidth: cellSize }}
              className="flex items-end justify-center pb-1"
            >
              <span
                className="text-[9px] font-bold text-slate-400 uppercase tracking-wide block"
                style={{ writingMode: 'vertical-rl', transform: 'rotate(180deg)', maxHeight: 56, overflow: 'hidden', textOverflow: 'ellipsis' }}
              >
                {sym.split('@')[0]}
              </span>
            </div>
          ))}
        </div>

        {/* Rows */}
        {symbols.map((rowSym, i) => (
          <div key={rowSym} className="flex items-center">
            {/* Row label */}
            <div
              style={{ width: labelWidth, minWidth: labelWidth }}
              className="text-[9px] font-bold text-slate-400 uppercase tracking-wide pr-2 text-right truncate"
            >
              {rowSym.split('@')[0]}
            </div>

            {/* Cells */}
            {symbols.map((colSym, j) => {
              const val = matrix[i]?.[j] ?? 0
              const isDiag = i === j
              return (
                <div
                  key={colSym}
                  className="relative group flex items-center justify-center border border-black/10"
                  style={{
                    width: cellSize,
                    minWidth: cellSize,
                    height: cellSize,
                    backgroundColor: isDiag ? '#2a2e42' : corrColor(val),
                    opacity: isDiag ? 0.6 : 1,
                  }}
                >
                  <span
                    className="text-[10px] font-bold tabular-nums select-none"
                    style={{ color: isDiag ? '#64748b' : Math.abs(val) > 0.5 ? '#fff' : '#1e293b' }}
                  >
                    {val.toFixed(2)}
                  </span>
                  {!isDiag && (
                    <HoverTooltip direction="up" align="center" className="w-max whitespace-nowrap z-50">
                      {rowSym.split('@')[0]} × {colSym.split('@')[0]}: {val.toFixed(3)}
                    </HoverTooltip>
                  )}
                </div>
              )
            })}
          </div>
        ))}

        {/* Legend */}
        <div className="flex items-center gap-3 mt-4 pl-14">
          <span className="text-[9px] text-slate-500 uppercase tracking-widest">−1</span>
          <div
            className="h-2 rounded-full flex-1"
            style={{
              background: 'linear-gradient(to right, rgb(55,55,255), rgb(215,215,215), rgb(255,55,55))',
              maxWidth: 160,
            }}
          />
          <span className="text-[9px] text-slate-500 uppercase tracking-widest">+1</span>
        </div>
      </div>
    </div>
  )
}
