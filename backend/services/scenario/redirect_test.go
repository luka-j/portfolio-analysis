package scenario

import (
	"testing"
	"time"

	"portfolio-analysis/models"
)

type mockRedirectMarketProvider struct {
	prices map[string]float64
}

func (m *mockRedirectMarketProvider) GetLatestPrice(symbol string, cachedOnly bool) (float64, error) {
	if p, ok := m.prices[symbol]; ok {
		return p, nil
	}
	return 1.0, nil
}

func (m *mockRedirectMarketProvider) GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error) {
	if p, ok := m.prices[symbol]; ok {
		return []models.PricePoint{{Date: to, Close: p, AdjClose: p}}, nil
	}
	return []models.PricePoint{{Date: to, Close: 1.0, AdjClose: 1.0}}, nil
}

func (m *mockRedirectMarketProvider) TradingDates(from, to time.Time) ([]time.Time, error) {
	return nil, nil
}

func TestBuildRedirectScenario(t *testing.T) {
	mp := &mockRedirectMarketProvider{
		prices: map[string]float64{
			"SPY": 100.0,
		},
	}

	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC)

	// User buys $1000 of AAPL at t1
	// User sells $500 of AAPL at t2 (which produces a positive flow if not consumed within expiry, wait, default is 3 days).
	// So t1: -$1000 (deposit). t2: +$500 (withdrawal).
	realData := &models.FlexQueryData{
		Trades: []models.Trade{
			{
				Symbol:   "AAPL",
				Currency: "USD",
				Quantity: 10,
				Price:    100,
				Proceeds: -1000,
				BuySell:  "BUY",
				DateTime: t1,
			},
			{
				Symbol:   "AAPL",
				Currency: "USD",
				Quantity: -4,
				Price:    125,
				Proceeds: 500,
				BuySell:  "SELL",
				DateTime: t2,
			},
		},
	}

	spec := ScenarioSpec{
		Base: BaseModeRedirect,
		Basket: &Basket{
			Mode:             BasketModeWeight,
			NotionalCurrency: "USD",
			Items: []BasketItem{
				{Symbol: "SPY", Weight: 1, Currency: "USD"},
			},
		},
	}

	outData, err := buildRedirectScenario(spec, realData, mp, nil)
	if err != nil {
		t.Fatalf("buildRedirectScenario failed: %v", err)
	}

	// We expect two trades:
	// 1. Buy SPY with $1000 at t1. SPY price is 100, so qty = 10.
	// 2. Sell SPY for $500 at t2. SPY price is 100, so qty = 5.

	if len(outData.Trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(outData.Trades))
	}

	if outData.Trades[0].Symbol != "SPY" || outData.Trades[0].Quantity != 10 {
		t.Errorf("expected buy 10 SPY, got %f", outData.Trades[0].Quantity)
	}
	if outData.Trades[0].Proceeds != -1000 {
		t.Errorf("expected proceeds -1000, got %f", outData.Trades[0].Proceeds)
	}

	if outData.Trades[1].Symbol != "SPY" || outData.Trades[1].Quantity != -5 {
		t.Errorf("expected sell 5 SPY, got %f", outData.Trades[1].Quantity)
	}
	if outData.Trades[1].Proceeds != 500 {
		t.Errorf("expected proceeds 500, got %f", outData.Trades[1].Proceeds)
	}
}
