package scenario

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"portfolio-analysis/models"
)

// mockMarketProvider is a minimal market.Provider implementation for tests.
// Only the methods hit by scenario code are used; others panic if called.
type mockMarketProvider struct {
	latest map[string]float64
	hist   map[string][]models.PricePoint
	errSym string
}

func (m *mockMarketProvider) GetLatestPrice(symbol string, cachedOnly bool) (float64, error) {
	if symbol == m.errSym {
		return 0, errors.New("synthetic error")
	}
	p, ok := m.latest[symbol]
	if !ok {
		return 0, errors.New("no price")
	}
	return p, nil
}

func (m *mockMarketProvider) GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error) {
	pts, ok := m.hist[symbol]
	if !ok {
		return nil, errors.New("no history")
	}
	return pts, nil
}

func (m *mockMarketProvider) TradingDates(from, to time.Time) ([]time.Time, error) {
	return nil, nil
}

// ---------- validateSpec ----------

func TestValidateSpec(t *testing.T) {
	cases := []struct {
		name    string
		spec    ScenarioSpec
		wantErr bool
	}{
		{"real base no mods", ScenarioSpec{Base: BaseModeReal}, false},
		{"bad base", ScenarioSpec{Base: "weird"}, true},
		{"basket weight not summing to 1",
			ScenarioSpec{Base: BaseModeEmpty, Basket: &Basket{
				Mode: BasketModeWeight, NotionalValue: 1000, NotionalCurrency: "USD",
				Items: []BasketItem{{Symbol: "AAPL", Weight: 0.5}, {Symbol: "MSFT", Weight: 0.3}},
			}}, true},
		{"basket weight summing to 1",
			ScenarioSpec{Base: BaseModeEmpty, Basket: &Basket{
				Mode: BasketModeWeight, NotionalValue: 1000, NotionalCurrency: "USD",
				Items: []BasketItem{{Symbol: "AAPL", Weight: 0.6}, {Symbol: "MSFT", Weight: 0.4}},
			}}, false},
		{"backtest without basket",
			ScenarioSpec{Base: BaseModeEmpty, Backtest: &BacktestConfig{
				StartDate: NewDateOnly(time.Now()), InitialAmount: 1, Currency: "USD",
			}}, true},
		{"adjustment sell_pct out of range",
			ScenarioSpec{Base: BaseModeReal, Adjustments: []Adjustment{
				{Symbol: "AAPL", Action: ActionSellPct, Value: 150},
			}}, true},
		{"adjustment buy with no value",
			ScenarioSpec{Base: BaseModeReal, Adjustments: []Adjustment{
				{Symbol: "AAPL", Action: ActionBuy, Value: 0},
			}}, true},
		{"unknown action",
			ScenarioSpec{Base: BaseModeReal, Adjustments: []Adjustment{
				{Symbol: "AAPL", Action: "teleport", Value: 1},
			}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSpec(tc.spec)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateSpec = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---------- DateOnly ----------

func TestDateOnlyRoundTrip(t *testing.T) {
	d := NewDateOnly(time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"2024-06-15"` {
		t.Fatalf("marshal = %s", b)
	}
	var d2 DateOnly
	if err := json.Unmarshal(b, &d2); err != nil {
		t.Fatal(err)
	}
	if !d2.Time().Equal(d.Time()) {
		t.Errorf("round-trip mismatch: %v vs %v", d2.Time(), d.Time())
	}

	if err := json.Unmarshal([]byte(`"not-a-date"`), &d2); err == nil {
		t.Error("expected error for bad date")
	}
}

// ---------- filterDataAsOf ----------

func TestFilterDataAsOf(t *testing.T) {
	makeTrade := func(sym string, dt time.Time) models.Trade {
		return models.Trade{Symbol: sym, DateTime: dt, Quantity: 1, Price: 100, BuySell: "BUY", Currency: "USD"}
	}
	data := &models.FlexQueryData{
		Trades: []models.Trade{
			makeTrade("AAPL", time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)),
			makeTrade("MSFT", time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)),
			makeTrade("GOOG", time.Date(2024, 12, 1, 10, 0, 0, 0, time.UTC)),
		},
	}
	out := filterDataAsOf(data, time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
	if len(out.Trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(out.Trades))
	}
	if out.Trades[0].Symbol != "AAPL" || out.Trades[1].Symbol != "MSFT" {
		t.Errorf("unexpected surviving trades: %+v", out.Trades)
	}
	// Original must be untouched (independent slice).
	if len(data.Trades) != 3 {
		t.Error("original data was mutated")
	}
}

// ---------- computeHoldingLots ----------

func TestComputeHoldingLots_MergesExchanges(t *testing.T) {
	trades := []models.Trade{
		{Symbol: "AAPL", ListingExchange: "NASDAQ", Quantity: 10, BuySell: "BUY", Currency: "USD"},
		{Symbol: "AAPL", ListingExchange: "NASDAQ", Quantity: -3, BuySell: "SELL", Currency: "USD"},
		{Symbol: "AAPL", ListingExchange: "XETRA", Quantity: 5, BuySell: "BUY", Currency: "EUR"},
	}
	lots := computeHoldingLots(trades)
	got := lots["AAPL"]
	if len(got) != 2 {
		t.Fatalf("expected 2 lots, got %d (%+v)", len(got), got)
	}
	var nasdaq, xetra holdingLot
	for _, l := range got {
		switch l.exchange {
		case "NASDAQ":
			nasdaq = l
		case "XETRA":
			xetra = l
		}
	}
	if nasdaq.qty != 7 {
		t.Errorf("NASDAQ qty = %f, want 7", nasdaq.qty)
	}
	if xetra.qty != 5 || xetra.currency != "EUR" {
		t.Errorf("XETRA = %+v", xetra)
	}
}

// ---------- applyAdjustments ----------

func TestApplyAdjustments_SellAllAndBuy(t *testing.T) {
	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{Symbol: "AAPL", ListingExchange: "NASDAQ", Currency: "USD", Quantity: 10, BuySell: "BUY"},
		},
	}
	mp := &mockMarketProvider{latest: map[string]float64{"AAPL": 200, "NVDA": 500}}

	err := applyAdjustments(data, []Adjustment{
		{Symbol: "AAPL", Action: ActionSellAll},
		{Symbol: "NVDA", Action: ActionBuy, Value: 1000, Currency: "USD"},
	}, mp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(data.Trades))
	}
	// Sell trade should have negative quantity, positive proceeds.
	sell := data.Trades[1]
	if sell.Symbol != "AAPL" || sell.Quantity != -10 || sell.Proceeds != 2000 || sell.BuySell != "SELL" {
		t.Errorf("sell trade wrong: %+v", sell)
	}
	// Buy NVDA: qty = 1000/500 = 2
	buy := data.Trades[2]
	if buy.Symbol != "NVDA" || buy.Quantity != 2 || buy.Proceeds != -1000 || buy.BuySell != "BUY" {
		t.Errorf("buy trade wrong: %+v", buy)
	}
}

func TestApplyAdjustments_SellQtyExceedsHolding(t *testing.T) {
	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{Symbol: "AAPL", ListingExchange: "NASDAQ", Currency: "USD", Quantity: 5, BuySell: "BUY"},
		},
	}
	mp := &mockMarketProvider{latest: map[string]float64{"AAPL": 100}}
	err := applyAdjustments(data, []Adjustment{
		{Symbol: "AAPL", Action: ActionSellQty, Value: 10},
	}, mp, nil)
	if err == nil {
		t.Fatal("expected error for overselling")
	}
}

func TestApplyAdjustments_SellAllMissingIsSilent(t *testing.T) {
	data := &models.FlexQueryData{}
	mp := &mockMarketProvider{}
	err := applyAdjustments(data, []Adjustment{
		{Symbol: "AAPL", Action: ActionSellAll},
	}, mp, nil)
	if err != nil {
		t.Errorf("sell_all on empty portfolio should be silent, got %v", err)
	}
}

func TestApplyAdjustments_SellPctMissingErrors(t *testing.T) {
	data := &models.FlexQueryData{}
	mp := &mockMarketProvider{latest: map[string]float64{"AAPL": 100}}
	err := applyAdjustments(data, []Adjustment{
		{Symbol: "AAPL", Action: ActionSellPct, Value: 50},
	}, mp, nil)
	if err == nil {
		t.Fatal("expected error for sell_pct on missing position")
	}
}

// ---------- backtest helpers ----------

func TestResolveTargetWeights(t *testing.T) {
	b := &Basket{Mode: BasketModeWeight, Items: []BasketItem{
		{Symbol: "A", Weight: 0.7}, {Symbol: "B", Weight: 0.3},
	}}
	w, err := resolveTargetWeights(b)
	if err != nil {
		t.Fatal(err)
	}
	if w["A"] != 0.7 || w["B"] != 0.3 {
		t.Errorf("weights = %+v", w)
	}

	// Quantity mode → equal weights
	b2 := &Basket{Mode: BasketModeQuantity, Items: []BasketItem{
		{Symbol: "A", Quantity: 1}, {Symbol: "B", Quantity: 2}, {Symbol: "C", Quantity: 3},
	}}
	w2, _ := resolveTargetWeights(b2)
	for _, s := range []string{"A", "B", "C"} {
		if w2[s] < 0.333 || w2[s] > 0.334 {
			t.Errorf("%s weight = %f", s, w2[s])
		}
	}
}

func TestNextPeriodStart(t *testing.T) {
	jan := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if got := nextPeriodStart(jan, ContributionMonthly); got.Month() != time.February || got.Day() != 1 {
		t.Errorf("monthly next = %v", got)
	}
	if got := nextPeriodStart(jan, ContributionQuarterly); got.Month() != time.April || got.Day() != 1 {
		t.Errorf("quarterly next = %v", got)
	}
	dec := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)
	if got := nextPeriodStart(dec, ContributionQuarterly); got.Year() != 2025 || got.Month() != time.January {
		t.Errorf("quarterly across year = %v", got)
	}
	if got := nextPeriodStart(jan, ContributionAnnually); got.Year() != 2025 {
		t.Errorf("annual next = %v", got)
	}
}

func TestBasketWeights(t *testing.T) {
	items := []BasketItem{
		{Symbol: "A", Quantity: 10},
		{Symbol: "B", Quantity: 20},
	}
	prices := map[string]float64{"A": 100, "B": 100}
	w, err := BasketWeights(items, prices)
	if err != nil {
		t.Fatal(err)
	}
	if w["A"] < 0.333 || w["A"] > 0.334 {
		t.Errorf("A weight = %f", w["A"])
	}
	if w["B"] < 0.666 || w["B"] > 0.667 {
		t.Errorf("B weight = %f", w["B"])
	}

	// Missing price → error.
	if _, err := BasketWeights(items, map[string]float64{"A": 100}); err == nil {
		t.Error("expected error for missing price")
	}
}
