package models

// API response types for all HTTP endpoints.
// Domain types and ORM types live in domain.go and db.go respectively.

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// UploadResponse is returned after a successful FlexQuery upload.
type UploadResponse struct {
	Message               string `json:"message"`
	PositionsCount        int    `json:"positions_count"`
	TradesCount           int    `json:"trades_count"`
	CashTransactionsCount int    `json:"cash_transactions_count"`
}

// PositionValue shows one position's value in one or more display currencies.
// Prices, CostBases, and Values are maps keyed by currency code (e.g. "USD", "CZK"),
// populated for every currency requested via the ?currencies param.
// Price, CostBasis, and Value hold the primary (first) currency as convenience scalars.
type PositionValue struct {
	Symbol          string  `json:"symbol"`
	ListingExchange string  `json:"listing_exchange,omitempty"`
	Quantity        float64 `json:"quantity"`
	NativeCurrency  string  `json:"native_currency"`
	YahooSymbol     string  `json:"yahoo_symbol,omitempty"`

	// Per-currency maps (populated when ?currencies is used).
	Prices    map[string]float64 `json:"prices,omitempty"`
	CostBases map[string]float64 `json:"cost_bases,omitempty"`
	Values    map[string]float64 `json:"values,omitempty"`

	Price      float64 `json:"price"`
	CostBasis  float64 `json:"cost_basis"`
	RealizedGL float64 `json:"realized_gl"`
	Value      float64 `json:"value"`
	Commission float64 `json:"commission"`

	BondDuration *float64 `json:"bond_duration,omitempty"` // bond ETF: effective duration in years
	Name         string   `json:"name,omitempty"`          // security long name from asset_fundamentals
	ISIN         string   `json:"isin,omitempty"`          // ISIN from IB FlexQuery via asset_fundamentals
	AssetType    string   `json:"asset_type,omitempty"`    // "Stock", "ETF", "Bond ETF", "Commodity", "Unknown"
	PriceStatus  string   `json:"price_status,omitempty"`  // "" (ok) | "no_data" | "stale" | "fetch_failed"
}

// PortfolioValueResponse is the response for GET /portfolio/value.
// When PendingCash > 0, a synthetic PENDING_CASH position is included in Positions
// representing sale proceeds that have not yet been reinvested.
type PortfolioValueResponse struct {
	Value           float64         `json:"value"`
	Currency        string          `json:"currency"`
	Positions       []PositionValue `json:"positions"`
	HasTransactions bool            `json:"has_transactions"`
	PendingCash     float64         `json:"pending_cash,omitempty"`
}

// TradeEntry is a frontend-friendly representation of a single trade.
type TradeEntry struct {
	ID             string   `json:"id"`                      // UUID from Transaction.PublicID
	EntryMethod    string   `json:"entry_method,omitempty"`  // "manual", "flexquery", "etrade_benefits", "etrade_sales"
	Date           string   `json:"date"`
	Side           string   `json:"side"` // BUY or SELL
	Quantity       float64  `json:"quantity"`
	Price          float64  `json:"price"`
	NativeCurrency string   `json:"native_currency"`
	ConvertedPrice float64  `json:"converted_price"`
	Commission     float64  `json:"commission"`
	Proceeds       float64  `json:"proceeds"`
	TaxCostBasis   *float64 `json:"tax_cost_basis,omitempty"`
	YahooSymbol    string   `json:"yahoo_symbol,omitempty"`
}

// TradesResponse is the response for GET /portfolio/trades.
type TradesResponse struct {
	Symbol          string       `json:"symbol"`
	Currency        string       `json:"currency"`
	DisplayCurrency string       `json:"display_currency"`
	Trades          []TradeEntry `json:"trades"`
}

// DailyValue is one day's portfolio value.
type DailyValue struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// PortfolioHistoryResponse is the response for GET /portfolio/history.
type PortfolioHistoryResponse struct {
	Currency        string       `json:"currency"`
	AccountingModel string       `json:"accounting_model"`
	Data            []DailyValue `json:"data"`
}

// MarketHistoryResponse is the response for GET /market/history.
type MarketHistoryResponse struct {
	Symbol string       `json:"symbol"`
	Data   []PricePoint `json:"data"`
}

// StatsResponse is the response for GET /portfolio/stats.
type StatsResponse struct {
	Currency        string                 `json:"currency"`
	AccountingModel string                 `json:"accounting_model"`
	Statistics      map[string]interface{} `json:"statistics"`
}

// BenchmarkResult holds comparison metrics for one benchmark.
// If Error is non-empty the metrics are zero and should not be used.
type BenchmarkResult struct {
	Symbol           string  `json:"symbol"`
	Error            string  `json:"error,omitempty"`
	Alpha            float64 `json:"alpha"`
	Beta             float64 `json:"beta"`
	TreynorRatio     float64 `json:"treynor_ratio"`
	TrackingError    float64 `json:"tracking_error"`
	InformationRatio float64 `json:"information_ratio"`
	Correlation      float64 `json:"correlation"`
}

