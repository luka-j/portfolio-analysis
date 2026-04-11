package portfolio

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/cashbucket"
	"portfolio-analysis/services/fifo"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/stats"
)

// Service reconstructs and values portfolios from FlexQuery data.
type Service struct {
	MarketProvider       market.Provider
	CurrentPriceProvider market.CurrentPriceProvider
	FXService            *fx.Service
	CashBucketExpiryDays int
}

// NewService creates a new portfolio service.
func NewService(mp market.Provider, fxSvc *fx.Service, cpp market.CurrentPriceProvider, cashBucketExpiryDays int) *Service {
	return &Service{MarketProvider: mp, CurrentPriceProvider: cpp, FXService: fxSvc, CashBucketExpiryDays: cashBucketExpiryDays}
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
func (s *Service) GetCurrentValue(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool) (*models.PortfolioValueResponse, error) {
	holdings := s.GetCurrentHoldings(data)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	lookback := today.AddDate(0, 0, -5)

	costBasisMap, realizedGLMap, commissionsMap := s.computeCurrentValueMaps(data, currency, acctModel, cachedOnly)

	yMap := s.getYahooSymbolMap(data)

	var totalValue float64
	positions := make([]models.PositionValue, 0, len(holdings))
	for _, h := range holdings {
		k := posKey(h.Symbol, h.ListingExchange)
		querySymbol := h.Symbol
		if ys, ok := yMap[k]; ok && ys != "" {
			querySymbol = ys
		}

		latestPrice := 0.0
		var currentPriceErr, histErr error
		if s.CurrentPriceProvider != nil {
			p, err := s.CurrentPriceProvider.GetCurrentPrice(querySymbol, cachedOnly)
			if err != nil {
				log.Printf("Warning: fetching current price for %s (mapped to %s): %v; falling back to history", h.Symbol, querySymbol, err)
				currentPriceErr = err
			} else {
				latestPrice = p
			}
		}
		if latestPrice == 0 {
			prices, err := s.MarketProvider.GetHistory(querySymbol, lookback, today, cachedOnly)
			if err != nil {
				log.Printf("Warning: fetching price for %s (mapped to %s): %v", h.Symbol, querySymbol, err)
				histErr = err
			} else if len(prices) > 0 {
				latestPrice = prices[len(prices)-1].AdjClose
				if latestPrice == 0 {
					latestPrice = prices[len(prices)-1].Close
				}
			}
		}

		// Determine price status on a best-effort basis.
		var priceStatus string
		if latestPrice == 0 {
			if histErr != nil || currentPriceErr != nil {
				priceStatus = "fetch_failed"
			} else if checker, ok := s.MarketProvider.(market.PriceStatusChecker); ok && checker.HasCachedData(querySymbol) {
				priceStatus = "stale"
			} else {
				priceStatus = "no_data"
			}
		}
		nativeValue := h.Quantity * latestPrice

		var convertedPrice, convertedValue float64
		var err error
		if currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal {
			convertedPrice = latestPrice
			convertedValue = nativeValue
		} else {
			convertedPrice, err = s.FXService.ConvertSpot(latestPrice, h.Currency, currency, cachedOnly)
			if err != nil {
				return nil, fmt.Errorf("converting price %s to %s: %w", h.Currency, currency, err)
			}
			convertedValue, err = s.FXService.ConvertSpot(nativeValue, h.Currency, currency, cachedOnly)
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
			PriceStatus:     priceStatus,
		})
	}

	// Compute pending cash from unsettled sale buckets.
	pendingCash, err := s.computePendingCash(data, currency, acctModel, cachedOnly, today)
	if err != nil {
		log.Printf("Warning: computing pending cash: %v", err)
	} else if pendingCash > 0 {
		totalValue += pendingCash
		positions = append(positions, models.PositionValue{
			Symbol:         "PENDING_CASH",
			NativeCurrency: currency,
			Prices:         map[string]float64{currency: 1},
			CostBases:      map[string]float64{currency: 0},
			Values:         map[string]float64{currency: pendingCash},
			Price:          1,
			Value:          pendingCash,
			Quantity:       pendingCash,
		})
	}

	return &models.PortfolioValueResponse{
		Value:     totalValue,
		Currency:  currency,
		Positions: positions,
		PendingCash: pendingCash,
	}, nil
}

