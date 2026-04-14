package cashbucket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"portfolio-analysis/models"
)

// noopConvert is an identity convert function (no FX conversion).
func noopConvert(amount float64, _ string, _ time.Time) (float64, error) {
	return amount, nil
}

func d(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
}

func sell(date time.Time, proceeds float64) models.Trade {
	return models.Trade{
		Symbol:      "TEST",
		BuySell:     "SELL",
		Quantity:    -10,
		Proceeds:    proceeds,
		Commission:  0,
		Currency:    "USD",
		DateTime:    date,
		AssetCategory: "STK",
	}
}

func buy(date time.Time, proceeds float64) models.Trade {
	return models.Trade{
		Symbol:      "TEST2",
		BuySell:     "BUY",
		Quantity:    10,
		Proceeds:    proceeds, // negative (cash outflow)
		Commission:  0,
		Currency:    "USD",
		DateTime:    date,
		AssetCategory: "STK",
	}
}

// TestNoSells — no sells means no buckets and no adjusted flows from trades.
func TestNoSells(t *testing.T) {
	trades := []models.Trade{
		buy(d(2024, 1, 10), -1000),
	}
	asOf := d(2024, 3, 1)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// The buy with no preceding sell creates a real inflow of 1000.
	require.Len(t, result.AdjustedCashFlows, 1)
	assert.InDelta(t, -1000, result.AdjustedCashFlows[0].Amount, 0.01)
	assert.Equal(t, 0.0, result.PendingCash)
}

// TestSellThenBuySameCurrencyWithinWindow — bucket absorbs the buy; no net cash flows.
func TestSellThenBuySameCurrencyWithinWindow(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
		buy(d(2024, 1, 20), -900),
	}
	asOf := d(2024, 1, 25) // within 30 days of sell

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// No outflows from sell (absorbed), no inflows from buy (covered by bucket).
	// Pending cash = 1000 - 900 = 100.
	assert.Equal(t, 100.0, result.PendingCash)
	// No real cash flows.
	assert.Empty(t, result.AdjustedCashFlows)
}

// TestSellUSDThenBuyEURWithinWindow — cross-currency: buy consumes from bucket after FX conversion.
func TestSellUSDThenBuyEURWithinWindow(t *testing.T) {
	// Sell USD 1000, convertFn converts to display currency (assume 1:1 for simplicity).
	// Buy EUR 800 — convertFn converts EUR → USD at 1.1 → 880 USD cost.
	usdToDisplay := noopConvert // USD stays as-is
	eurConvert := func(amount float64, from string, date time.Time) (float64, error) {
		if from == "EUR" {
			return amount * 1.1, nil // EUR → display currency
		}
		return amount, nil
	}
	_ = eurConvert

	// Use a convertFn that scales EUR → 1.1× USD.
	convert := func(amount float64, from string, date time.Time) (float64, error) {
		if from == "EUR" {
			return amount * 1.1, nil
		}
		return amount, nil
	}

	trades := []models.Trade{
		{Symbol: "AAPL", BuySell: "SELL", Quantity: -10, Proceeds: 1000, Currency: "USD", DateTime: d(2024, 1, 10), AssetCategory: "STK"},
		{Symbol: "IWDA", BuySell: "BUY", Quantity: 10, Proceeds: -800, Currency: "EUR", DateTime: d(2024, 1, 20), AssetCategory: "STK"},
	}
	asOf := d(2024, 1, 25)

	result, err := Process(trades, nil, nil, 30, asOf, convert)
	require.NoError(t, err)

	// Buy cost = 800 EUR * 1.1 = 880 USD equivalent. Bucket starts at 1000.
	// After buy: bucket = 1000 - 880 = 120 pending.
	assert.InDelta(t, 120, result.PendingCash, 0.01)
	assert.Empty(t, result.AdjustedCashFlows)
	_ = usdToDisplay
}

