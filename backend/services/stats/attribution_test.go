package stats

import (
	"math"
	"testing"
)

func TestAttribution_EmptyTotals(t *testing.T) {
	got := CalculateAttribution(map[string][]float64{"AAPL": {100, 101}}, nil, nil)
	if got != nil {
		t.Fatalf("expected nil for empty totals, got %+v", got)
	}
}

func TestAttribution_MismatchedLengthsSkipped(t *testing.T) {
	totals := []float64{100, 101, 102}
	vals := map[string][]float64{
		"GOOD": {50, 51, 52},
		"BAD":  {50, 51}, // wrong length
	}
	res := CalculateAttribution(vals, nil, totals)
	if len(res) != 1 || res[0].Symbol != "GOOD" {
		t.Fatalf("expected only GOOD result, got %+v", res)
	}
}

func TestAttribution_SingleStockFullPeriod_NoCashflows(t *testing.T) {
	// One stock, always 100% of portfolio, gaining 10% over 2 days.
	vals := map[string][]float64{"ONLY": {100, 105, 110}}
	totals := []float64{100, 105, 110}
	res := CalculateAttribution(vals, nil, totals)
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	r := res[0]
	// AvgWeight over all 3 days = (1+1+1)/3 = 1.
	if math.Abs(r.AvgWeight-1.0) > 1e-9 {
		t.Fatalf("AvgWeight: want 1, got %.9f", r.AvgWeight)
	}
	// Chained return: (105/100)(110/105)-1 = 0.10
	if math.Abs(r.Return-0.10) > 1e-9 {
		t.Fatalf("Return: want 0.10, got %.9f", r.Return)
	}
	// Contribution = w1*r1 + w2*r2 where w_i = prev/totals[i-1] = 1 for both days.
	// Sum = 0.05 + (110-105)/105 = 0.05 + 0.047619… = 0.097619
	want := 0.05 + (110.0-105.0)/105.0
	if math.Abs(r.Contribution-want) > 1e-9 {
		t.Fatalf("Contribution: want %.9f, got %.9f", want, r.Contribution)
	}
}

func TestAttribution_CashFlowNeutralisation_BuyDoesNotInflateReturn(t *testing.T) {
	// Day 0: 100. Day 1: price rose 10% → 110, then user bought 90 worth → value=200.
	// Day 2: no trade, flat price → 200.
	// Without cashflow-adjustment the naive return (200/100-1 = 100%) is wrong;
	// true position return is just the 10% price gain on day 1, flat on day 2.
	vals := map[string][]float64{"X": {100, 200, 200}}
	cfs := map[string][]float64{"X": {0, 90, 0}}
	totals := []float64{100, 200, 200}
	res := CalculateAttribution(vals, cfs, totals)
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	// Day 1 adj return: (200-90)/100 - 1 = 0.10. Day 2: 0.
	if math.Abs(res[0].Return-0.10) > 1e-9 {
		t.Fatalf("CF-adjusted Return: want 0.10, got %.9f", res[0].Return)
	}
}

func TestAttribution_SellDoesNotDeflateReturn(t *testing.T) {
	// Day 0: 200. Day 1: price flat but user sold 100 worth → value=100.
	// Naive: (100/200 - 1) = -50% return, which is wrong; true return is 0.
	vals := map[string][]float64{"X": {200, 100, 100}}
	cfs := map[string][]float64{"X": {0, -100, 0}} // sells are negative
	totals := []float64{200, 100, 100}
	res := CalculateAttribution(vals, cfs, totals)
	// Day 1 adj return: (100-(-100))/200 - 1 = 200/200 - 1 = 0.
	if math.Abs(res[0].Return-0.0) > 1e-9 {
		t.Fatalf("CF-adjusted Return for sell: want 0, got %.9f", res[0].Return)
	}
}