// computeCurrentValueMaps returns cost-basis, realized GL, and commissions maps in
// a single pass over trades, calling fifo.Match once per position instead of twice.
func (s *Service) computeCurrentValueMaps(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool) (costBasisMap, realizedGLMap, commissionsMap map[string]float64) {
	costBasisMap = make(map[string]float64)
	realizedGLMap = make(map[string]float64)
	commissionsMap = make(map[string]float64)

	isOriginal := currency == "Original" || currency == "original" || acctModel == models.AccountingModelOriginal

	// Group all trades by posKey for FIFO matching (includes TRANSFER_IN, which
	// represents received shares with a cost basis; FX trades are grouped under
	// their own keys and silently ignored when building PositionValue entries).
	tradesByKey := make(map[string][]models.Trade)
	for _, t := range data.Trades {
		k := posKey(t.Symbol, t.ListingExchange)
		tradesByKey[k] = append(tradesByKey[k], t)
	}

	for key, trades := range tradesByKey {
		sort.Slice(trades, func(i, j int) bool {
			return trades[i].DateTime.Before(trades[j].DateTime)
		})
		openLots, matchedSells := fifo.Match(trades)

		// ── Cost basis ──────────────────────────────────────────────────────
		nativeCurrency := ""
		if len(openLots) > 0 {
			nativeCurrency = openLots[0].Curr
		} else if len(trades) > 0 {
			nativeCurrency = trades[0].Currency
		}
		if len(openLots) == 0 {
			costBasisMap[key] = 0
		} else {
			var totalCost, totalQty float64
			for _, l := range openLots {
				var p float64
				if isOriginal {
					p = l.Price
				} else if acctModel == models.AccountingModelSpot {
					p, _ = s.FXService.ConvertSpot(l.Price, l.Curr, currency, cachedOnly)
				} else {
					p, _ = s.FXService.Convert(l.Price, l.Curr, currency, l.Date, cachedOnly)
				}
				totalCost += l.Qty * p
				totalQty += l.Qty
			}
			if totalQty > 0 {
				costBasisMap[key] = totalCost / totalQty
			} else {
				costBasisMap[key], _ = s.FXService.ConvertSpot(0, nativeCurrency, currency, cachedOnly)
			}
		}

		// ── Realized GL ─────────────────────────────────────────────────────
		var gl float64
		for _, m := range matchedSells {
			var profit float64
			if isOriginal {
				profit = m.Qty * (m.SellPrice - m.CostPrice)
			} else if acctModel == models.AccountingModelSpot {
				sp, _ := s.FXService.ConvertSpot(m.SellPrice, m.Curr, currency, cachedOnly)
				cp, _ := s.FXService.ConvertSpot(m.CostPrice, m.Curr, currency, cachedOnly)
				profit = m.Qty * (sp - cp)
			} else {
				sp, _ := s.FXService.Convert(m.SellPrice, m.Curr, currency, m.SellDate, cachedOnly)
				cp, _ := s.FXService.Convert(m.CostPrice, m.Curr, currency, m.CostDate, cachedOnly)
				profit = m.Qty * (sp - cp)
			}
			gl += profit
			if m.Comm != 0 {
				if isOriginal {
					gl += m.Comm
				} else if acctModel == models.AccountingModelSpot {
					c, _ := s.FXService.ConvertSpot(m.Comm, m.Curr, currency, cachedOnly)
					gl += c
				} else {
					c, _ := s.FXService.Convert(m.Comm, m.Curr, currency, m.SellDate, cachedOnly)
					gl += c
				}
			}
		}
		realizedGLMap[key] = gl

		// ── Commissions (FX trades and transfers excluded) ───────────────────
		for _, t := range trades {
			if isFXTrade(t) || t.BuySell == "TRANSFER_IN" || t.Commission == 0 {
				continue
			}
			var comm float64
			if isOriginal {
				comm = t.Commission
			} else if acctModel == models.AccountingModelSpot {
				comm, _ = s.FXService.ConvertSpot(t.Commission, t.Currency, currency, cachedOnly)
			} else {
				comm, _ = s.FXService.Convert(t.Commission, t.Currency, currency, t.DateTime, cachedOnly)
			}
			commissionsMap[key] += comm
		}
	}

	// ── OpenPositions fallback for cost basis ────────────────────────────────
	for _, op := range data.OpenPositions {
		if _, seen := costBasisMap[op.Symbol]; seen {
			continue
		}
		if isOriginal {
			costBasisMap[op.Symbol] = op.CostBasisPerShare
		} else if acctModel == models.AccountingModelSpot {
			costBasisMap[op.Symbol], _ = s.FXService.ConvertSpot(op.CostBasisPerShare, op.Currency, currency, cachedOnly)
		}
	}

	return
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
			cp, err := s.FXService.Convert(t.Price, t.Currency, displayCurrency, t.DateTime, false) // trades usually don't need cachedOnly logic
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
			ID:             t.PublicID,
			EntryMethod:    t.EntryMethod,
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
// Prices are loaded one symbol at a time so each symbol's slice is GC-eligible
// before the next one loads, keeping peak memory proportional to a single
// symbol's history rather than all symbols' histories combined.
func (s *Service) GetDailyValues(
	data *models.FlexQueryData,
	from, to time.Time,
	currency string,
	acctModel models.AccountingModel,
	cachedOnly bool,
) (*models.PortfolioHistoryResponse, error) {

	// Validate single currency when using original accounting model.
	if acctModel == models.AccountingModelOriginal {
		currencies := make(map[string]bool)
		for _, t := range data.Trades {
			if !isFXTrade(t) && t.BuySell != "TRANSFER_IN" && t.Currency != "" {
				currencies[t.Currency] = true
			}
		}
		if len(currencies) > 1 {
			return nil, fmt.Errorf("cannot aggregate multi-currency portfolio using 'original' accounting model")
		}
	}

	// One lightweight DB query to get the trading calendar for the range.
	validDates, err := s.MarketProvider.TradingDates(from, to)
	if err != nil {
		return nil, fmt.Errorf("GetDailyValues: trading dates: %w", err)
	}

	// Pre-load FX histories as sorted slices (typically 0–2 currency pairs).
	// Only needed for historical accounting mode; spot mode queries live rates per call.
	fxData := make(map[string][]models.PricePoint) // pairKey → sorted []PricePoint
	if acctModel == models.AccountingModelHistorical || acctModel == "" {
		nativeCurrencies := make(map[string]bool)
		for _, t := range data.Trades {
			if !isFXTrade(t) && t.Currency != "" && t.Currency != currency {
				nativeCurrencies[t.Currency] = true
			}
		}
		for fromCur := range nativeCurrencies {
			pairKey := fromCur + currency
			fxSymbol := fmt.Sprintf("%s%s=X", fromCur, currency)
			pts, err := s.MarketProvider.GetHistory(fxSymbol, from.AddDate(0, 0, -5), to, cachedOnly)
			if err != nil {
				log.Printf("Warning: pre-fetching FX %s: %v\n", fxSymbol, err)
				continue
			}
			fxData[pairKey] = pts // already sorted ASC by GetHistory
		}
	}

	// Pending-cash balance changes from sale buckets.
	var balanceChanges []cashbucket.BalanceChange
	if s.CashBucketExpiryDays > 0 {
		var tradeFlows []models.Trade
		for _, t := range data.Trades {
			if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
				continue
			}
			tradeFlows = append(tradeFlows, t)
		}
		bucketConvertFn := func(amount float64, cur string, date time.Time) (float64, error) {
			if cur == currency || acctModel == models.AccountingModelOriginal || s.FXService == nil {
				return amount, nil
			}
			if acctModel == models.AccountingModelSpot {
				return s.FXService.ConvertSpot(amount, cur, currency, cachedOnly)
			}
			return s.FXService.Convert(amount, cur, currency, date, cachedOnly)
		}
		br, err := cashbucket.Process(tradeFlows, nil, s.CashBucketExpiryDays, to, bucketConvertFn)
		if err != nil {
			return nil, fmt.Errorf("GetDailyValues bucket balance: %w", err)
		}
		balanceChanges = br.BalanceChanges
	}

	// Group and sort trades by posKey. FX trades and transfers are excluded —
	// they don't contribute to equity position values.
	yMap := s.getYahooSymbolMap(data)
	tradesByKey := make(map[string][]models.Trade)
	for _, t := range data.Trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		k := posKey(t.Symbol, t.ListingExchange)
		tradesByKey[k] = append(tradesByKey[k], t)
	}
	for k := range tradesByKey {
		sort.Slice(tradesByKey[k], func(i, j int) bool {
			return tradesByKey[k][i].DateTime.Before(tradesByKey[k][j].DateTime)
		})
	}

	// Column-major accumulation: one symbol at a time.
	// dailyTotals[i] is the running portfolio value for validDates[i].
	dailyTotals := make([]float64, len(validDates))
	fromMidnight := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())

	for k, trades := range tradesByKey {
		// Resolve the Yahoo Finance query symbol.
		querySymbol := k
		if idx := strings.Index(k, "@"); idx != -1 {
			querySymbol = k[:idx]
		}
		if ys, ok := yMap[k]; ok && ys != "" {
			querySymbol = ys
		}

		prices, err := s.MarketProvider.GetHistory(querySymbol, from, to, cachedOnly)
		if err != nil {
			log.Printf("Warning: fetching %s historical data: %v\n", querySymbol, err)
		}
		// After this symbol's inner loop, prices is GC-eligible.

		nativeCurrency := ""
		for _, t := range trades {
			if t.Currency != "" {
				nativeCurrency = t.Currency
				break
			}
		}

		// Fast-forward trades that settled before the start of the requested range
		// to establish the opening position.
		qty := 0.0
		ti := 0
		for ti < len(trades) && trades[ti].DateTime.Before(fromMidnight) {
			qty += trades[ti].Quantity
			ti++
		}

		for i, d := range validDates {
			// Apply all trades through the end of this trading day.
			endOfDay := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 999999999, d.Location())
			for ti < len(trades) && !trades[ti].DateTime.After(endOfDay) {
				qty += trades[ti].Quantity
				ti++
			}

			if qty > -1e-5 && qty < 1e-5 {
				continue
			}
			price := priceAt(prices, d)
			if price == 0 {
				continue
			}
			nativeVal := qty * price

			switch acctModel {
			case models.AccountingModelOriginal:
				dailyTotals[i] += nativeVal
			case models.AccountingModelSpot:
				if nativeCurrency == currency || nativeCurrency == "" {
					dailyTotals[i] += nativeVal
				} else {
					v, err := s.FXService.ConvertSpot(nativeVal, nativeCurrency, currency, cachedOnly)
					if err != nil {
						return nil, err
					}
					dailyTotals[i] += v
				}
			default: // historical — use pre-fetched fxData sorted slices
				if nativeCurrency == currency || nativeCurrency == "" {
					dailyTotals[i] += nativeVal
				} else {
					pairKey := nativeCurrency + currency
					if pts, ok := fxData[pairKey]; ok {
						if rate := fxRateAt(pts, d); rate != 0 {
							dailyTotals[i] += nativeVal * rate
							continue
						}
					}
					// fxData miss — fall back to live DB query (should be rare).
					v, err := s.FXService.Convert(nativeVal, nativeCurrency, currency, d, cachedOnly)
					if err != nil {
						return nil, err
					}
					dailyTotals[i] += v
				}
			}
		}
		// prices slice is now eligible for GC before the next symbol loads.
	}

	// Apply pending cash and produce the final result slice.
	bcIdx := 0
	pendingTotal := 0.0
	result := make([]models.DailyValue, 0, len(validDates))
	for i, d := range validDates {
		for bcIdx < len(balanceChanges) && !balanceChanges[bcIdx].Date.After(d) {
			pendingTotal += balanceChanges[bcIdx].Delta
			bcIdx++
		}
		total := dailyTotals[i]
		if pendingTotal > 0 {
			total += pendingTotal
		}
		result = append(result, models.DailyValue{
			Date:  d.Format("2006-01-02"),
			Value: total,
		})
	}

	return &models.PortfolioHistoryResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Data:            result,
	}, nil
}

