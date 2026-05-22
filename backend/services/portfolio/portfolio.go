package portfolio

import (
	"golang.org/x/sync/singleflight"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
)

// Service reconstructs and values portfolios from FlexQuery data.
type Service struct {
	MarketProvider       market.Provider
	FXService            *fx.Service
	CashBucketExpiryDays int

	// sfDailyValues collapses concurrent GetDailyValuesFor calls for the same
	// (userHash, from, to, currency, acctModel) tuple. This matters
	// because history/stats/returns handlers all derive their output from the
	// same underlying daily-values computation, and the frontend commonly fires
	// them in parallel on landing.
	sfDailyValues singleflight.Group
}

// NewService creates a new portfolio service.
func NewService(mp market.Provider, fxSvc *fx.Service, cashBucketExpiryDays int) *Service {
	return &Service{MarketProvider: mp, FXService: fxSvc, CashBucketExpiryDays: cashBucketExpiryDays}
}

// fxMemo is a per-request memoization layer on top of FXService. It collapses
// repeated (fromCurrency, toCurrency[, date]) lookups inside a single request so
// that we never call the underlying FX provider twice for the same conversion.
// Rates are computed lazily and stored with a sync.RWMutex so the memo is safe
// for concurrent use by the parallel holdings loop.
// isFXTrade delegates to the centralized check in models.
func isFXTrade(t models.Trade) bool {
	return models.IsFXTrade(t)
}

// posKey returns a composite key for a (symbol, exchange) pair.
// When exchange is empty the key is just the symbol, preserving backward compatibility.
func posKey(symbol, exchange string) string {
	if exchange == "" {
		return symbol
	}
	return symbol + "@" + exchange
}

// getYahooSymbolMap extracts a map from composite posKey to YahooSymbol.
func (s *Service) getYahooSymbolMap(data *models.FlexQueryData) map[string]string {
	m := make(map[string]string)
	for _, t := range data.Trades {
		if t.YahooSymbol != "" {
			m[posKey(t.Symbol, t.ListingExchange)] = t.YahooSymbol
		}
	}
	for _, p := range data.OpenPositions {
		if p.YahooSymbol != "" {
			m[p.Symbol] = p.YahooSymbol
		}
	}
	return m
}
