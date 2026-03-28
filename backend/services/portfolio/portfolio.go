package portfolio

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"gofolio-analysis/models"
	"gofolio-analysis/services/fifo"
	"gofolio-analysis/services/fx"
	"gofolio-analysis/services/market"
)

// Service reconstructs and values portfolios from FlexQuery data.
type Service struct {
	MarketProvider market.Provider
	FXService      *fx.Service
}

// NewService creates a new portfolio service.
func NewService(mp market.Provider, fxSvc *fx.Service) *Service {
	return &Service{MarketProvider: mp, FXService: fxSvc}
}

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

// GetCurrentValue returns the portfolio value in the requested display currency.
func (s *Service) GetCurrentValue(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) (*models.PortfolioValueResponse, error) {
	holdings := s.GetCurrentHoldings(data)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	lookback := today.AddDate(0, 0, -5)

	costBasisMap := s.computeCostBasis(data, currency, acctModel)
	realizedGLMap := s.computeRealizedGL(data, currency, acctModel)
	commissionsMap := s.computeCommissions(data, currency, acctModel)

	yMap := s.getYahooSymbolMap(data)

	var totalValue float64
	var positions []models.PositionValue
	for _, h := range holdings {
		k := posKey(h.Symbol, h.ListingExchange)
		querySymbol := h.Symbol
		if ys, ok := yMap[k]; ok && ys != "" {
			querySymbol = ys
		}

		prices, err := s.MarketProvider.GetHistory(querySymbol, lookback, today)
		if err != nil {
			log.Printf("Warning: fetching price for %s (mapped to %s): %v", h.Symbol, querySymbol, err)
		}

		latestPrice := 0.0
		if len(prices) > 0 {
			latestPrice = prices[len(prices)-1].AdjClose
			if latestPrice == 0 {
				latestPrice = prices[len(prices)-1].Close
			}
		}
		nativeValue := h.Quantity * latestPrice

		var convertedPrice, convertedValue float64
		if currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal {
			convertedPrice = latestPrice
			convertedValue = nativeValue
		} else {
			convertedPrice, err = s.FXService.ConvertSpot(latestPrice, h.Currency, currency)
			if err != nil {
				return nil, fmt.Errorf("converting price %s to %s: %w", h.Currency, currency, err)
			}
			convertedValue, err = s.FXService.ConvertSpot(nativeValue, h.Currency, currency)
			if err != nil {
				return nil, fmt.Errorf("converting value %s to %s: %w", h.Currency, currency, err)
			}
		}
		totalValue += convertedValue

		positions = append(positions, models.PositionValue{
			Symbol:          h.Symbol,
			ListingExchange: h.ListingExchange,
			YahooSymbol:     yMap[k],
			Quantity:        h.Quantity,
			NativeCurrency:  h.Currency,
			Prices:          map[string]float64{currency: convertedPrice},
			CostBases:       map[string]float64{currency: costBasisMap[k]},
			Values:          map[string]float64{currency: convertedValue},
			Price:           convertedPrice,
			CostBasis:       costBasisMap[k],
			RealizedGL:      realizedGLMap[k],
			Value:           convertedValue,
			Commission:      commissionsMap[k],
		})
	}

	return &models.PortfolioValueResponse{
		Value:     totalValue,
		Currency:  currency,
		Positions: positions,
	}, nil
}

