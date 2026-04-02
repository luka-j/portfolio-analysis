package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gofolio-analysis/config"
	"gofolio-analysis/models"
	"gofolio-analysis/router"
	breakdownsvc "gofolio-analysis/services/breakdown"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/fundamentals"
	"gofolio-analysis/services/fx"
	"gofolio-analysis/services/portfolio"
	"gofolio-analysis/services/tax"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// ---------- Mock market data provider ----------

type mockMarketProvider struct {
	prices     map[string][]models.PricePoint
	currencies map[string]string // symbol → native currency; used to implement CurrencyGetter
}

func (m *mockMarketProvider) GetCurrency(symbol string) (string, error) {
	return m.currencies[symbol], nil
}

func newMockMarketProvider() *mockMarketProvider {
	m := &mockMarketProvider{
		prices:     make(map[string][]models.PricePoint),
		currencies: make(map[string]string),
	}

	// AAPL: $195
	m.addPrice("AAPL", 195.0)
	// MSFT: $420
	m.addPrice("MSFT", 420.0)
	// VWCE.DE: €110
	m.addPrice("VWCE.DE", 110.0)
	// VUAA: $90 (same symbol, different exchanges)
	m.addPrice("VUAA", 90.0)
	// SPY benchmark: $500
	m.addPrice("SPY", 500.0)

	// FX rates: USDEUR=X → 0.92, USDCZK=X → 23.50, EURUSD=X → 1.087, EURCZK=X → 25.50
	m.addPrice("USDEUR=X", 0.92)
	m.addPrice("USDCZK=X", 23.50)
	m.addPrice("EURUSD=X", 1.087)
	m.addPrice("EURCZK=X", 25.50)
	m.addPrice("USDUSD=X", 1.0)
	m.addPrice("EUREUR=X", 1.0)

	return m
}

func (m *mockMarketProvider) addPrice(symbol string, price float64) {
	// Generate prices for a year range so any lookback works.
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var points []models.PricePoint
	for d := start; d.Before(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
		points = append(points, models.PricePoint{
			Date:     d,
			Open:     price,
			High:     price * 1.01,
			Low:      price * 0.99,
			Close:    price,
			AdjClose: price,
			Volume:   1000000,
		})
	}
	m.prices[symbol] = points
}

