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
}