// computeCostBasis returns a map of posKey -> average cost basis per share in the given currency.
// Only open FIFO lots contribute, giving the weighted average for the current position.
func (s *Service) computeCostBasis(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) map[string]float64 {
	result := make(map[string]float64)

	tradesByKey := make(map[string][]models.Trade)
	for _, t := range data.Trades {
		k := posKey(t.Symbol, t.ListingExchange)
		tradesByKey[k] = append(tradesByKey[k], t)
	}

	for key, trades := range tradesByKey {
		sort.Slice(trades, func(i, j int) bool {
			return trades[i].DateTime.Before(trades[j].DateTime)
		})

		openLots, _ := fifo.Match(trades)

		nativeCurrency := ""
		if len(openLots) > 0 {
			nativeCurrency = openLots[0].Curr
		} else if len(trades) > 0 {
			nativeCurrency = trades[0].Currency
		}

		if len(openLots) == 0 {
			result[key] = 0
			continue
		}

		var totalCost, totalQty float64
		for _, l := range openLots {
			var priceInCur float64
			if currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal {
				priceInCur = l.Price
			} else if acctModel == models.AccountingModelSpot {
				priceInCur, _ = s.FXService.ConvertSpot(l.Price, l.Curr, currency)
			} else {
				priceInCur, _ = s.FXService.Convert(l.Price, l.Curr, currency, l.Date)
			}
			totalCost += l.Qty * priceInCur
			totalQty += l.Qty
		}
		if totalQty > 0 {
			result[key] = totalCost / totalQty
		} else {
			converted, _ := s.FXService.ConvertSpot(0, nativeCurrency, currency)
			result[key] = converted
		}
	}

	// Also handle symbols that appear only in OpenPositions (no matching trades).
	for _, op := range data.OpenPositions {
		if _, seen := result[op.Symbol]; seen {
			continue
		}
		if currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal {
			result[op.Symbol] = op.CostBasisPerShare
		} else if acctModel == models.AccountingModelSpot {
			converted, _ := s.FXService.ConvertSpot(op.CostBasisPerShare, op.Currency, currency)
			result[op.Symbol] = converted
		}
	}

	return result
}

// computeRealizedGL computes total realized gain/loss per position in the given currency.
func (s *Service) computeRealizedGL(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) map[string]float64 {
	result := make(map[string]float64)
	tradesByKey := make(map[string][]models.Trade)
	for _, t := range data.Trades {
		k := posKey(t.Symbol, t.ListingExchange)
		tradesByKey[k] = append(tradesByKey[k], t)
	}

	for key, trades := range tradesByKey {
		sort.Slice(trades, func(i, j int) bool {
			return trades[i].DateTime.Before(trades[j].DateTime)
		})

		_, matchedSells := fifo.Match(trades)

		var realizedGL float64
		for _, m := range matchedSells {
			var profit float64
			if currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal {
				profit = m.Qty * (m.SellPrice - m.CostPrice)
			} else if acctModel == models.AccountingModelSpot {
				sellPriceSpot, _ := s.FXService.ConvertSpot(m.SellPrice, m.Curr, currency)
				costPriceSpot, _ := s.FXService.ConvertSpot(m.CostPrice, m.Curr, currency)
				profit = m.Qty * (sellPriceSpot - costPriceSpot)
			} else {
				sellPriceHist, _ := s.FXService.Convert(m.SellPrice, m.Curr, currency, m.SellDate)
				costPriceHist, _ := s.FXService.Convert(m.CostPrice, m.Curr, currency, m.CostDate)
				profit = m.Qty * (sellPriceHist - costPriceHist)
			}
			realizedGL += profit

			if m.Comm != 0 {
				if currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal {
					realizedGL += m.Comm
				} else if acctModel == models.AccountingModelSpot {
					commSpot, _ := s.FXService.ConvertSpot(m.Comm, m.Curr, currency)
					realizedGL += commSpot
				} else {
					commHist, _ := s.FXService.Convert(m.Comm, m.Curr, currency, m.SellDate)
					realizedGL += commHist
				}
			}
		}
		result[key] = realizedGL
	}

	return result
}

// computeCommissions sums all trade commissions per position in the given currency.
func (s *Service) computeCommissions(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) map[string]float64 {
	result := make(map[string]float64)

	tradesByKey := make(map[string][]models.Trade)
	for _, t := range data.Trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		k := posKey(t.Symbol, t.ListingExchange)
		tradesByKey[k] = append(tradesByKey[k], t)
	}

	for key, trades := range tradesByKey {
		var total float64
		for _, t := range trades {
			if t.Commission == 0 {
				continue
			}
			var comm float64
			if currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal {
				comm = t.Commission
			} else if acctModel == models.AccountingModelSpot {
				comm, _ = s.FXService.ConvertSpot(t.Commission, t.Currency, currency)
			} else {
				comm, _ = s.FXService.Convert(t.Commission, t.Currency, currency, t.DateTime)
			}
			total += comm
		}
		result[key] = total
	}

	return result
}

