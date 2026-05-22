package portfolio

import (
	"fmt"
	"log/slog"
	"portfolio-analysis/models"
	"portfolio-analysis/services/cashbucket"
	"portfolio-analysis/services/stats"
	"sort"
	"strings"
	"sync"
	"time"
)

// GetDailyValues returns the portfolio value for each day in [from, to].
// Concurrent callers with the same (userHash, from, to, currency, acctModel)
// tuple share one underlying computation via singleflight — this is the dominant
// wall-clock win when the landing page fires history + stats + returns in parallel
// for the same range. Results are cloned so downstream mutation is safe.
func (s *Service) GetDailyValues(
	data *models.FlexQueryData,
	from, to time.Time,
	currency string,
	acctModel models.AccountingModel,
) (*models.PortfolioHistoryResponse, error) {
	key := fmt.Sprintf("dv|%s|%s|%s|%s|%s",
		data.UserHash,
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
		currency,
		string(acctModel),
	)
	v, err, _ := s.sfDailyValues.Do(key, func() (interface{}, error) {
		return s.getDailyValuesUncached(data, from, to, currency, acctModel)
	})
	if err != nil {
		return nil, err
	}
	orig := v.(*models.PortfolioHistoryResponse)
	dataCopy := make([]models.DailyValue, len(orig.Data))
	copy(dataCopy, orig.Data)
	return &models.PortfolioHistoryResponse{
		Currency:        orig.Currency,
		AccountingModel: orig.AccountingModel,
		Data:            dataCopy,
	}, nil
}

// getDailyValuesUncached performs the actual daily-value computation; always
// called through GetDailyValues so the singleflight wrapper can dedup in-flight
// requests across concurrent handlers.
func (s *Service) getDailyValuesUncached(
	data *models.FlexQueryData,
	from, to time.Time,
	currency string,
	acctModel models.AccountingModel,
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
			pts, err := s.MarketProvider.GetHistory(fxSymbol, from.AddDate(0, 0, -5), to)
			if err != nil {
				slog.Warn("portfolio: FX history prefetch failed", "pair", fxSymbol, "err", err)
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
				return s.FXService.ConvertSpot(amount, cur, currency)
			}
			return s.FXService.Convert(amount, cur, currency, date)
		}
		br, err := cashbucket.Process(tradeFlows, nil, makeDividendSlice(data.CashDividends), s.CashBucketExpiryDays, to, bucketConvertFn)
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

	type symbolFetch struct {
		k           string
		querySymbol string
		prices      []models.PricePoint
		err         error
	}
	fetches := make([]*symbolFetch, 0, len(tradesByKey))
	for k := range tradesByKey {
		querySymbol := k
		if idx := strings.Index(k, "@"); idx != -1 {
			querySymbol = k[:idx]
		}
		if ys, ok := yMap[k]; ok && ys != "" {
			querySymbol = ys
		}
		fetches = append(fetches, &symbolFetch{k: k, querySymbol: querySymbol})
	}

	var wg sync.WaitGroup
	for _, f := range fetches {
		wg.Add(1)
		go func(req *symbolFetch) {
			defer wg.Done()
			req.prices, req.err = s.MarketProvider.GetHistory(req.querySymbol, from, to)
		}(f)
	}
	wg.Wait()

	for _, f := range fetches {
		k := f.k
		trades := tradesByKey[k]
		querySymbol := f.querySymbol

		if f.err != nil {
			slog.Warn("portfolio: historical price fetch failed", "symbol", querySymbol, "err", f.err)
		}
		prices := f.prices
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
					v, err := s.FXService.ConvertSpot(nativeVal, nativeCurrency, currency)
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
					v, err := s.FXService.Convert(nativeVal, nativeCurrency, currency, d)
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
// Concurrent callers with the same (userHash, currency, acctModel, asOf)
// tuple share one underlying computation via singleflight. Results are cloned per
// caller for safety.
func (s *Service) GetCashFlows(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, asOf time.Time) ([]models.CashFlow, error) {
	key := fmt.Sprintf("cf|%s|%s|%s|%s",
		data.UserHash,
		currency,
		string(acctModel),
		asOf.Format("2006-01-02"),
	)
	v, err, _ := s.sfDailyValues.Do(key, func() (interface{}, error) {
		return s.getCashFlowsUncached(data, currency, acctModel, asOf)
	})
	if err != nil {
		return nil, err
	}
	orig := v.([]models.CashFlow)
	out := make([]models.CashFlow, len(orig))
	copy(out, orig)
	return out, nil
}

// getCashFlowsUncached performs the actual cash-flow computation; always invoked
// through GetCashFlows so the singleflight wrapper can dedup concurrent calls.
func (s *Service) getCashFlowsUncached(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, asOf time.Time) ([]models.CashFlow, error) {
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
				amount, err = s.FXService.ConvertSpot(amount, ct.Currency, currency)
			} else {
				amount, err = s.FXService.Convert(amount, ct.Currency, currency, ct.DateTime)
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
			return s.FXService.ConvertSpot(amount, from, currency)
		}
		return s.FXService.Convert(amount, from, currency, date)
	}

	result, err := cashbucket.Process(rawTradeFlows, dividendFlows, makeDividendSlice(data.CashDividends), s.CashBucketExpiryDays, asOf, convertFn)
	if err != nil {
		return nil, fmt.Errorf("cashbucket.Process: %w", err)
	}

	return result.AdjustedCashFlows, nil
}