// StandaloneResult holds standalone risk metrics for one security or portfolio.
// If Error is non-empty the metrics are zero and should not be used.
type StandaloneResult struct {
	Symbol       string  `json:"symbol"`
	Error        string  `json:"error,omitempty"`
	SharpeRatio  float64 `json:"sharpe_ratio"`
	VAMI         float64 `json:"vami"`
	Volatility   float64 `json:"volatility"`
	SortinoRatio float64 `json:"sortino_ratio"`
	MaxDrawdown  float64 `json:"max_drawdown"`
}

// StandaloneResponse is the response for GET /portfolio/standalone.
type StandaloneResponse struct {
	Currency        string             `json:"currency"`
	AccountingModel string             `json:"accounting_model"`
	Results         []StandaloneResult `json:"results"`
}

// CompareResponse is the response for GET /portfolio/compare.
type CompareResponse struct {
	Currency        string            `json:"currency"`
	AccountingModel string            `json:"accounting_model"`
	Benchmarks      []BenchmarkResult `json:"benchmarks"`
}

// BreakdownEntry represents one slice of a portfolio breakdown dimension.
type BreakdownEntry struct {
	Label      string  `json:"label"`
	Value      float64 `json:"value"`      // portfolio value in display currency
	Percentage float64 `json:"percentage"` // 0–100
}

// BreakdownSection is one dimension of portfolio breakdown (e.g. "By Country").
type BreakdownSection struct {
	Title   string           `json:"title"`
	Note    string           `json:"note,omitempty"` // optional explanatory note shown in GUI
	Entries []BreakdownEntry `json:"entries"`
}

// BreakdownResponse is the response for GET /portfolio/breakdown.
type BreakdownResponse struct {
	Currency string             `json:"currency"`
	Sections []BreakdownSection `json:"sections"`
}

// DrawdownPoint is a single day in a drawdown time series.
type DrawdownPoint struct {
	Date        string  `json:"date"`
	DrawdownPct float64 `json:"drawdown_pct"` // fraction below peak, e.g. -0.15 = -15%
	Peak        float64 `json:"peak"`
	Wealth      float64 `json:"wealth"`
}

// DrawdownResult is the drawdown series for one symbol (or "Portfolio").
type DrawdownResult struct {
	Symbol string          `json:"symbol"`
	Error  string          `json:"error,omitempty"`
	Series []DrawdownPoint `json:"series"`
}

// DrawdownResponse is the response for GET /portfolio/drawdown.
type DrawdownResponse struct {
	Currency        string           `json:"currency"`
	AccountingModel string           `json:"accounting_model"`
	Series          []DrawdownPoint  `json:"series"` // backward-compat: portfolio-only series
	Results         []DrawdownResult `json:"results"`
}

// RollingPoint is a single observation in a rolling-metric time series.
type RollingPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// RollingSeriesResult is the rolling metric series for one symbol (or "Portfolio").
type RollingSeriesResult struct {
	Symbol string         `json:"symbol"`
	Error  string         `json:"error,omitempty"`
	Series []RollingPoint `json:"series"`
}

// RollingResponse is the response for GET /portfolio/rolling.
type RollingResponse struct {
	Currency        string                `json:"currency"`
	AccountingModel string                `json:"accounting_model"`
	Metric          string                `json:"metric"`
	Window          int                   `json:"window"`
	Results         []RollingSeriesResult `json:"results"`
}

// AttributionResult holds return-attribution data for one position.
type AttributionResult struct {
	Symbol       string  `json:"symbol"`
	AvgWeight    float64 `json:"avg_weight"`   // time-weighted average portfolio weight 0–1
	Return       float64 `json:"return"`       // price return over the period
	Contribution float64 `json:"contribution"` // avg_weight × return
}

// AttributionResponse is the response for GET /portfolio/attribution.
type AttributionResponse struct {
	Currency        string              `json:"currency"`
	AccountingModel string              `json:"accounting_model"`
	TotalTWR        float64             `json:"total_twr"`
	Positions       []AttributionResult `json:"positions"`
}

// CumulativePoint is a single observation in a cumulative-return time series.
type CumulativePoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"` // cumulative return, in percent
}

// CumulativeSeriesResult is the cumulative series for one symbol (or "Portfolio" / "Portfolio-MWR").
type CumulativeSeriesResult struct {
	Symbol string            `json:"symbol"`
	Error  string            `json:"error,omitempty"`
	Series []CumulativePoint `json:"series"`
}

// CumulativeResponse is the response for GET /portfolio/cumulative.
type CumulativeResponse struct {
	Currency        string                   `json:"currency"`
	AccountingModel string                   `json:"accounting_model"`
	Results         []CumulativeSeriesResult `json:"results"`
}

// CorrelationMatrixResponse is the response for GET /portfolio/correlations.
type CorrelationMatrixResponse struct {
	Currency        string      `json:"currency"`
	AccountingModel string      `json:"accounting_model"`
	Symbols         []string    `json:"symbols"`
	Matrix          [][]float64 `json:"matrix"`
}
