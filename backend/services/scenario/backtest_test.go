package scenario

import (
	"errors"
	"testing"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
)

// TestBuildBacktest checks the core simulation execution under various parameters.
func TestBuildBacktest(t *testing.T) {
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)


	// Setup fake price points for symbols
	ptsA := []models.PricePoint{
		{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 100, AdjClose: 100},
		{Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), Close: 110, AdjClose: 110},
		{Date: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), Close: 105, AdjClose: 105},
		{Date: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), Close: 120, AdjClose: 120},
		{Date: time.Date(2024, 5, 2, 0, 0, 0, 0, time.UTC), Close: 130, AdjClose: 130},
		{Date: time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC), Close: 125, AdjClose: 125},
	}

	ptsB := []models.PricePoint{
		{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 50, AdjClose: 50},
		{Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), Close: 48, AdjClose: 48},
		{Date: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), Close: 52, AdjClose: 52},
		{Date: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), Close: 60, AdjClose: 60},
		{Date: time.Date(2024, 5, 2, 0, 0, 0, 0, time.UTC), Close: 58, AdjClose: 58},
		{Date: time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC), Close: 55, AdjClose: 55},
	}

	ptsFX := []models.PricePoint{
		{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Close: 1.1, AdjClose: 1.1},
	}

	mp := &mockMarketProvider{
		latest: map[string]float64{"AAPL": 100, "MSFT": 50},
		hist: map[string][]models.PricePoint{
			"AAPL":     ptsA,
			"MSFT":     ptsB,
			"EURUSD=X": ptsFX,
		},
	}

	fxSvc := fx.NewService(mp, nil)

	getSpec := func() ScenarioSpec {
		return ScenarioSpec{
			Basket: &Basket{
				Mode: BasketModeWeight,
				Items: []BasketItem{
					{Symbol: "AAPL", Weight: 0.5, Currency: "USD"},
					{Symbol: "MSFT", Weight: 0.5, Currency: "USD"},
				},
			},
			Backtest: &BacktestConfig{
				StartDate:          NewDateOnly(startDate),
				InitialAmount:      10000,
				Currency:           "USD",
				Contribution:       ContributionMonthly,
				ContributionAmount: 1000,
				Rebalance:          RebalanceModeQuarterly,
			},
		}
	}

	t.Run("success simulation", func(t *testing.T) {
		res, err := buildBacktest(getSpec(), mp, fxSvc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Trades) == 0 {
			t.Fatal("expected trades to be generated, got 0")
		}

		// Verify initial buys
		var initialAAPL, initialMSFT float64
		for _, trade := range res.Trades {
			if trade.DateTime.Format("2006-01-02") == "2024-01-02" {
				if trade.Symbol == "AAPL" {
					initialAAPL = trade.Quantity
				} else if trade.Symbol == "MSFT" {
					initialMSFT = trade.Quantity
				}
			}
		}

		// Initial Amount: 10,000. Apple weight: 0.5 ($5000) at $100 -> 50 shares
		if initialAAPL != 50 {
			t.Errorf("expected initial AAPL quantity to be 50, got %f", initialAAPL)
		}
		// MSFT weight: 0.5 ($5000) at $50 -> 100 shares
		if initialMSFT != 100 {
			t.Errorf("expected initial MSFT quantity to be 100, got %f", initialMSFT)
		}
	})

	t.Run("future start date errors", func(t *testing.T) {
		badSpec := getSpec()
		badSpec.Backtest.StartDate = NewDateOnly(time.Now().AddDate(1, 0, 0))
		_, err := buildBacktest(badSpec, mp, fxSvc)
		if err == nil {
			t.Fatal("expected error for future start date")
		}
	})

	t.Run("no history error", func(t *testing.T) {
		badSpec := getSpec()
		badSpec.Basket.Items = []BasketItem{{Symbol: "UNKNOWN", Weight: 1.0}}
		_, err := buildBacktest(badSpec, mp, fxSvc)
		if err == nil {
			t.Fatal("expected error for missing history")
		}
	})

	t.Run("threshold mode rebalance", func(t *testing.T) {
		threshSpec := getSpec()
		threshSpec.Backtest.Rebalance = RebalanceModeThreshold
		threshSpec.Backtest.RebalanceThreshold = 5.0 // 5% drift

		res, err := buildBacktest(threshSpec, mp, fxSvc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Trades) == 0 {
			t.Fatal("expected trades to be generated")
		}
	})

	t.Run("FX conversion in allocation", func(t *testing.T) {
		fxSpec := getSpec()
		// Force currency conversion from EUR (backtest baseline) to USD for AAPL/MSFT
		fxSpec.Backtest.Currency = "EUR"
		res, err := buildBacktest(fxSpec, mp, fxSvc)
		if err != nil {
			t.Fatalf("unexpected error with FX: %v", err)
		}
		if len(res.Trades) == 0 {
			t.Fatal("expected trades")
		}
	})
}

// TestBacktestHelpers checks non-replicated edge cases in backtest utility functions.
func TestBacktestHelpers(t *testing.T) {
	dates := []time.Time{
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}

	t.Run("nearestTradingDayDates fallback", func(t *testing.T) {
		target := time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC)
		res := nearestTradingDayDates(target, dates)
		expected := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
		if !res.Equal(expected) {
			t.Errorf("expected %v, got %v", expected, res)
		}

		// fully empty or out of range fallback
		res2 := nearestTradingDayDates(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), nil)
		if !res2.IsZero() {
			t.Errorf("expected zero time for empty dates, got %v", res2)
		}
	})

	t.Run("firstTradingDayOnOrAfter", func(t *testing.T) {
		pts := []models.PricePoint{
			{Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
			{Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)},
		}
		res := firstTradingDayOnOrAfter(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), pts)
		if !res.Equal(time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("expected 2024-01-05, got %v", res)
		}

		resEmpty := firstTradingDayOnOrAfter(time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC), pts)
		if !resEmpty.IsZero() {
			t.Errorf("expected zero, got %v", resEmpty)
		}
	})

	t.Run("drifted with empty value", func(t *testing.T) {
		syms := []backtestSymbol{
			{symbol: "AAPL", priceMap: map[string]float64{"2024-01-02": 100}},
		}
		holdingQty := map[string]float64{"AAPL": 0}
		targetWeights := map[string]float64{"AAPL": 1.0}
		res := drifted(syms, holdingQty, targetWeights, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), 5.0)
		if res {
			t.Error("drifted should be false when total value <= 0")
		}
	})

	t.Run("resolveTargetWeights equal weights", func(t *testing.T) {
		b := &Basket{
			Mode: BasketModeQuantity,
			Items: []BasketItem{
				{Symbol: "A"},
				{Symbol: "B"},
			},
		}
		w, err := resolveTargetWeights(b)
		if err != nil {
			t.Fatal(err)
		}
		if w["A"] != 0.5 || w["B"] != 0.5 {
			t.Errorf("expected equal 0.5 weights, got A=%f, B=%f", w["A"], w["B"])
		}
	})

	t.Run("rebalanceToCadence defaults", func(t *testing.T) {
		cad := rebalanceToCadence(RebalanceModeNone)
		if cad != ContributionMonthly {
			t.Errorf("expected ContributionMonthly as fallback, got %v", cad)
		}
	})
}

// mockMarketProviderError implements a provider that fails
type mockMarketProviderError struct {
	market.Provider
}

func (m *mockMarketProviderError) GetHistory(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	return nil, errors.New("provider failure")
}
