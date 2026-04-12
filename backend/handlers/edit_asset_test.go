package handlers

import (
	"bytes"
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
)

func setupEditAssetDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("file:edit_asset_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{}, &models.AssetFundamental{}, &models.Transaction{}, &models.LLMCache{},
	))
	return db
}

func editAssetRouter(db *gorm.DB) (*gin.Engine, *models.User) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Seed a user.
	user := models.User{TokenHash: "testhash"}
	db.Create(&user)

	repo := &flexquery.Repository{DB: db}
	ph := NewPortfolioHandler(repo, nil, nil)

	r.PUT("/portfolio/assets/:symbol", func(c *gin.Context) {
		c.Set(middleware.UserHashKey, "testhash")
		ph.EditAsset(c)
	})
	return r, &user
}

// TestEditAsset_UpdatesNameAndSetsUserSource verifies that supplying name/country/sector
// sets DataSource="User" so the background job stops overwriting the record.
func TestEditAsset_UpdatesNameAndSetsUserSource(t *testing.T) {
	db := setupEditAssetDB(t)
	r, user := editAssetRouter(db)

	body, _ := json.Marshal(map[string]string{
		"name":    "Custom Name",
		"country": "Germany",
		"sector":  "Industrials",
	})
	req := httptest.NewRequest(http.MethodPut, "/portfolio/assets/AAPL", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var rec models.AssetFundamental
	require.NoError(t, db.Where("user_id = ? AND symbol = ?", user.ID, "AAPL").First(&rec).Error)
	assert.Equal(t, "Custom Name", rec.Name)
	assert.Equal(t, "Germany", rec.Country)
	assert.Equal(t, "Industrials", rec.Sector)
	assert.Equal(t, "User", rec.DataSource, "DataSource must be 'User' when background-managed fields are provided")
}

// TestEditAsset_ListingExchangeOnlyUpdatedWhenEmpty verifies that listing_exchange is only
// applied to transactions that currently have no exchange set.
func TestEditAsset_ListingExchangeOnlyUpdatedWhenEmpty(t *testing.T) {
	db := setupEditAssetDB(t)
	r, user := editAssetRouter(db)

	base := time.Now().UTC()
	// One transaction with no exchange (should be updated), one with an existing exchange (must not change).
	db.Create(&models.Transaction{UserID: user.ID, Symbol: "VOD", Type: "Trade", Currency: "GBP", DateTime: base, ListingExchange: ""})
	db.Create(&models.Transaction{UserID: user.ID, Symbol: "VOD", Type: "Trade", Currency: "GBP", DateTime: base.Add(time.Hour), ListingExchange: "LSE"})

	body, _ := json.Marshal(map[string]string{"listing_exchange": "XLON"})
	req := httptest.NewRequest(http.MethodPut, "/portfolio/assets/VOD", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var txns []models.Transaction
	db.Where("user_id = ? AND symbol = ?", user.ID, "VOD").Find(&txns)
	require.Len(t, txns, 2)
	exchangeValues := map[string]bool{}
	for _, tx := range txns {
		exchangeValues[tx.ListingExchange] = true
	}
	assert.True(t, exchangeValues["XLON"], "empty-exchange row must be updated to XLON")
	assert.True(t, exchangeValues["LSE"], "non-empty-exchange row must remain LSE")
}
