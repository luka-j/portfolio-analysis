package portfolio

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

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
	FXService            *fx.Service
	CashBucketExpiryDays int

	// sfDailyValues collapses concurrent GetDailyValuesFor calls for the same
	// (userHash, from, to, currency, acctModel, cachedOnly) tuple. This matters
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
type fxMemo struct {
	fx         *fx.Service
	cachedOnly bool

	mu       sync.RWMutex
	spot     map[string]float64            // key: "from|to"
	hist     map[string]map[string]float64 // outer: "from|to", inner: "YYYY-MM-DD"
}

func newFXMemo(svc *fx.Service, cachedOnly bool) *fxMemo {
	return &fxMemo{
		fx:         svc,
		cachedOnly: cachedOnly,
		spot:       make(map[string]float64),
		hist:       make(map[string]map[string]float64),
	}
}

// SpotRate returns a cached spot rate, fetching once per (from, to) pair.
func (m *fxMemo) SpotRate(from, to string) (float64, error) {
	if from == to || from == "" || to == "" {
		return 1.0, nil
	}
	key := from + "|" + to
	m.mu.RLock()
	if r, ok := m.spot[key]; ok {
		m.mu.RUnlock()
		return r, nil
	}
	m.mu.RUnlock()
	r, err := m.fx.GetSpotRate(from, to, m.cachedOnly)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.spot[key] = r
	m.mu.Unlock()
	return r, nil
}

// HistoricalRate returns a cached historical rate, fetching once per (from, to, date).
func (m *fxMemo) HistoricalRate(from, to string, date time.Time) (float64, error) {
	if from == to || from == "" || to == "" {
		return 1.0, nil
	}
	key := from + "|" + to
	ds := date.Format("2006-01-02")
	m.mu.RLock()
	if inner, ok := m.hist[key]; ok {
		if r, ok2 := inner[ds]; ok2 {
			m.mu.RUnlock()
			return r, nil
		}
	}
	m.mu.RUnlock()
	r, err := m.fx.GetRate(from, to, date, m.cachedOnly)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	inner := m.hist[key]
	if inner == nil {
		inner = make(map[string]float64)
		m.hist[key] = inner
	}
	inner[ds] = r
	m.mu.Unlock()
	return r, nil
}

// ConvertSpot converts an amount at the memoized spot rate.
func (m *fxMemo) ConvertSpot(amount float64, from, to string) (float64, error) {
	if amount == 0 || from == to {
		return amount, nil
	}
	r, err := m.SpotRate(from, to)
	if err != nil {
		return 0, err
	}
	return amount * r, nil
}