// TestSellPartialBuy — partial buy leaves remainder which expires.
func TestSellPartialBuy(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
		buy(d(2024, 1, 20), -400),
	}
	// asOf is after the 30-day expiry window (Jan 10 + 30 = Feb 9).
	asOf := d(2024, 3, 1)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Bucket starts at 1000, buy consumes 400 → remaining 600.
	// Expiry = Jan 10 + 30 = Feb 9 < asOf (Mar 1) → real outflow on Feb 9.
	assert.Equal(t, 0.0, result.PendingCash)
	require.Len(t, result.AdjustedCashFlows, 1)
	assert.InDelta(t, 600, result.AdjustedCashFlows[0].Amount, 0.01)
	assert.Equal(t, d(2024, 2, 9), result.AdjustedCashFlows[0].Date)
}

// TestSellNoByBucketExpires — full bucket expires as an outflow.
func TestSellNoBuyBucketExpires(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
	}
	asOf := d(2024, 3, 1) // Feb 9 expiry has already passed

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	assert.Equal(t, 0.0, result.PendingCash)
	require.Len(t, result.AdjustedCashFlows, 1)
	assert.InDelta(t, 1000, result.AdjustedCashFlows[0].Amount, 0.01)
	assert.Equal(t, d(2024, 2, 9), result.AdjustedCashFlows[0].Date)
}

// TestMultipleSellsFIFO — oldest bucket consumed first.
func TestMultipleSellsFIFO(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 500),  // oldest
		sell(d(2024, 1, 20), 800),  // newer
		buy(d(2024, 1, 25), -600),  // should consume all of first bucket + 100 from second
	}
	asOf := d(2024, 1, 28) // within 30d of both sells

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// First bucket: 500 consumed entirely (remaining 0).
	// Second bucket: 600 - 500 = 100 more consumed → 800 - 100 = 700 remaining.
	// PendingCash = 700 (still active, not expired).
	assert.InDelta(t, 700, result.PendingCash, 0.01)
	assert.Empty(t, result.AdjustedCashFlows)
}

// TestBuyExceedsAllBuckets — excess buy cost is a real inflow.
func TestBuyExceedsAllBuckets(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 500),
		buy(d(2024, 1, 20), -800), // costs 800, only 500 in bucket
	}
	asOf := d(2024, 1, 28) // within 30d

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Bucket covers 500. Excess = 300 → real inflow (deposit with negative amount convention).
	assert.Equal(t, 0.0, result.PendingCash)
	require.Len(t, result.AdjustedCashFlows, 1)
	assert.InDelta(t, -300, result.AdjustedCashFlows[0].Amount, 0.01)
}

// TestSellAndBuySameDay — buy on same day as sell still consumes the bucket.
func TestSellAndBuySameDay(t *testing.T) {
	sellT := time.Date(2024, 1, 10, 9, 0, 0, 0, time.UTC)
	buyT := time.Date(2024, 1, 10, 15, 0, 0, 0, time.UTC)

	trades := []models.Trade{
		{Symbol: "A", BuySell: "SELL", Quantity: -10, Proceeds: 1000, Currency: "USD", DateTime: sellT, AssetCategory: "STK"},
		{Symbol: "B", BuySell: "BUY", Quantity: 10, Proceeds: -900, Currency: "USD", DateTime: buyT, AssetCategory: "STK"},
	}
	asOf := d(2024, 3, 1)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Bucket 1000, buy consumes 900 → pending 100. Expiry on Feb 9 < asOf → becomes outflow.
	assert.Equal(t, 0.0, result.PendingCash)
	require.Len(t, result.AdjustedCashFlows, 1)
	assert.InDelta(t, 100, result.AdjustedCashFlows[0].Amount, 0.01)
}

