package portfolio

import (
	"fmt"
	"log/slog"
	"portfolio-analysis/models"
	"portfolio-analysis/services/cashbucket"
	"portfolio-analysis/services/fifo"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
	"sort"
	"sync"
	"time"
)

// GetCurrentValue returns the portfolio value in the requested display currency.
// Equivalent to GetCurrentValueMulti with a single-currency slice; retained for
// callers that only need one currency projection.
func (s *Service) GetCurrentValue(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) (*models.PortfolioValueResponse, error) {
	results, err := s.GetCurrentValueMulti(data, []string{currency}, acctModel)
	if err != nil {
		return nil, err
	}
	return results[currency], nil
}

// GetCurrentValueMulti computes the portfolio value in every requested display currency
// in a single pass. Price data is fetched once per symbol (in parallel, deduplicated via
// the market provider's singleflight) and projected to all target currencies locally,
// so wall-clock cost is O(unique_symbols / limiter_rate) rather than O(currencies × symbols).
// The first currency in the slice is the "primary" one, used for scalar Price/Value fields.
func (s *Service) GetCurrentValueMulti(data *models.FlexQueryData, currencies []string, acctModel models.AccountingModel) (map[string]*models.PortfolioValueResponse, error) {
	if len(currencies) == 0 {
		return nil, fmt.Errorf("GetCurrentValueMulti: at least one currency required")
	}
	holdings := s.GetCurrentHoldings(data)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yMap := s.getYahooSymbolMap(data)

	isOriginalMode := acctModel == models.AccountingModelOriginal

	// ── Phase 1: Fetch all latest prices in parallel ───────────────────────────
	// Sequential GetLatestPrice calls at a 5 req/s limiter floor scale N/5 seconds
	// for N symbols; parallelising lets the limiter run at full capacity while also
	// interleaving HTTP + DB I/O. Singleflight in the Yahoo provider dedupes any
	// repeated symbol fetches issued concurrently by other handlers.
	type priceResult struct {
		price float64
		err   error
	}
	priceByKey := make(map[string]priceResult, len(holdings))
	var priceMu sync.Mutex
	var wg sync.WaitGroup

	var inception time.Time
	for _, t := range data.Trades {
		if inception.IsZero() || t.DateTime.Before(inception) {
			inception = t.DateTime
		}
	}
	if inception.IsZero() {
		inception = today.AddDate(-5, 0, 0)
	} else {
		inception = time.Date(inception.Year(), inception.Month(), inception.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -5)
	}

	for _, h := range holdings {
		k := posKey(h.Symbol, h.ListingExchange)
		querySymbol := h.Symbol
		if ys, ok := yMap[k]; ok && ys != "" {
			querySymbol = ys
		}
		wg.Add(1)
		go func(k, sym string) {
			defer wg.Done()
			p, err := s.MarketProvider.GetLatestPrice(sym)
			priceMu.Lock()
			priceByKey[k] = priceResult{price: p, err: err}
			priceMu.Unlock()

			// Pre-warm caches in the background.
			// If it's a false request, we pre-warm both the latest price and history.
			// If it's a fresh request, the latest price was already fetched, but we still pre-warm history
			// in the background so that subsequent timeline queries (history, returns, stats) are warmed.
			go func() {
				if false {
					_, _ = s.MarketProvider.GetLatestPrice(sym)
				}
				_, _ = s.MarketProvider.GetHistory(sym, inception, today)
			}()
		}(k, querySymbol)
	}
	wg.Wait()

	// ── Phase 2: Compute cost-basis/realized-GL/commissions per currency ───────
	// These also go through the FX service; with the memo they collapse to one
	// lookup per unique (from, to[, date]) tuple for the whole request.
	type perCurrencyMaps struct {
		costBasis  map[string]float64
		realizedGL map[string]float64
		commission map[string]float64
	}
	sharedMemo := fx.NewMemo(s.FXService)

	ccyMaps := make(map[string]perCurrencyMaps, len(currencies))
	for _, cur := range currencies {
		cb, gl, comm := s.computeCurrentValueMapsMemo(data, cur, acctModel, sharedMemo)
		ccyMaps[cur] = perCurrencyMaps{costBasis: cb, realizedGL: gl, commission: comm}
	}

	// ── Phase 3: Build per-currency PositionValue slices ──────────────────────
	results := make(map[string]*models.PortfolioValueResponse, len(currencies))
	totalByCcy := make(map[string]float64, len(currencies))
	positionsByCcy := make(map[string][]models.PositionValue, len(currencies))
	for _, cur := range currencies {
		positionsByCcy[cur] = make([]models.PositionValue, 0, len(holdings))
	}

	for _, h := range holdings {
		k := posKey(h.Symbol, h.ListingExchange)
		querySymbol := h.Symbol
		if ys, ok := yMap[k]; ok && ys != "" {
			querySymbol = ys
		}
		pr := priceByKey[k]
		latestPrice := pr.price
		fetchErr := pr.err
		if fetchErr != nil {
			slog.Warn("portfolio: latest price fetch failed", "symbol", h.Symbol, "query_symbol", querySymbol, "err", fetchErr)
		}

		var priceStatus string
		if latestPrice == 0 {
			if fetchErr != nil {
				priceStatus = "fetch_failed"
			} else if checker, ok := s.MarketProvider.(market.PriceStatusChecker); ok && checker.HasCachedData(querySymbol) {
				priceStatus = "stale"
			} else {
				priceStatus = "no_data"
			}
		}
		nativeValue := h.Quantity * latestPrice

		posPrices := make(map[string]float64, len(currencies))
		posCostBases := make(map[string]float64, len(currencies))
		posValues := make(map[string]float64, len(currencies))

		for _, cur := range currencies {
			var convertedPrice, convertedValue float64
			if isOriginalMode || acctModel == models.AccountingModelOriginal {
				convertedPrice = latestPrice
				convertedValue = nativeValue
			} else {
				var fxErr error
				convertedPrice, fxErr = sharedMemo.ConvertSpot(latestPrice, h.Currency, cur)
				if fxErr != nil {
					return nil, fmt.Errorf("converting price %s to %s: %w", h.Currency, cur, fxErr)
				}
				convertedValue, fxErr = sharedMemo.ConvertSpot(nativeValue, h.Currency, cur)
				if fxErr != nil {
					return nil, fmt.Errorf("converting value %s to %s: %w", h.Currency, cur, fxErr)
				}
			}
			totalByCcy[cur] += convertedValue
			posPrices[cur] = convertedPrice
			posValues[cur] = convertedValue
			posCostBases[cur] = ccyMaps[cur].costBasis[k]
		}

		for _, cur := range currencies {
			maps := ccyMaps[cur]
			positionsByCcy[cur] = append(positionsByCcy[cur], models.PositionValue{
				Symbol:          h.Symbol,
				ListingExchange: h.ListingExchange,
				YahooSymbol:     yMap[k],
				Quantity:        h.Quantity,
				NativeCurrency:  h.Currency,
				Prices:          posPrices,
				CostBases:       posCostBases,
				Values:          posValues,
				Price:           posPrices[cur],
				CostBasis:       maps.costBasis[k],
				RealizedGL:      maps.realizedGL[k],
				Value:           posValues[cur],
				Commission:      maps.commission[k],
				PriceStatus:     priceStatus,
			})
		}
	}

	// ── Phase 4: Pending cash per currency ─────────────────────────────────────
	pendingCashByCcy := make(map[string]float64, len(currencies))
	hasPendingCash := false
	for _, cur := range currencies {
		pendingCash, err := s.computePendingCashMemo(data, cur, acctModel, sharedMemo, today)
		if err != nil {
			slog.Warn("portfolio: pending cash computation failed", "currency", cur, "err", err)
			pendingCash = 0
		}
		pendingCashByCcy[cur] = pendingCash
		if pendingCash > 0 {
			hasPendingCash = true
		}
	}

	if hasPendingCash {
		posPrices := make(map[string]float64, len(currencies))
		posCostBases := make(map[string]float64, len(currencies))
		posValues := make(map[string]float64, len(currencies))
		for _, cur := range currencies {
			posPrices[cur] = 1
			posCostBases[cur] = 0
			posValues[cur] = pendingCashByCcy[cur]
			totalByCcy[cur] += pendingCashByCcy[cur]
		}
		for _, cur := range currencies {
			if pendingCashByCcy[cur] > 0 {
				positionsByCcy[cur] = append(positionsByCcy[cur], models.PositionValue{
					Symbol:         "PENDING_CASH",
					NativeCurrency: cur,
					Prices:         posPrices,
					CostBases:      posCostBases,
					Values:         posValues,
					Price:          1,
					Value:          pendingCashByCcy[cur],
					Quantity:       pendingCashByCcy[cur],
				})
			}
		}
	}

	for _, cur := range currencies {
		results[cur] = &models.PortfolioValueResponse{
			Value:       totalByCcy[cur],
			Currency:    cur,
			Positions:   positionsByCcy[cur],
			PendingCash: pendingCashByCcy[cur],
		}
	}
	return results, nil
}

