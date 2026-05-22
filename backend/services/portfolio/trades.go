package portfolio

import (
	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
	"sort"
	"sync"
)

// GetTradesForSymbol returns the trades for a specific symbol+exchange in a
// frontend-friendly format, with prices converted to displayCurrency.
// When acctModel is AccountingModelOriginal, prices are kept in their native currency
// and displayCurrency is ignored for conversion (but echoed in the response).
func (s *Service) GetTradesForSymbol(data *models.FlexQueryData, symbol, exchange, displayCurrency string, acctModel models.AccountingModel) (*models.TradesResponse, error) {
	var entries []models.TradeEntry
	nativeCurrency := ""
	isOriginal := acctModel == models.AccountingModelOriginal

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
		if !isOriginal && t.Currency != displayCurrency {
			cp, err := s.FXService.Convert(t.Price, t.Currency, displayCurrency, t.DateTime) // trades usually don't need false logic
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

// GetAllEnrichedTrades returns all trades enriched with FIFO realized and unrealized gains.
func (s *Service) GetAllEnrichedTrades(data *models.FlexQueryData, displayCurrency string, acctModel models.AccountingModel) ([]models.EnrichedTrade, error) {
	if data == nil || len(data.Trades) == 0 {
		return []models.EnrichedTrade{}, nil
	}

	yMap := s.getYahooSymbolMap(data)
	isOriginal := acctModel == models.AccountingModelOriginal
	memo := fx.NewMemo(s.FXService)

	// ── Phase 1: Fetch all latest prices in parallel ───────────────────────────
	uniqueKeys := make(map[string]string) // posKey -> querySymbol
	for _, t := range data.Trades {
		if isFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		k := posKey(t.Symbol, t.ListingExchange)
		if _, ok := uniqueKeys[k]; !ok {
			qs := t.Symbol
			if t.YahooSymbol != "" {
				qs = t.YahooSymbol
			} else if ys, ok := yMap[k]; ok && ys != "" {
				qs = ys
			}
			uniqueKeys[k] = qs
		}
	}

	type priceResult struct {
		price float64
		err   error
	}
	priceByKey := make(map[string]priceResult, len(uniqueKeys))
	var priceMu sync.Mutex
	var wg sync.WaitGroup

	for k, sym := range uniqueKeys {
		wg.Add(1)
		go func(k, sym string) {
			defer wg.Done()
			p, err := s.MarketProvider.GetLatestPrice(sym)
			priceMu.Lock()
			priceByKey[k] = priceResult{price: p, err: err}
			priceMu.Unlock()
		}(k, sym)
	}
	wg.Wait()

	// ── Phase 2: Group trades by key for FIFO matching ──────────────────────────
	tradesByKey := make(map[string][]models.Trade)
	tradesIndicesByKey := make(map[string][]int) // tracks original index in data.Trades
	for idx, t := range data.Trades {
		k := posKey(t.Symbol, t.ListingExchange)
		tradesByKey[k] = append(tradesByKey[k], t)
		tradesIndicesByKey[k] = append(tradesIndicesByKey[k], idx)
	}

	// We'll prepare a slice of EnrichedTrade aligned index-by-index with data.Trades
	enrichedList := make([]models.EnrichedTrade, len(data.Trades))

	// Populate basic fields first
	for idx, t := range data.Trades {
		convertedPrice := t.Price
		if !isOriginal && t.Currency != displayCurrency {
			cp, err := memo.Convert(t.Price, t.Currency, displayCurrency, t.DateTime)
			if err == nil {
				convertedPrice = cp
			}
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

		enrichedList[idx] = models.EnrichedTrade{
			ID:              t.PublicID,
			EntryMethod:     t.EntryMethod,
			Date:            t.DateTime.Format("2006-01-02"),
			DateTime:        t.DateTime,
			Side:            side,
			Symbol:          t.Symbol,
			ListingExchange: t.ListingExchange,
			Quantity:        qty,
			Price:           t.Price,
			NativeCurrency:  t.Currency,
			ConvertedPrice:  convertedPrice,
			Commission:      t.Commission,
			Proceeds:        t.Proceeds,
			YahooSymbol:     t.YahooSymbol,
		}
	}

	// ── Phase 3: Run FIFO matching per key ──────────────────────────────────────
	for key, groupTrades := range tradesByKey {
		if len(groupTrades) == 0 {
			continue
		}
		if isFXTrade(groupTrades[0]) {
			continue
		}

		type indexedTrade struct {
			originalIndex int
			trade         models.Trade
		}
		sorted := make([]indexedTrade, len(groupTrades))
		for i, t := range groupTrades {
			sorted[i] = indexedTrade{
				originalIndex: tradesIndicesByKey[key][i],
				trade:         t,
			}
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].trade.DateTime.Before(sorted[j].trade.DateTime)
		})

		type buyLot struct {
			sortedIndex int
			qty         float64
		}
		var openLots []buyLot

		for idx, it := range sorted {
			t := it.trade
			if t.BuySell == "TRANSFER_IN" {
				continue
			}

			if t.BuySell == "STOCK_DIVIDEND" {
				openLots = append(openLots, buyLot{
					sortedIndex: idx,
					qty:         t.Quantity,
				})
				continue
			}

			if t.Quantity > 0 {
				openLots = append(openLots, buyLot{
					sortedIndex: idx,
					qty:         t.Quantity,
				})
			} else if t.Quantity < 0 {
				sellQty := -t.Quantity
				comm := t.Commission
				var realizedGain float64

				for sellQty > 1e-9 && len(openLots) > 0 {
					lot := &openLots[0]
					buyTrade := sorted[lot.sortedIndex].trade

					matchQty := lot.qty
					if matchQty > sellQty {
						matchQty = sellQty
					}

					var profit float64
					if isOriginal {
						profit = matchQty * (t.Price - buyTrade.Price)
					} else if acctModel == models.AccountingModelSpot {
						sp, _ := memo.ConvertSpot(t.Price, t.Currency, displayCurrency)
						cp, _ := memo.ConvertSpot(buyTrade.Price, buyTrade.Currency, displayCurrency)
						profit = matchQty * (sp - cp)
					} else {
						sp, _ := memo.Convert(t.Price, t.Currency, displayCurrency, t.DateTime)
						cp, _ := memo.Convert(buyTrade.Price, buyTrade.Currency, displayCurrency, buyTrade.DateTime)
						profit = matchQty * (sp - cp)
					}
					realizedGain += profit

					lot.qty -= matchQty
					sellQty -= matchQty
					if lot.qty <= 1e-9 {
						openLots = openLots[1:]
					}
				}

				if comm != 0 {
					var convertedComm float64
					if isOriginal {
						convertedComm = comm
					} else if acctModel == models.AccountingModelSpot {
						convertedComm, _ = memo.ConvertSpot(comm, t.Currency, displayCurrency)
					} else {
						convertedComm, _ = memo.Convert(comm, t.Currency, displayCurrency, t.DateTime)
					}
					realizedGain += convertedComm
				}

				enrichedList[it.originalIndex].RealizedGain = realizedGain
			}
		}

		// ── Phase 4: Compute unrealized gain on remaining open lots ──────────────
		latestPrice := priceByKey[key].price
		for _, lot := range openLots {
			if lot.qty <= 1e-9 {
				continue
			}
			it := sorted[lot.sortedIndex]
			buyTrade := it.trade

			var convertedLatestPrice float64
			if isOriginal {
				convertedLatestPrice = latestPrice
			} else {
				convertedLatestPrice, _ = memo.ConvertSpot(latestPrice, buyTrade.Currency, displayCurrency)
			}

			var convertedBuyPrice float64
			if isOriginal {
				convertedBuyPrice = buyTrade.Price
			} else if acctModel == models.AccountingModelSpot {
				convertedBuyPrice, _ = memo.ConvertSpot(buyTrade.Price, buyTrade.Currency, displayCurrency)
			} else {
				convertedBuyPrice, _ = memo.Convert(buyTrade.Price, buyTrade.Currency, displayCurrency, buyTrade.DateTime)
			}

			unrealizedGain := 0.0
			if latestPrice > 0 {
				unrealizedGain = lot.qty * (convertedLatestPrice - convertedBuyPrice)
			}

			enrichedList[it.originalIndex].UnrealizedGain = unrealizedGain
		}
	}

	// Sort final list chronologically, newest first
	sort.Slice(enrichedList, func(i, j int) bool {
		if enrichedList[i].DateTime.Equal(enrichedList[j].DateTime) {
			return enrichedList[i].Symbol < enrichedList[j].Symbol
		}
		return enrichedList[i].DateTime.After(enrichedList[j].DateTime)
	})

	return enrichedList, nil
}
