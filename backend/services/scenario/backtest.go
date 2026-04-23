package scenario

import (
	"fmt"
	"sort"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
)

// backtestSymbol holds pre-fetched historical price data for one symbol.
type backtestSymbol struct {
	symbol   string
	exchange string
	currency string
	prices   []models.PricePoint // sorted ascending by date
	priceMap map[string]float64  // date YYYY-MM-DD → adjusted close
}

// buildBacktest generates a complete synthetic trade stream for a historical backtest.
// It produces:
//  1. Initial buy trades at StartDate proportional to basket weights.
//  2. Periodic contribution buy trades according to ContributionCadence.
//  3. Rebalancing sell/buy trades at the configured schedule.
//
// All prices are sourced from historical market data (GetHistory), not live prices.
func buildBacktest(spec ScenarioSpec, mp market.Provider, fxSvc *fx.Service) (*models.FlexQueryData, error) {
	cfg := spec.Backtest
	basket := spec.Basket
	now := time.Now().UTC()
	endDate := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, time.UTC)

	if cfg.StartDate.Time().After(endDate) {
		return nil, fmt.Errorf("backtest start_date is in the future")
	}

	targetWeights, err := resolveTargetWeights(basket)
	if err != nil {
		return nil, err
	}

	// Pre-fetch historical prices for each symbol.
	syms := make([]backtestSymbol, 0, len(basket.Items))
	for _, item := range basket.Items {
		// Basket items may be entered as IBKR tickers (e.g. "4GLD"); the market provider
		// expects Yahoo symbols (e.g. "4GLD.DE"). For basket items we only have item.Symbol
		// as input, so we try the symbol as-is first. Users must enter Yahoo-format symbols
		// for backtest to work — this is documented in the scenario editor UI.
		pts, err := mp.GetHistory(item.Symbol, cfg.StartDate.Time().AddDate(0, 0, -5), endDate, false)
		if err != nil || len(pts) == 0 {
			return nil, fmt.Errorf("no historical data for backtest symbol %s: %w", item.Symbol, err)
		}
		pm := make(map[string]float64, len(pts))
		for _, p := range pts {
			adj := p.AdjClose
			if adj == 0 {
				adj = p.Close
			}
			pm[p.Date.Format("2006-01-02")] = adj
		}
		cur := item.Currency
		if cur == "" {
			cur = cfg.Currency
		}
		syms = append(syms, backtestSymbol{
			symbol:   item.Symbol,
			exchange: item.Exchange,
			currency: cur,
			prices:   pts,
			priceMap: pm,
		})
	}

	// Build a union of all trading dates across all symbols so the backtest calendar
	// isn't dictated by the first symbol alone (which could have gaps others don't).
	unionDates := unionTradingDates(syms)
	actualStart := firstTradingDayOnOrAfterDates(cfg.StartDate.Time(), unionDates)
	if actualStart.IsZero() {
		return nil, fmt.Errorf("no trading data found on or after backtest start date %s", cfg.StartDate.Time().Format("2006-01-02"))
	}

	data := &models.FlexQueryData{}

	// Initial allocation.
	if err := addAllocationTrades(data, syms, targetWeights, cfg.InitialAmount, cfg.Currency, actualStart, fxSvc); err != nil {
		return nil, fmt.Errorf("initial allocation: %w", err)
	}

	// Running quantity tracker, updated as trades are appended.
	holdingQty := make(map[string]float64, len(syms))
	for _, t := range data.Trades {
		holdingQty[symbolKey(t.Symbol, t.ListingExchange)] += t.Quantity
	}

	// Build the timeline of events.
	type eventKind int
	const (
		evContribution eventKind = iota
		evRebalance
	)
	type event struct {
		date time.Time
		kind eventKind
	}
	var events []event

	if cfg.Contribution != ContributionNone && cfg.ContributionAmount > 0 {
		for d := nextPeriodStart(actualStart, cfg.Contribution); !d.After(endDate); d = nextPeriodStart(d, cfg.Contribution) {
			events = append(events, event{date: d, kind: evContribution})
		}
	}

	if cfg.Rebalance == RebalanceModeMonthly || cfg.Rebalance == RebalanceModeQuarterly || cfg.Rebalance == RebalanceModeAnnually {
		cad := rebalanceToCadence(cfg.Rebalance)
		for d := nextPeriodStart(actualStart, cad); !d.After(endDate); d = nextPeriodStart(d, cad) {
			events = append(events, event{date: d, kind: evRebalance})
		}
	}

	// Threshold mode: check on each monthly tick.
	if cfg.Rebalance == RebalanceModeThreshold && cfg.RebalanceThreshold > 0 {
		for d := nextPeriodStart(actualStart, ContributionMonthly); !d.After(endDate); d = nextPeriodStart(d, ContributionMonthly) {
			events = append(events, event{date: d, kind: evRebalance})
		}
	}

	sort.Slice(events, func(i, j int) bool { return events[i].date.Before(events[j].date) })

	for _, ev := range events {
		tradeDate := nearestTradingDayDates(ev.date, unionDates)
		if tradeDate.IsZero() {
			continue
		}

		switch ev.kind {
		case evContribution:
			prevLen := len(data.Trades)
			if err := addAllocationTrades(data, syms, targetWeights, cfg.ContributionAmount, cfg.Currency, tradeDate, fxSvc); err != nil {
				return nil, fmt.Errorf("contribution on %s: %w", tradeDate.Format("2006-01-02"), err)
			}
			for _, t := range data.Trades[prevLen:] {
				holdingQty[symbolKey(t.Symbol, t.ListingExchange)] += t.Quantity
			}

		case evRebalance:
			// For threshold mode, skip if drift is below the threshold.
			if cfg.Rebalance == RebalanceModeThreshold {
				if !drifted(syms, holdingQty, targetWeights, tradeDate, cfg.RebalanceThreshold) {
					continue
				}
			}
			trades, err := buildRebalanceTrades(syms, holdingQty, targetWeights, tradeDate)
			if err != nil {
				return nil, fmt.Errorf("rebalance on %s: %w", tradeDate.Format("2006-01-02"), err)
			}
			data.Trades = append(data.Trades, trades...)
			for _, t := range trades {
				holdingQty[symbolKey(t.Symbol, t.ListingExchange)] += t.Quantity
			}
		}
	}

	return data, nil
}