func (m *mockMarketProvider) GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error) {
	points, ok := m.prices[symbol]
	if !ok {
		return nil, fmt.Errorf("no mock data for symbol %s", symbol)
	}

	var result []models.PricePoint
	for _, p := range points {
		if !p.Date.Before(from) && !p.Date.After(to) {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockMarketProvider) GetCurrentPrice(symbol string, cachedOnly bool) (float64, error) {
	points, ok := m.prices[symbol]
	if !ok || len(points) == 0 {
		return 0, fmt.Errorf("no mock data for symbol %s", symbol)
	}
	return points[len(points)-1].Close, nil
}

// ---------- Test helpers ----------

func setupTestServer(t *testing.T) (*httptest.Server, *gorm.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gofolio-test-*")
	require.NoError(t, err)

	cfg := &config.Config{
		Port:                  "0",
		DataDir:               tmpDir,
		AllowedTokenHashes:    nil, // open mode
		FundamentalsProviders: "Yahoo",
		BreakdownProviders:    "Yahoo",
	}

	// Setup in-memory sqlite for tests
	dbName := fmt.Sprintf("file:%s?mode=memory&cache=shared", filepath.Base(tmpDir))
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)
	db.AutoMigrate(
		&models.User{}, &models.Transaction{}, &models.MarketData{},
		&models.AssetFundamental{}, &models.EtfBreakdown{}, &models.LLMCache{},
	)

	mockMarket := newMockMarketProvider()
	fxSvc := fx.NewService(mockMarket, nil) // CNB is nil for this test
	repo := flexquery.NewRepository(db)
	portfolioSvc := portfolio.NewService(mockMarket, fxSvc, nil)
	taxSvc := tax.NewService(fxSvc)

	// Build fundamentals service with no providers — no external calls in tests.
	allFundamentals := map[string]fundamentals.FundamentalsProvider{}
	allBreakdowns := map[string]fundamentals.ETFBreakdownProvider{}
	fundamentalsSvc := fundamentals.BuildFromConfig(db, cfg.FundamentalsProviders, cfg.BreakdownProviders, allFundamentals, allBreakdowns, nil)
	breakdownService := breakdownsvc.NewService(db)

	router := router.SetupRouter(cfg, repo, db, mockMarket, mockMarket, fxSvc, portfolioSvc, taxSvc, fundamentalsSvc, breakdownService, nil)
	ts := httptest.NewServer(router)

	cleanup := func() {
		ts.Close()
		os.RemoveAll(tmpDir)
	}

	return ts, db, cleanup
}

func uploadFlexQuery(t *testing.T, ts *httptest.Server, token string) {
	t.Helper()

	xmlPath := filepath.Join("testdata", "sample_flexquery.xml")
	xmlData, err := os.ReadFile(xmlPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "flexquery.xml")
	require.NoError(t, err)
	_, err = part.Write(xmlData)
	require.NoError(t, err)
	writer.Close()

	req, err := http.NewRequest("POST", ts.URL+"/api/v1/portfolio/upload", &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Auth-Token", token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, readBody(t, resp))
}

func doGet(t *testing.T, ts *httptest.Server, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", ts.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("X-Auth-Token", token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

// ---------- Tests ----------

const testToken = "my-secret-test-token"

func TestUploadFlexQuery(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	// Verify response by uploading again and checking.
	xmlPath := filepath.Join("testdata", "sample_flexquery.xml")
	xmlData, err := os.ReadFile(xmlPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "flexquery.xml")
	require.NoError(t, err)
	_, err = part.Write(xmlData)
	require.NoError(t, err)
	writer.Close()

	req, err := http.NewRequest("POST", ts.URL+"/api/v1/portfolio/upload", &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Auth-Token", testToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.UploadResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, 3, result.PositionsCount, "should have 3 open positions")
	assert.Equal(t, 7, result.TradesCount, "should have 7 trades (5 original + 2 VUAA)")
	assert.Equal(t, 4, result.CashTransactionsCount, "should have 4 cash transactions")
}

func TestAuthRequired(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	req, err := http.NewRequest("GET", ts.URL+"/api/v1/portfolio/value", nil)
	require.NoError(t, err)
	// No auth token.
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetPortfolioValue(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/value?currencies=USD,EUR,CZK", testToken)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.PortfolioValueResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Expected USD value includes AAPL, MSFT, VWCE.DE (from OpenPositions) + VUAA (from trades)
	// AAPL: 10 * 195 = 1950 USD
	// MSFT: 8 * 420 = 3360 USD
	// VWCE.DE: 20 * 110 EUR = 2200 EUR → 2200 * 1.087 = 2391.40 USD
	// VUAA@LSE: 50 shares * 90 (mock price) USD = 4500 USD
	// VUAA@XMIL: 30 shares * 90 (mock price assumes same yahoo symbol) EUR → 30 * 90 * 1.087 = 2934.90 USD
	// Total USD ≈ 15136.30 (approximate, depends on mock price lookup for VUAA)
	// We just verify the positions count and that values are reasonable
	// Check we have 5 positions (AAPL, MSFT, VWCE.DE from OpenPositions + VUAA@LSE, VUAA@XMIL from trades).
	assert.Len(t, result.Positions, 5, "should have 5 positions (3 from OpenPositions + 2 VUAA listings)")
}

func TestGetPortfolioHistory(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/history?from=2024-06-01&to=2024-06-05&currency=USD&accounting_model=historical", testToken)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.PortfolioHistoryResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "USD", result.Currency)
	assert.Equal(t, "historical", result.AccountingModel)
	assert.Greater(t, len(result.Data), 0, "should have daily values")

	// Each day should have a positive value.
	for _, dv := range result.Data {
		assert.Greater(t, dv.Value, 0.0, "daily value on %s should be positive", dv.Date)
	}
}

func TestGetMarketHistory(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp := doGet(t, ts, "/api/v1/market/history?symbol=AAPL&from=2024-06-01&to=2024-06-05", testToken)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "AAPL", result["symbol"])
	data, ok := result["data"].([]interface{})
	require.True(t, ok, "data should be an array")
	assert.Greater(t, len(data), 0, "should have price points")
}

func TestGetPortfolioStats(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/stats?currency=USD&accounting_model=historical&from=2024-01-01&to=2026-12-31", testToken)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.StatsResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "USD", result.Currency)
	assert.Equal(t, "historical", result.AccountingModel)

	// TWR should be present.
	_, hasTWR := result.Statistics["twr"]
	assert.True(t, hasTWR, "should have TWR statistic")

	// MWR should be present.
	_, hasMWR := result.Statistics["mwr"]
	assert.True(t, hasMWR, "should have MWR statistic")
}

func TestComparePortfolio(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/compare?symbols=SPY&currency=USD&from=2024-06-01&to=2024-12-31&risk_free_rate=0.05&accounting_model=historical", testToken)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "USD", result.Currency)
	assert.Equal(t, "historical", result.AccountingModel)
	assert.Len(t, result.Benchmarks, 1, "should have 1 benchmark")
	assert.Equal(t, "SPY", result.Benchmarks[0].Symbol)

	// With constant mock prices, returns are 0, so beta and alpha should be near 0.
	// But correlation is undefined (stdev=0), so we just check the fields exist.
	bm := result.Benchmarks[0]
	t.Logf("Benchmark metrics: alpha=%.4f beta=%.4f sharpe=%.4f treynor=%.4f te=%.4f ir=%.4f corr=%.4f",
		// SharpeRatio is not in BenchmarkResult
		bm.Alpha, bm.Beta, 0.0, bm.TreynorRatio, bm.TrackingError, bm.InformationRatio, bm.Correlation)
}

func TestCompare_FXConversionChangesMetrics(t *testing.T) {
	// Build a market mock where a EUR-denominated benchmark has the same USD value every day
	// (EUR price = 100/EURUSD), so after FX conversion to USD it shows 0% returns.
	// A second series (EUR_BENCH_RAW) has identical EUR prices but no declared currency,
	// so it is treated as USD — it shows the declining EUR prices as if they were USD returns.
	// The two benchmarks must produce different tracking errors.

	m := &mockMarketProvider{
		prices:     make(map[string][]models.PricePoint),
		currencies: make(map[string]string),
	}
	// Standard portfolio holdings and FX rates (copied from newMockMarketProvider).
	m.addPrice("AAPL", 195.0)
	m.addPrice("MSFT", 420.0)
	m.addPrice("VWCE.DE", 110.0)
	m.addPrice("VUAA", 90.0)
	m.addPrice("USDEUR=X", 0.92)
	m.addPrice("USDCZK=X", 23.50)
	m.addPrice("EURCZK=X", 25.50)
	m.addPrice("USDUSD=X", 1.0)
	m.addPrice("EUREUR=X", 1.0)

	// Build a varying EURUSD rate (starts at 1.0, +0.001/day) and matching EUR benchmark prices.
	// EUR_BENCH prices in EUR = 100/EURUSD → after FX conversion to USD: (100/EURUSD)*EURUSD = 100 (flat).
	// EUR_BENCH_RAW has the same EUR prices but no currency declared → treated as USD (declining values).
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	var eurusdPts, eurBenchPts []models.PricePoint
	eurusd := 1.0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		eurusdPts = append(eurusdPts, models.PricePoint{Date: d, Close: eurusd, AdjClose: eurusd})
		p := 100.0 / eurusd
		eurBenchPts = append(eurBenchPts, models.PricePoint{Date: d, Close: p, AdjClose: p})
		eurusd += 0.001
	}
	m.prices["EURUSD=X"] = eurusdPts
	m.prices["EUR_BENCH"] = eurBenchPts     // FX conversion declared → ~0% USD returns
	m.prices["EUR_BENCH_RAW"] = eurBenchPts // No currency declared → declining "USD" returns
	m.currencies["EUR_BENCH"] = "EUR"

	// Spin up an isolated test server using this mock as both Provider and CurrencyGetter.
	tmpDir, err := os.MkdirTemp("", "gofolio-fxtest-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Port: "0", DataDir: tmpDir,
		FundamentalsProviders: "Yahoo", BreakdownProviders: "Yahoo",
	}
	dbName := fmt.Sprintf("file:%s?mode=memory&cache=shared", filepath.Base(tmpDir))
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)
	db.AutoMigrate(&models.User{}, &models.Transaction{}, &models.MarketData{}, &models.AssetFundamental{}, &models.EtfBreakdown{}, &models.LLMCache{})

	fxSvc := fx.NewService(m, nil)
	repo := flexquery.NewRepository(db)
	portfolioSvc := portfolio.NewService(m, fxSvc, nil)
	taxSvc := tax.NewService(fxSvc)
	allF := map[string]fundamentals.FundamentalsProvider{}
	allB := map[string]fundamentals.ETFBreakdownProvider{}
	fundSvc := fundamentals.BuildFromConfig(db, cfg.FundamentalsProviders, cfg.BreakdownProviders, allF, allB, nil)
	bkdSvc := breakdownsvc.NewService(db)

	router := router.SetupRouter(cfg, repo, db, m, m, fxSvc, portfolioSvc, taxSvc, fundSvc, bkdSvc, nil)
	ts := httptest.NewServer(router)
	defer ts.Close()

	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts,
		"/api/v1/portfolio/compare?symbols=EUR_BENCH,EUR_BENCH_RAW&currency=USD&from=2024-06-01&to=2024-12-31&accounting_model=historical",
		testToken)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.CompareResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.Benchmarks, 2)

	var eurBench, eurBenchRaw *models.BenchmarkResult
	for i := range result.Benchmarks {
		b := &result.Benchmarks[i]
		switch b.Symbol {
		case "EUR_BENCH":
			eurBench = b
		case "EUR_BENCH_RAW":
			eurBenchRaw = b
		}
	}
	require.NotNil(t, eurBench, "EUR_BENCH must be in response")
	require.NotNil(t, eurBenchRaw, "EUR_BENCH_RAW must be in response")

	// After FX conversion EUR_BENCH is flat in USD (0% returns), so its tracking error and
	// alpha differ from EUR_BENCH_RAW which shows the raw declining EUR prices as USD.
	assert.NotEqual(t, eurBench.TrackingError, eurBenchRaw.TrackingError,
		"FX conversion must change tracking error: EUR_BENCH=%.6f EUR_BENCH_RAW=%.6f",
		eurBench.TrackingError, eurBenchRaw.TrackingError)
	assert.NotEqual(t, eurBench.Alpha, eurBenchRaw.Alpha,
		"FX conversion must change alpha: EUR_BENCH=%.6f EUR_BENCH_RAW=%.6f",
		eurBench.Alpha, eurBenchRaw.Alpha)
	t.Logf("EUR_BENCH (FX-converted):  TE=%.6f alpha=%.6f", eurBench.TrackingError, eurBench.Alpha)
	t.Logf("EUR_BENCH_RAW (no FX):     TE=%.6f alpha=%.6f", eurBenchRaw.TrackingError, eurBenchRaw.Alpha)
}

