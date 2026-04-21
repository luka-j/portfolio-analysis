package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/portfolio"
)

func setupStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("file:stats_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.Transaction{},
		&models.MarketData{},
		&models.AssetFundamental{},
		&models.EtfBreakdown{},
		&models.LLMCache{},
		&models.CurrentPrice{},
		&models.CorporateActionRecord{},
		&models.CashDividendRecord{},
	))
	return db
}

func TestStatsHandler_CumulativeAndSymbols(t *testing.T) {
	db := setupStatsDB(t)

	user := models.User{TokenHash: "testhash"}
	require.NoError(t, db.Create(&user).Error)

	day1 := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	day3 := day1.AddDate(0, 0, 2)
	day4 := day1.AddDate(0, 0, 3)
	day5 := day1.AddDate(0, 0, 4)

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
				{Date: day3, Close: 105, AdjClose: 105},
				{Date: day4, Close: 105, AdjClose: 105},
				{Date: day5, Close: 110, AdjClose: 110},
			},
			"SPY": {
				{Date: day1.AddDate(0, 0, -7), Close: 400, AdjClose: 400},
				{Date: day1, Close: 400, AdjClose: 400},
				{Date: day2, Close: 405, AdjClose: 405},
				{Date: day3, Close: 410, AdjClose: 410},
				{Date: day4, Close: 415, AdjClose: 415},
				{Date: day5, Close: 420, AdjClose: 420},
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
	r.GET("/portfolio/cumulative", sh.GetCumulative)
	r.GET("/portfolio/drawdown", sh.GetDrawdown)

	t.Run("Cumulative", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/portfolio/cumulative?currency=USD&from=2024-01-10&to=2024-01-14&symbols=SPY", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp models.CumulativeResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

		require.Len(t, resp.Results, 2, "should have Portfolio and SPY")
		assert.Equal(t, "Portfolio", resp.Results[0].Symbol)
		assert.Equal(t, "SPY", resp.Results[1].Symbol)
		assert.NotEmpty(t, resp.Results[0].Series)
		assert.NotEmpty(t, resp.Results[1].Series)
	})

	t.Run("Drawdown", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/portfolio/drawdown?currency=USD&from=2024-01-10&to=2024-01-14&symbols=SPY", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var resp models.DrawdownResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

		require.Len(t, resp.Results, 2, "should have Portfolio and SPY")
		assert.Equal(t, "Portfolio", resp.Results[0].Symbol)
		assert.Equal(t, "SPY", resp.Results[1].Symbol)
		assert.NotEmpty(t, resp.Results[0].Series)
		assert.NotEmpty(t, resp.Results[1].Series)
	})
}
