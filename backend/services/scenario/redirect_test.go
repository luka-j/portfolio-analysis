package scenario

import (
	"testing"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
)

func TestBuildRedirectScenario(t *testing.T) {
	mp := &mockMarketProvider{
		latest: map[string]float64{
			"AAPL": 150,
			"MSFT": 300,
		},
		hist: map[string][]models.PricePoint{
			"AAPL": {
				{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 100},
				{Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), Close: 110},
			},
			"MSFT": {
				{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 200},
				{Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), Close: 220},
			},
			"EURUSD=X": {
				{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 1.1},
			},
		},
	}
	fxSvc := fx.NewService(mp, nil)

	realData := &models.FlexQueryData{
		Trades: []models.Trade{
			// Deposit equivalent via BUY
			{Symbol: "AAPL", Quantity: 10, Price: 100, Proceeds: -1000, DateTime: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), BuySell: "BUY", Currency: "USD"},
			// Withdrawal equivalent via SELL
			{Symbol: "AAPL", Quantity: -5, Price: 110, Proceeds: 550, DateTime: time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC), BuySell: "SELL", Currency: "USD"},
		},
	}

	t.Run("redirect scenario success", func(t *testing.T) {
		spec := ScenarioSpec{
			Base: BaseModeRedirect,
			Basket: &Basket{
				Mode:             BasketModeWeight,
				NotionalCurrency: "USD",
				Items: []BasketItem{
					{Symbol: "MSFT", Weight: 1.0, Currency: "USD"},
				},
			},
		}

		res, err := Build(spec, realData, mp, fxSvc)
		if err != nil {
			t.Fatalf("unexpected Build error: %v", err)
		}

		if len(res.Trades) != 2 {
			t.Fatalf("expected 2 trades, got %d", len(res.Trades))
		}

		// Initial Buy: $1000 worth of MSFT on 2024-01-02. MSFT price = 200. Qty = 5.
		// Sell on 2024-02-01: Proportional sell. $550 withdrawal. MSFT price = 220. Qty = 2.5.
		var buyQty, sellQty float64
		for _, trade := range res.Trades {
			if trade.BuySell == "BUY" {
				buyQty = trade.Quantity
			} else if trade.BuySell == "SELL" {
				sellQty = trade.Quantity
			}
		}

		if buyQty != 5 {
			t.Errorf("expected buy quantity to be 5, got %f", buyQty)
		}
		if sellQty != -2.5 {
			t.Errorf("expected sell quantity to be -2.5, got %f", sellQty)
		}
	})

	t.Run("redirect scenario validation errors", func(t *testing.T) {
		// Missing basket
		specNoBasket := ScenarioSpec{Base: BaseModeRedirect}
		_, err := Build(specNoBasket, realData, mp, fxSvc)
		if err == nil {
			t.Fatal("expected error for missing basket in redirect scenario")
		}

		// Incorrect basket mode
		specBadMode := ScenarioSpec{
			Base: BaseModeRedirect,
			Basket: &Basket{
				Mode: BasketModeQuantity,
			},
		}
		_, err = Build(specBadMode, realData, mp, fxSvc)
		if err == nil {
			t.Fatal("expected error for quantity basket mode in redirect scenario")
		}
	})

	t.Run("getPriceAt pricing errors", func(t *testing.T) {
		// Test getPriceAt error cases indirectly via a symbol that doesn't exist
		spec := ScenarioSpec{
			Base: BaseModeRedirect,
			Basket: &Basket{
				Mode: BasketModeWeight,
				Items: []BasketItem{
					{Symbol: "NON_EXISTENT", Weight: 1.0},
				},
			},
		}
		res, err := Build(spec, realData, mp, fxSvc)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		// Since it falls back to 1 when non-existent and LatestPrice also fails
		if len(res.Trades) == 0 {
			t.Fatal("expected trades even with missing prices due to fallbacks")
		}
	})

	t.Run("TRANSFER_IN handling", func(t *testing.T) {
		transferData := &models.FlexQueryData{
			Trades: []models.Trade{
				{Symbol: "AAPL", Quantity: 10, Price: 100, DateTime: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), BuySell: "TRANSFER_IN", Currency: "USD"},
			},
		}
		spec := ScenarioSpec{
			Base: BaseModeRedirect,
			Basket: &Basket{
				Mode:             BasketModeWeight,
				NotionalCurrency: "USD",
				Items: []BasketItem{
					{Symbol: "MSFT", Weight: 1.0, Currency: "USD"},
				},
			},
		}
		res, err := Build(spec, transferData, mp, fxSvc)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}
		if len(res.Trades) == 0 {
			t.Error("expected trades generated from TRANSFER_IN")
		}
	})
}

func TestBuildOtherModes(t *testing.T) {
	mp := &mockMarketProvider{
		latest: map[string]float64{"AAPL": 150},
	}
	realData := &models.FlexQueryData{
		Trades: []models.Trade{
			{Symbol: "AAPL", Quantity: 10, Price: 100, DateTime: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), BuySell: "BUY", Currency: "USD"},
		},
	}

	t.Run("BaseModeReal with BaseAsOf", func(t *testing.T) {
		asOf := NewDateOnly(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
		spec := ScenarioSpec{
			Base:     BaseModeReal,
			BaseAsOf: &asOf,
		}
		res, err := Build(spec, realData, mp, nil)
		if err != nil {
			t.Fatalf("unexpected Build error: %v", err)
		}
		if len(res.Trades) != 0 {
			t.Errorf("expected 0 trades because trade is after BaseAsOf, got %d", len(res.Trades))
		}
	})

	t.Run("BaseModeReal with Adjustments", func(t *testing.T) {
		spec := ScenarioSpec{
			Base: BaseModeReal,
			Adjustments: []Adjustment{
				{Symbol: "AAPL", Action: ActionSellAll},
			},
		}
		res, err := Build(spec, realData, mp, nil)
		if err != nil {
			t.Fatalf("unexpected Build error: %v", err)
		}
		if len(res.Trades) != 2 {
			t.Fatalf("expected 2 trades (1 original, 1 synthetic sell), got %d", len(res.Trades))
		}
	})

	t.Run("getPriceAt fallbacks", func(t *testing.T) {
		// Mock with empty price history to test getPriceAt no price data error
		mpErr := &mockMarketProviderError{}
		_, err := getPriceAt(mpErr, "AAPL", time.Now())
		if err == nil {
			t.Fatal("expected error from provider failure")
		}
	})
}