// resolveTargetWeights extracts a symbol → weight map from the basket.
// For quantity-mode baskets, items are equally weighted (backtest should use weight mode).
func resolveTargetWeights(basket *Basket) (map[string]float64, error) {
	weights := make(map[string]float64, len(basket.Items))
	if basket.Mode == BasketModeWeight {
		for _, item := range basket.Items {
			weights[item.Symbol] = item.Weight
		}
		return weights, nil
	}
	n := float64(len(basket.Items))
	for _, item := range basket.Items {
		weights[item.Symbol] = 1.0 / n
	}
	return weights, nil
}

// addAllocationTrades appends buy trades for each symbol proportional to targetWeights,
// spending totalAmount units of currency.
func addAllocationTrades(
	data *models.FlexQueryData,
	syms []backtestSymbol,
	targetWeights map[string]float64,
	totalAmount float64,
	currency string,
	tradeDate time.Time,
	fxSvc *fx.Service,
) error {
	ds := tradeDate.Format("2006-01-02")
	for _, sym := range syms {
		weight := targetWeights[sym.symbol]
		if weight <= 0 {
			continue
		}
		allocationCurrency := totalAmount * weight
		allocationNative := allocationCurrency
		if sym.currency != currency && fxSvc != nil {
			rate, err := fxSvc.GetRate(currency, sym.currency, tradeDate, false)
			if err != nil || rate == 0 {
				return fmt.Errorf("FX %s→%s on %s for %s: %w", currency, sym.currency, ds, sym.symbol, err)
			}
			allocationNative = allocationCurrency * rate
		}
		price, ok := sym.priceMap[ds]
		if !ok || price == 0 {
			return fmt.Errorf("no price for %s on %s", sym.symbol, ds)
		}
		qty := allocationNative / price
		if qty <= 0 {
			continue
		}
		data.Trades = append(data.Trades, syntheticBuyTrade(sym.symbol, sym.exchange, sym.currency, qty, price, tradeDate))
	}
	return nil
}

// buildRebalanceTrades generates sell/buy trades to restore target weights as of tradeDate.
func buildRebalanceTrades(
	syms []backtestSymbol,
	holdingQty map[string]float64,
	targetWeights map[string]float64,
	tradeDate time.Time,
) ([]models.Trade, error) {
	ds := tradeDate.Format("2006-01-02")
	totalValue := 0.0
	for _, sym := range syms {
		price := sym.priceMap[ds]
		totalValue += holdingQty[symbolKey(sym.symbol, sym.exchange)] * price
	}
	if totalValue <= 0 {
		return nil, nil
	}
	var trades []models.Trade
	for _, sym := range syms {
		price, ok := sym.priceMap[ds]
		if !ok || price == 0 {
			continue
		}
		key := symbolKey(sym.symbol, sym.exchange)
		currentQty := holdingQty[key]
		targetQty := (totalValue * targetWeights[sym.symbol]) / price
		delta := targetQty - currentQty
		if delta > 1e-8 {
			trades = append(trades, syntheticBuyTrade(sym.symbol, sym.exchange, sym.currency, delta, price, tradeDate))
		} else if delta < -1e-8 {
			trades = append(trades, syntheticSellTrade(sym.symbol, sym.exchange, sym.currency, -delta, price, tradeDate))
		}
	}
	return trades, nil
}

