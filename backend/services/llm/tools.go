package llm

import (
	"context"

	"google.golang.org/genai"
)

// ToolName constants for all LLM-callable portfolio tools.
const (
	ToolGetCurrentAllocations       = "get_current_allocations"
	ToolGetRiskMetrics              = "get_risk_metrics"
	ToolGetBenchmarkMetrics         = "get_benchmark_metrics"
	ToolGetAssetFundamentals        = "get_asset_fundamentals"
	ToolGetTaxImpact                = "get_tax_impact"
	ToolGetPositionsWithCostBasis   = "get_open_positions_with_cost_basis"
	ToolGetRecentTransactions       = "get_recent_transactions"
	ToolGetFXImpact                 = "get_fx_impact"
	ToolGetHistoricalPerformance    = "get_historical_performance_series"
)

// ToolExecutor is a callback invoked by the LLM loop when the model requests a function call.
// It receives the raw genai.FunctionCall and must return a JSON-serialisable result map (or an error).
// Returning an error causes an error payload to be fed back to the model so it can recover gracefully.
type ToolExecutor func(ctx context.Context, call *genai.FunctionCall) (map[string]any, error)

// PortfolioTools returns the genai.Tool bundle containing all native portfolio function declarations.
// These are passed verbatim to the Gemini API; no external calls are made at declaration time.
func PortfolioTools() *genai.Tool {
	strParam := func(desc string) *genai.Schema {
		return &genai.Schema{Type: genai.TypeString, Description: desc}
	}
	numParam := func(desc string) *genai.Schema {
		return &genai.Schema{Type: genai.TypeNumber, Description: desc}
	}

	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{
			{
				Name:        ToolGetCurrentAllocations,
				Description: "Returns the user's current portfolio allocations as percentage weights. Use this whenever you need to understand the user's holdings, their names, and how much of the portfolio each represents.",
				Parameters:  &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{}},
			},
			{
				Name:        ToolGetRiskMetrics,
				Description: "Computes portfolio risk and return statistics for a given date range. Returns TWR, MWR, Sharpe ratio, Sortino ratio, VAMI, annualised volatility, and Max Drawdown. Use this before interpreting any risk-related question.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"from_date":      strParam("Start date for the analysis period (YYYY-MM-DD). Use the earliest available date if the user wants all-time metrics."),
						"to_date":        strParam("End date for the analysis period (YYYY-MM-DD). Defaults to today if omitted."),
						"risk_free_rate": numParam("Annualised risk-free rate as a decimal (e.g. 0.05 for 5%). Defaults to 0.05 if omitted."),
					},
					Required: []string{"from_date", "to_date"},
				},
			},
			{
				Name:        ToolGetBenchmarkMetrics,
				Description: "Compares the portfolio against a benchmark security (e.g. 'SPY', 'VWCE.DE', 'QQQ'). Returns Alpha, Beta, Treynor, Tracking Error, Information Ratio, and Correlation. Always call this before answering benchmark or tracking-error questions.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"benchmark_symbol": strParam("Yahoo Finance ticker of the benchmark (e.g. 'SPY', 'QQQ', 'VWCE.DE')."),
						"from_date":        strParam("Start date (YYYY-MM-DD)."),
						"to_date":          strParam("End date (YYYY-MM-DD). Defaults to today if omitted."),
						"risk_free_rate":   numParam("Annualised risk-free rate as a decimal (e.g. 0.05). Defaults to 0.05 if omitted."),
					},
					Required: []string{"benchmark_symbol", "from_date", "to_date"},
				},
			},
			{
				Name:        ToolGetAssetFundamentals,
				Description: "Looks up stored fundamental data for a specific ticker: asset type (Stock/ETF/Bond ETF), country, sector, and for ETFs the pre-aggregated country/sector/bond-rating breakdown weights. Use this when the user asks about sector exposure, country risk, or a specific holding's characteristics.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"symbol": strParam("Ticker symbol to look up (e.g. 'AAPL', 'SPP1', 'QQQ')."),
					},
					Required: []string{"symbol"},
				},
			},
			{
				Name:        ToolGetPositionsWithCostBasis,
				Description: "Returns all currently open portfolio positions with their absolute quantity, average cost basis, current price, and unrealized gain/loss. Use this when analyzing underwater positions, highest gaining stocks, or tax-loss harvesting opportunities.",
				Parameters:  &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{}},
			},
			{
				Name:        ToolGetTaxImpact,
				Description: "Calculates the portfolio tax events for a given calendar year. Returns total investment income and employment income (ESPP/RSU vests) properly offset by commissions. Useful for estimating annual tax obligations.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"year": numParam("4-digit calendar year (e.g. 2024)."),
					},
					Required: []string{"year"},
				},
			},
			{
				Name:        ToolGetRecentTransactions,
				Description: "Returns chronological trading history for a specific symbol. Shows precise buy/sell dates, prices, quantities, and commissions. Use this when you need to know exactly when a user entered or exited a position.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"symbol": strParam("Ticker symbol (e.g. 'AAPL')."),
						"limit":  numParam("Number of most recent trades to fetch. Max 100."),
					},
					Required: []string{"symbol", "limit"},
				},
			},
			{
				Name:        ToolGetFXImpact,
				Description: "Calculates the portfolio total value using both live spot exchange rates and historical transaction exchange rates. Shows exactly how much currency fluctuation has impacted the value of the portfolio from the user's display currency perspective.",
				Parameters:  &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{}},
			},
			{
				Name:        ToolGetHistoricalPerformance,
				Description: "Returns a high-level time series array of the portfolio's total value going back in time to analyze drawdown patterns. Sampled monthly to keep data compact.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"from_date": strParam("Start date (YYYY-MM-DD). Use earliest if all-time."),
						"to_date":   strParam("End date (YYYY-MM-DD). Defaults to today."),
					},
					Required: []string{"from_date"},
				},
			},
		},
	}
}
