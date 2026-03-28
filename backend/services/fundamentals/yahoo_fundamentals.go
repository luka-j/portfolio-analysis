package fundamentals

import (
	"time"

	"gofolio-analysis/models"
	"gofolio-analysis/services/market"
)

// YahooFundamentalsProvider fetches asset fundamentals from Yahoo Finance's
// quoteSummary endpoint (assetProfile + fundProfile + price modules).
// Covers both stocks (name/country/sector) and ETFs/funds (name only;
// country/sector come from the ETF breakdown tier for funds).
type YahooFundamentalsProvider struct {
	yahoo     *market.YahooFinanceService
	rateLimit RateLimitConfig
}

// NewYahooFundamentalsProvider creates a new Yahoo fundamentals provider.
// rpm is the requests-per-minute cap (0 = unlimited).
func NewYahooFundamentalsProvider(yahoo *market.YahooFinanceService, rpm int) *YahooFundamentalsProvider {
	return &YahooFundamentalsProvider{
		yahoo: yahoo,
		rateLimit: RateLimitConfig{
			RequestsPerMinute: rpm,
			CooldownDuration:  15 * time.Minute,
		},
	}
}

// Name returns the provider identifier.
func (p *YahooFundamentalsProvider) Name() string { return "Yahoo" }

// RateLimit returns the configured rate limit.
func (p *YahooFundamentalsProvider) RateLimit() RateLimitConfig { return p.rateLimit }

// FetchFundamentals fetches name, country, sector, and exchange for the given symbol
// from Yahoo Finance. Returns nil, nil when the symbol is not found.
func (p *YahooFundamentalsProvider) FetchFundamentals(symbol string) (*models.AssetFundamental, error) {
	profile, err := p.yahoo.GetAssetProfile(symbol)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, nil
	}
	return &models.AssetFundamental{
		Symbol:      symbol,
		Name:        profile.Name,
		Country:     emptyToUnknown(profile.Country),
		Sector:      emptyToUnknown(profile.Sector),
		Exchange:    profile.Exchange,
		DataSource:  "Yahoo",
		LastUpdated: time.Now().UTC(),
	}, nil
}
