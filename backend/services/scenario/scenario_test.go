package scenario

import (
	"encoding/json"
	"errors"
	"strings"
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

func (m *mockMarketProvider) GetLatestPrice(symbol string) (float64, error) {
	if symbol == m.errSym {
		return 0, errors.New("synthetic error")
	}
	p, ok := m.latest[symbol]
	if !ok {
		return 0, errors.New("no price")
	}
	return p, nil
}

func (m *mockMarketProvider) GetHistory(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	pts, ok := m.hist[symbol]
	if !ok {
		return nil, errors.New("no history")
	}
	var res []models.PricePoint
	for _, p := range pts {
		if !p.Date.Before(from) && !p.Date.After(to) {
			res = append(res, p)
		}
	}
	if len(res) == 0 {
		return pts, nil
	}
	return res, nil
}

func (m *mockMarketProvider) TradingDates(from, to time.Time) ([]time.Time, error) {
	// collect all unique dates from hist
	seen := make(map[time.Time]bool)
	for _, pts := range m.hist {
		for _, p := range pts {
			if !p.Date.Before(from) && !p.Date.After(to) {
				seen[p.Date] = true
			}
		}
	}
	var dates []time.Time
	for d := range seen {
		dates = append(dates, d)
	}
	// simple sort is not available in time, so we just return the collected dates.
	// Actually we should sort them.
	// For testing, we can just return a synthetic list based on from/to.
	var res []time.Time
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		res = append(res, d)
	}
	return res, nil
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

func TestRenderSummary(t *testing.T) {
	asOf := NewDateOnly(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	adjDate := NewDateOnly(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))
	spec := ScenarioSpec{
		Base:     BaseModeReal,
		BaseAsOf: &asOf,
		Adjustments: []Adjustment{
			{Symbol: "AAPL", Action: ActionSellQty, Value: 10, Date: &adjDate},
			{Symbol: "MSFT", Action: ActionSellPct, Value: 50, Date: &adjDate},
			{Symbol: "GOOG", Action: ActionSellAll},
			{Symbol: "AMZN", Action: ActionBuy, Value: 1000, Currency: "USD"},
		},
		Basket: &Basket{
			Mode: BasketModeWeight,
			Items: []BasketItem{
				{Symbol: "AAPL", Weight: 0.6},
			},
		},
		Backtest: &BacktestConfig{
			StartDate: NewDateOnly(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)),
		},
	}

	summary := RenderSummary(spec)
	expectedSubstrings := []string{
		"base=real as of 2024-01-01",
		"sell 10 shares of AAPL on 2024-02-01",
		"sell 50% of MSFT on 2024-02-01",
		"sell all GOOG",
		"buy 1000 USD of AMZN",
		"basket (AAPL at 60.0%)",
		"backtest (start 2024-03-01)",
	}
	for _, sub := range expectedSubstrings {
		if !strings.Contains(summary, sub) {
			t.Errorf("expected summary to contain %q, got %q", sub, summary)
		}
	}

	// Test basket quantity mode
	specQty := ScenarioSpec{
		Base: BaseModeEmpty,
		Basket: &Basket{
			Mode: BasketModeQuantity,
			Items: []BasketItem{
				{Symbol: "MSFT", Quantity: 5},
			},
		},
	}
	summaryQty := RenderSummary(specQty)
	if !strings.Contains(summaryQty, "basket (MSFT 5 shares)") {
		t.Errorf("expected summary to contain 'basket (MSFT 5 shares)', got %q", summaryQty)
	}
}

