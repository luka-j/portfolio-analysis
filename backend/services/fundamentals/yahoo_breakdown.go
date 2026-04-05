package fundamentals

import (
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/market"
)

// YahooBreakdownProvider adapts market.YahooFinanceService to the ETFBreakdownProvider interface.
type YahooBreakdownProvider struct {
	yahoo     *market.YahooFinanceService
	rateLimit RateLimitConfig
}

// NewYahooBreakdownProvider creates a new Yahoo breakdown provider.
func NewYahooBreakdownProvider(yahoo *market.YahooFinanceService, rpm, rpd int) *YahooBreakdownProvider {
	return &YahooBreakdownProvider{
		yahoo: yahoo,
		rateLimit: RateLimitConfig{
			RequestsPerMinute: rpm,
			RequestsPerDay:    rpd,
			CooldownDuration:  15 * time.Minute,
		},
	}
}

func (p *YahooBreakdownProvider) Name() string { return "Yahoo" }

func (p *YahooBreakdownProvider) RateLimit() RateLimitConfig { return p.rateLimit }

// FetchETFBreakdown fetches sector/country/bond-rating breakdown for an ETF from Yahoo quoteSummary.
func (p *YahooBreakdownProvider) FetchETFBreakdown(fundSymbol string) (*ETFBreakdownData, error) {
	summary, err := p.yahoo.GetETFBreakdown(fundSymbol)
	if err != nil {
		return nil, err
	}
	if summary == nil || len(summary.Breakdowns) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	rows := make([]models.EtfBreakdown, 0, len(summary.Breakdowns))
	for _, r := range summary.Breakdowns {
		rows = append(rows, models.EtfBreakdown{
			FundSymbol:  fundSymbol,
			Dimension:   r.Dimension,
			Label:       r.Label,
			Weight:      r.Weight,
			DataSource:  "Yahoo",
			LastUpdated: now,
		})
	}

	var durPtr *float64
	if summary.Duration > 0 {
		d := summary.Duration
		durPtr = &d
	}

	return &ETFBreakdownData{
		Rows:      rows,
		IsBondETF: summary.IsBondETF,
		Duration:  durPtr,
	}, nil
}
