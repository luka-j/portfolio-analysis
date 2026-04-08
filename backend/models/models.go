package models

import (
	"strings"
	"time"
)

// IsFXTrade returns true if the trade is a currency conversion (FX) trade.
func IsFXTrade(t Trade) bool {
	cat := strings.ToUpper(t.AssetCategory)
	return cat == "CASH" || (len(t.Symbol) == 7 && t.Symbol[3] == '.')
}

// ---------- Domain types ----------

// Trade represents a single trade from the FlexQuery report.
type Trade struct {
	TransactionID   string    `json:"transaction_id,omitempty"` // IB's native tradeID
	Conid           string    `json:"conid,omitempty"`          // IB permanent contract ID
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
	Symbol            string  `json:"symbol"`
	AssetCategory     string  `json:"asset_category"`
	Currency          string  `json:"currency"`
	Quantity          float64 `json:"quantity"`
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
