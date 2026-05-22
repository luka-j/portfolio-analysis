package scenario

import (
	"testing"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
)

// TestApplyBasket tests basket application logic in weight and quantity modes.
func TestApplyBasket(t *testing.T) {
	mp := &mockMarketProvider{
		latest: map[string]float64{
			"AAPL": 150,
			"MSFT": 300,
		},
		hist: map[string][]models.PricePoint{
			"EURUSD=X": {
				{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 1.1},
			},
		},
	}
	fxSvc := fx.NewService(mp, nil)

	t.Run("weight mode with cost basis and FX", func(t *testing.T) {
		data := &models.FlexQueryData{}
		cb := 140.0
		basket := &Basket{
			Mode:             BasketModeWeight,
			NotionalValue:    10000,
			NotionalCurrency: "EUR",
			Items: []BasketItem{
				{Symbol: "AAPL", Weight: 0.5, Currency: "USD", CostBasis: &cb},
				{Symbol: "MSFT", Weight: 0.5, Currency: "EUR"},
			},
		}

		err := applyBasket(data, basket, mp, fxSvc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data.Trades) != 2 {
			t.Fatalf("expected 2 trades, got %d", len(data.Trades))
		}

		// Apple: EUR 5000 allocated. EURUSD=1.1, so Native allocated: USD 5500.
		// Price used: cost basis = $140. Qty = 5500 / 140 = 39.2857
		var aaplTrade models.Trade
		for _, trade := range data.Trades {
			if trade.Symbol == "AAPL" {
				aaplTrade = trade
			}
		}
		expectedQty := 5500.0 / 140.0
		if aaplTrade.Quantity < expectedQty-0.0001 || aaplTrade.Quantity > expectedQty+0.0001 {
			t.Errorf("expected Apple quantity to be ~%f, got %f", expectedQty, aaplTrade.Quantity)
		}
	})

	t.Run("quantity mode with latest price", func(t *testing.T) {
		data := &models.FlexQueryData{}
		basket := &Basket{
			Mode: BasketModeQuantity,
			Items: []BasketItem{
				{Symbol: "MSFT", Quantity: 10, Currency: "USD"},
			},
		}

		err := applyBasket(data, basket, mp, fxSvc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data.Trades) != 1 {
			t.Fatalf("expected 1 trade, got %d", len(data.Trades))
		}

		trade := data.Trades[0]
		if trade.Quantity != 10 {
			t.Errorf("expected qty 10, got %f", trade.Quantity)
		}
		if trade.Price != 300 {
			t.Errorf("expected price 300, got %f", trade.Price)
		}
	})

	t.Run("missing price error", func(t *testing.T) {
		data := &models.FlexQueryData{}
		basket := &Basket{
			Mode: BasketModeQuantity,
			Items: []BasketItem{
				{Symbol: "UNKNOWN", Quantity: 10},
			},
		}

		err := applyBasket(data, basket, mp, fxSvc)
		if err == nil {
			t.Fatal("expected error for missing price")
		}
	})

	t.Run("zero price error", func(t *testing.T) {
		data := &models.FlexQueryData{}
		mpZero := &mockMarketProvider{
			latest: map[string]float64{"AAPL": 0},
		}
		basket := &Basket{
			Mode: BasketModeQuantity,
			Items: []BasketItem{
				{Symbol: "AAPL", Quantity: 10},
			},
		}

		err := applyBasket(data, basket, mpZero, fxSvc)
		if err == nil {
			t.Fatal("expected error for zero price")
		}
	})

	t.Run("weight mode zero price error", func(t *testing.T) {
		data := &models.FlexQueryData{}
		mpZero := &mockMarketProvider{
			latest: map[string]float64{"AAPL": 0},
		}
		basket := &Basket{
			Mode:             BasketModeWeight,
			NotionalValue:    1000,
			NotionalCurrency: "USD",
			Items: []BasketItem{
				{Symbol: "AAPL", Weight: 1.0},
			},
		}

		err := applyBasket(data, basket, mpZero, fxSvc)
		if err == nil {
			t.Fatal("expected error for zero price in weight mode")
		}
	})

	t.Run("FX conversion error", func(t *testing.T) {
		data := &models.FlexQueryData{}
		mpErr := &mockMarketProvider{
			latest: map[string]float64{"AAPL": 100},
			hist:   map[string][]models.PricePoint{}, // no EURUSD FX rate
		}
		fxSvcErr := fx.NewService(mpErr, nil)
		basket := &Basket{
			Mode:             BasketModeWeight,
			NotionalValue:    1000,
			NotionalCurrency: "EUR",
			Items: []BasketItem{
				{Symbol: "AAPL", Weight: 1.0, Currency: "USD"},
			},
		}

		err := applyBasket(data, basket, mpErr, fxSvcErr)
		if err == nil {
			t.Fatal("expected error for failed FX conversion")
		}
	})

	t.Run("FX rate zero error", func(t *testing.T) {
		data := &models.FlexQueryData{}
		mpZero := &mockMarketProvider{
			latest: map[string]float64{"AAPL": 100},
			hist: map[string][]models.PricePoint{
				"EURUSD=X": {{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 0}},
			},
		}
		fxSvcZero := fx.NewService(mpZero, nil)
		basket := &Basket{
			Mode:             BasketModeWeight,
			NotionalValue:    1000,
			NotionalCurrency: "EUR",
			Items: []BasketItem{
				{Symbol: "AAPL", Weight: 1.0, Currency: "USD"},
			},
		}

		err := applyBasket(data, basket, mpZero, fxSvcZero)
		if err == nil {
			t.Fatal("expected error for zero FX rate")
		}
	})
}
