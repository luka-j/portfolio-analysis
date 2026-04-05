package breakdown

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

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbName := fmt.Sprintf("file:breakdown_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(&models.AssetFundamental{}, &models.EtfBreakdown{})
	require.NoError(t, err)
	return db
}

func pos(symbol, yahooSym string, value float64) models.PositionValue {
	return models.PositionValue{
		Symbol:      symbol,
		YahooSymbol: yahooSym,
		Values:      map[string]float64{"USD": value},
	}
}

func TestCalculateStockPosition(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().UTC()
	db.Create(&models.AssetFundamental{
		Symbol: "AAPL", Name: "Apple Inc.", AssetType: "Stock",
		Country: "US", Sector: "Technology",
		DataSource: "test", LastUpdated: now,
	})

	svc := NewService(db)
	result, err := svc.Calculate([]models.PositionValue{pos("AAPL", "", 1000)}, "USD")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "USD", result.Currency)
	assert.Len(t, result.Sections, 4)

	byCountry := findSection(t, result.Sections, "By Country")
	assert.Equal(t, "US", byCountry.Entries[0].Label)
	assert.InDelta(t, 100.0, byCountry.Entries[0].Percentage, 0.1)

	bySector := findSection(t, result.Sections, "By Sector")
	assert.Equal(t, "Technology", bySector.Entries[0].Label)
}

func TestCalculateETFWithBreakdown(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().UTC()
	db.Create(&models.AssetFundamental{
		Symbol: "VWCE.DE", Name: "Vanguard FTSE All-World", AssetType: "ETF",
		DataSource: "test", LastUpdated: now,
	})
	db.Create(&models.EtfBreakdown{FundSymbol: "VWCE.DE", Dimension: "sector", Label: "Technology", Weight: 0.25, LastUpdated: now})
	db.Create(&models.EtfBreakdown{FundSymbol: "VWCE.DE", Dimension: "sector", Label: "Healthcare", Weight: 0.15, LastUpdated: now})
	db.Create(&models.EtfBreakdown{FundSymbol: "VWCE.DE", Dimension: "country", Label: "United States", Weight: 0.60, LastUpdated: now})
	db.Create(&models.EtfBreakdown{FundSymbol: "VWCE.DE", Dimension: "country", Label: "Japan", Weight: 0.10, LastUpdated: now})

	svc := NewService(db)
	result, err := svc.Calculate([]models.PositionValue{pos("VWCE.DE", "VWCE.DE", 1000)}, "USD")
	require.NoError(t, err)

	bySector := findSection(t, result.Sections, "By Sector")
	sectorMap := sectionMap(bySector)
	// weights sum to 0.40, normalized: Tech=0.25/0.40=62.5%, Health=0.15/0.40=37.5%
	assert.InDelta(t, 62.5, sectorMap["Technology"], 0.5)
	assert.InDelta(t, 37.5, sectorMap["Healthcare"], 0.5)

	byCountry := findSection(t, result.Sections, "By Country")
	countryMap := sectionMap(byCountry)
	// weights sum to 0.70, normalized: US=0.60/0.70≈85.7%, JP=0.10/0.70≈14.3%
	assert.InDelta(t, 85.7, countryMap["United States"], 0.5)
	assert.InDelta(t, 14.3, countryMap["Japan"], 0.5)

	// ETF should appear as a single asset line item.
	byAsset := findSection(t, result.Sections, "By Asset")
	assert.Len(t, byAsset.Entries, 1)
	assert.Equal(t, "Vanguard FTSE All-World", byAsset.Entries[0].Label)
}

func TestCalculateETFWithMissingBreakdown(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().UTC()
	db.Create(&models.AssetFundamental{
		Symbol: "SPY", AssetType: "ETF",
		DataSource: "test", LastUpdated: now,
	})
	// No EtfBreakdown rows seeded.

	svc := NewService(db)
	result, err := svc.Calculate([]models.PositionValue{pos("SPY", "", 2000)}, "USD")
	require.NoError(t, err)

	byCountry := findSection(t, result.Sections, "By Country")
	assert.Equal(t, "Unknown", byCountry.Entries[0].Label)

	bySector := findSection(t, result.Sections, "By Sector")
	assert.Equal(t, "Unknown", bySector.Entries[0].Label)
}

func TestCalculateMixedPortfolio(t *testing.T) {
	db := setupTestDB(t)
	now := time.Now().UTC()
	db.Create(&models.AssetFundamental{Symbol: "AAPL", Name: "Apple Inc.", AssetType: "Stock", Country: "US", Sector: "Technology", LastUpdated: now})
	db.Create(&models.AssetFundamental{Symbol: "VWCE.DE", Name: "Vanguard FTSE", AssetType: "ETF", LastUpdated: now})
	db.Create(&models.EtfBreakdown{FundSymbol: "VWCE.DE", Dimension: "sector", Label: "Technology", Weight: 0.30, LastUpdated: now})
	db.Create(&models.EtfBreakdown{FundSymbol: "VWCE.DE", Dimension: "country", Label: "United States", Weight: 0.60, LastUpdated: now})

	svc := NewService(db)
	positions := []models.PositionValue{
		pos("AAPL", "", 1000),
		pos("VWCE.DE", "VWCE.DE", 1000),
	}
	result, err := svc.Calculate(positions, "USD")
	require.NoError(t, err)

	// Total = 2000. Sections should sum to ~100%.
	for _, section := range result.Sections {
		var total float64
		for _, e := range section.Entries {
			total += e.Percentage
		}
		assert.InDelta(t, 100.0, total, 2.0, "section %q should sum to ~100%%", section.Title)
	}

	byType := findSection(t, result.Sections, "By Asset Type")
	typeMap := sectionMap(byType)
	assert.InDelta(t, 50.0, typeMap["Stock"], 0.5)
	assert.InDelta(t, 50.0, typeMap["ETF"], 0.5)
}

func TestCalculateEmptyPortfolio(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	result, err := svc.Calculate(nil, "USD")
	require.NoError(t, err)
	assert.Equal(t, "USD", result.Currency)
	assert.Empty(t, result.Sections)
}

func findSection(t *testing.T, sections []models.BreakdownSection, title string) models.BreakdownSection {
	t.Helper()
	for _, s := range sections {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("section %q not found", title)
	return models.BreakdownSection{}
}

func sectionMap(s models.BreakdownSection) map[string]float64 {
	m := make(map[string]float64)
	for _, e := range s.Entries {
		m[e.Label] = e.Percentage
	}
	return m
}
