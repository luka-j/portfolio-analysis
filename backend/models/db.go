package models

import (
	"time"
)

// User represents a user in the database.
type User struct {
	ID        uint      `gorm:"primaryKey"`
	TokenHash string    `gorm:"uniqueIndex;not null"`
	CreatedAt time.Time
}

// Transaction represents a trade or cash cash transaction from FlexQuery.
type Transaction struct {
	ID            uint      `gorm:"primaryKey"`
	UserID        uint      `gorm:"index;not null"`
	Type          string    `gorm:"index;not null"` // e.g. "Trade", "Deposits/Withdrawals", "Dividends"
	TransactionID string    `gorm:"index"` // IB's native tradeID / transactionID — used for deduplication
	Symbol        string    `gorm:"index"`
	Currency      string    `gorm:"not null"`
	DateTime      time.Time `gorm:"index;not null"`
	Quantity      float64
	Price         float64
	Amount        float64
	Proceeds      float64
	Commission    float64
	BuySell          string
	ListingExchange  string `gorm:"index"`
	Description   string
	YahooSymbol   string    `gorm:"index"`
	AssetCategory string
	TaxCostBasis  *float64
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
	Provider string    `gorm:"default:'Yahoo'"`
}

// AssetFundamental stores fundamentals for a single security (stock, ETF, commodity).
// Symbol is the effective ticker used to query external APIs (YahooSymbol if set, else broker Symbol).
type AssetFundamental struct {
	ID          uint      `gorm:"primaryKey"`
	Symbol      string    `gorm:"uniqueIndex;not null"` // effective ticker (e.g. AAPL, VWCE.DE)
	Name        string
	AssetType   string    `gorm:"index"` // "Stock", "ETF", "Bond ETF", "Commodity", "Unknown"
	Country     string    `gorm:"index"`
	Sector      string    `gorm:"index"`
	Exchange    string
	Duration    *float64  // bond ETF: effective duration in years (from Yahoo bondHoldings)
	DataSource  string    // provider that supplied this record, e.g. "FMP"
	LastUpdated time.Time `gorm:"index"`
}

// EtfBreakdown stores aggregate country or sector weights for an ETF.
// One row per (fund_symbol, dimension, label) triple — e.g. ("VWCE.DE", "sector", "Technology", 0.25).
type EtfBreakdown struct {
	ID          uint      `gorm:"primaryKey"`
	FundSymbol  string    `gorm:"uniqueIndex:idx_etf_bd;not null;index"` // e.g. "VWCE.DE"
	Dimension   string    `gorm:"uniqueIndex:idx_etf_bd;not null"`        // "sector" or "country"
	Label       string    `gorm:"uniqueIndex:idx_etf_bd;not null"`        // e.g. "Technology", "United States"
	Weight      float64                                                   // fraction 0.0–1.0
	DataSource  string                                                    // "Yahoo"
	LastUpdated time.Time `gorm:"index"`
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
