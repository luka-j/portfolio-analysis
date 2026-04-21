export interface ToolDefinition {
  id: string
  label: string
  description: string
}

export const AVAILABLE_TOOLS: ToolDefinition[] = [
  { id: 'get_current_allocations', label: 'Allocations', description: 'See your current portfolio percentages.' },
  { id: 'get_open_positions_with_cost_basis', label: 'Positions & Cost Basis', description: 'See individual stocks, quantity, average cost, & PnL. (Exposes absolute monetary values)' },
  { id: 'get_tax_impact', label: 'Tax Events', description: 'Compute Czech tax rules for an upcoming year. (Exposes absolute monetary values)' },
  { id: 'get_recent_transactions', label: 'Recent Transactions', description: 'Retrieve latest buys/sells for a specific ticker.' },
  { id: 'get_historical_performance_series', label: 'Historical Performance', description: 'Analyze historic drawdown. (Exposes absolute monetary values)' },
  { id: 'get_asset_fundamentals', label: 'Asset Fundamentals', description: 'Examine country/sector weights.' },
  { id: 'get_benchmark_metrics', label: 'Benchmark Comparison', description: 'Measure alpha / beta / tracking error against any index or security.' },
  { id: 'get_risk_metrics', label: 'Risk Metrics', description: 'Measure Max Drawdown, Sharpe, Sortino ratios.' },
  { id: 'get_fx_impact', label: 'FX Impact', description: 'Assess how currency changes have moved portfolio value. (Exposes absolute monetary values)' },
]
