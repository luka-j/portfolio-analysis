package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"gofolio-analysis/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsBoundaryBug(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Upload sample flexquery
	uploadFlexQuery(t, ts, "test-token")

	// Create request
	url := ts.URL + "/api/v1/portfolio/stats?from=2025-12-13&to=2026-03-13&currency=USD&accounting_model=historical"
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Auth-Token", "test-token")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var stats models.StatsResponse
	err = json.NewDecoder(resp.Body).Decode(&stats)
	require.NoError(t, err)

	t.Logf("Stats: %+v", stats.Statistics)

	twrVal, ok := stats.Statistics["twr"].(float64)
	assert.True(t, ok, "twr should be a float64")
	mwrVal, ok := stats.Statistics["mwr"].(float64)
	assert.True(t, ok, "mwr should be a float64")

	t.Logf("TWR: %f, MWR: %f", twrVal, mwrVal)

	// Fetch history directly
	historyUrl := ts.URL + "/api/v1/portfolio/history?from=2025-12-13&to=2026-03-13&currency=USD&accounting_model=historical"
	req2, _ := http.NewRequest("GET", historyUrl, nil)
	req2.Header.Set("X-Auth-Token", "test-token")
	resp2, _ := client.Do(req2)
	var hist models.PortfolioHistoryResponse
	json.NewDecoder(resp2.Body).Decode(&hist)
	resp2.Body.Close()
	if len(hist.Data) > 0 {
		t.Logf("History Data Length: %d", len(hist.Data))
		t.Logf("History[0]: %s - %f", hist.Data[0].Date, hist.Data[0].Value)
		t.Logf("History[1]: %s - %f", hist.Data[1].Date, hist.Data[1].Value)
		t.Logf("History[n-2]: %s - %f", hist.Data[len(hist.Data)-2].Date, hist.Data[len(hist.Data)-2].Value)
		t.Logf("History[n-1]: %s - %f", hist.Data[len(hist.Data)-1].Date, hist.Data[len(hist.Data)-1].Value)
	}

	// The problem was TWR dropping into extremely high figures due to cashflow bug.
	// Since there is no cashflow within 2025-12-13 and 2026-03-13, the return should be relatively small
	// corresponding strictly to market growth (which is perfectly flat at $0 growth in our mock market setup)

	assert.InDelta(t, 0.0, twrVal, 0.001, "TWR should be approx 0% because market mock prices are perfectly flat!")
	// MWR might be close to 0 as well.
}

func TestStatsInitialDepositBug(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Upload sample flexquery
	uploadFlexQuery(t, ts, "test-token")

	// Fetch fixed test date
	to := "2026-03-15"
	from := "2024-01-01" // Include inception entirely

	// Create request spanning full duration
	url := fmt.Sprintf("%s/api/v1/portfolio/stats?from=%s&to=%s&currency=USD&accounting_model=historical", ts.URL, from, to)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Auth-Token", "test-token")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var stats models.StatsResponse
	err = json.NewDecoder(resp.Body).Decode(&stats)
	require.NoError(t, err)

	twrVal, ok := stats.Statistics["twr"].(float64)
	assert.True(t, ok, "twr should be a float64")

	// Ensure TWR does not drop to exactly -1.0 (which was the bug upon initial deposit computation error)
	assert.NotEqual(t, -1.0, float64(int(twrVal*100))/100.0, "TWR should not be -100% exactly.")

	// TWR for the mock data since inception should be strongly POSITIVE because
	// the mock market prices (AAPL=195, MSFT=420, VWCE=110) are permanently higher
	// than the buying prices in the sample flexquery (AAPL=185, MSFT=400, VWCE=100).
	// This generates instant profit correctly recognized by the algorithm.
	assert.Greater(t, twrVal, 0.0, "TWR should be > 0 due to embedded profit")
	assert.Less(t, twrVal, 2.0, "TWR should be reasonable")
}