// priceAt returns the last known AdjClose (fallback: Close) price on or before d
// using binary search. prices must be sorted ascending by Date.
func priceAt(prices []models.PricePoint, d time.Time) float64 {
	if len(prices) == 0 {
		return 0
	}
	// Find the first index whose Date is strictly after d, then step back one.
	i := sort.Search(len(prices), func(j int) bool {
		return prices[j].Date.After(d)
	}) - 1
	if i < 0 {
		return 0
	}
	p := prices[i].AdjClose
	if p == 0 {
		p = prices[i].Close
	}
	return p
}

// fxRateAt returns the last known Close FX rate on or before d using binary search.
// prices must be sorted ascending by Date.
func fxRateAt(prices []models.PricePoint, d time.Time) float64 {
	if len(prices) == 0 {
		return 0
	}
	i := sort.Search(len(prices), func(j int) bool {
		return prices[j].Date.After(d)
	}) - 1
	if i < 0 {
		return 0
	}
	return prices[i].Close
}

// GetCashFlows returns external cash flows for IRR/TWR calculation, converted to the target currency.
// It applies cash-bucket logic to prevent cross-broker reinvestments from appearing as outflows+inflows.
func (s *Service) GetCashFlows(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool, asOf time.Time) ([]models.CashFlow, error) {
	var rawTradeFlows []models.Trade
	for _, t := range data.Trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		rawTradeFlows = append(rawTradeFlows, t)
	}

	// Collect dividend/withholding flows (pass through unchanged).
	var dividendFlows []models.CashFlow
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
				amount, err = s.FXService.ConvertSpot(amount, ct.Currency, currency, cachedOnly)
			} else {
				amount, err = s.FXService.Convert(amount, ct.Currency, currency, ct.DateTime, cachedOnly)
			}
			if err != nil {
				return nil, err
			}
		}
		dividendFlows = append(dividendFlows, models.CashFlow{Date: ct.DateTime, Amount: amount})
	}

	// Build a convert function for the cashbucket processor.
	convertFn := func(amount float64, from string, date time.Time) (float64, error) {
		if from == currency || acctModel == models.AccountingModelOriginal {
			return amount, nil
		}
		if acctModel == models.AccountingModelSpot {
			return s.FXService.ConvertSpot(amount, from, currency, cachedOnly)
		}
		return s.FXService.Convert(amount, from, currency, date, cachedOnly)
	}

	result, err := cashbucket.Process(rawTradeFlows, dividendFlows, s.CashBucketExpiryDays, asOf, convertFn)
	if err != nil {
		return nil, fmt.Errorf("cashbucket.Process: %w", err)
	}

	return result.AdjustedCashFlows, nil
}

