package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User represents a user in the database.
type User struct {
	ID        uint   `gorm:"primaryKey"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	CreatedAt time.Time
}

// Transaction represents a trade or cash cash transaction from FlexQuery.
type Transaction struct {
	ID              uint      `gorm:"primaryKey"`
	UserID          uint      `gorm:"index;not null"`
	Type            string    `gorm:"index;not null"` // e.g. "Trade", "Deposits/Withdrawals", "Dividends"
	TransactionID   string    `gorm:"index"`          // IB's native tradeID / transactionID — used for deduplication
	Symbol          string    `gorm:"index"`
	Currency        string    `gorm:"not null"`
	DateTime        time.Time `gorm:"index;not null"`
	Quantity        float64
	Price           float64
	Amount          float64
	Proceeds        float64
	Commission      float64
	BuySell         string
	ListingExchange string `gorm:"index"`
	Description     string
	YahooSymbol     string `gorm:"index"`
	AssetCategory   string
	TaxCostBasis    *float64
	Conid           string `gorm:"index"` // IB permanent contract ID; empty for eTrade/cash txns
	ISIN            string `gorm:"index"` // raw ISIN from IB FlexQuery; seeded into asset_fundamentals by background bootstrap
	PublicID        string `gorm:"uniqueIndex"` // UUID; stable external ID used in API responses and DELETE endpoint
	EntryMethod     string `gorm:"index"`               // "manual", "flexquery", "etrade_benefits", "etrade_sales"
}

// BeforeCreate generates a UUID PublicID for new Transaction rows if one is not already set.
func (t *Transaction) BeforeCreate(_ *gorm.DB) error {
	if t.PublicID == "" {
		t.PublicID = uuid.New().String()
	}
	return nil
}

// MarketData represents cached end-of-day price data from Yahoo Finance.
type MarketData struct {
	ID       uint      `gorm:"primaryKey"`
	Symbol   string    `gorm:"uniqueIndex:idx_symbol_date;not null"`
	Date     time.Time `gorm:"uniqueIndex:idx_symbol_date;not null"`
	Open     float64
	High     float64
	Low      float64
	Close    float64
	AdjClose float64
	Volume   int64
	Provider string `gorm:"default:'Yahoo'"`
}

// AssetFundamental stores the descriptive profile of a single security (stock, ETF, commodity)
// scoped to a specific user. This is the authoritative source for all asset-level metadata consumed
// by the API and LLM layers. Each user has their own row per symbol so edits are personal.
//
// Symbol is the effective ticker used to query external APIs (YahooSymbol if set, else broker Symbol).
// The unique constraint is (user_id, symbol) — same symbol can exist for multiple users.
//
// DataSource: "Yahoo" or "IB" for background-populated rows; "User" for manually edited rows.
// Rows with DataSource="User" are never overwritten by the background fetch job.
//
// ISIN: seeded from transactions.isin (IB FlexQuery source) by the background bootstrap.
// It is display-only here — the canonical persistence path is transactions → background seed → this table.
type AssetFundamental struct {
	ID          uint   `gorm:"primaryKey"`
	UserID      uint   `gorm:"uniqueIndex:user_symbol;not null;default:0"` // owner; matches users.id
	Symbol      string `gorm:"uniqueIndex:user_symbol;not null"` // effective ticker (e.g. AAPL, VWCE.DE)
	Conid       string `gorm:"index"`                            // IB permanent contract ID; empty for non-IB securities
	ISIN        string `gorm:"index"`                            // display-only; seeded from transactions by bootstrap
	Name        string
	AssetType   string `gorm:"index"` // "Stock", "ETF", "Bond ETF", "Commodity", "Unknown"
	Country     string `gorm:"index"`
	Sector      string `gorm:"index"`
	Currency    string   // native trading currency, e.g. "USD", "EUR" (from IB transactions or Yahoo)
	Duration    *float64 // bond ETF: effective duration in years (from Yahoo bondHoldings)
	DataSource  string   // "Yahoo", "IB", or "User" (user edits are never overwritten by the background job)
	LastUpdated time.Time `gorm:"index"`
}

// EtfBreakdown stores aggregate country or sector weights for an ETF.
// One row per (fund_symbol, dimension, label) triple — e.g. ("VWCE.DE", "sector", "Technology", 0.25).
type EtfBreakdown struct {
	ID          uint      `gorm:"primaryKey"`
	FundSymbol  string    `gorm:"uniqueIndex:idx_etf_bd;not null;index"` // e.g. "VWCE.DE"
	Dimension   string    `gorm:"uniqueIndex:idx_etf_bd;not null"`       // "sector" or "country"
	Label       string    `gorm:"uniqueIndex:idx_etf_bd;not null"`       // e.g. "Technology", "United States"
	Weight      float64   // fraction 0.0–1.0
	DataSource  string    // "Yahoo"
	LastUpdated time.Time `gorm:"index"`
}

// CurrentPrice stores the most recently fetched real-time market price for a symbol.
type CurrentPrice struct {
	ID        uint   `gorm:"primaryKey"`
	Symbol    string `gorm:"uniqueIndex;not null"`
	Price     float64
	FetchedAt time.Time `gorm:"index"`
}

// LLMCache stores cached responses from the LLM.
type LLMCache struct {
	ID         uint      `gorm:"primaryKey"`
	UserHash   string    `gorm:"uniqueIndex:idx_llmcache_user_prompt;not null"`
	PromptType string    `gorm:"uniqueIndex:idx_llmcache_user_prompt;not null"` // e.g. "summary_1d", "canned_analysis"
	Model      string    `gorm:"uniqueIndex:idx_llmcache_user_prompt;not null"` // "flash" | "pro"
	Response   string    // The markdown/text response
	CreatedAt  time.Time `gorm:"index"`
}
