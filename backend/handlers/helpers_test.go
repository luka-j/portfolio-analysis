package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"portfolio-analysis/models"
)

// mockCurrencyGetter implements market.CurrencyGetter
type mockCurrencyGetter struct {
	ccy string
}

func (m *mockCurrencyGetter) GetCurrency(symbol string) (string, error) {
	return m.ccy, nil
}

func TestBuildBenchmarkPriceMap(t *testing.T) {
	from := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	// mock prices with a missing day in the middle (e.g. weekend)
	mockProvider := &mockHandlerMarketProvider{
		prices: map[string][]models.PricePoint{
			"SPY": {
				{Date: from.AddDate(0, 0, -2), Close: 98, AdjClose: 98}, // pre-fetch
				{Date: from, Close: 100, AdjClose: 100}, // Jan 10
				{Date: from.AddDate(0, 0, 1), Close: 101, AdjClose: 101}, // Jan 11
				// Jan 12, 13 missing
				{Date: from.AddDate(0, 0, 4), Close: 104, AdjClose: 104}, // Jan 14
				{Date: from.AddDate(0, 0, 5), Close: 105, AdjClose: 105}, // Jan 15
			},
		},
	}
	cg := &mockCurrencyGetter{ccy: "USD"}

	res, err := buildBenchmarkPriceMap(mockProvider, cg, "SPY", from, to, "USD", models.AccountingModelHistorical, false)
	require.NoError(t, err)

	assert.Equal(t, 100.0, res["2024-01-10"])
	assert.Equal(t, 101.0, res["2024-01-11"])
	assert.Equal(t, 101.0, res["2024-01-12"], "should forward fill missing day")
	assert.Equal(t, 101.0, res["2024-01-13"], "should forward fill missing day")
	assert.Equal(t, 104.0, res["2024-01-14"])
	assert.Equal(t, 105.0, res["2024-01-15"])
}

func TestAlignBenchmarkReturns(t *testing.T) {
	priceMap := map[string]float64{
		"2024-01-10": 100.0,
		"2024-01-11": 101.0,
		"2024-01-12": 101.0, // missing data locally, but in map
		"2024-01-14": 104.0,
	}

	startDates := []string{"2024-01-10", "2024-01-11", "2024-01-13"}
	endDates := []string{"2024-01-11", "2024-01-12", "2024-01-14"}

	rets, dates := alignBenchmarkReturns(priceMap, startDates, endDates)
	// First interval: 101/100 - 1 = 0.01
	// Second interval: 101/101 - 1 = 0.0
	// Third interval: 2024-01-13 missing in priceMap, so skipped!

	require.Len(t, rets, 2)
	require.Len(t, dates, 2)
	assert.InDelta(t, 0.01, rets[0], 1e-4)
	assert.Equal(t, "2024-01-11", dates[0])
	assert.InDelta(t, 0.0, rets[1], 1e-4)
	assert.Equal(t, "2024-01-12", dates[1])
}