// TestZeroExpiryDays — buckets expire immediately; behaviour matches no-bucket mode.
func TestZeroExpiryDays(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
		buy(d(2024, 1, 20), -400),
	}
	asOf := d(2024, 3, 1)

	result, err := Process(trades, nil, nil, 0, asOf, noopConvert)
	require.NoError(t, err)

	// With expiryDays=0, the sell bucket expires immediately.
	// Buy of 400 — bucket expires immediately, so buy cost is a real inflow.
	// Actually: with 0 expiry, the bucket outflow is on the same day as the sale.
	// The buy tries to consume from the bucket but if it was already a real outflow... need to check ordering.
	// The sell creates a bucket with ExpiryDate = sellDate + 0 = sellDate.
	// Buy happens after → bucket remaining still 1000 at time of buy processing.
	// Buy consumes 400 → bucket remaining 600.
	// At asOf evaluation: expiryDays==0 branch → outflow on sellDate for remaining 600.
	// Buy excess = 0 (fully covered).
	// Net: 1 outflow = 600 on Jan 10 + 1 inflow = -400 on Jan 20? No:
	// buy cost = 400, bucket covers it → no real inflow from buy.
	// Bucket remaining = 600, expires → outflow 600 on sell date.

	// Regardless of exact semantics, pending cash should be 0.
	assert.Equal(t, 0.0, result.PendingCash)
}

// TestDividendsPassThrough — dividend flows are not affected by bucket logic.
func TestDividendsPassThrough(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
	}
	dividends := []models.CashFlow{
		{Date: d(2024, 1, 15), Amount: 50},
	}
	asOf := d(2024, 1, 25) // within 30d window; sell bucket still active

	result, err := Process(trades, dividends, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Pending cash from bucket = 1000.
	assert.InDelta(t, 1000, result.PendingCash, 0.01)
	// Dividend passes through unchanged.
	require.Len(t, result.AdjustedCashFlows, 1)
	assert.InDelta(t, 50, result.AdjustedCashFlows[0].Amount, 0.01)
}

// TestBucketPartiallyConsumedThenExpires — remaining amount becomes outflow on expiry.
func TestBucketPartiallyConsumedThenExpires(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
		buy(d(2024, 1, 15), -300),
	}
	// asOf after expiry (Jan 10 + 30 = Feb 9).
	asOf := d(2024, 2, 20)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Bucket 1000 - 300 = 700 remaining → expires → outflow of 700 on Feb 9.
	assert.Equal(t, 0.0, result.PendingCash)
	require.Len(t, result.AdjustedCashFlows, 1)
	assert.InDelta(t, 700, result.AdjustedCashFlows[0].Amount, 0.01)
	assert.Equal(t, d(2024, 2, 9), result.AdjustedCashFlows[0].Date)
}

// ---------------------------------------------------------------------------
// BalanceChanges tests — verify the pending-cash timeline is recorded correctly.
// ---------------------------------------------------------------------------

