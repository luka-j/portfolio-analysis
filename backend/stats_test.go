package main

import (
	"encoding/json"
	"fmt"
	"math"
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

// assertFiniteFloat64 fails the test if v is NaN or Inf.
func assertFiniteFloat64(t *testing.T, v float64, name string) {
	t.Helper()
	assert.False(t, math.IsNaN(v), "%s must not be NaN", name)
	assert.False(t, math.IsInf(v, 0), "%s must not be Inf", name)
}

// ═══════════════════════════════════════════════════════════════
// GetStats – validation / error path tests
// ═══════════════════════════════════════════════════════════════

// TestStats_EmptyPortfolio documents the behaviour when no data has been uploaded.
// LoadSaved returns an empty portfolio (not an error), so GetStats responds 200.
// GetDailyValues returns all-zero rows; TWR handles constant-zero series and MWR
// fails (no cash flows), so the stats map may mix float64 and error-object values.
// The key invariant is that the handler does not crash and returns valid JSON.
func TestStats_EmptyPortfolio(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	// "other-token" user has never uploaded anything.
	resp := doGet(t, ts, "/api/v1/portfolio/stats?from=2024-01-01&to=2025-01-01&currency=USD", "other-token")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.StatsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "USD", result.Currency)
	assert.NotNil(t, result.Statistics)
	t.Logf("empty portfolio stats: %v", result.Statistics)

	// TWR or MWR might be float64 or an error object depending on how the service
	// handles all-zero value series; the important thing is both keys are present.
	assert.Contains(t, result.Statistics, "twr", "twr key must always be present")
	assert.Contains(t, result.Statistics, "mwr", "mwr key must always be present")

	// Neither value should be NaN or Inf if they happen to be floats.
	if twrVal, ok := result.Statistics["twr"].(float64); ok {
		assertFiniteFloat64(t, twrVal, "twr")
	}
	if mwrVal, ok := result.Statistics["mwr"].(float64); ok {
		assertFiniteFloat64(t, mwrVal, "mwr")
	}
}