// drifted returns true if any symbol has drifted from its target by more than thresholdPct.
func drifted(
	syms []backtestSymbol,
	holdingQty map[string]float64,
	targetWeights map[string]float64,
	tradeDate time.Time,
	thresholdPct float64,
) bool {
	ds := tradeDate.Format("2006-01-02")
	totalValue := 0.0
	for _, sym := range syms {
		totalValue += holdingQty[symbolKey(sym.symbol, sym.exchange)] * sym.priceMap[ds]
	}
	if totalValue <= 0 {
		return false
	}
	for _, sym := range syms {
		p := sym.priceMap[ds]
		actual := (holdingQty[symbolKey(sym.symbol, sym.exchange)] * p) / totalValue
		target := targetWeights[sym.symbol]
		drift := actual - target
		if drift < 0 {
			drift = -drift
		}
		if drift*100 > thresholdPct {
			return true
		}
	}
	return false
}

// nextPeriodStart returns the first day of the next period after t.
func nextPeriodStart(t time.Time, cadence ContributionCadence) time.Time {
	switch cadence {
	case ContributionMonthly:
		return time.Date(t.Year(), t.Month()+1, 1, 12, 0, 0, 0, time.UTC)
	case ContributionQuarterly:
		nextQStart := ((int(t.Month())-1)/3+1)*3 + 1
		if nextQStart > 12 {
			return time.Date(t.Year()+1, 1, 1, 12, 0, 0, 0, time.UTC)
		}
		return time.Date(t.Year(), time.Month(nextQStart), 1, 12, 0, 0, 0, time.UTC)
	case ContributionAnnually:
		return time.Date(t.Year()+1, t.Month(), 1, 12, 0, 0, 0, time.UTC)
	default:
		return t.AddDate(0, 1, 0)
	}
}

// rebalanceToCadence maps a RebalanceMode to a ContributionCadence for period generation.
func rebalanceToCadence(r RebalanceMode) ContributionCadence {
	switch r {
	case RebalanceModeMonthly:
		return ContributionMonthly
	case RebalanceModeQuarterly:
		return ContributionQuarterly
	case RebalanceModeAnnually:
		return ContributionAnnually
	default:
		return ContributionMonthly
	}
}

// firstTradingDayOnOrAfter returns the first price point date on or after t.
func firstTradingDayOnOrAfter(t time.Time, pts []models.PricePoint) time.Time {
	for _, p := range pts {
		if !p.Date.Before(t) {
			return p.Date
		}
	}
	return time.Time{}
}

// unionTradingDates returns a sorted, deduped slice of all trading dates across all symbols.
// Using the union rather than a single symbol's calendar ensures that a backtest event
// scheduled for e.g. a US market holiday that is an open day elsewhere still triggers.
func unionTradingDates(syms []backtestSymbol) []time.Time {
	seen := make(map[string]time.Time)
	for _, s := range syms {
		for _, p := range s.prices {
			k := p.Date.Format("2006-01-02")
			if _, ok := seen[k]; !ok {
				seen[k] = p.Date
			}
		}
	}
	out := make([]time.Time, 0, len(seen))
	for _, d := range seen {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// firstTradingDayOnOrAfterDates is the union-calendar analogue of firstTradingDayOnOrAfter.
func firstTradingDayOnOrAfterDates(t time.Time, dates []time.Time) time.Time {
	for _, d := range dates {
		if !d.Before(t) {
			return d
		}
	}
	return time.Time{}
}

// nearestTradingDayDates returns the date in `dates` nearest to t (on or after),
// falling back to the last available date before t.
func nearestTradingDayDates(t time.Time, dates []time.Time) time.Time {
	for _, d := range dates {
		if !d.Before(t) {
			return d
		}
	}
	for i := len(dates) - 1; i >= 0; i-- {
		if !dates[i].After(t) {
			return dates[i]
		}
	}
	return time.Time{}
}
