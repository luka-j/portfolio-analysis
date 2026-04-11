package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"portfolio-analysis/models"
)

// ---------- computeMA unit tests ----------

func makePoints(closes []float64) []models.PricePoint {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	pts := make([]models.PricePoint, len(closes))
	for i, c := range closes {
		pts[i] = models.PricePoint{
			Date:     base.AddDate(0, 0, i),
			Close:    c,
			AdjClose: c,
		}
	}
	return pts
}

func TestComputeMA_Basic(t *testing.T) {
	// 5 points, ma_days=3: first 2 MA nil, points 3-5 have correct averages.
	closes := []float64{10, 20, 30, 40, 50}
	result := computeMA(makePoints(closes), 3)
	require.Len(t, result, 5)

	assert.Nil(t, result[0].MA, "MA should be nil before window fills")
	assert.Nil(t, result[1].MA, "MA should be nil before window fills")
	require.NotNil(t, result[2].MA)
	assert.InDelta(t, 20.0, *result[2].MA, 1e-9, "MA(3) at index 2")
	require.NotNil(t, result[3].MA)
	assert.InDelta(t, 30.0, *result[3].MA, 1e-9, "MA(3) at index 3")
	require.NotNil(t, result[4].MA)
	assert.InDelta(t, 40.0, *result[4].MA, 1e-9, "MA(3) at index 4")
}

func TestComputeMA_SinglePoint(t *testing.T) {
	result := computeMA(makePoints([]float64{42}), 5)
	require.Len(t, result, 1)
	assert.Equal(t, 42.0, result[0].Close)
	assert.Nil(t, result[0].MA, "window cannot fill with only 1 point")
}

func TestComputeMA_WindowEqualsLength(t *testing.T) {
	closes := []float64{1, 2, 3, 4, 5}
	result := computeMA(makePoints(closes), 5)
	require.Len(t, result, 5)
	for i := 0; i < 4; i++ {
		assert.Nil(t, result[i].MA, "MA should be nil until window fills at last point")
	}
	require.NotNil(t, result[4].MA)
	assert.InDelta(t, 3.0, *result[4].MA, 1e-9, "average of 1..5 is 3")
}

func TestComputeMA_AllSamePrice(t *testing.T) {
	closes := []float64{100, 100, 100, 100, 100}
	result := computeMA(makePoints(closes), 3)
	for i := 2; i < 5; i++ {
		require.NotNil(t, result[i].MA)
		assert.InDelta(t, 100.0, *result[i].MA, 1e-9)
	}
}

func TestComputeMA_Empty(t *testing.T) {
	result := computeMA([]models.PricePoint{}, 10)
	assert.Empty(t, result)
}

func TestComputeMA_DateAndClosePreserved(t *testing.T) {
	closes := []float64{5, 10, 15}
	result := computeMA(makePoints(closes), 2)
	assert.Equal(t, "2024-01-01", result[0].Date)
	assert.Equal(t, 5.0, result[0].Close)
	assert.Nil(t, result[0].MA)
	assert.Equal(t, "2024-01-02", result[1].Date)
	assert.Equal(t, 10.0, result[1].Close)
	require.NotNil(t, result[1].MA)
	assert.InDelta(t, 7.5, *result[1].MA, 1e-9)
}

// ---------- GetSecurityChart handler tests ----------

// mockHandlerMarketProvider is a minimal market.Provider for handler tests.
type mockHandlerMarketProvider struct {
	prices map[string][]models.PricePoint
}

func (m *mockHandlerMarketProvider) GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error) {
	pts, ok := m.prices[symbol]
	if !ok {
		return nil, fmt.Errorf("no mock data for %s", symbol)
	}
	var result []models.PricePoint
	for _, p := range pts {
		if !p.Date.Before(from) && !p.Date.After(to) {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockHandlerMarketProvider) TradingDates(from, to time.Time) ([]time.Time, error) {
	seen := map[time.Time]bool{}
	for _, pts := range m.prices {
		for _, p := range pts {
			if !p.Date.Before(from) && !p.Date.After(to) {
				seen[p.Date] = true
			}
		}
	}
	dates := make([]time.Time, 0, len(seen))
	for d := range seen {
		dates = append(dates, d)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	return dates, nil
}

func (m *mockHandlerMarketProvider) GetLatestPrice(symbol string, cachedOnly bool) (float64, error) {
	pts, ok := m.prices[symbol]
	if !ok || len(pts) == 0 {
		return 0, fmt.Errorf("no mock data for %s", symbol)
	}
	last := pts[len(pts)-1]
	if last.AdjClose != 0 {
		return last.AdjClose, nil
	}
	return last.Close, nil
}

func newTestHandler() (*MarketHandler, *mockHandlerMarketProvider) {
	mock := &mockHandlerMarketProvider{
		prices: make(map[string][]models.PricePoint),
	}
	// DB is nil — resolveYahooSymbol falls back to the broker symbol, which is fine for tests.
	// Populate AAPL with 90 trading days (Mon-Fri) starting 2024-01-02.
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	var pts []models.PricePoint
	price := 150.0
	for d := start; len(pts) < 90; d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		pts = append(pts, models.PricePoint{Date: d, Close: price, AdjClose: price})
		price += 1.0
	}
	mock.prices["AAPL"] = pts

	h := NewMarketHandler(mock, nil, nil)
	return h, mock
}

func setupTestRouter(h *MarketHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/market/security-chart", h.GetSecurityChart)
	return r
}

func TestGetSecurityChart_MissingSymbol(t *testing.T) {
	h, _ := newTestHandler()
	r := setupTestRouter(h)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/market/security-chart?from=2024-01-02", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetSecurityChart_MissingFrom(t *testing.T) {
	h, _ := newTestHandler()
	r := setupTestRouter(h)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/market/security-chart?symbol=AAPL", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetSecurityChart_InvalidMaDays(t *testing.T) {
	h, _ := newTestHandler()
	r := setupTestRouter(h)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/market/security-chart?symbol=AAPL&from=2024-01-02&ma_days=1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetSecurityChart_ValidRequest(t *testing.T) {
	h, _ := newTestHandler()
	r := setupTestRouter(h)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/market/security-chart?symbol=AAPL&from=2024-01-02&to=2024-04-30&ma_days=5", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "AAPL", resp["symbol"])
	assert.Equal(t, float64(5), resp["ma_days"])

	data, ok := resp["data"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data)

	// First 4 points should have null MA (window of 5 not yet filled).
	for i := 0; i < 4 && i < len(data); i++ {
		pt := data[i].(map[string]interface{})
		assert.Nil(t, pt["ma"], "MA should be null before window fills (point %d)", i)
	}
	// 5th point should have non-null MA.
	if len(data) >= 5 {
		pt := data[4].(map[string]interface{})
		assert.NotNil(t, pt["ma"], "MA should be non-null once window fills")
	}
}

func TestGetSecurityChart_UnknownSymbol(t *testing.T) {
	h, _ := newTestHandler()
	r := setupTestRouter(h)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/market/security-chart?symbol=UNKNOWN&from=2024-01-02&to=2024-04-30", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
