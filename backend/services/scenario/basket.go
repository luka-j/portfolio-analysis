package scenario

import (
	"fmt"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
)

// applyBasket appends synthetic buy Trades to data for each item in the basket.
// In weight mode, each item's allocation = notional × weight, converted to the item's
// currency via fxSvc when necessary. The market price is used as cost basis unless
// item.CostBasis is explicitly set.
func applyBasket(data *models.FlexQueryData, basket *Basket, mp market.Provider, fxSvc *fx.Service) error {
	acquiredAt := today()
	if basket.AcquiredAt != nil {
		acquiredAt = basket.AcquiredAt.Time()
	}

	for _, item := range basket.Items {
		var qty, price float64

		itemCurrency := item.Currency
		if itemCurrency == "" {
			itemCurrency = basket.NotionalCurrency
		}
		if itemCurrency == "" {
			itemCurrency = "USD"
		}

		if basket.Mode == BasketModeWeight {
			// Determine the amount allocated to this item in NotionalCurrency.
			allocationNotional := basket.NotionalValue * item.Weight

			// Convert to itemCurrency if needed.
			allocationNative := allocationNotional
			if itemCurrency != basket.NotionalCurrency && fxSvc != nil {
				rate, err := fxSvc.GetRate(basket.NotionalCurrency, itemCurrency, acquiredAt, false)
				if err != nil {
					return fmt.Errorf("converting %s→%s for %s: %w", basket.NotionalCurrency, itemCurrency, item.Symbol, err)
				}
				if rate == 0 {
					return fmt.Errorf("zero FX rate %s→%s for basket item %s", basket.NotionalCurrency, itemCurrency, item.Symbol)
				}
				allocationNative = allocationNotional * rate
			}

			// Determine the price to use for buying.
			if item.CostBasis != nil {
				price = *item.CostBasis
			} else {
				var err error
				price, err = mp.GetLatestPrice(item.Symbol, false)
				if err != nil {
					return fmt.Errorf("getting price for basket item %s: %w", item.Symbol, err)
				}
			}
			if price == 0 {
				return fmt.Errorf("price unavailable for basket item %s", item.Symbol)
			}
			qty = allocationNative / price

		} else {
			// Quantity mode — qty is given directly.
			qty = item.Quantity
			if item.CostBasis != nil {
				price = *item.CostBasis
			} else {
				var err error
				price, err = mp.GetLatestPrice(item.Symbol, false)
				if err != nil {
					return fmt.Errorf("getting price for basket item %s: %w", item.Symbol, err)
				}
				if price == 0 {
					return fmt.Errorf("price unavailable for basket item %s", item.Symbol)
				}
			}
		}

		if qty <= 1e-10 {
			continue
		}
		data.Trades = append(data.Trades, syntheticBuyTrade(item.Symbol, item.Exchange, itemCurrency, qty, price, acquiredAt))
	}

	return nil
}

// BasketWeights returns the weight of each basket item expressed as a fraction of
// total portfolio value. Used by the frontend to convert between weight and quantity.
// Prices must be provided externally (the frontend fetches them via the portfolio endpoint).
func BasketWeights(items []BasketItem, priceBySymbol map[string]float64) (map[string]float64, error) {
	totalValue := 0.0
	values := make(map[string]float64, len(items))
	for _, item := range items {
		price, ok := priceBySymbol[item.Symbol]
		if !ok || price == 0 {
			return nil, fmt.Errorf("price missing for %s", item.Symbol)
		}
		v := item.Quantity * price
		values[item.Symbol] = v
		totalValue += v
	}
	if totalValue == 0 {
		return make(map[string]float64), nil
	}
	weights := make(map[string]float64, len(items))
	for sym, v := range values {
		weights[sym] = v / totalValue
	}
	return weights, nil
}
