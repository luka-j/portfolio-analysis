package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/portfolio"
)

func TestStatsHandler_GetAnalysisDashboard(t *testing.T) {
	db := setupStatsDB(t)

	user := models.User{TokenHash: "testhash"}
	require.NoError(t, db.Create(&user).Error)

	day1 := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)

	require.NoError(t, db.Create(&models.Transaction{
		UserID:   user.ID,
		Symbol:   "AAPL",
		Type:     "Trade",
		BuySell:  "BUY",
		Quantity: 10,
		Price:    100,
		Proceeds: -1000,
		Currency: "USD",
		DateTime: day1,
	}).Error)

	repo := &flexquery.Repository{DB: db}

	mockMarket := &mockHandlerMarketProvider{
		prices: map[string][]models.PricePoint{
			"AAPL": {
				{Date: day1.AddDate(0, 0, -7), Close: 100, AdjClose: 100},
				{Date: day1, Close: 100, AdjClose: 100},
				{Date: day2, Close: 102, AdjClose: 102},
			},
		},
	}

	fxSvc := fx.NewService(mockMarket, nil)
	ps := portfolio.NewService(mockMarket, fxSvc, 0)
	cg := &mockCurrencyGetter{ccy: "USD"}
	sh := NewStatsHandler(repo, ps, mockMarket, fxSvc, cg)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.UserHashKey, "testhash")
		c.Next()
	})
	r.GET("/portfolio/analysis-dashboard", sh.GetAnalysisDashboard)

	t.Run("ValidRequest", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/portfolio/analysis-dashboard?currency=USD&from=2024-01-10&to=2024-01-11", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp models.AnalysisDashboardResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

		assert.Equal(t, "USD", resp.Currency)
		assert.NotEmpty(t, resp.Stats)
		assert.NotEmpty(t, resp.TWRHistory)
		assert.NotEmpty(t, resp.MWRHistory)
	})

	t.Run("MissingCurrency", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/portfolio/analysis-dashboard?from=2024-01-10&to=2024-01-11", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestStatsHandler_GetAnalysisHoldings(t *testing.T) {
	db := setupStatsDB(t)

	user := models.User{TokenHash: "testhash"}
	require.NoError(t, db.Create(&user).Error)

	day1 := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)

	require.NoError(t, db.Create(&models.Transaction{
		UserID:   user.ID,
		Symbol:   "AAPL",
		Type:     "Trade",
		BuySell:  "BUY",
		Quantity: 10,
		Price:    100,
		Proceeds: -1000,
		Currency: "USD",
		DateTime: day1,
	}).Error)

	repo := &flexquery.Repository{DB: db}

	mockMarket := &mockHandlerMarketProvider{
		prices: map[string][]models.PricePoint{
			"AAPL": {
				{Date: day1.AddDate(0, 0, -7), Close: 100, AdjClose: 100},
				{Date: day1, Close: 100, AdjClose: 100},
				{Date: day2, Close: 102, AdjClose: 102},
			},
		},
	}

	fxSvc := fx.NewService(mockMarket, nil)
	ps := portfolio.NewService(mockMarket, fxSvc, 0)
	cg := &mockCurrencyGetter{ccy: "USD"}
	sh := NewStatsHandler(repo, ps, mockMarket, fxSvc, cg)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.UserHashKey, "testhash")
		c.Next()
	})
	r.GET("/portfolio/analysis-holdings", sh.GetAnalysisHoldings)

	t.Run("ValidRequest", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/portfolio/analysis-holdings?currency=USD&from=2024-01-10&to=2024-01-11", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp models.AnalysisHoldingsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

		assert.Equal(t, "USD", resp.Currency)
		assert.NotNil(t, resp.Attribution)
		assert.NotNil(t, resp.Correlations)
		// AAPL should be in attribution
		assert.NotEmpty(t, resp.Attribution.Positions)
		assert.Equal(t, "AAPL", resp.Attribution.Positions[0].Symbol)
	})

	t.Run("MissingCurrency", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/portfolio/analysis-holdings?from=2024-01-10&to=2024-01-11", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}