func TestMultipleUsers(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Upload with user 1.
	uploadFlexQuery(t, ts, "user1-token")

	// User 2 should NOT see user 1's portfolio, but DO get a success due to auto-creation
	resp := doGet(t, ts, "/api/v1/portfolio/value", "user2-token")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "user2 should be auto-created and succeed")
	
	var result models.PortfolioValueResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Empty(t, result.Positions, "user2 should have no positions")
}

func TestAccountingModels(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	// Test all three accounting models return valid responses.
	for _, model := range []string{"historical", "spot", "original"} {
		t.Run(model, func(t *testing.T) {
			url := fmt.Sprintf("/api/v1/portfolio/history?from=2024-06-01&to=2024-06-05&currency=USD&accounting_model=%s", model)
			resp := doGet(t, ts, url, testToken)
			defer resp.Body.Close()

			if model == "original" {
				// The test portfolio is multi-currency, which is correctly rejected
				// by the new check we added. Let's assert it returns an error rather than 200.
				assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
			} else {
				assert.Equal(t, http.StatusOK, resp.StatusCode)

				var result models.PortfolioHistoryResponse
				err := json.NewDecoder(resp.Body).Decode(&result)
				require.NoError(t, err)
				assert.Equal(t, model, result.AccountingModel)
			}
		})
	}
}

