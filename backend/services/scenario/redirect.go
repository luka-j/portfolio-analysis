package scenario

import (
	"fmt"
	"sort"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/cashbucket"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
)

func getPriceAt(mp market.Provider, symbol string, date time.Time) (float64, error) {
	from := date.AddDate(0, 0, -5)
	prices, err := mp.GetHistory(symbol, from, date, false)
	if err != nil {
		return 0, err
	}
	if len(prices) == 0 {
		return 0, fmt.Errorf("no price data")
	}
	last := prices[len(prices)-1]
	if last.AdjClose != 0 {
		return last.AdjClose, nil
	}
	return last.Close, nil
}

// buildRedirectScenario generates a counterfactual portfolio by extracting
// historical cash flows from the real portfolio and applying them to the target basket.
func buildRedirectScenario(spec ScenarioSpec, realData *models.FlexQueryData, mp market.Provider, fxSvc *fx.Service) (*models.FlexQueryData, error) {
	if spec.Basket == nil {
		return nil, fmt.Errorf("basket is required for redirect scenario")
	}
	if spec.Basket.Mode != BasketModeWeight {
		return nil, fmt.Errorf("redirect scenario requires basket mode 'weight'")
	}

	// 1. Extract net cash flows using cashbucket.
	// We use the same standard expiry days as the real portfolio (e.g., 3 days).
	// For scenarios, a default like 3 is reasonable to prevent immediate buy/sell pairs
	// from generating false inflows/outflows.
	expiryDays := 3

	var tradeFlows []models.Trade
	for _, t := range realData.Trades {
		if models.IsFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		tradeFlows = append(tradeFlows, t)
	}

	var bucketDividends []cashbucket.Dividend
	for _, d := range realData.CashDividends {
		bucketDividends = append(bucketDividends, cashbucket.Dividend{
			DateTime: d.DateTime,
			Amount:   d.Amount,
			Currency: d.Currency,
		})
	}

	// We convert all cash flows to the basket's notional currency.
	targetCur := spec.Basket.NotionalCurrency
	if targetCur == "" {
		targetCur = "USD"
	}

	convertFn := func(amount float64, cur string, date time.Time) (float64, error) {
		if cur == targetCur || fxSvc == nil {
			return amount, nil
		}
		return fxSvc.Convert(amount, cur, targetCur, date, false)
	}

	res, err := cashbucket.Process(tradeFlows, nil, bucketDividends, expiryDays, time.Now().UTC(), convertFn)
	if err != nil {
		return nil, fmt.Errorf("processing cash flows: %w", err)
	}

	data := &models.FlexQueryData{
		UserHash: realData.UserHash,
		Trades:   make([]models.Trade, 0),
	}

	// 2. Normalize basket weights.
	var totalWeight float64
	for _, item := range spec.Basket.Items {
		totalWeight += item.Weight
	}
	if totalWeight <= 0 {
		return nil, fmt.Errorf("basket total weight must be > 0")
	}

	// 3. For each adjusted cash flow, generate synthetic trades.
	simHoldings := make(map[string]float64)

	for _, cf := range res.AdjustedCashFlows {
		if cf.Amount == 0 {
			continue
		}

		if cf.Amount < 0 {
			// INFLOW (Deposit): Buy the basket.
			depositAmount := -cf.Amount
			for _, item := range spec.Basket.Items {
				w := item.Weight / totalWeight
				allocAmount := depositAmount * w
				if allocAmount <= 0 {
					continue
				}

				price, err := getPriceAt(mp, item.Symbol, cf.Date)
				if err != nil || price == 0 {
					price, _ = mp.GetLatestPrice(item.Symbol, false)
					if price == 0 {
						price = 1 // Prevent div by zero
					}
				}

				assetCur := item.Currency
				if assetCur == "" {
					assetCur = targetCur
				}

				var allocAmountNative float64 = allocAmount
				if assetCur != targetCur && fxSvc != nil {
					allocAmountNative, _ = fxSvc.Convert(allocAmount, targetCur, assetCur, cf.Date, false)
				}

				qty := allocAmountNative / price
				simHoldings[item.Symbol] += qty

				data.Trades = append(data.Trades, syntheticBuyTrade(
					item.Symbol,
					item.Exchange,
					assetCur,
					qty,
					price,
					cf.Date,
				))
			}
		} else {
			// OUTFLOW (Withdrawal): Sell from current holdings proportionally.
			withdrawalAmount := cf.Amount
			
			var totalValue float64
			type holdingVal struct {
				sym   string
				exch  string
				cur   string
				qty   float64
				price float64
				val   float64 // in targetCur
			}
			var hVals []holdingVal

			for _, item := range spec.Basket.Items {
				qty := simHoldings[item.Symbol]
				if qty <= 0 {
					continue
				}

				price, err := getPriceAt(mp, item.Symbol, cf.Date)
				if err != nil || price == 0 {
					price, _ = mp.GetLatestPrice(item.Symbol, false)
					if price == 0 {
						price = 1
					}
				}

				assetCur := item.Currency
				if assetCur == "" {
					assetCur = targetCur
				}

				nativeVal := qty * price
				valTargetCur := nativeVal
				if assetCur != targetCur && fxSvc != nil {
					valTargetCur, _ = fxSvc.Convert(nativeVal, assetCur, targetCur, cf.Date, false)
				}

				hVals = append(hVals, holdingVal{
					sym:   item.Symbol,
					exch:  item.Exchange,
					cur:   assetCur,
					qty:   qty,
					price: price,
					val:   valTargetCur,
				})
				totalValue += valTargetCur
			}

			if totalValue <= 0 {
				continue // Nothing to sell
			}

			if withdrawalAmount > totalValue {
				withdrawalAmount = totalValue
			}

			for _, hv := range hVals {
				frac := hv.val / totalValue
				sellValTargetCur := withdrawalAmount * frac

				sellValNative := sellValTargetCur
				if hv.cur != targetCur && fxSvc != nil {
					sellValNative, _ = fxSvc.Convert(sellValTargetCur, targetCur, hv.cur, cf.Date, false)
				}

				sellQty := sellValNative / hv.price
				if sellQty > hv.qty {
					sellQty = hv.qty
				}

				simHoldings[hv.sym] -= sellQty

				data.Trades = append(data.Trades, syntheticSellTrade(
					hv.sym,
					hv.exch,
					hv.cur,
					sellQty,
					hv.price,
					cf.Date,
				))
			}
		}
	}

	sort.Slice(data.Trades, func(i, j int) bool {
		return data.Trades[i].DateTime.Before(data.Trades[j].DateTime)
	})

	return data, nil
}
