package models

import (
	"fmt"
	"strings"
	"time"
)

// IsFXTrade returns true if the trade is a currency conversion (FX) trade.
func IsFXTrade(t Trade) bool {
	cat := strings.ToUpper(t.AssetCategory)
	return cat == "CASH" || (len(t.Symbol) == 7 && t.Symbol[3] == '.')
}

// Trade represents a single trade from the FlexQuery report.
type Trade struct {
	TransactionID   string    `json:"transaction_id,omitempty"` // IB's native tradeID
	Conid           string    `json:"conid,omitempty"`          // IB permanent contract ID
	Symbol          string    `json:"symbol"`
	ISIN            string    `json:"isin,omitempty"` // ISIN from IB FlexQuery
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
	PublicID        string    `json:"-"` // UUID; threaded through from the DB row for delete support
	EntryMethod     string    `json:"-"` // "manual", "flexquery", "etrade_benefits", "etrade_sales"
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
	// ParsedCorporateActions holds raw corporate actions from the XML, processed by the repository.
	ParsedCorporateActions []ParsedCorporateAction `json:"-"`
	// CashDividends holds cash dividend corporate actions loaded from DB, fed into cashbucket.
	CashDividends []CashDividend `json:"-"`
	// UserHash is the scoped owner of this data set, set by Repository.LoadSaved.
	// Used by the portfolio service for request-level singleflight dedup so that
	// concurrent handlers for the same user share a single computation.
	UserHash string `json:"-"`
}

// ParsedCorporateAction is the output of parsing one CorporateAction XML element.
type ParsedCorporateAction struct {
	ActionID    string
	Type        string // IC, FS, RS, SD, CD
	Symbol      string
	Conid       string
	Currency    string
	Quantity    float64
	Amount      float64
	DateTime    time.Time
	Description string
}

// CashDividend is a corporate-action cash dividend used by the cashbucket package.
// Loaded from the cash_dividend_records table by Repository.LoadSaved.
type CashDividend struct {
	ActionID    string
	Symbol      string
	Currency    string
	Amount      float64
	DateTime    time.Time
	Description string
}

// ImportedCorporateAction is a summary of one corporate action returned in the upload API response.
type ImportedCorporateAction struct {
	ActionID    string  `json:"action_id"`
	Type        string  `json:"type"`                    // IC, FS, RS, SD, CD
	Symbol      string  `json:"symbol"`
	NewSymbol   string  `json:"new_symbol,omitempty"`
	Date        string  `json:"date"`                    // YYYY-MM-DD
	Description string  `json:"description"`
	SplitRatio  float64 `json:"split_ratio,omitempty"`
	Quantity    float64 `json:"quantity,omitempty"`
	Amount      float64 `json:"amount,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	IsNew       bool    `json:"is_new"` // false = already existed (idempotent skip)
}

// ImportResult bundles trade and corporate-action import summaries from ParseAndSave.
type ImportResult struct {
	Transactions     []ImportedTransaction
	CorporateActions []ImportedCorporateAction
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
// An empty string is treated as "historical". Use ValidateAccountingModel when an
// explicit unknown value should be rejected rather than silently defaulted.
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

// ValidateAccountingModel parses s and returns an error for any value that is
// not one of the three known models. An empty string is accepted and treated as
// "historical" so that optional fields in JSON bodies are handled cleanly.
func ValidateAccountingModel(s string) (AccountingModel, error) {
	switch s {
	case "", "historical":
		return AccountingModelHistorical, nil
	case "spot":
		return AccountingModelSpot, nil
	case "original":
		return AccountingModelOriginal, nil
	default:
		return AccountingModelHistorical, fmt.Errorf("unknown accounting_model %q: must be one of historical, spot, original", s)
	}
}
