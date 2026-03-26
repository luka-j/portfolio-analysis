package fundamentals

import (
	"time"

	"gofolio-analysis/models"
)

// RateLimitConfig configures per-provider request budgets.
// Defaults reflect free-tier limits; override via environment variables.
type RateLimitConfig struct {
	RequestsPerMinute int
	RequestsPerDay    int
	CooldownDuration  time.Duration // pause duration once a limit is detected
}

// FundamentalsProvider fetches asset fundamentals (sector, country) for a symbol.
type FundamentalsProvider interface {
	// Name returns the canonical provider identifier (e.g. "FMP").
	Name() string
	// FetchFundamentals fetches and returns fundamentals for the given ticker.
	// Returns nil, nil when the symbol is not found (not an error).
	FetchFundamentals(symbol string) (*models.AssetFundamental, error)
	// RateLimit returns the provider's configured request budget.
	RateLimit() RateLimitConfig
}

// ETFBreakdownData is the result of a breakdown fetch, including bond metadata.
type ETFBreakdownData struct {
	Rows      []models.EtfBreakdown
	IsBondETF bool
	Duration  *float64 // nil if not available
}

// ETFBreakdownProvider fetches aggregate sector/country/bond-rating breakdown for an ETF.
type ETFBreakdownProvider interface {
	// Name returns the canonical provider identifier (e.g. "Yahoo").
	Name() string
	// FetchETFBreakdown fetches aggregate breakdown rows for the given ETF symbol.
	// Returns nil, nil when no data is available (not an error).
	FetchETFBreakdown(fundSymbol string) (*ETFBreakdownData, error)
	// RateLimit returns the provider's configured request budget.
	RateLimit() RateLimitConfig
}