// GetTradesForSymbol returns the trades for a specific symbol+exchange in a
// frontend-friendly format, with prices converted to displayCurrency.
func (s *Service) GetTradesForSymbol(data *models.FlexQueryData, symbol, exchange, displayCurrency string) (*models.TradesResponse, error) {
	var entries []models.TradeEntry
	nativeCurrency := ""

	for _, t := range data.Trades {
		if t.BuySell == "TRANSFER_IN" {
			continue
		}
		if t.Symbol != symbol {
			continue
		}
		if exchange != "" && t.ListingExchange != exchange {
			continue
		}
		nativeCurrency = t.Currency

		convertedPrice := t.Price
		if displayCurrency != "Original" && displayCurrency != "original" && t.Currency != displayCurrency {
			cp, err := s.FXService.Convert(t.Price, t.Currency, displayCurrency, t.DateTime)
			if err != nil {
				// Fall back to native price on FX error
				cp = t.Price
			}
			convertedPrice = cp
		}

		side := t.BuySell
		if side == "" {
			side = "BUY"
			if t.Quantity < 0 {
				side = "SELL"
			}
		}

		qty := t.Quantity
		if qty < 0 {
			qty = -qty
		}

		entries = append(entries, models.TradeEntry{
			Date:           t.DateTime.Format("2006-01-02"),
			Side:           side,
			Quantity:       qty,
			Price:          t.Price,
			NativeCurrency: t.Currency,
			ConvertedPrice: convertedPrice,
			Commission:     t.Commission,
			Proceeds:       t.Proceeds,
			TaxCostBasis:   t.TaxCostBasis,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Date > entries[j].Date
	})

	return &models.TradesResponse{
		Symbol:          symbol,
		Currency:        nativeCurrency,
		DisplayCurrency: displayCurrency,
		Trades:          entries,
	}, nil
}

// GetDailyValues returns the portfolio value for each day in [from, to].
func (s *Service) GetDailyValues(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel) (*models.PortfolioHistoryResponse, error) {
	// Build daily holdings via trade replay.
	dailyHoldings := s.buildDailyHoldings(data, from, to)

	// Pre-fetch prices for all symbols we'll need.
	symbols := s.allSymbols(data)
	// priceCache: posKey -> date_str -> adj-close price (fix 1.4)
	priceCache := make(map[string]map[string]float64)
	validDates := make(map[string]bool)
	yMap := s.getYahooSymbolMap(data)

	for _, pk := range symbols {
		querySymbol := pk
		// If the symbol has a '@' separator, split it to get the base symbol for fallback
		baseSymbol := pk
		if idx := strings.Index(pk, "@"); idx != -1 {
			baseSymbol = pk[:idx]
		}

		if ys, ok := yMap[pk]; ok && ys != "" {
			querySymbol = ys
		} else {
			// Fallback: if no exchange-specific mapping, try the base symbol
			querySymbol = baseSymbol
		}

		prices, err := s.MarketProvider.GetHistory(querySymbol, from, to)
		if err != nil {
			log.Printf("Warning: fetching %s historical data (mapped to %s): %v\n", pk, querySymbol, err)
			prices = []models.PricePoint{}
		}

		pc := make(map[string]float64)
		for _, p := range prices {
			ds := p.Date.Format("2006-01-02")
			validDates[ds] = true
			// Prefer AdjClose (split-adjusted); fall back to Close if zero.
			px := p.AdjClose
			if px == 0 {
				px = p.Close
			}
			pc[ds] = px
		}

		// Forward-fill missing days (weekends/holidays) so prices carry over.
		var lastPrice float64
		if len(prices) > 0 {
			lp := prices[0].AdjClose
			if lp == 0 {
				lp = prices[0].Close
			}
			lastPrice = lp
		}
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			if p, ok := pc[ds]; ok {
				lastPrice = p
			} else if lastPrice != 0 {
				pc[ds] = lastPrice
			}
		}

		priceCache[pk] = pc
	}

	// --- Fix 2.1: Pre-fetch FX rate histories before the daily loop ---
	// Collect the unique native currencies we'll need to convert.
	nativeCurrencies := make(map[string]bool)
	for _, dayHoldings := range dailyHoldings {
		for _, h := range dayHoldings {
			if h.Currency != currency && h.Currency != "" {
				nativeCurrencies[h.Currency] = true
			}
		}
	}

	// Fix 1.8: validate single currency when using original accounting model
	allCurrencies := make(map[string]bool)
	for _, dayHoldings := range dailyHoldings {
		for _, h := range dayHoldings {
			if h.Currency != "" {
				allCurrencies[h.Currency] = true
			}
		}
	}
	if acctModel == models.AccountingModelOriginal && len(allCurrencies) > 1 {
		return nil, fmt.Errorf("cannot aggregate multi-currency portfolio using 'original' accounting model")
	}
	// fxCache: "FROMTO" -> date_str -> rate
	fxCache := make(map[string]map[string]float64)
	if acctModel == models.AccountingModelHistorical || acctModel == "" {
		for fromCur := range nativeCurrencies {
			pairKey := fromCur + currency
			fxSymbol := fmt.Sprintf("%s%s=X", fromCur, currency)
			points, err := s.MarketProvider.GetHistory(fxSymbol, from.AddDate(0, 0, -5), to)
			if err != nil {
				// If FX pair fails, we'll fall back to spot; leave cache empty for this pair.
				log.Printf("Warning: pre-fetching FX %s: %v\n", fxSymbol, err)
				continue
			}
			pc := make(map[string]float64)
			for _, p := range points {
				pc[p.Date.Format("2006-01-02")] = p.Close
			}
			// Forward-fill FX rates.
			var lastRate float64
			if len(points) > 0 {
				lastRate = points[0].Close
			}
			for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
				ds := d.Format("2006-01-02")
				if r, ok := pc[ds]; ok {
					lastRate = r
				} else if lastRate != 0 {
					pc[ds] = lastRate
				}
			}
			fxCache[pairKey] = pc
		}
	}

	var result []models.DailyValue
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")

		// Omit weekends & bank holidays ONLY if we have some valid market dates.
		// If validDates is empty (e.g. all symbols failed), we show all days.
		if len(validDates) > 0 && !validDates[ds] {
			continue
		}

		holdings := dailyHoldings[ds]
		totalValue := 0.0

		for _, h := range holdings {
			k := posKey(h.Symbol, h.ListingExchange)
			pc, ok := priceCache[k]
			if !ok {
				continue
			}
			price, ok := pc[ds]
			if !ok {
				continue
			}
			nativeValue := h.Quantity * price

			switch acctModel {
			case models.AccountingModelOriginal:
				totalValue += nativeValue
			case models.AccountingModelSpot:
				converted, err := s.FXService.ConvertSpot(nativeValue, h.Currency, currency)
				if err != nil {
					return nil, err
				}
				totalValue += converted
			default: // historical — use pre-fetched fxCache
				if h.Currency == currency || h.Currency == "" {
					totalValue += nativeValue
					continue
				}
				pairKey := h.Currency + currency
				if rateMap, ok := fxCache[pairKey]; ok {
					if rate, ok := rateMap[ds]; ok && rate != 0 {
						totalValue += nativeValue * rate
						continue
					}
				}
				// fxCache miss — fall back to live DB query (should be rare).
				converted, err := s.FXService.Convert(nativeValue, h.Currency, currency, d)
				if err != nil {
					return nil, err
				}
				totalValue += converted
			}
		}

		result = append(result, models.DailyValue{Date: ds, Value: totalValue})
	}

	return &models.PortfolioHistoryResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Data:            result,
	}, nil
}