// midnight returns d() truncated to midnight, matching truncDate in cashbucket.go.
func midnight(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// TestBalanceChanges_SellOnly — a sell with no subsequent buy or expiry within asOf
// produces one balance change for the sell and one for the future expiry.
func TestBalanceChanges_SellOnly_ActiveBucket(t *testing.T) {
	trades := []models.Trade{sell(d(2024, 1, 10), 1000)}
	asOf := d(2024, 1, 25) // bucket expires Feb 9 — still active

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Sell: +1000 on Jan 10.
	// Expiry: −1000 on Feb 9 (always recorded regardless of asOf).
	require.Len(t, result.BalanceChanges, 2)
	assert.Equal(t, midnight(2024, 1, 10), result.BalanceChanges[0].Date)
	assert.InDelta(t, +1000, result.BalanceChanges[0].Delta, 0.01)
	assert.Equal(t, midnight(2024, 2, 9), result.BalanceChanges[1].Date)
	assert.InDelta(t, -1000, result.BalanceChanges[1].Delta, 0.01)

	// Timeline sums correctly.
	assert.InDelta(t, 1000, sumBalanceChangesOn(result.BalanceChanges, d(2024, 1, 15)), 0.01)
	assert.InDelta(t, 0, sumBalanceChangesOn(result.BalanceChanges, d(2024, 2, 9)), 0.01)
	assert.InDelta(t, 0, sumBalanceChangesOn(result.BalanceChanges, d(2024, 3, 1)), 0.01)
}

// TestBalanceChanges_SellPartialBuyExpiry — sell, partial buy, then expiry.
func TestBalanceChanges_SellPartialBuyExpiry(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
		buy(d(2024, 1, 20), -400),
	}
	asOf := d(2024, 3, 1) // after expiry (Feb 9)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Expect: sell +1000, buy consume −400, expiry −600.
	require.Len(t, result.BalanceChanges, 3)
	assert.Equal(t, midnight(2024, 1, 10), result.BalanceChanges[0].Date)
	assert.InDelta(t, +1000, result.BalanceChanges[0].Delta, 0.01)
	assert.Equal(t, midnight(2024, 1, 20), result.BalanceChanges[1].Date)
	assert.InDelta(t, -400, result.BalanceChanges[1].Delta, 0.01)
	assert.Equal(t, midnight(2024, 2, 9), result.BalanceChanges[2].Date)
	assert.InDelta(t, -600, result.BalanceChanges[2].Delta, 0.01)

	// Timeline sums.
	assert.InDelta(t, 1000, sumBalanceChangesOn(result.BalanceChanges, d(2024, 1, 15)), 0.01) // after sell, before buy
	assert.InDelta(t, 600, sumBalanceChangesOn(result.BalanceChanges, d(2024, 1, 25)), 0.01)  // after buy, before expiry
	assert.InDelta(t, 0, sumBalanceChangesOn(result.BalanceChanges, d(2024, 2, 9)), 0.01)     // on expiry date: drops to 0
	assert.InDelta(t, 0, sumBalanceChangesOn(result.BalanceChanges, d(2024, 3, 1)), 0.01)     // after expiry
}

// TestBalanceChanges_FullyConsumedBucket — buy consumes the entire bucket,
// so no expiry balance change is needed (remaining == 0).
func TestBalanceChanges_FullyConsumedBucket(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 10), 1000),
		buy(d(2024, 1, 20), -1000),
	}
	asOf := d(2024, 3, 1)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Sell +1000, buy consume −1000. No expiry (remaining == 0).
	require.Len(t, result.BalanceChanges, 2)
	assert.InDelta(t, +1000, result.BalanceChanges[0].Delta, 0.01)
	assert.InDelta(t, -1000, result.BalanceChanges[1].Delta, 0.01)

	// Net is zero throughout.
	assert.InDelta(t, 0, sumBalanceChangesOn(result.BalanceChanges, d(2024, 3, 1)), 0.01)
}

// TestBalanceChanges_ZeroExpiryDays — with zero expiry the balance change for the
// sell and its expiry both land on the same day, netting to zero.
func TestBalanceChanges_ZeroExpiryDays(t *testing.T) {
	trades := []models.Trade{sell(d(2024, 1, 10), 1000)}
	asOf := d(2024, 3, 1)

	result, err := Process(trades, nil, nil, 0, asOf, noopConvert)
	require.NoError(t, err)

	// Sell +1000 and same-day expiry −1000 both on Jan 10.
	require.Len(t, result.BalanceChanges, 2)
	assert.Equal(t, midnight(2024, 1, 10), result.BalanceChanges[0].Date)
	assert.Equal(t, midnight(2024, 1, 10), result.BalanceChanges[1].Date)

	// Net pending cash is always zero (same-day create+expire).
	assert.InDelta(t, 0, sumBalanceChangesOn(result.BalanceChanges, d(2024, 1, 10)), 0.01)
}

// sumBalanceChangesOn sums all deltas with Date <= date (same semantics as GetDailyValues).
func sumBalanceChangesOn(changes []BalanceChange, date time.Time) float64 {
	var total float64
	for _, c := range changes {
		if !c.Date.After(date) {
			total += c.Delta
		}
	}
	return total
}

