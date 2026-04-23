package scenario

import (
	"fmt"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/market"
)

// priceAt returns the price of querySymbol on dt (using historical close),
// falling back to the latest known price when dt is zero or represents today
// (i.e. no historical adjustment needed). A non-nil dt indicates a historical
// lookup; callers pass the adjustment's effective date.
func priceAt(mp market.Provider, querySymbol string, dt time.Time) (float64, error) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if dt.IsZero() || !dt.Before(today) {
		return mp.GetLatestPrice(querySymbol, false)
	}
	// Fetch a small window ending at dt to tolerate weekends/holidays.
	start := dt.AddDate(0, 0, -10)
	end := time.Date(dt.Year(), dt.Month(), dt.Day(), 23, 59, 59, 0, time.UTC)
	pts, err := mp.GetHistory(querySymbol, start, end, false)
	if err != nil {
		return 0, fmt.Errorf("fetching history for %s at %s: %w", querySymbol, dt.Format("2006-01-02"), err)
	}
	// Take the latest point on or before dt.
	var best float64
	for _, p := range pts {
		if !p.Date.After(end) {
			v := p.AdjClose
			if v == 0 {
				v = p.Close
			}
			if v != 0 {
				best = v
			}
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("no price for %s on or before %s", querySymbol, dt.Format("2006-01-02"))
	}
	return best, nil
}

// holdingLot represents a quantity of a symbol on a specific exchange.
type holdingLot struct {
	exchange string
	qty      float64
	currency string
}

// computeHoldingLots returns one lot per (symbol, exchange) pair, summing quantities
// across all non-FX, non-transfer trades. When the same symbol trades on multiple
// exchanges each exchange gets its own lot so synthetic sell trades can carry the
// correct exchange and remain matched by the portfolio service's FIFO logic.
func computeHoldingLots(trades []models.Trade) map[string][]holdingLot {
	type lotKey struct{ symbol, exchange string }
	totals := make(map[lotKey]holdingLot)
	for _, t := range trades {
		if models.IsFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		k := lotKey{t.Symbol, t.ListingExchange}
		lot := totals[k]
		lot.exchange = t.ListingExchange
		lot.qty += t.Quantity
		if lot.currency == "" {
			lot.currency = t.Currency
		}
		totals[k] = lot
	}
	bySymbol := make(map[string][]holdingLot)
	for k, lot := range totals {
		if lot.qty > 1e-8 {
			bySymbol[k.symbol] = append(bySymbol[k.symbol], lot)
		}
	}
	return bySymbol
}

// resolveLots returns the lots to act on for the given adjustment.
// When adj.Exchange is specified only that exchange's lot is returned;
// otherwise all lots for the symbol are returned (covering the common
// case where the frontend omits the exchange).
func resolveLots(lots map[string][]holdingLot, symbol, exchange string) []holdingLot {
	all := lots[symbol]
	if exchange == "" {
		return all
	}
	for _, l := range all {
		if l.exchange == exchange {
			return []holdingLot{l}
		}
	}
	return nil
}