// GetCashFlows returns external cash flows for IRR calculation, converted to the target currency.
func (s *Service) GetCashFlows(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) ([]models.CashFlow, error) {
	var flows []models.CashFlow

	for _, t := range data.Trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		// A buy introduces cash into the equity portfolio (Deposit). t.Proceeds is negative.
		// A sell extracts cash (Withdrawal). t.Proceeds is positive.
		amount := t.Proceeds + t.Commission
		var err error
		if t.Currency != currency && acctModel != models.AccountingModelOriginal {
			if acctModel == models.AccountingModelSpot {
				amount, err = s.FXService.ConvertSpot(amount, t.Currency, currency)
			} else {
				amount, err = s.FXService.Convert(amount, t.Currency, currency, t.DateTime)
			}
			if err != nil {
				return nil, err
			}
		}
		flows = append(flows, models.CashFlow{Date: t.DateTime, Amount: amount})
	}

	for _, ct := range data.CashTransactions {
		isValidTx := ct.Type == "Dividends" ||
			ct.Type == "Withholding Tax" ||
			ct.Type == "Payment In Lieu Of Dividends"
		if !isValidTx {
			continue
		}
		amount := ct.Amount
		var err error
		if ct.Currency != currency && acctModel != models.AccountingModelOriginal {
			if acctModel == models.AccountingModelSpot {
				amount, err = s.FXService.ConvertSpot(amount, ct.Currency, currency)
			} else {
				amount, err = s.FXService.Convert(amount, ct.Currency, currency, ct.DateTime)
			}
			if err != nil {
				return nil, err
			}
		}
		flows = append(flows, models.CashFlow{Date: ct.DateTime, Amount: amount})
	}
	sort.Slice(flows, func(i, j int) bool { return flows[i].Date.Before(flows[j].Date) })
	return flows, nil
}