func TestPriceAt_Historical(t *testing.T) {
	dt := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	t.Run("history returns adjclose", func(t *testing.T) {
		mp := &mockMarketProvider{
			hist: map[string][]models.PricePoint{
				"AAPL": {
					{Date: time.Date(2024, 1, 9, 0, 0, 0, 0, time.UTC), AdjClose: 150},
				},
			},
		}
		p, err := priceAt(mp, "AAPL", dt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != 150 {
			t.Errorf("expected 150, got %f", p)
		}
	})

	t.Run("history returns close", func(t *testing.T) {
		mp := &mockMarketProvider{
			hist: map[string][]models.PricePoint{
				"AAPL": {
					{Date: time.Date(2024, 1, 9, 0, 0, 0, 0, time.UTC), Close: 145},
				},
			},
		}
		p, err := priceAt(mp, "AAPL", dt)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != 145 {
			t.Errorf("expected 145, got %f", p)
		}
	})

	t.Run("history empty returns error", func(t *testing.T) {
		mp := &mockMarketProvider{
			hist: map[string][]models.PricePoint{
				"AAPL": {},
			},
		}
		_, err := priceAt(mp, "AAPL", dt)
		if err == nil {
			t.Fatal("expected error for empty history")
		}
	})

	t.Run("provider error returns error", func(t *testing.T) {
		mp := &mockMarketProvider{
			errSym: "AAPL",
		}
		_, err := priceAt(mp, "AAPL", dt)
		if err == nil {
			t.Fatal("expected error from provider")
		}
	})
}

func TestResolveLots_ExchangeFilter(t *testing.T) {
	lots := map[string][]holdingLot{
		"AAPL": {
			{exchange: "NASDAQ", qty: 10, currency: "USD"},
			{exchange: "XETRA", qty: 5, currency: "EUR"},
		},
	}

	t.Run("empty exchange returns all", func(t *testing.T) {
		res := resolveLots(lots, "AAPL", "")
		if len(res) != 2 {
			t.Errorf("expected 2 lots, got %d", len(res))
		}
	})

	t.Run("matching exchange returns one", func(t *testing.T) {
		res := resolveLots(lots, "AAPL", "NASDAQ")
		if len(res) != 1 || res[0].exchange != "NASDAQ" {
			t.Errorf("expected NASDAQ lot, got %v", res)
		}
	})

	t.Run("non-matching exchange returns nil", func(t *testing.T) {
		res := resolveLots(lots, "AAPL", "NYSE")
		if len(res) != 0 {
			t.Errorf("expected nil/empty, got %v", res)
		}
	})
}

func TestApplyAdjustments_Complex(t *testing.T) {
	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{Symbol: "AAPL", ListingExchange: "NASDAQ", Currency: "USD", Quantity: 10, BuySell: "BUY"},
			{Symbol: "AAPL", ListingExchange: "XETRA", Currency: "EUR", Quantity: 5, BuySell: "BUY"},
		},
	}
	mp := &mockMarketProvider{
		latest: map[string]float64{"AAPL": 200},
	}

	// 1. SellPct AAPL on NASDAQ
	err := applyAdjustments(data, []Adjustment{
		{Symbol: "AAPL", Exchange: "NASDAQ", Action: ActionSellPct, Value: 50}, // sells 5 shares
	}, mp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have generated 1 synthetic trade and 1 withdrawal
	var sellNasdaqTrade *models.Trade
	for i := range data.Trades {
		trade := &data.Trades[i]
		if trade.ListingExchange == "NASDAQ" && trade.BuySell == "SELL" {
			sellNasdaqTrade = trade
		}
	}
	if sellNasdaqTrade == nil {
		t.Fatal("expected synthetic sell trade on NASDAQ")
	}
	if sellNasdaqTrade.Quantity != -5 {
		t.Errorf("expected sell quantity to be -5, got %f", sellNasdaqTrade.Quantity)
	}
	if len(data.CashTransactions) != 1 || data.CashTransactions[0].Amount != -1000 {
		t.Errorf("expected synthetic withdrawal of -1000, got %v", data.CashTransactions)
	}

	// Clear trades & transactions for next sub-tests
	data.Trades = []models.Trade{
		{Symbol: "AAPL", ListingExchange: "NASDAQ", Currency: "USD", Quantity: 10, BuySell: "BUY"},
		{Symbol: "AAPL", ListingExchange: "XETRA", Currency: "EUR", Quantity: 5, BuySell: "BUY"},
	}
	data.CashTransactions = nil

	// 2. SellQty AAPL on XETRA
	err = applyAdjustments(data, []Adjustment{
		{Symbol: "AAPL", Exchange: "XETRA", Action: ActionSellQty, Value: 3}, // sells 3 shares
	}, mp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sellXetraTrade *models.Trade
	for i := range data.Trades {
		trade := &data.Trades[i]
		if trade.ListingExchange == "XETRA" && trade.BuySell == "SELL" {
			sellXetraTrade = trade
		}
	}
	if sellXetraTrade == nil {
		t.Fatal("expected synthetic sell trade on XETRA")
	}
	if sellXetraTrade.Quantity != -3 {
		t.Errorf("expected sell quantity to be -3, got %f", sellXetraTrade.Quantity)
	}

	// 3. Error cases
	// SellPct missing position
	err = applyAdjustments(data, []Adjustment{
		{Symbol: "MSFT", Action: ActionSellPct, Value: 50},
	}, mp, nil)
	if err == nil {
		t.Fatal("expected error for SellPct on missing position")
	}

	// SellQty exceeding position
	err = applyAdjustments(data, []Adjustment{
		{Symbol: "AAPL", Exchange: "XETRA", Action: ActionSellQty, Value: 10},
	}, mp, nil)
	if err == nil {
		t.Fatal("expected error for SellQty exceeding position")
	}
}