// applyAdjustments appends synthetic sell/buy Trades to data based on the given adjustments.
// Sell quantities are validated against current holdings; missing positions are skipped silently
// for sell_all (and return an error for sell_qty/sell_pct if qty is zero).
//
// When adj.Date is nil, defaultDate is used (e.g. the scenario's BaseAsOf). When both are nil
// the current date is used. For historical dates the price is sourced from GetHistory; for
// current/future dates GetLatestPrice is used.
//
// applyAdjustments also appends matching CashTransaction entries so that MWR calculations see
// the capital inflow/outflow associated with each synthetic trade. Without this, a scenario
// "buy" would look like a free gain of value.
func applyAdjustments(data *models.FlexQueryData, adjustments []Adjustment, mp market.Provider, defaultDate *time.Time) error {
	lots := computeHoldingLots(data.Trades)

	for _, adj := range adjustments {
		adjDate := today()
		switch {
		case adj.Date != nil:
			adjDate = adj.Date.Time()
		case defaultDate != nil:
			adjDate = *defaultDate
		}

		// Currency resolution: prefer the one found on matching existing trades (respecting
		// the adj.Exchange filter), then the user-supplied adj.Currency, then a USD fallback.
		// Using adj.Currency when explicitly provided means a mismatched exchange doesn't
		// silently default to USD.
		nativeCurrency := findSymbolCurrency(data.Trades, adj.Symbol, adj.Exchange)
		if nativeCurrency == "" {
			nativeCurrency = adj.Currency
		}
		if nativeCurrency == "" {
			nativeCurrency = "USD"
		}

		// Resolve the Yahoo Finance ticker so the market provider can fetch the price.
		// IBKR symbols (e.g. "4GLD") differ from Yahoo tickers (e.g. "4GLD.DE");
		// the mapping is stored in the YahooSymbol field on existing trades.
		querySymbol := findYahooSymbol(data.Trades, adj.Symbol, adj.Exchange)

		switch adj.Action {

		case ActionSellAll:
			targets := resolveLots(lots, adj.Symbol, adj.Exchange)
			if len(targets) == 0 {
				continue // nothing to sell — skip silently
			}
			price, err := priceAt(mp, querySymbol, adjDate)
			if err != nil {
				return fmt.Errorf("getting price for %s: %w", adj.Symbol, err)
			}
			if price == 0 {
				return fmt.Errorf("price unavailable for %s", adj.Symbol)
			}
			for _, lot := range targets {
				cur := lot.currency
				if cur == "" {
					cur = nativeCurrency
				}
				data.Trades = append(data.Trades, syntheticSellTrade(adj.Symbol, lot.exchange, cur, lot.qty, price, adjDate))
				data.CashTransactions = append(data.CashTransactions, syntheticWithdrawal(cur, lot.qty*price, adjDate, adj.Symbol))
			}

		case ActionSellPct:
			targets := resolveLots(lots, adj.Symbol, adj.Exchange)
			if len(targets) == 0 {
				return fmt.Errorf("cannot sell_pct: no position in %s", adj.Symbol)
			}
			price, err := priceAt(mp, querySymbol, adjDate)
			if err != nil {
				return fmt.Errorf("getting price for %s: %w", adj.Symbol, err)
			}
			if price == 0 {
				return fmt.Errorf("price unavailable for %s", adj.Symbol)
			}
			pct := adj.Value / 100.0
			for _, lot := range targets {
				sellQty := lot.qty * pct
				if sellQty <= 1e-8 {
					continue
				}
				cur := lot.currency
				if cur == "" {
					cur = nativeCurrency
				}
				data.Trades = append(data.Trades, syntheticSellTrade(adj.Symbol, lot.exchange, cur, sellQty, price, adjDate))
				data.CashTransactions = append(data.CashTransactions, syntheticWithdrawal(cur, sellQty*price, adjDate, adj.Symbol))
			}

		case ActionSellQty:
			targets := resolveLots(lots, adj.Symbol, adj.Exchange)
			var totalQty float64
			for _, l := range targets {
				totalQty += l.qty
			}
			if adj.Value > totalQty+1e-8 {
				return fmt.Errorf("sell_qty %.4f exceeds current holding %.4f for %s", adj.Value, totalQty, adj.Symbol)
			}
			price, err := priceAt(mp, querySymbol, adjDate)
			if err != nil {
				return fmt.Errorf("getting price for %s: %w", adj.Symbol, err)
			}
			if price == 0 {
				return fmt.Errorf("price unavailable for %s", adj.Symbol)
			}
			// Sell from each exchange proportionally.
			remaining := adj.Value
			for i, lot := range targets {
				var sellQty float64
				if i == len(targets)-1 {
					sellQty = remaining
				} else {
					sellQty = adj.Value * (lot.qty / totalQty)
				}
				if sellQty <= 1e-8 {
					continue
				}
				cur := lot.currency
				if cur == "" {
					cur = nativeCurrency
				}
				data.Trades = append(data.Trades, syntheticSellTrade(adj.Symbol, lot.exchange, cur, sellQty, price, adjDate))
				data.CashTransactions = append(data.CashTransactions, syntheticWithdrawal(cur, sellQty*price, adjDate, adj.Symbol))
				remaining -= sellQty
			}

		case ActionBuy:
			currency := adj.Currency
			if currency == "" {
				currency = nativeCurrency
			}
			price, err := priceAt(mp, querySymbol, adjDate)
			if err != nil {
				return fmt.Errorf("getting price for %s: %w", adj.Symbol, err)
			}
			if price == 0 {
				return fmt.Errorf("price unavailable for %s", adj.Symbol)
			}
			qty := adj.Value / price
			data.Trades = append(data.Trades, syntheticBuyTrade(adj.Symbol, adj.Exchange, currency, qty, price, adjDate))
			data.CashTransactions = append(data.CashTransactions, syntheticDeposit(currency, adj.Value, adjDate, adj.Symbol))
		}
	}

	return nil
}

// syntheticDeposit builds a CashTransaction representing capital injected to fund a buy.
// The MWR/TWR code consumes CashTransactions as CashFlow rows; marking these as "Deposits/Withdrawals"
// matches the treatment of real IB deposits.
func syntheticDeposit(currency string, amount float64, dt time.Time, symbol string) models.CashTransaction {
	return models.CashTransaction{
		Type:        "Deposits/Withdrawals",
		Currency:    currency,
		Amount:      amount,
		DateTime:    dt,
		Description: "scenario buy: " + symbol,
		Symbol:      "",
	}
}

// syntheticWithdrawal builds the mirror of syntheticDeposit for scenario sells.
func syntheticWithdrawal(currency string, amount float64, dt time.Time, symbol string) models.CashTransaction {
	return models.CashTransaction{
		Type:        "Deposits/Withdrawals",
		Currency:    currency,
		Amount:      -amount,
		DateTime:    dt,
		Description: "scenario sell: " + symbol,
		Symbol:      "",
	}
}