// GetDailyReturns returns cash-flow-adjusted daily portfolio return series for statistics.
// Cash flows (deposits/withdrawals) are removed from each day's return so that the series
// reflects pure market performance, comparable to a benchmark's price return series.
func (s *Service) GetDailyReturns(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel) ([]float64, []string, []string, error) {
	hist, err := s.GetDailyValues(data, from, to, currency, acctModel)
	if err != nil {
		return nil, nil, nil, err
	}

	cashFlows, err := s.GetCashFlows(data, currency, acctModel)
	if err != nil {
		return nil, nil, nil, err
	}

	var returns []float64
	var startDates []string
	var endDates []string

	cfIdx := 0
	// Skip any cash flows that occur on or before the first daily value date.
	if len(hist.Data) > 0 {
		for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= hist.Data[0].Date {
			cfIdx++
		}
	}

	for i := 1; i < len(hist.Data); i++ {
		prev := hist.Data[i-1].Value
		cur := hist.Data[i].Value
		dateStr := hist.Data[i].Date

		cfAmount := 0.0
		for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= dateStr {
			cfAmount += cashFlows[cfIdx].Amount
			cfIdx++
		}

		// Adjust the opening value for any external cash flow that arrived in this sub-period.
		adjustedPrev := prev - cfAmount
		if adjustedPrev <= 0 {
			continue
		}
		returns = append(returns, (cur/adjustedPrev)-1)
		startDates = append(startDates, hist.Data[i-1].Date)
		endDates = append(endDates, dateStr)
	}
	return returns, startDates, endDates, nil
}