func TestAttribution_AvgWeightOverFullPeriod_BrieflyHeldPosition(t *testing.T) {
	// "BRIEF" is held only on day 1 (weight ~0.5 there), zero on days 0/2/3/4.
	// AvgWeight over all 5 days should be 0.5/5 = 0.10, NOT 0.5.
	vals := map[string][]float64{
		"BRIEF": {0, 50, 0, 0, 0},
		"STAY":  {100, 50, 100, 100, 100},
	}
	totals := []float64{100, 100, 100, 100, 100}
	res := CalculateAttribution(vals, nil, totals)
	var brief *AttributionResult
	for i := range res {
		if res[i].Symbol == "BRIEF" {
			brief = &res[i]
		}
	}
	if brief == nil {
		t.Fatal("BRIEF missing from results")
	}
	if math.Abs(brief.AvgWeight-0.10) > 1e-9 {
		t.Fatalf("AvgWeight for briefly-held position: want 0.10, got %.9f", brief.AvgWeight)
	}
}

func TestAttribution_SortedByAbsContribution(t *testing.T) {
	// Two positions with known contributions of opposite signs.
	vals := map[string][]float64{
		"WIN":  {100, 110, 110}, // +10% day 1, flat day 2
		"LOSE": {100, 100, 95},  // flat day 1, -5% day 2
	}
	totals := []float64{200, 210, 205}
	res := CalculateAttribution(vals, nil, totals)
	if len(res) != 2 {
		t.Fatalf("want 2 results, got %d", len(res))
	}
	// Expect WIN first (larger |contribution|).
	aw := math.Abs(res[0].Contribution)
	bw := math.Abs(res[1].Contribution)
	if aw < bw {
		t.Fatalf("results not sorted by |contribution| desc: %+v", res)
	}
}

func TestAttribution_ZeroPrevValueSkipsDay(t *testing.T) {
	// Position opens partway through. On the opening day, prev=0 must not divide by zero.
	vals := map[string][]float64{"OPEN": {0, 0, 100, 110}}
	cfs := map[string][]float64{"OPEN": {0, 0, 100, 0}}
	totals := []float64{100, 100, 200, 210}
	res := CalculateAttribution(vals, cfs, totals)
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	// Only the last day contributes: (110-0)/100 - 1 = 0.10.
	if math.Abs(res[0].Return-0.10) > 1e-9 {
		t.Fatalf("Return: want 0.10, got %.9f", res[0].Return)
	}
	// AvgWeight averaged over 4 days: (0+0+100/200+110/210)/4
	want := (0 + 0 + 0.5 + 110.0/210.0) / 4
	if math.Abs(res[0].AvgWeight-want) > 1e-9 {
		t.Fatalf("AvgWeight: want %.9f, got %.9f", want, res[0].AvgWeight)
	}
}

func TestAttribution_DiverseMultiCurrencyPortfolio(t *testing.T) {
	// Inputs are already in the display currency (handler's responsibility).
	// Verify three positions with realistic patterns sum to within reasonable
	// tolerance of the portfolio TWR (sum of contributions approximates TWR).
	//
	// Days 0..3. All values in display currency (e.g. USD).
	vals := map[string][]float64{
		"US_STOCK": {500, 505, 515, 520}, // +1%, +1.98%, +0.97%
		"EU_STOCK": {300, 297, 300, 306}, // −1%, +1.01%, +2%
		"JP_ETF":   {200, 202, 200, 210}, // +1%, −0.99%, +5%
	}
	totals := []float64{1000, 1004, 1015, 1036}
	res := CalculateAttribution(vals, nil, totals)
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %d", len(res))
	}
	// Sum of contributions vs portfolio daily returns chained sum (approximation).
	var sumContrib float64
	for _, r := range res {
		sumContrib += r.Contribution
	}
	// Portfolio daily returns (no cashflows) = totals[i]/totals[i-1] - 1, summed.
	var portSumSimple float64
	for i := 1; i < len(totals); i++ {
		portSumSimple += totals[i]/totals[i-1] - 1
	}
	// Contribution sum should equal the simple sum of daily returns exactly
	// (because w_i = prev_val/prev_total and each daily return decomposes linearly
	// across positions). Tolerance allows for floating-point drift.
	if math.Abs(sumContrib-portSumSimple) > 1e-9 {
		t.Fatalf("sum(contributions)=%.9f vs sum(daily rets)=%.9f", sumContrib, portSumSimple)
	}
}