func TestGetPortfolioTrades(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	t.Run("AAPL trades in CZK", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?symbol=AAPL&currency=CZK", testToken)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

			var result map[string]interface{}
			err := json.NewDecoder(resp.Body).Decode(&result)
			require.NoError(t, err)

			assert.Equal(t, "AAPL", result["symbol"])
			assert.Equal(t, "USD", result["currency"])
			assert.Equal(t, "CZK", result["display_currency"])

			// Sample data has 3 AAPL trades: Buy 10@185, Buy 5@175, Sell 5@195
			trades, _ := result["trades"].([]interface{})
			require.Len(t, trades, 3, "AAPL should have 3 trades")

			first := trades[0].(map[string]interface{})
			assert.Equal(t, "SELL", first["side"])
			assert.Equal(t, 5.0, first["quantity"])
			assert.Equal(t, 195.0, first["price"])
			assert.Equal(t, "USD", first["native_currency"])
			assert.Equal(t, "2024-06-10", first["date"])

			// ConvertedPrice should be price * USDCZK rate (23.50)
			assert.InDelta(t, 195.0*23.50, first["converted_price"].(float64), 0.01,
				"converted price should be native price * USDCZK rate")

			last := trades[2].(map[string]interface{})
			assert.Equal(t, "BUY", last["side"])
			assert.Equal(t, 10.0, last["quantity"])
			assert.Equal(t, 185.0, last["price"])
	})

	t.Run("AAPL trades in native currency", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?symbol=AAPL&currency=USD", testToken)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// When display currency = native currency, converted_price == price
		trades, _ := result["trades"].([]interface{})
		for _, tr := range trades {
			trMap := tr.(map[string]interface{})
			assert.Equal(t, trMap["price"].(float64), trMap["converted_price"].(float64),
				"when display=native, converted_price should equal price")
		}
	})

	t.Run("VWCE.DE trades EUR to CZK", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?symbol=VWCE.DE&currency=CZK", testToken)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, "VWCE.DE", result["symbol"])
		assert.Equal(t, "EUR", result["currency"])
		assert.Equal(t, "CZK", result["display_currency"])
		trades, _ := result["trades"].([]interface{})
		require.Len(t, trades, 1, "VWCE.DE should have 1 trade")

		tr := trades[0].(map[string]interface{})
		assert.Equal(t, 100.0, tr["price"].(float64))
		// Price = 100 EUR; converted at EURCZK=25.50 → 2550 CZK
		assert.InDelta(t, 100.0*25.50, tr["converted_price"].(float64), 0.01,
			"EUR→CZK conversion via EURCZK rate")
	})

	t.Run("empty symbol returns 400", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?currency=CZK", testToken)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown symbol returns empty trades", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/trades?symbol=GOOG&currency=CZK", testToken)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		trades, _ := result["trades"].([]interface{})
		assert.Empty(t, trades, "unknown symbol should return no trades")
	})
}