// buildDailyHoldings builds a map from date string to holdings on that date.
func (s *Service) buildDailyHoldings(data *models.FlexQueryData, from, to time.Time) map[string][]models.Holding {
	sortedTrades := make([]models.Trade, len(data.Trades))
	copy(sortedTrades, data.Trades)
	sort.Slice(sortedTrades, func(i, j int) bool {
		return sortedTrades[i].DateTime.Before(sortedTrades[j].DateTime)
	})

	result := make(map[string][]models.Holding)
	currentHoldings := make(map[string]*models.Holding)
	tradeIdx := 0

	// Fast-forward trades that occurred before the 'from' date
	for tradeIdx < len(sortedTrades) {
		t := sortedTrades[tradeIdx]
		// Use midnight for 'from' comparison
		fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
		if t.DateTime.Before(fromDate) {
			if !isFXTrade(t) && t.BuySell != "TRANSFER_IN" {
				k := posKey(t.Symbol, t.ListingExchange)
				if _, ok := currentHoldings[k]; !ok {
					currentHoldings[k] = &models.Holding{Symbol: t.Symbol, Currency: t.Currency, ListingExchange: t.ListingExchange}
				}
				currentHoldings[k].Quantity += t.Quantity
			}
			tradeIdx++
		} else {
			break
		}
	}

	// For each day in the requested range, apply any trades that occur on that day,
	// then save a snapshot of the resulting holdings.
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		endOfDay := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 999999999, d.Location())

		for tradeIdx < len(sortedTrades) && !sortedTrades[tradeIdx].DateTime.After(endOfDay) {
			t := sortedTrades[tradeIdx]
			if !isFXTrade(t) && t.BuySell != "TRANSFER_IN" {
				k := posKey(t.Symbol, t.ListingExchange)
				if _, ok := currentHoldings[k]; !ok {
					currentHoldings[k] = &models.Holding{Symbol: t.Symbol, Currency: t.Currency, ListingExchange: t.ListingExchange}
				}
				currentHoldings[k].Quantity += t.Quantity
			}
			tradeIdx++
		}

		ds := d.Format("2006-01-02")
		var snapshot []models.Holding
		for _, h := range currentHoldings {
			// Float precision check: skip very tiny remainders after selling out
			if h.Quantity > 0.00001 || h.Quantity < -0.00001 {
				snapshot = append(snapshot, *h)
			}
		}
		result[ds] = snapshot
	}

	return result
}

// GetCumulativeTWR computes the day-by-day cumulative Time-Weighted Return series
// over [from, to].  Each data point expresses the portfolio's growth factor (as a
// percentage) relative to the first day, properly adjusted for external cash flows
// (deposits / withdrawals) so that capital movements do not distort the metric.
func (s *Service) GetCumulativeTWR(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel) (*models.PortfolioHistoryResponse, error) {
	hist, err := s.GetDailyValues(data, from, to, currency, acctModel)
	if err != nil {
		return nil, err
	}
	if len(hist.Data) < 2 {
		return &models.PortfolioHistoryResponse{
			Currency:        currency,
			AccountingModel: string(acctModel),
			Data:            hist.Data,
		}, nil
	}

	cashFlows, err := s.GetCashFlows(data, currency, acctModel)
	if err != nil {
		return nil, err
	}

	cfIdx := 0
	// Skip any cash flows that occur on or before the first daily value date.
	for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= hist.Data[0].Date {
		cfIdx++
	}

	// Chain sub-period returns.
	// cumProduct is the running product of (1 + sub-period return).
	// Start at 1.0 (= 0% growth).
	cumProduct := 1.0
	result := make([]models.DailyValue, 0, len(hist.Data))
	// First point is always 0% growth.
	result = append(result, models.DailyValue{Date: hist.Data[0].Date, Value: 0})

	for i := 1; i < len(hist.Data); i++ {
		prevValue := hist.Data[i-1].Value
		curValue := hist.Data[i].Value
		dateStr := hist.Data[i].Date

		// Accumulate any cash flows that arrived strictly after the previous
		// period's date and on or before the current period's date.
		// A deposit (negative amount in our convention) adds to the base; a withdrawal subtracts.
		cfAmount := 0.0
		for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= dateStr {
			cfAmount += cashFlows[cfIdx].Amount
			cfIdx++
		}

		adjustedPrev := prevValue - cfAmount

		if adjustedPrev > 0 {
			subReturn := curValue / adjustedPrev
			cumProduct *= subReturn
		}

		result = append(result, models.DailyValue{
			Date:  dateStr,
			Value: (cumProduct - 1.0) * 100, // express as percentage
		})
	}

	return &models.PortfolioHistoryResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Data:            result,
	}, nil
}

func (s *Service) allSymbols(data *models.FlexQueryData) []string {
	symSet := make(map[string]bool)
	for _, op := range data.OpenPositions {
		if op.AssetCategory == "CASH" {
			continue
		}
		// OpenPositions might not have exchange, so posKey returns just symbol
		symSet[posKey(op.Symbol, "")] = true
	}
	for _, t := range data.Trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		symSet[posKey(t.Symbol, t.ListingExchange)] = true
	}
	var syms []string
	for s := range symSet {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	return syms
}
