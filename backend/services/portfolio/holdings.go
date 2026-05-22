package portfolio

import (
	"portfolio-analysis/models"
	"sort"
)

// GetCurrentHoldings returns the current holdings from a FlexQuery data set.
// It uses OpenPositions if available, otherwise reconstructs from trades.
func (s *Service) GetCurrentHoldings(data *models.FlexQueryData) []models.Holding {
	if len(data.OpenPositions) > 0 {
		holdings := make([]models.Holding, 0, len(data.OpenPositions))
		for _, op := range data.OpenPositions {
			holdings = append(holdings, models.Holding{
				Symbol:   op.Symbol,
				Quantity: op.Quantity,
				Currency: op.Currency,
			})
		}
		return holdings
	}
	return s.reconstructFromTrades(data.Trades)
}

// reconstructFromTrades builds holdings by netting all trades, keyed by symbol@exchange.
func (s *Service) reconstructFromTrades(trades []models.Trade) []models.Holding {
	posMap := make(map[string]*models.Holding)
	for _, t := range trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		k := posKey(t.Symbol, t.ListingExchange)
		h, ok := posMap[k]
		if !ok {
			h = &models.Holding{Symbol: t.Symbol, Currency: t.Currency, ListingExchange: t.ListingExchange}
			posMap[k] = h
		}
		h.Quantity += t.Quantity
	}

	var result []models.Holding
	for _, h := range posMap {
		result = append(result, *h)
	}
	sort.Slice(result, func(i, j int) bool {
		return posKey(result[i].Symbol, result[i].ListingExchange) < posKey(result[j].Symbol, result[j].ListingExchange)
	})
	return result
}