func TestCostBasisInPortfolioValue(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	resp := doGet(t, ts, "/api/v1/portfolio/value?currencies=USD", testToken)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.PortfolioValueResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	require.Len(t, result.Positions, 5, "should have 5 positions (3 from OpenPositions + 2 VUAA listings)")

	// Build lookup by symbol.
	bySymbol := make(map[string]models.PositionValue)
	for _, p := range result.Positions {
		bySymbol[p.Symbol] = p
	}

	// The sample XML has the following AAPL trades:
	//   Buy 10 @ 185 (2024-01-15)
	//   Buy  5 @ 175 (2024-03-20)
	//   Sell 5 @ 195 (2024-06-10)
	// FIFO matching: sell 5 consumes 5 shares from the first lot (buy 10@185).
	// Remaining open lots: 5@185, 5@175  →  FIFO cost = (5*185 + 5*175) / 10 = 180.00
	// This matches IB's own costBasisPrice="180.00" in the XML.
	t.Run("AAPL computed cost basis from trades", func(t *testing.T) {
		aapl, ok := bySymbol["AAPL"]
		require.True(t, ok, "AAPL should be in positions")
		// FIFO: remaining lots are [5@185, 5@175] → avg 180.00
		expected := (5.0*185.0 + 5.0*175.0) / 10.0 // 180.00
		assert.InDelta(t, expected, aapl.CostBases["USD"], 0.01,
			"AAPL cost basis should be FIFO-weighted avg of remaining open lots")
		assert.Equal(t, 195.0, aapl.Prices["USD"])
		assert.Equal(t, 10.0, aapl.Quantity)
	})

	t.Run("MSFT computed cost basis from trades", func(t *testing.T) {
		msft, ok := bySymbol["MSFT"]
		require.True(t, ok, "MSFT should be in positions")
		assert.InDelta(t, 400.0, msft.CostBases["USD"], 0.01,
			"MSFT cost basis should be computed from buy trades")
	})

	t.Run("VWCE.DE computed cost basis from trades (EUR)", func(t *testing.T) {
		vwce, ok := bySymbol["VWCE.DE"]
		require.True(t, ok, "VWCE.DE should be in positions")
		// VWCE.DE cost in XML=100 EUR. 
		// Currencies=USD requested, so cost is converted: 100 EUR * 1.087 EUR/USD = 108.7 USD
		expected := 108.7
		assert.InDelta(t, expected, vwce.CostBases["USD"], 0.01,
			"VWCE.DE cost basis should be 108.7 USD (100 EUR * 1.087)")
		assert.Equal(t, "EUR", vwce.NativeCurrency)
	})
}