// GetDailyReturns returns cash-flow-adjusted daily portfolio return series for statistics.
// Cash flows (deposits/withdrawals) are removed from each day's return so that the series
// reflects pure market performance, comparable to a benchmark's price return series.
func (s *Service) GetDailyReturns(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel) ([]float64, []string, []string, error) {
	hist, err := s.GetDailyValues(data, from, to, currency, acctModel)
	if err != nil {
		return nil, nil, nil, err
	}

	cashFlows, err := s.GetCashFlows(data, currency, acctModel, to)
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

// PerPositionDailyValues holds per-symbol daily value slices aligned to the same date grid.
type PerPositionDailyValues struct {
	Dates             []string             // trading dates in [from, to]
	BySymbol          map[string][]float64 // posKey → daily value in display currency (len == len(Dates))
	CashFlowsBySymbol map[string][]float64 // posKey → per-day signed cash impact of trades (buys > 0, sells < 0), display currency
	Totals            []float64            // portfolio daily total (sum across all positions, no pending cash)
}

// GetDailyValuesPerPosition returns per-position daily values in the display currency.
// It mirrors the inner per-symbol loop from getDailyValuesUncached but accumulates into
// a map instead of a single total. Pending cash is excluded — only equity positions appear.
func (s *Service) GetDailyValuesPerPosition(
	data *models.FlexQueryData,
	from, to time.Time,
	currency string,
	acctModel models.AccountingModel,
) (*PerPositionDailyValues, error) {
	if acctModel == models.AccountingModelOriginal {
		currencies := make(map[string]bool)
		for _, t := range data.Trades {
			if !isFXTrade(t) && t.BuySell != "TRANSFER_IN" && t.Currency != "" {
				currencies[t.Currency] = true
			}
		}
		if len(currencies) > 1 {
			return nil, fmt.Errorf("cannot compute per-position values for multi-currency portfolio with 'original' accounting model")
		}
	}

	validDates, err := s.MarketProvider.TradingDates(from, to)
	if err != nil {
		return nil, fmt.Errorf("GetDailyValuesPerPosition: trading dates: %w", err)
	}
	if len(validDates) == 0 {
		return &PerPositionDailyValues{Dates: []string{}, BySymbol: map[string][]float64{}, Totals: []float64{}}, nil
	}

	// Pre-fetch FX histories for historical mode (same logic as getDailyValuesUncached).
	fxData := make(map[string][]models.PricePoint)
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
			pts, err := s.MarketProvider.GetHistory(fxSymbol, from.AddDate(0, 0, -5), to)
			if err != nil {
				slog.Warn("portfolio: FX history prefetch failed (per-position)", "pair", fxSymbol, "err", err)
				continue
			}
			fxData[pairKey] = pts
		}
	}

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

	type symbolFetch struct {
		k           string
		querySymbol string
		prices      []models.PricePoint
		err         error
	}
	fetches := make([]*symbolFetch, 0, len(tradesByKey))
	for k := range tradesByKey {
		qs := k
		if idx := strings.Index(k, "@"); idx != -1 {
			qs = k[:idx]
		}
		if ys, ok := yMap[k]; ok && ys != "" {
			qs = ys
		}
		fetches = append(fetches, &symbolFetch{k: k, querySymbol: qs})
	}

	var wg sync.WaitGroup
	for _, f := range fetches {
		wg.Add(1)
		go func(req *symbolFetch) {
			defer wg.Done()
			req.prices, req.err = s.MarketProvider.GetHistory(req.querySymbol, from, to)
		}(f)
	}
	wg.Wait()

	n := len(validDates)
	dates := make([]string, n)
	for i, d := range validDates {
		dates[i] = d.Format("2006-01-02")
	}
	totals := make([]float64, n)
	bySymbol := make(map[string][]float64, len(fetches))
	cashFlowsBySymbol := make(map[string][]float64, len(fetches))

	fromMidnight := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())

	// fxRateAtDisplay returns the native→display conversion rate on day d, or 1 when native==display.
	fxRateAtDisplay := func(nativeCcy string, d time.Time) float64 {
		if nativeCcy == "" || nativeCcy == currency {
			return 1
		}
		switch acctModel {
		case models.AccountingModelOriginal:
			return 1
		case models.AccountingModelSpot:
			r, err := s.FXService.ConvertSpot(1, nativeCcy, currency)
			if err != nil {
				return 0
			}
			return r
		default:
			pairKey := nativeCcy + currency
			if pts, ok := fxData[pairKey]; ok {
				if rate := fxRateAt(pts, d); rate != 0 {
					return rate
				}
			}
			r, err := s.FXService.Convert(1, nativeCcy, currency, d)
			if err != nil {
				return 0
			}
			return r
		}
	}

	// buildDailyCashflows computes signed per-day cash impact of trades (buys=+cost, sells=-proceeds)
	// in the display currency, aligned to validDates.
	buildDailyCashflows := func(trades []models.Trade, nativeCcy string) []float64 {
		cfs := make([]float64, n)
		if len(trades) == 0 {
			return cfs
		}
		// Index validDates by YYYY-MM-DD.
		dateIdx := make(map[string]int, n)
		for i, d := range validDates {
			dateIdx[d.Format("2006-01-02")] = i
		}
		for _, t := range trades {
			if t.DateTime.Before(fromMidnight) {
				continue
			}
			tradeDay := time.Date(t.DateTime.Year(), t.DateTime.Month(), t.DateTime.Day(), 0, 0, 0, 0, t.DateTime.Location())
			ds := tradeDay.Format("2006-01-02")
			idx, ok := dateIdx[ds]
			if !ok {
				// Non-trading day: attribute to next trading day in range.
				for i, d := range validDates {
					if !d.Before(tradeDay) {
						idx = i
						ok = true
						break
					}
				}
				if !ok {
					continue
				}
			}
			nativeCost := t.Quantity * t.Price
			rate := fxRateAtDisplay(nativeCcy, tradeDay)
			if rate == 0 {
				continue
			}
			cfs[idx] += nativeCost * rate
		}
		return cfs
	}

	for _, f := range fetches {
		k := f.k
		trades := tradesByKey[k]
		prices := f.prices

		if f.err != nil {
			slog.Warn("portfolio: historical price fetch failed (per-position)", "symbol", f.querySymbol, "err", f.err)
		}

		nativeCurrency := ""
		for _, t := range trades {
			if t.Currency != "" {
				nativeCurrency = t.Currency
				break
			}
		}

		qty := 0.0
		ti := 0
		for ti < len(trades) && trades[ti].DateTime.Before(fromMidnight) {
			qty += trades[ti].Quantity
			ti++
		}

		vals := make([]float64, n)
		for i, d := range validDates {
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

			var displayVal float64
			switch acctModel {
			case models.AccountingModelOriginal:
				displayVal = nativeVal
			case models.AccountingModelSpot:
				if nativeCurrency == currency || nativeCurrency == "" {
					displayVal = nativeVal
				} else {
					v, err := s.FXService.ConvertSpot(nativeVal, nativeCurrency, currency)
					if err != nil {
						continue
					}
					displayVal = v
				}
			default: // historical
				if nativeCurrency == currency || nativeCurrency == "" {
					displayVal = nativeVal
				} else {
					pairKey := nativeCurrency + currency
					if pts, ok := fxData[pairKey]; ok {
						if rate := fxRateAt(pts, d); rate != 0 {
							displayVal = nativeVal * rate
						} else {
							v, err := s.FXService.Convert(nativeVal, nativeCurrency, currency, d)
							if err != nil {
								continue
							}
							displayVal = v
						}
					} else {
						v, err := s.FXService.Convert(nativeVal, nativeCurrency, currency, d)
						if err != nil {
							continue
						}
						displayVal = v
					}
				}
			}

			vals[i] = displayVal
			totals[i] += displayVal
		}
		bySymbol[k] = vals
		cashFlowsBySymbol[k] = buildDailyCashflows(trades, nativeCurrency)
	}

	return &PerPositionDailyValues{
		Dates:             dates,
		BySymbol:          bySymbol,
		CashFlowsBySymbol: cashFlowsBySymbol,
		Totals:            totals,
	}, nil
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

	cashFlows, err := s.GetCashFlows(data, currency, acctModel, to)
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

func (s *Service) GetCumulativeMWR(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel) (*models.PortfolioHistoryResponse, error) {
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

	cashFlows, err := s.GetCashFlows(data, currency, acctModel, to)
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

// makeDividendSlice converts FlexQueryData cash dividends to cashbucket.Dividend structs.
func makeDividendSlice(cds []models.CashDividend) []cashbucket.Dividend {
	divs := make([]cashbucket.Dividend, len(cds))
	for i, cd := range cds {
		divs[i] = cashbucket.Dividend{DateTime: cd.DateTime, Amount: cd.Amount, Currency: cd.Currency}
	}
	return divs
}