func TestStats_MissingCurrency(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)
	resp := doGet(t, ts, "/api/v1/portfolio/stats?from=2024-01-01&to=2025-01-01", testToken)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStats_MissingDateRange(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	t.Run("missing from", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/stats?to=2025-01-01&currency=USD", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
	t.Run("missing to", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/stats?from=2024-01-01&currency=USD", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
	t.Run("to before from", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/stats?from=2025-01-01&to=2024-01-01&currency=USD", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestStats_SpotAccountingModel(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/stats?from=2024-01-01&to=2026-03-15&currency=USD&accounting_model=spot", testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.StatsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "spot", result.AccountingModel)

	twrVal, ok := result.Statistics["twr"].(float64)
	assert.True(t, ok, "twr should be float64")
	assertFiniteFloat64(t, twrVal, "twr")

	mwrVal, ok := result.Statistics["mwr"].(float64)
	assert.True(t, ok, "mwr should be float64")
	assertFiniteFloat64(t, mwrVal, "mwr")
}

// TestStats_ToDateBeyondLastPricing verifies that when `to` extends past the last
// available market data the handler clips the MWR end date to the actual last priced
// day instead of using the raw query param. Both TWR and MWR must be finite floats.
func TestStats_ToDateBeyondLastPricing(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	// Mock data ends at 2026-12-30; request a `to` of 2027-02-01 (beyond mock data).
	resp := doGet(t, ts, "/api/v1/portfolio/stats?from=2024-01-01&to=2027-02-01&currency=USD&accounting_model=historical", testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.StatsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	twrVal, ok := result.Statistics["twr"].(float64)
	assert.True(t, ok, "twr should be float64 even when to > last pricing date")
	assertFiniteFloat64(t, twrVal, "twr")

	mwrVal, ok := result.Statistics["mwr"].(float64)
	assert.True(t, ok, "mwr should be float64 even when to > last pricing date")
	assertFiniteFloat64(t, mwrVal, "mwr")

	// The MWR end date is clipped to the last priced day (2026-12-30), so the result
	// must be the same as requesting to=2026-12-30 explicitly.
	resp2 := doGet(t, ts, "/api/v1/portfolio/stats?from=2024-01-01&to=2026-12-30&currency=USD&accounting_model=historical", testToken)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	var result2 models.StatsResponse
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&result2))
	mwrVal2, ok2 := result2.Statistics["mwr"].(float64)
	require.True(t, ok2, "mwr from exact-date request should be float64")
	assert.InDelta(t, mwrVal2, mwrVal, 1e-9, "MWR with to=2027-02-01 must equal MWR with to=2026-12-30 (end date clipped to last priced day)")
}

// ═══════════════════════════════════════════════════════════════
// Compare – validation / error path tests
// ═══════════════════════════════════════════════════════════════

// TestCompare_EmptyPortfolio verifies that when no data has been uploaded Compare
// returns a benchmark result with a non-empty Error field rather than silently
// returning all-zero metrics.
func TestCompare_EmptyPortfolio(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	// "other-token" user has never uploaded anything.
	resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=SPY&from=2024-01-01&to=2025-01-01&currency=USD&risk_free_rate=0", "other-token")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Benchmarks, 1)

	b := result.Benchmarks[0]
	assert.Equal(t, "SPY", b.Symbol)
	assert.NotEmpty(t, b.Error, "empty portfolio should produce an Error field, not silent zero metrics")
}

func TestCompare_MissingParams(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	t.Run("missing symbols", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/compare?from=2024-01-01&to=2025-01-01&currency=USD", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
	t.Run("missing currency", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=SPY&from=2024-01-01&to=2025-01-01", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
	t.Run("missing from", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=SPY&to=2025-01-01&currency=USD", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
	t.Run("missing to", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=SPY&from=2024-01-01&currency=USD", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
	t.Run("to before from", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=SPY&from=2025-01-01&to=2024-01-01&currency=USD", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

// TestCompare_UnknownSymbol verifies that an unrecognised benchmark symbol produces
// a 200 response with a benchmark entry carrying a non-empty Error field, rather than
// a 500 that discards all other results.
func TestCompare_UnknownSymbol(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=DOES_NOT_EXIST&from=2024-01-01&to=2025-01-01&currency=USD", testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Benchmarks, 1)

	b := result.Benchmarks[0]
	assert.Equal(t, "DOES_NOT_EXIST", b.Symbol)
	assert.NotEmpty(t, b.Error, "failed symbol should carry a non-empty Error field")
}

// TestCompare_PartialFailure verifies that one failed symbol does not discard the
// successfully-computed metrics for the other symbols in the same request.
func TestCompare_PartialFailure(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	// SPY is in the mock; DOES_NOT_EXIST is not.
	resp := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=SPY,DOES_NOT_EXIST&from=2024-01-01&to=2026-03-15&currency=USD&risk_free_rate=0",
		testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Benchmarks, 2, "both symbols must appear in the response")

	bySymbol := make(map[string]models.BenchmarkResult)
	for _, b := range result.Benchmarks {
		bySymbol[b.Symbol] = b
	}

	spy := bySymbol["SPY"]
	assert.Empty(t, spy.Error, "SPY should succeed with no error")
	assertFiniteFloat64(t, spy.Alpha, "SPY.alpha")
	assertFiniteFloat64(t, spy.Beta, "SPY.beta")

	bad := bySymbol["DOES_NOT_EXIST"]
	assert.NotEmpty(t, bad.Error, "DOES_NOT_EXIST should carry an error")
}

// ═══════════════════════════════════════════════════════════════
// Compare – happy-path / metric correctness tests
// ═══════════════════════════════════════════════════════════════

func TestCompare_BasicSanity(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=SPY&from=2024-01-01&to=2026-03-15&currency=USD&accounting_model=historical", testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	assert.Equal(t, "USD", result.Currency)
	assert.Equal(t, "historical", result.AccountingModel)
	require.Len(t, result.Benchmarks, 1)

	b := result.Benchmarks[0]
	assert.Equal(t, "SPY", b.Symbol)
	assertFiniteFloat64(t, b.Alpha, "alpha")
	assertFiniteFloat64(t, b.Beta, "beta")
	// SharpeRatio is not in BenchmarkResult
	// assertFiniteFloat64(t, b.SharpeRatio, "sharpe_ratio")}
	assertFiniteFloat64(t, b.TreynorRatio, "treynor_ratio")
	assertFiniteFloat64(t, b.TrackingError, "tracking_error")
	assertFiniteFloat64(t, b.InformationRatio, "information_ratio")
	assertFiniteFloat64(t, b.Correlation, "correlation")
}

// TestCompare_FlatPriceMetrics_ZeroRiskFreeRate verifies metric values when both the
// portfolio and the benchmark have perfectly flat prices (zero daily returns throughout).
// With rf=0: all metrics must be exactly 0.0 — the near-zero stddev guards in
// CalculateBenchmarkMetrics prevent division by floating-point noise.
func TestCompare_FlatPriceMetrics_ZeroRiskFreeRate(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	// Use a sub-period with no cash flows so portfolio returns are flat.
	// rf=0 → alpha = 0 when all returns are 0.
	resp := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=SPY&from=2025-06-01&to=2026-03-13&currency=USD&accounting_model=historical&risk_free_rate=0",
		testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Benchmarks, 1)

	b := result.Benchmarks[0]
	assert.InDelta(t, 0.0, b.Alpha, 1e-9, "alpha must be 0 with flat prices and rf=0")
	assert.Equal(t, 0.0, b.Beta, "beta must be 0 when benchmark stddev is below threshold")
	// SharpeRatio is not in BenchmarkResult
	// assert.Equal(t, 0.0, b.SharpeRatio, "sharpe must be 0 when portfolio stddev is below threshold")
	assert.Equal(t, 0.0, b.TreynorRatio, "treynor must be 0 when beta is below threshold")
	assert.InDelta(t, 0.0, b.TrackingError, 1e-9, "tracking error must be 0 with flat prices")
	assert.Equal(t, 0.0, b.InformationRatio, "IR must be 0 when diff stddev is below threshold")
	assert.Equal(t, 0.0, b.Correlation, "correlation must be 0 when stddevs are below threshold")
}

// TestCompare_DefaultRiskFreeRate verifies that the default risk-free rate (5%) drives
// a measurable alpha even with flat prices — the portfolio earns 0% but the risk-free
// rate is positive, so Jensen's alpha must be negative.
func TestCompare_DefaultRiskFreeRate(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	// Sub-period with no cash flows; default rf=0.05 applies.
	resp := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=SPY&from=2025-06-01&to=2026-03-13&currency=USD&accounting_model=historical",
		testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Benchmarks, 1)

	b := result.Benchmarks[0]
	// With flat prices (0% return) and positive rf, alpha = (1 + 0 - dailyRf)^252 - 1 < 0.
	assert.Less(t, b.Alpha, 0.0, "alpha must be negative when portfolio earns 0% against a positive rf")
	assert.Greater(t, b.Alpha, -1.0, "alpha must be greater than -100%")
	assertFiniteFloat64(t, b.Alpha, "alpha")
}

// TestCompare_MultipleSymbols verifies that multiple comma-separated symbols are all
// returned in the response in the order they were requested.
func TestCompare_MultipleSymbols(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=SPY,MSFT&from=2025-06-01&to=2026-03-13&currency=USD&risk_free_rate=0",
		testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Benchmarks, 2, "two symbols should produce two benchmark results")

	symbols := []string{result.Benchmarks[0].Symbol, result.Benchmarks[1].Symbol}
	assert.Contains(t, symbols, "SPY")
	assert.Contains(t, symbols, "MSFT")

	for _, b := range result.Benchmarks {
		assertFiniteFloat64(t, b.Alpha, b.Symbol+".alpha")
		assertFiniteFloat64(t, b.Beta, b.Symbol+".beta")
		assertFiniteFloat64(t, b.Correlation, b.Symbol+".correlation")
	}
}

// TestCompare_SpotAccountingModel verifies the Compare endpoint accepts spot
// accounting and returns a structurally valid response.
func TestCompare_SpotAccountingModel(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=SPY&from=2024-01-01&to=2026-03-15&currency=USD&accounting_model=spot&risk_free_rate=0",
		testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "spot", result.AccountingModel)
	require.Len(t, result.Benchmarks, 1)
	assert.Equal(t, "SPY", result.Benchmarks[0].Symbol)
	assertFiniteFloat64(t, result.Benchmarks[0].Alpha, "alpha")
}

// TestCompare_EURCurrencyFXConversion verifies that Compare applies FX conversion
// when the display currency differs from the benchmark's native currency.
// SPY is USD-listed; requesting EUR should trigger USD→EUR conversion on the benchmark
// price series. With flat mock FX rates the conversion is a constant multiplier that
// cancels in returns, so metrics should be identical to the USD case.
func TestCompare_EURCurrencyFXConversion(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	respUSD := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=SPY&from=2025-06-01&to=2026-03-13&currency=USD&accounting_model=historical&risk_free_rate=0",
		testToken)
	defer respUSD.Body.Close()
	require.Equal(t, http.StatusOK, respUSD.StatusCode)
	var usdResult models.CompareResponse
	require.NoError(t, json.NewDecoder(respUSD.Body).Decode(&usdResult))

	respEUR := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=SPY&from=2025-06-01&to=2026-03-13&currency=EUR&accounting_model=historical&risk_free_rate=0",
		testToken)
	defer respEUR.Body.Close()
	require.Equal(t, http.StatusOK, respEUR.StatusCode)
	var eurResult models.CompareResponse
	require.NoError(t, json.NewDecoder(respEUR.Body).Decode(&eurResult))

	require.Len(t, usdResult.Benchmarks, 1)
	require.Len(t, eurResult.Benchmarks, 1)

	// With flat mock FX rates, a constant multiplier cancels in daily returns,
	// so all metrics should be identical regardless of display currency.
	usdB := usdResult.Benchmarks[0]
	eurB := eurResult.Benchmarks[0]
	assert.InDelta(t, usdB.Alpha, eurB.Alpha, 1e-9, "alpha should be equal under flat FX rates")
	assert.InDelta(t, usdB.Beta, eurB.Beta, 1e-9, "beta should be equal under flat FX rates")
	assert.InDelta(t, usdB.Correlation, eurB.Correlation, 1e-9, "correlation should be equal under flat FX rates")
}