func TestCostBasisFromTradesWhenNoOpenPosition(t *testing.T) {
	// This test creates a custom FlexQuery without OpenPositions
	// to verify the fallback VWAP-from-buys calculation.
	xmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<FlexQueryResponse queryName="TestQuery" type="AF">
  <FlexStatements count="1">
    <FlexStatement accountId="U9999999" fromDate="2024-01-02" toDate="2024-12-31" period="LastYear" whenGenerated="2025-01-01;00:00:00">
      <Trades>
        <Trade symbol="TSLA" assetCategory="STK" currency="USD" dateTime="2024-01-10;10:00:00" tradeDate="2024-01-10" quantity="10" tradePrice="200.00" proceeds="-2000.00" ibCommission="-1.00" buySell="BUY" />
        <Trade symbol="TSLA" assetCategory="STK" currency="USD" dateTime="2024-02-15;10:00:00" tradeDate="2024-02-15" quantity="5" tradePrice="250.00" proceeds="-1250.00" ibCommission="-1.00" buySell="BUY" />
        <Trade symbol="TSLA" assetCategory="STK" currency="USD" dateTime="2024-03-20;10:00:00" tradeDate="2024-03-20" quantity="-3" tradePrice="280.00" proceeds="840.00" ibCommission="-1.00" buySell="SELL" />
      </Trades>
      <OpenPositions>
      </OpenPositions>
      <CashTransactions>
        <CashTransaction type="Deposits/Withdrawals" currency="USD" amount="-5000.00" dateTime="2024-01-05;08:00:00" reportDate="2024-01-05" description="Deposit" symbol="" />
      </CashTransactions>
    </FlexStatement>
  </FlexStatements>
</FlexQueryResponse>`)

	tmpDir, err := os.MkdirTemp("", "gofolio-costbasis-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Port:                  "0",
		DataDir:               tmpDir,
		AllowedTokenHashes:    nil,
		FundamentalsProviders: "Yahoo",
		BreakdownProviders:    "Yahoo",
	}

	// Setup in-memory sqlite for tests
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.AutoMigrate(
		&models.User{}, &models.Transaction{}, &models.MarketData{},
		&models.AssetFundamental{}, &models.EtfBreakdown{}, &models.LLMCache{},
	)

	mockMarket := newMockMarketProvider()
	// Add TSLA at $300
	mockMarket.addPrice("TSLA", 300.0)

	fxSvc := fx.NewService(mockMarket, nil)
	repo := flexquery.NewRepository(db)
	portfolioSvc := portfolio.NewService(mockMarket, fxSvc, nil)
	taxSvc := tax.NewService(fxSvc)

	allFundamentals2 := map[string]fundamentals.FundamentalsProvider{}
	allBreakdowns2 := map[string]fundamentals.ETFBreakdownProvider{}
	fundamentalsSvc2 := fundamentals.BuildFromConfig(db, cfg.FundamentalsProviders, cfg.BreakdownProviders, allFundamentals2, allBreakdowns2, nil)
	breakdownService2 := breakdownsvc.NewService(db)

	router := router.SetupRouter(cfg, repo, db, mockMarket, nil, fxSvc, portfolioSvc, taxSvc, fundamentalsSvc2, breakdownService2, nil)
	tsServer := httptest.NewServer(router)
	defer tsServer.Close()

	token := "costbasis-test-token"

	// Upload custom XML.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "flexquery.xml")
	part.Write(xmlData)
	writer.Close()

	req, _ := http.NewRequest("POST", tsServer.URL+"/api/v1/portfolio/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Auth-Token", token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Get portfolio value — no OpenPositions means cost basis computed from trades.
	resp = doGet(t, tsServer, "/api/v1/portfolio/value?currencies=USD", token)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.PortfolioValueResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	require.Len(t, result.Positions, 1)
	tsla := result.Positions[0]

	assert.Equal(t, "TSLA", tsla.Symbol)

	// Holdings after trades: buy 10 + buy 5 - sell 3 = 12 shares
	assert.Equal(t, 12.0, tsla.Quantity)

	// FIFO cost basis: sell 3 consumes 3 lots from the first buy (10@200).
	// Remaining open lots: 7@200, 5@250 → cost = (7*200 + 5*250) / 12 = (1400+1250)/12 = 220.833...
	expectedCostBasis := (7.0*200.0 + 5.0*250.0) / 12.0
	assert.InDelta(t, expectedCostBasis, tsla.CostBases["USD"], 0.01,
		"cost basis should be FIFO-weighted avg of remaining open lots: %.2f", expectedCostBasis)

	t.Logf("TSLA: qty=%.0f price=%.2f cost_basis=%.2f unrealized_gl=%.2f",
		tsla.Quantity, tsla.Prices["USD"], tsla.CostBases["USD"], (tsla.Prices["USD"]-tsla.CostBases["USD"])*tsla.Quantity)
}
func TestMapSymbol(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	// Attempt mapping AAPL -> AAPL_Mapped
	payload := `{"yahoo_symbol":"AAPL_Mapped"}`
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/portfolio/symbols/AAPL/mapping?exchange=NASDAQ", strings.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("X-Auth-Token", testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Fetch portfolio value and verify Apple has been mapped
	respVal := doGet(t, ts, "/api/v1/portfolio/value?currencies=USD", testToken)
	defer respVal.Body.Close()

	assert.Equal(t, http.StatusOK, respVal.StatusCode)

	var pvr models.PortfolioValueResponse
	err = json.NewDecoder(respVal.Body).Decode(&pvr)
	require.NoError(t, err)

	for _, pos := range pvr.Positions {
		if pos.Symbol == "AAPL" {
			assert.Equal(t, "AAPL_Mapped", pos.YahooSymbol, "YahooSymbol should be AAPL_Mapped for AAPL trades")
		}
	}
}

func TestPortfolioValueAccountingModels(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	currencies := "USD,CZK"
	
	t.Run("spot model", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/value?currencies="+currencies+"&accounting_model=spot", testToken)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var res models.PortfolioValueResponse
		json.NewDecoder(resp.Body).Decode(&res)

		// AAPL (USD native)
		// Spot: Prices["CZK"] = Price(195 USD) * FX(23.50 CZK/USD) = 4582.5
		// FIFO CostBasis: remaining lots [5@185, 5@175] → avg 180.00 USD. Spot: 180.00 * 23.50 = 4230.00
		for _, p := range res.Positions {
			if p.Symbol == "AAPL" {
				assert.InDelta(t, 4582.5, p.Prices["CZK"], 0.1)
				assert.InDelta(t, 4230.0, p.CostBases["CZK"], 0.1)
			}
		}
	})

	t.Run("historical model", func(t *testing.T) {
		resp := doGet(t, ts, "/api/v1/portfolio/value?currencies="+currencies+"&accounting_model=historical", testToken)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var res models.PortfolioValueResponse
		json.NewDecoder(resp.Body).Decode(&res)

		// VWCE.DE (EUR native)
		// Historical: CostBasis = Weighted average of cost converted at date of trade.
		// Mock data for VWCE.DE: Buy 20 @ 100 EUR on 2024-01-20.
		// Mock EURUSD rate: 1.087 (fixed in mock provider regardless of date for simplicity)
		// 100 EUR * 1.087 = 108.7 USD.
		for _, p := range res.Positions {
			if p.Symbol == "VWCE.DE" {
				assert.InDelta(t, 108.7, p.CostBases["USD"], 0.1)
			}
		}
	})
}

func TestGetTaxReport(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	uploadFlexQuery(t, ts, testToken)

	// Test with a JSON body
	reqBody := `{"year": 2024}`
	req, err := http.NewRequest("POST", ts.URL+"/api/v1/tax/report", strings.NewReader(reqBody))
	require.NoError(t, err)
	req.Header.Set("X-Auth-Token", testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, float64(2024), result["year"])

	inv := result["investment_income"].(map[string]interface{})
	assert.Greater(t, inv["total_cost_czk"].(float64), 0.0)
	assert.Greater(t, inv["total_benefit_czk"].(float64), 0.0)

	txs := inv["transactions"].([]interface{})
	require.NotEmpty(t, txs)
}

func TestGetPortfolioBreakdown(t *testing.T) {
	ts, db, cleanup := setupTestServer(t)
	defer cleanup()

	// Upload sample portfolio.
	uploadFlexQuery(t, ts, testToken)

	// Seed AssetFundamental rows so the breakdown has something to work with.
	now := time.Now().UTC()
	db.Create(&models.AssetFundamental{
		Symbol: "AAPL", Name: "Apple Inc.", AssetType: "Stock",
		Country: "US", Sector: "Technology",
		DataSource: "test", LastUpdated: now,
	})
	db.Create(&models.AssetFundamental{
		Symbol: "VWCE.DE", Name: "Vanguard FTSE All-World ETF", AssetType: "ETF",
		DataSource: "test", LastUpdated: now,
	})
	// Seed aggregate breakdown for VWCE.DE.
	db.Create(&models.EtfBreakdown{
		FundSymbol: "VWCE.DE", Dimension: "sector", Label: "Technology",
		Weight: 0.25, DataSource: "test", LastUpdated: now,
	})
	db.Create(&models.EtfBreakdown{
		FundSymbol: "VWCE.DE", Dimension: "sector", Label: "Healthcare",
		Weight: 0.15, DataSource: "test", LastUpdated: now,
	})
	db.Create(&models.EtfBreakdown{
		FundSymbol: "VWCE.DE", Dimension: "country", Label: "United States",
		Weight: 0.60, DataSource: "test", LastUpdated: now,
	})

	resp := doGet(t, ts, "/api/v1/portfolio/breakdown?currency=USD", testToken)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result models.BreakdownResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "USD", result.Currency)
	// Expect 4 sections: By Asset Type, By Asset, By Country, By Sector.
	assert.Len(t, result.Sections, 4)

	// Each section's entries should sum to ~100% of the total.
	for _, section := range result.Sections {
		var totalPct float64
		for _, e := range section.Entries {
			totalPct += e.Percentage
		}
		// Allow ±5% rounding tolerance.
		assert.InDelta(t, 100.0, totalPct, 5.0, "section %q percentages should sum to ~100", section.Title)
	}
}
