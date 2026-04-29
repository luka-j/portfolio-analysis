export type ChartMode = 'twr' | 'mwr' | 'drawdown' | 'rolling_sharpe' | 'rolling_sortino' | 'rolling_volatility' | 'rolling_beta'
export type HoldingsView = 'attribution' | 'correlation'

export interface AnalysisParams {
  currency: string
  acctModel: 'historical' | 'spot'
  from: string
  to: string
  effectiveFrom: string
  active: number | null
  compare: number | null
  riskFreeRate: number
  scenarios: Array<{ id: number; name: string }>
}

export function formatSymbolName(sym: string, scenarios: Array<{id: number, name: string}>) {
  if (sym.startsWith('scenario:')) {
    const sid = parseInt(sym.replace('scenario:', ''), 10)
    const s = scenarios.find(x => x.id === sid)
    return s ? `[S] ${s.name}` : sym
  }
  return sym
}