// computeCurrentValueMaps is a backwards-compatible wrapper around
// computeCurrentValueMapsMemo that allocates a fresh FX memo for the call.
func (s *Service) computeCurrentValueMaps(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) (costBasisMap, realizedGLMap, commissionsMap map[string]float64) {
	return s.computeCurrentValueMapsMemo(data, currency, acctModel, fx.NewMemo(s.FXService))
}

// computeCurrentValueMapsMemo returns cost-basis, realized GL, and commissions maps in
// a single pass over trades, calling fifo.Match once per position instead of twice.
// The FX memo collapses repeated currency lookups across all trades in the request.
func (s *Service) computeCurrentValueMapsMemo(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, memo *fx.Memo) (costBasisMap, realizedGLMap, commissionsMap map[string]float64) {
	costBasisMap = make(map[string]float64)
	realizedGLMap = make(map[string]float64)
	commissionsMap = make(map[string]float64)

	isOriginal := acctModel == models.AccountingModelOriginal

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
					p, _ = memo.ConvertSpot(l.Price, l.Curr, currency)
				} else {
					p, _ = memo.Convert(l.Price, l.Curr, currency, l.Date)
				}
				totalCost += l.Qty * p
				totalQty += l.Qty
			}
			if totalQty > 0 {
				costBasisMap[key] = totalCost / totalQty
			} else {
				costBasisMap[key], _ = memo.ConvertSpot(0, nativeCurrency, currency)
			}
		}

		// ── Realized GL ─────────────────────────────────────────────────────
		var gl float64
		for _, m := range matchedSells {
			var profit float64
			if isOriginal {
				profit = m.Qty * (m.SellPrice - m.CostPrice)
			} else if acctModel == models.AccountingModelSpot {
				sp, _ := memo.ConvertSpot(m.SellPrice, m.Curr, currency)
				cp, _ := memo.ConvertSpot(m.CostPrice, m.Curr, currency)
				profit = m.Qty * (sp - cp)
			} else {
				sp, _ := memo.Convert(m.SellPrice, m.Curr, currency, m.SellDate)
				cp, _ := memo.Convert(m.CostPrice, m.Curr, currency, m.CostDate)
				profit = m.Qty * (sp - cp)
			}
			gl += profit
			if m.Comm != 0 {
				if isOriginal {
					gl += m.Comm
				} else if acctModel == models.AccountingModelSpot {
					c, _ := memo.ConvertSpot(m.Comm, m.Curr, currency)
					gl += c
				} else {
					c, _ := memo.Convert(m.Comm, m.Curr, currency, m.SellDate)
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
				comm, _ = memo.ConvertSpot(t.Commission, t.Currency, currency)
			} else {
				comm, _ = memo.Convert(t.Commission, t.Currency, currency, t.DateTime)
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
			costBasisMap[op.Symbol], _ = memo.ConvertSpot(op.CostBasisPerShare, op.Currency, currency)
		}
	}

	return
}

// computePendingCash returns the current aggregate value of all active (non-expired) cash buckets.
func (s *Service) computePendingCash(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, asOf time.Time) (float64, error) {
	return s.computePendingCashMemo(data, currency, acctModel, fx.NewMemo(s.FXService), asOf)
}

// computePendingCashMemo is the memo-aware variant so that callers computing
// multi-currency values can share one FX memo across phases.
func (s *Service) computePendingCashMemo(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, memo *fx.Memo, asOf time.Time) (float64, error) {
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
			return memo.ConvertSpot(amount, from, currency)
		}
		return memo.Convert(amount, from, currency, date)
	}

	result, err := cashbucket.Process(trades, nil, makeDividendSlice(data.CashDividends), s.CashBucketExpiryDays, asOf, convertFn)
	if err != nil {
		return 0, fmt.Errorf("computePendingCash: %w", err)
	}
	return result.PendingCash, nil
}
