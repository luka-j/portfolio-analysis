package fundamentals

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"portfolio-analysis/models"
)

func setupFundamentalsDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("file:fundamentals_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(name), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.AssetFundamental{}, &models.Transaction{}, &models.User{}, &models.MarketData{}))
	return db
}

func newService(db *gorm.DB) *Service {
	return NewService(db, nil, nil, nil)
}

// TestUpsertFundamentals_SkipsUserEditedRecords verifies that when a row exists with
// DataSource="User", upsertFundamentals does not overwrite it.
func TestUpsertFundamentals_SkipsUserEditedRecords(t *testing.T) {
	db := setupFundamentalsDB(t)
	svc := newService(db)

	// Pre-seed a user-edited record.
	now := time.Now().UTC()
	original := models.AssetFundamental{
		UserID:      1,
		Symbol:      "AAPL",
		Name:        "My Custom Name",
		Country:     "France",
		AssetType:   "Stock",
		DataSource:  "User",
		LastUpdated: now,
	}
	require.NoError(t, db.Create(&original).Error)

	// Try to overwrite via the background job.
	svc.upsertFundamentals("AAPL", &models.AssetFundamental{
		Symbol:      "AAPL",
		Name:        "Apple Inc.",
		Country:     "United States",
		DataSource:  "Yahoo",
		LastUpdated: time.Now().UTC(),
	}, 1)

	var result models.AssetFundamental
	require.NoError(t, db.Where("user_id = ? AND symbol = ?", uint(1), "AAPL").First(&result).Error)
	assert.Equal(t, "My Custom Name", result.Name, "user-edited name must not be overwritten")
	assert.Equal(t, "France", result.Country, "user-edited country must not be overwritten")
	assert.Equal(t, "User", result.DataSource)
}

// TestUpsertFundamentals_OverwritesNonUserRecords verifies that rows with DataSource != "User"
// are updated by the background job.
func TestUpsertFundamentals_OverwritesNonUserRecords(t *testing.T) {
	db := setupFundamentalsDB(t)
	svc := newService(db)

	// Pre-seed a Yahoo-sourced record.
	old := models.AssetFundamental{
		UserID:      2,
		Symbol:      "MSFT",
		Name:        "Old Name",
		Country:     "Unknown",
		DataSource:  "Yahoo",
		LastUpdated: time.Now().UTC().Add(-48 * time.Hour),
	}
	require.NoError(t, db.Create(&old).Error)

	svc.upsertFundamentals("MSFT", &models.AssetFundamental{
		Symbol:      "MSFT",
		Name:        "Microsoft Corporation",
		Country:     "United States",
		DataSource:  "Yahoo",
		LastUpdated: time.Now().UTC(),
	}, 2)

	var result models.AssetFundamental
	require.NoError(t, db.Where("user_id = ? AND symbol = ?", uint(2), "MSFT").First(&result).Error)
	assert.Equal(t, "Microsoft Corporation", result.Name, "stale name must be updated by background job")
	assert.Equal(t, "United States", result.Country)
}

// TestCollectPortfolioSymbols_ReturnsUserIDMap verifies that symbols are grouped by user ID.
func TestCollectPortfolioSymbols_ReturnsUserIDMap(t *testing.T) {
	db := setupFundamentalsDB(t)
	svc := newService(db)

	base := time.Now().UTC()
	txns := []models.Transaction{
		{UserID: 1, Symbol: "AAPL", Type: "Trade", Currency: "USD", DateTime: base},
		{UserID: 2, Symbol: "AAPL", Type: "Trade", Currency: "USD", DateTime: base},
		{UserID: 1, Symbol: "MSFT", Type: "Trade", Currency: "USD", DateTime: base},
	}
	require.NoError(t, db.Create(&txns).Error)

	// Also seed market data so the symbols pass the hasMarketData filter.
	db.Create(&models.MarketData{Symbol: "AAPL", Date: base, Close: 100})
	db.Create(&models.MarketData{Symbol: "MSFT", Date: base, Close: 200})

	results, err := svc.collectPortfolioSymbols()
	require.NoError(t, err)

	bySymbol := make(map[string][]uint)
	for _, sw := range results {
		bySymbol[sw.Symbol] = sw.UserIDs
	}

	require.Contains(t, bySymbol, "AAPL")
	assert.ElementsMatch(t, []uint{1, 2}, bySymbol["AAPL"], "both users should appear for AAPL")

	require.Contains(t, bySymbol, "MSFT")
	assert.Equal(t, []uint{1}, bySymbol["MSFT"])
}
