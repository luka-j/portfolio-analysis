package models

import "time"

// ---------- Domain types ----------

// Trade represents a single trade from the FlexQuery report.
type Trade struct {
	TransactionID   string    `json:"transaction_id,omitempty"` // IB's native tradeID
	Symbol          string    `json:"symbol"`
	AssetCategory   string    `json:"asset_category"`
	Currency        string    `json:"currency"`
	ListingExchange string    `json:"listing_exchange,omitempty"`
	DateTime        time.Time `json:"date_time"`
	Quantity        float64   `json:"quantity"`
	Price           float64   `json:"price"`
	Proceeds        float64   `json:"proceeds"`
	Commission      float64   `json:"commission"`
	BuySell         string    `json:"buy_sell"`
	TaxCostBasis    *float64  `json:"tax_cost_basis,omitempty"`
	YahooSymbol     string    `json:"yahoo_symbol,omitempty"`
}

// OpenPosition represents an open position from the FlexQuery report.
type OpenPosition struct {
	Symbol        string  `json:"symbol"`
	AssetCategory string  `json:"asset_category"`
	Currency      string  `json:"currency"`
	Quantity      float64 `json:"quantity"`
	MarkPrice         float64 `json:"mark_price"`
	PositionValue     float64 `json:"position_value"`
	CostBasisPerShare float64 `json:"cost_basis_per_share"`
	YahooSymbol       string  `json:"yahoo_symbol,omitempty"`
}

// CashTransaction represents a cash flow (deposit, withdrawal, dividend, etc.).
type CashTransaction struct {
	TransactionID string    `json:"transaction_id,omitempty"` // IB's native transactionID
	Type          string    `json:"type"`
	Currency      string    `json:"currency"`
	Amount        float64   `json:"amount"`
	DateTime      time.Time `json:"date_time"`
	Description   string    `json:"description"`
	Symbol        string    `json:"symbol,omitempty"`
}

// FlexQueryData holds all parsed data from an IB FlexQuery report.
type FlexQueryData struct {
	AccountID        string            `json:"account_id"`
	Trades           []Trade           `json:"trades"`
	OpenPositions    []OpenPosition    `json:"open_positions"`
	CashTransactions []CashTransaction `json:"cash_transactions"`
}

// CashFlow is used for MWR/IRR calculation.
type CashFlow struct {
	Date   time.Time `json:"date"`
	Amount float64   `json:"amount"`
}

// PricePoint represents a single day of OHLCV data.
type PricePoint struct {
	Date     time.Time `json:"date"`
	Open     float64   `json:"open"`
	High     float64   `json:"high"`
	Low      float64   `json:"low"`
	Close    float64   `json:"close"`
	AdjClose float64   `json:"adj_close"`
	Volume   int64     `json:"volume"`
}

// Holding tracks quantity of a symbol at a point in time.
type Holding struct {
	Symbol          string  `json:"symbol"`
	Quantity        float64 `json:"quantity"`
	Currency        string  `json:"currency"` // native currency of the security
	ListingExchange string  `json:"listing_exchange,omitempty"`
}

// ---------- Accounting model ----------

// AccountingModel defines how multi-currency values are converted.
type AccountingModel string

const (
	// AccountingModelHistorical converts at the FX rate on each valuation date.
	AccountingModelHistorical AccountingModel = "historical"
	// AccountingModelSpot converts everything at today's FX rate.
	AccountingModelSpot AccountingModel = "spot"
	// AccountingModelOriginal performs no conversion; values stay in native currency.
	AccountingModelOriginal AccountingModel = "original"
)

// ParseAccountingModel parses a string into an AccountingModel, defaulting to historical.
func ParseAccountingModel(s string) AccountingModel {
	switch s {
	case "spot":
		return AccountingModelSpot
	case "original":
		return AccountingModelOriginal
	default:
		return AccountingModelHistorical
	}
}

// ---------- API response types ----------

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

// PositionValue shows one position's value in multiple currencies.
type PositionValue struct {
	Symbol          string             `json:"symbol"`
	ListingExchange string             `json:"listing_exchange,omitempty"`
	Quantity        float64            `json:"quantity"`
	NativeCurrency  string             `json:"native_currency"`
	YahooSymbol     string             `json:"yahoo_symbol,omitempty"`
	Prices          map[string]float64 `json:"prices"`
	CostBases       map[string]float64 `json:"cost_bases"`
	RealizedGLs     map[string]float64 `json:"realized_gls"`
	Values          map[string]float64 `json:"values"`
	Commissions     map[string]float64 `json:"commissions"`
}

// PortfolioValueResponse is the response for GET /portfolio/value.
type PortfolioValueResponse struct {
	Values    map[string]float64 `json:"values"`
	Positions []PositionValue    `json:"positions"`
}

// TradeEntry is a frontend-friendly representation of a single trade.
type TradeEntry struct {
	Date           string  `json:"date"`
	Side           string  `json:"side"` // BUY or SELL
	Quantity       float64 `json:"quantity"`
	Price          float64 `json:"price"`
	NativeCurrency string  `json:"native_currency"`
	ConvertedPrice float64 `json:"converted_price"`
	Commission     float64 `json:"commission"`
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
type BenchmarkResult struct {
	Symbol           string  `json:"symbol"`
	Alpha            float64 `json:"alpha"`
	Beta             float64 `json:"beta"`
	SharpeRatio      float64 `json:"sharpe_ratio"`
	TreynorRatio     float64 `json:"treynor_ratio"`
	TrackingError    float64 `json:"tracking_error"`
	InformationRatio float64 `json:"information_ratio"`
	Correlation      float64 `json:"correlation"`
}

// CompareResponse is the response for GET /portfolio/compare.
type CompareResponse struct {
	Currency        string            `json:"currency"`
	AccountingModel string            `json:"accounting_model"`
	Benchmarks      []BenchmarkResult `json:"benchmarks"`
}