// TestCompare_CustomRiskFreeRateParam verifies the risk_free_rate query parameter is
// wired through correctly. Providing rf=0 and rf=0.05 should produce different alphas
// with flat prices.
func TestCompare_CustomRiskFreeRateParam(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()
	uploadFlexQuery(t, ts, testToken)

	base := "/api/v1/portfolio/compare?symbols=SPY&from=2025-06-01&to=2026-03-13&currency=USD&accounting_model=historical"

	resp0 := doGet(t, ts, base+"&risk_free_rate=0", testToken)
	defer resp0.Body.Close()
	require.Equal(t, http.StatusOK, resp0.StatusCode)
	var r0 models.CompareResponse
	require.NoError(t, json.NewDecoder(resp0.Body).Decode(&r0))

	resp5 := doGet(t, ts, base+"&risk_free_rate=0.05", testToken)
	defer resp5.Body.Close()
	require.Equal(t, http.StatusOK, resp5.StatusCode)
	var r5 models.CompareResponse
	require.NoError(t, json.NewDecoder(resp5.Body).Decode(&r5))

	require.Len(t, r0.Benchmarks, 1)
	require.Len(t, r5.Benchmarks, 1)

	alpha0 := r0.Benchmarks[0].Alpha
	alpha5 := r5.Benchmarks[0].Alpha

	// With flat prices (0% return): alpha(rf=0)=0, alpha(rf=0.05)<0.
	assert.InDelta(t, 0.0, alpha0, 1e-9, "alpha with rf=0 should be 0")
	assert.Less(t, alpha5, alpha0, "higher rf should produce lower alpha with zero-return portfolio")
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