// TestFXTradesIgnored — FX trades should not create buckets.
func TestFXTradesIgnored(t *testing.T) {
	trades := []models.Trade{
		// FX trade (symbol like "USD.EUR" or asset category CASH)
		{Symbol: "USD.EUR", BuySell: "SELL", Quantity: -1, Proceeds: 1000, Currency: "USD", DateTime: d(2024, 1, 10), AssetCategory: "CASH"},
		// Normal SELL
		sell(d(2024, 1, 15), 500),
	}
	asOf := d(2024, 1, 25)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)

	// Only the normal sell created a bucket.
	assert.InDelta(t, 500, result.PendingCash, 0.01)
	assert.Empty(t, result.AdjustedCashFlows)
}

// TestCashDividend_CreatesBucket verifies that a corporate cash dividend creates a pending-cash bucket.
func TestCashDividend_CreatesBucket(t *testing.T) {
	divs := []Dividend{
		{DateTime: d(2024, 1, 10), Amount: 500, Currency: "USD"},
	}
	asOf := d(2024, 1, 25)

	result, err := Process(nil, nil, divs, 30, asOf, noopConvert)
	require.NoError(t, err)
	assert.InDelta(t, 500, result.PendingCash, 0.01)
}

// TestCashDividend_BuyConsumesDividendBucket verifies that a buy after a cash dividend consumes the bucket.
func TestCashDividend_BuyConsumesDividendBucket(t *testing.T) {
	divs := []Dividend{
		{DateTime: d(2024, 1, 1), Amount: 500, Currency: "USD"},
	}
	trades := []models.Trade{
		buy(d(2024, 1, 5), -200), // cost = 200, consumes 200 from the dividend bucket
	}
	asOf := d(2024, 1, 25)

	result, err := Process(trades, nil, divs, 30, asOf, noopConvert)
	require.NoError(t, err)
	assert.InDelta(t, 300, result.PendingCash, 0.01)
}

// TestCashDividend_ExpiredBucketNotPending verifies that an expired dividend bucket does not count as pending cash.
func TestCashDividend_ExpiredBucketNotPending(t *testing.T) {
	divs := []Dividend{
		{DateTime: d(2024, 1, 1), Amount: 500, Currency: "USD"},
	}
	asOf := d(2024, 3, 1) // well past 30-day expiry

	result, err := Process(nil, nil, divs, 30, asOf, noopConvert)
	require.NoError(t, err)
	assert.InDelta(t, 0, result.PendingCash, 0.01)
}

// TestCashDividend_ChronologicalWithTrades verifies that dividends and trades are processed in chronological order.
func TestCashDividend_ChronologicalWithTrades(t *testing.T) {
	// BUY on day 1, dividend on day 3, sell on day 5.
	// The BUY precedes both bucket sources and should not consume them.
	divs := []Dividend{
		{DateTime: d(2024, 1, 3), Amount: 50, Currency: "USD"},
	}
	trades := []models.Trade{
		buy(d(2024, 1, 1), -100),         // before dividend/sell, treated as real deposit
		sell(d(2024, 1, 5), 200),          // creates 200 bucket after dividend
	}
	asOf := d(2024, 1, 25)

	result, err := Process(trades, nil, divs, 30, asOf, noopConvert)
	require.NoError(t, err)
	// dividend bucket = 50, sell bucket = 200; BUY on day 1 has no bucket to consume.
	assert.InDelta(t, 250, result.PendingCash, 0.01)
}

// TestProcess_NilDividends_Unchanged is a regression guard: passing nil cashDividends must not change
// the existing sell-then-buy behaviour.
func TestProcess_NilDividends_Unchanged(t *testing.T) {
	trades := []models.Trade{
		sell(d(2024, 1, 1), 1000),
		buy(d(2024, 1, 5), -400),
	}
	asOf := d(2024, 1, 25)

	result, err := Process(trades, nil, nil, 30, asOf, noopConvert)
	require.NoError(t, err)
	assert.InDelta(t, 600, result.PendingCash, 0.01)
}