// Convert converts an amount at the memoized historical rate.
func (m *fxMemo) Convert(amount float64, from, to string, date time.Time) (float64, error) {
	if amount == 0 || from == to {
		return amount, nil
	}
	r, err := m.HistoricalRate(from, to, date)
	if err != nil {
		return 0, err
	}
	return amount * r, nil
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
// Equivalent to GetCurrentValueMulti with a single-currency slice; retained for
// callers that only need one currency projection.
func (s *Service) GetCurrentValue(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool) (*models.PortfolioValueResponse, error) {
	results, err := s.GetCurrentValueMulti(data, []string{currency}, acctModel, cachedOnly)
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
func (s *Service) GetCurrentValueMulti(data *models.FlexQueryData, currencies []string, acctModel models.AccountingModel, cachedOnly bool) (map[string]*models.PortfolioValueResponse, error) {
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
	for _, h := range holdings {
		k := posKey(h.Symbol, h.ListingExchange)
		querySymbol := h.Symbol
		if ys, ok := yMap[k]; ok && ys != "" {
			querySymbol = ys
		}
		wg.Add(1)
		go func(k, sym string) {
			defer wg.Done()
			p, err := s.MarketProvider.GetLatestPrice(sym, cachedOnly)
			priceMu.Lock()
			priceByKey[k] = priceResult{price: p, err: err}
			priceMu.Unlock()
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
	ccyMaps := make(map[string]perCurrencyMaps, len(currencies))
	for _, cur := range currencies {
		memo := newFXMemo(s.FXService, cachedOnly)
		cb, gl, comm := s.computeCurrentValueMapsMemo(data, cur, acctModel, memo)
		ccyMaps[cur] = perCurrencyMaps{costBasis: cb, realizedGL: gl, commission: comm}
	}

	// Dedicated memo for the price→value conversions below, shared across currencies
	// via a map so that each currency reuses its own memoized spot rates.
	valueMemo := make(map[string]*fxMemo, len(currencies))
	for _, cur := range currencies {
		valueMemo[cur] = newFXMemo(s.FXService, cachedOnly)
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
			log.Printf("Warning: fetching latest price for %s (mapped to %s): %v", h.Symbol, querySymbol, fetchErr)
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
				convertedPrice, fxErr = valueMemo[cur].ConvertSpot(latestPrice, h.Currency, cur)
				if fxErr != nil {
					return nil, fmt.Errorf("converting price %s to %s: %w", h.Currency, cur, fxErr)
				}
				convertedValue, fxErr = valueMemo[cur].ConvertSpot(nativeValue, h.Currency, cur)
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
		memo := newFXMemo(s.FXService, cachedOnly)
		pendingCash, err := s.computePendingCashMemo(data, cur, acctModel, memo, today)
		if err != nil {
			log.Printf("Warning: computing pending cash for %s: %v", cur, err)
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
func (s *Service) computeCurrentValueMaps(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool) (costBasisMap, realizedGLMap, commissionsMap map[string]float64) {
	return s.computeCurrentValueMapsMemo(data, currency, acctModel, newFXMemo(s.FXService, cachedOnly))
}

// computeCurrentValueMapsMemo returns cost-basis, realized GL, and commissions maps in
// a single pass over trades, calling fifo.Match once per position instead of twice.
// The FX memo collapses repeated currency lookups across all trades in the request.
func (s *Service) computeCurrentValueMapsMemo(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, memo *fxMemo) (costBasisMap, realizedGLMap, commissionsMap map[string]float64) {
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
// Concurrent callers with the same (userHash, from, to, currency, acctModel, cachedOnly)
// tuple share one underlying computation via singleflight — this is the dominant
// wall-clock win when the landing page fires history + stats + returns in parallel
// for the same range. Results are cloned so downstream mutation is safe.
func (s *Service) GetDailyValues(
	data *models.FlexQueryData,
	from, to time.Time,
	currency string,
	acctModel models.AccountingModel,
	cachedOnly bool,
) (*models.PortfolioHistoryResponse, error) {
	key := fmt.Sprintf("dv|%s|%s|%s|%s|%s|%v",
		data.UserHash,
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
		currency,
		string(acctModel),
		cachedOnly,
	)
	v, err, _ := s.sfDailyValues.Do(key, func() (interface{}, error) {
		return s.getDailyValuesUncached(data, from, to, currency, acctModel, cachedOnly)
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
		br, err := cashbucket.Process(tradeFlows, nil, nil, s.CashBucketExpiryDays, to, bucketConvertFn)
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
			req.prices, req.err = s.MarketProvider.GetHistory(req.querySymbol, from, to, cachedOnly)
		}(f)
	}
	wg.Wait()

	for _, f := range fetches {
		k := f.k
		trades := tradesByKey[k]
		querySymbol := f.querySymbol

		if f.err != nil {
			log.Printf("Warning: fetching %s historical data: %v\n", querySymbol, f.err)
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
// Concurrent callers with the same (userHash, currency, acctModel, cachedOnly, asOf)
// tuple share one underlying computation via singleflight. Results are cloned per
// caller for safety.
func (s *Service) GetCashFlows(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool, asOf time.Time) ([]models.CashFlow, error) {
	key := fmt.Sprintf("cf|%s|%s|%s|%v|%s",
		data.UserHash,
		currency,
		string(acctModel),
		cachedOnly,
		asOf.Format("2006-01-02"),
	)
	v, err, _ := s.sfDailyValues.Do(key, func() (interface{}, error) {
		return s.getCashFlowsUncached(data, currency, acctModel, cachedOnly, asOf)
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
func (s *Service) getCashFlowsUncached(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, cachedOnly bool, asOf time.Time) ([]models.CashFlow, error) {
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

	result, err := cashbucket.Process(rawTradeFlows, dividendFlows, nil, s.CashBucketExpiryDays, asOf, convertFn)
	if err != nil {
		return nil, fmt.Errorf("cashbucket.Process: %w", err)
	}

	return result.AdjustedCashFlows, nil
}

// GetDailyReturns returns cash-flow-adjusted daily portfolio return series for statistics.
// Cash flows (deposits/withdrawals) are removed from each day's return so that the series
// reflects pure market performance, comparable to a benchmark's price return series.
func (s *Service) GetDailyReturns(data *models.FlexQueryData, from, to time.Time, currency string, acctModel models.AccountingModel, cachedOnly bool) ([]float64, []string, []string, error) {
	hist, err := s.GetDailyValues(data, from, to, currency, acctModel, cachedOnly) 
	if err != nil {
		return nil, nil, nil, err
	}

	cashFlows, err := s.GetCashFlows(data, currency, acctModel, cachedOnly, to)
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
	return s.computePendingCashMemo(data, currency, acctModel, newFXMemo(s.FXService, cachedOnly), asOf)
}

// computePendingCashMemo is the memo-aware variant so that callers computing
// multi-currency values can share one FX memo across phases.
func (s *Service) computePendingCashMemo(data *models.FlexQueryData, currency string, acctModel models.AccountingModel, memo *fxMemo, asOf time.Time) (float64, error) {
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

	divs := make([]cashbucket.Dividend, len(data.CashDividends))
	for i, cd := range data.CashDividends {
		divs[i] = cashbucket.Dividend{DateTime: cd.DateTime, Amount: cd.Amount, Currency: cd.Currency}
	}

	result, err := cashbucket.Process(trades, nil, divs, s.CashBucketExpiryDays, asOf, convertFn)
	if err != nil {
		return 0, fmt.Errorf("computePendingCash: %w", err)
	}
	return result.PendingCash, nil
}