// GetDailyReturns returns cash-flow-adjusted daily portfolio return series for statistics.
// Cash flows (deposits/withdrawals) are removed from each day's return so that the series
// reflects pure market performance, comparable to a benchmark's price return series.
func (s *Service) GetDailyReturns(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel) ([]float64, []string, []string, error) {
	hist, err := s.GetDailyValues(data, from, to, currency, acctModel, false) // statistics usually want fresh data
	if err != nil {
		return nil, nil, nil, err
	}

	cashFlows, err := s.GetCashFlows(data, currency, acctModel, false, to)
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


// GetCumulativeTWR computes the day-by-day cumulative Time-Weighted Return series
// over [from, to].  Each data point expresses the portfolio's growth factor (as a
// percentage) relative to the first day, properly adjusted for external cash flows
// (deposits / withdrawals) so that capital movements do not distort the metric.
func (s *Service) GetCumulativeTWR(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel, cachedOnly bool) (*models.PortfolioHistoryResponse, error) {
	hist, err := s.GetDailyValues(data, from, to, currency, acctModel, cachedOnly)
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

	cashFlows, err := s.GetCashFlows(data, currency, acctModel, cachedOnly, to)
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

func (s *Service) GetCumulativeMWR(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel, cachedOnly bool) (*models.PortfolioHistoryResponse, error) {
	hist, err := s.GetDailyValues(data, from, to, currency, acctModel, cachedOnly)
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

	cashFlows, err := s.GetCashFlows(data, currency, acctModel, cachedOnly, to)
	if err != nil {
		return nil, err
	}

	result := make([]models.DailyValue, 0, len(hist.Data))
	// First point is always 0% growth.
	result = append(result, models.DailyValue{Date: hist.Data[0].Date, Value: 0})

	var baseCashFlows []models.CashFlow
	if hist.Data[0].Value > 0 {
		baseCashFlows = append(baseCashFlows, models.CashFlow{
			Date:   from,
			Amount: -hist.Data[0].Value,
		})
	}

	actualFromStr := hist.Data[0].Date

	cfIdx := 0
	for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= actualFromStr {
		cfIdx++
	}

	var currentCashFlows []models.CashFlow
	currentCashFlows = append(currentCashFlows, baseCashFlows...)

	for i := 1; i < len(hist.Data); i++ {
		curValue := hist.Data[i].Value
		dateStr := hist.Data[i].Date

		for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= dateStr {
			currentCashFlows = append(currentCashFlows, cashFlows[cfIdx])
			cfIdx++
		}

		curDate, _ := time.Parse("2006-01-02", dateStr)

		var mwrVal float64
		// We can only compute MWR if we have cash flows (which we always do via baseCashFlows)
		mwr, err := stats.CalculateMWR(currentCashFlows, curValue, curDate)
		if err == nil {
			mwrVal = mwr * 100 // express as percentage like TWR
		} else {
			// Fallback to previous MWR if it fails to converge
			if i > 1 {
				mwrVal = result[i-1].Value
			} else {
				mwrVal = 0
			}
		}

		result = append(result, models.DailyValue{
			Date:  dateStr,
			Value: mwrVal,
		})
	}

	return &models.PortfolioHistoryResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Data:            result,
	}, nil
}


// computePendingCash returns the current aggregate value of all active (non-expired) cash buckets.
func (s *Service) computePendingCash(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool, asOf time.Time) (float64, error) {
	if s.CashBucketExpiryDays == 0 {
		return 0, nil
	}

	var trades []models.Trade
	for _, t := range data.Trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		trades = append(trades, t)
	}

	convertFn := func(amount float64, from string, date time.Time) (float64, error) {
		if from == currency || acctModel == models.AccountingModelOriginal {
			return amount, nil
		}
		if acctModel == models.AccountingModelSpot {
			return s.FXService.ConvertSpot(amount, from, currency, cachedOnly)
		}
		return s.FXService.Convert(amount, from, currency, date, cachedOnly)
	}

	result, err := cashbucket.Process(trades, nil, s.CashBucketExpiryDays, asOf, convertFn)
	if err != nil {
		return 0, fmt.Errorf("computePendingCash: %w", err)
	}
	return result.PendingCash, nil
}
