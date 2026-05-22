package fundamentals

import (
	"context"
	"errors"
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
	require.NoError(t, db.AutoMigrate(&models.AssetFundamental{}, &models.Transaction{}, &models.User{}, &models.MarketData{}, &models.EtfBreakdown{}))
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

// --- Mocks ---

type mockQuoteTypeFetcher struct {
	qt   string
	name string
	err  error
}

func (m *mockQuoteTypeFetcher) GetQuoteType(symbol string) (string, string, error) {
	return m.qt, m.name, m.err
}

type mockFundamentalsProvider struct {
	name   string
	result *models.AssetFundamental
	err    error
	calls  int
	limit  RateLimitConfig
}

func (m *mockFundamentalsProvider) Name() string { return m.name }
func (m *mockFundamentalsProvider) RateLimit() RateLimitConfig { return m.limit }
func (m *mockFundamentalsProvider) FetchFundamentals(symbol string) (*models.AssetFundamental, error) {
	m.calls++
	return m.result, m.err
}

type mockBreakdownProvider struct {
	name   string
	result *ETFBreakdownData
	err    error
	calls  int
	limit  RateLimitConfig
}

func (m *mockBreakdownProvider) Name() string { return m.name }
func (m *mockBreakdownProvider) RateLimit() RateLimitConfig { return m.limit }
func (m *mockBreakdownProvider) FetchETFBreakdown(fundSymbol string) (*ETFBreakdownData, error) {
	m.calls++
	return m.result, m.err
}

// --- Rate Limiting Tests ---

func TestRateLimitState(t *testing.T) {
	state := &perProviderState{
		minuteReset: time.Now().Add(-time.Minute), // trigger reset
		dayReset:    time.Now().Add(-time.Hour),
	}
	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		RequestsPerDay:    10,
		CooldownDuration:  time.Second,
	}

	assert.True(t, state.available(cfg))
	state.consume()
	state.consume()
	assert.False(t, state.available(cfg), "should be unavailable after consuming limit")

	// Trigger cooldown
	state.triggerCooldown(cfg)
	assert.False(t, state.available(cfg), "should be unavailable in cooldown")

	// Trigger daily cooldown
	state.triggerDailyCooldown()
	assert.False(t, state.available(cfg), "should be unavailable in daily cooldown")
}

// --- Configuration & Parser Helpers ---

func TestConfigAndSplit(t *testing.T) {
	t.Run("splitNames", func(t *testing.T) {
		res := splitNames(" A, B ,C,,D ")
		assert.Equal(t, []string{"A", "B", "C", "D"}, res)
	})

	t.Run("BuildFromConfig", func(t *testing.T) {
		db := setupFundamentalsDB(t)
		allF := map[string]FundamentalsProvider{
			"mock": &mockFundamentalsProvider{name: "mock"},
		}
		allB := map[string]ETFBreakdownProvider{
			"mockB": &mockBreakdownProvider{name: "mockB"},
		}
		svc := BuildFromConfig(db, "mock,missing", "mockB", allF, allB, nil)
		assert.Len(t, svc.fundamentalsProviders, 1)
		assert.Len(t, svc.breakdownProviders, 1)
	})
}

// --- Asset Bootstrapping Tests ---

func TestBootstrapAssetTypes(t *testing.T) {
	db := setupFundamentalsDB(t)

	// Seed transaction with category and conid/isin/currency
	base := time.Now().UTC()
	tx := models.Transaction{
		UserID:         1,
		Symbol:         "AAPL",
		Type:           "Trade",
		AssetCategory:  "STK", // Stock
		Currency:       "USD",
		Conid:          "12345",
		ISIN:           "US0378331005",
		DateTime:       base,
	}
	require.NoError(t, db.Create(&tx).Error)

	// AAPL priced by Yahoo
	require.NoError(t, db.Create(&models.MarketData{Symbol: "AAPL", Date: base, Close: 150}).Error)

	// Fetcher returning stock type
	qtF := &mockQuoteTypeFetcher{qt: "Stock", name: "Apple Inc."}
	svc := NewService(db, nil, nil, qtF)

	// Pre-create the AssetFundamental row so seedConid and seedISIN can update it on the first call
	require.NoError(t, db.Create(&models.AssetFundamental{UserID: 1, Symbol: "AAPL"}).Error)

	ctx := context.Background()
	symbols, err := svc.collectAllSymbols()
	require.NoError(t, err)

	svc.bootstrapAssetTypes(ctx, symbols)

	// Verify conid, isin, currency and type were seeded
	fund, err := svc.GetFundamentals("AAPL", 1)
	require.NoError(t, err)
	require.NotNil(t, fund)
	assert.Equal(t, "12345", fund.Conid)
	assert.Equal(t, "US0378331005", fund.ISIN)
	assert.Equal(t, "USD", fund.Currency)
	assert.Equal(t, "Stock", fund.AssetType)
}

// --- Queue & Execution (Success/Error flows) ---

func TestRunFetchCycle_EnrichmentAndBreakdown(t *testing.T) {
	db := setupFundamentalsDB(t)

	// 1. Seed ETF transaction
	base := time.Now().UTC()
	tx := models.Transaction{
		UserID:         1,
		Symbol:         "URTH",
		Type:           "Trade",
		AssetCategory:  "ETF",
		Currency:       "USD",
		DateTime:       base,
	}
	require.NoError(t, db.Create(&tx).Error)
	require.NoError(t, db.Create(&models.MarketData{Symbol: "URTH", Date: base, Close: 100}).Error)

	// Pre-create an incomplete, stale record so it gets queued for enrichment
	require.NoError(t, db.Create(&models.AssetFundamental{
		UserID:      1,
		Symbol:      "URTH",
		AssetType:   "ETF",
		DataSource:  "IB",
		LastUpdated: time.Now().UTC().Add(-24 * time.Hour),
	}).Error)

	// 2. Setup mock providers
	mockFundP := &mockFundamentalsProvider{
		name: "mockFund",
		result: &models.AssetFundamental{
			Symbol:     "URTH",
			Name:       "iShares MSCI World ETF",
			Country:    "Global",
			DataSource: "mockFund",
		},
	}
	dur := 2.5
	mockBreakP := &mockBreakdownProvider{
		name: "mockBreak",
		result: &ETFBreakdownData{
			Rows: []models.EtfBreakdown{
				{FundSymbol: "URTH", Dimension: "country", Label: "United States", Weight: 0.7},
			},
			IsBondETF: true,
			Duration:  &dur,
		},
	}

	svc := NewService(db, []FundamentalsProvider{mockFundP}, []ETFBreakdownProvider{mockBreakP}, nil)

	// Run fetch cycle
	svc.runFetchCycle(context.Background())

	// Verify fundamentals populated
	fund, err := svc.GetFundamentals("URTH", 1)
	require.NoError(t, err)
	require.NotNil(t, fund)
	assert.Equal(t, "iShares MSCI World ETF", fund.Name)
	assert.Equal(t, "Global", fund.Country)
	assert.Equal(t, "Bond ETF", fund.AssetType, "should be promoted to Bond ETF")
	require.NotNil(t, fund.Duration)
	assert.Equal(t, 2.5, *fund.Duration)

	// Verify breakdowns populated
	bds, err := svc.GetBreakdowns("URTH")
	require.NoError(t, err)
	assert.Len(t, bds, 1)
	assert.Equal(t, "United States", bds[0].Label)
	assert.Equal(t, 0.7, bds[0].Weight)
}

func TestRunFetchCycle_RateLimitCooldown(t *testing.T) {
	db := setupFundamentalsDB(t)
	base := time.Now().UTC()
	tx := models.Transaction{UserID: 1, Symbol: "AAPL", Type: "Trade", Currency: "USD", DateTime: base}
	require.NoError(t, db.Create(&tx).Error)
	require.NoError(t, db.Create(&models.MarketData{Symbol: "AAPL", Date: base, Close: 100}).Error)

	// Provider returns 429 rate limit error
	mockFundP := &mockFundamentalsProvider{
		name:  "mockFund",
		err:   errors.New("rate limit exceeded (429)"),
		limit: RateLimitConfig{RequestsPerMinute: 10, CooldownDuration: time.Hour},
	}
	svc := NewService(db, []FundamentalsProvider{mockFundP}, nil, nil)

	svc.runFetchCycle(context.Background())

	// Provider should be put in cooldown
	state := svc.fundamentalsStates["mockFund"]
	assert.True(t, state.cooldownUntil.After(time.Now()))
}

// --- Helpers Tests ---

func TestHelpers(t *testing.T) {
	assert.True(t, sameDay(time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2024, 1, 2, 20, 0, 0, 0, time.UTC)))
	assert.False(t, sameDay(time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), time.Date(2024, 1, 3, 10, 0, 0, 0, time.UTC)))

	assert.True(t, isRateLimitErr(errors.New("rate limit reached")))
	assert.True(t, isRateLimitErr(errors.New("error 429")))
	assert.False(t, isRateLimitErr(nil))

	assert.True(t, isDailyRateLimitErr(errors.New("daily quota reached")))
	assert.False(t, isDailyRateLimitErr(nil))

	assert.Equal(t, "AAPL", effectiveSymbol("AAPL", ""))
	assert.Equal(t, "AAPL.US", effectiveSymbol("AAPL", "AAPL.US"))
}

// --- Start Background Fetcher Test ---

func TestStartBackgroundFetcher(t *testing.T) {
	db := setupFundamentalsDB(t)
	svc := NewService(db, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartBackgroundFetcher(ctx)

	// Trigger non-blocking fetch
	svc.TriggerFetch()

	// Sleep briefly and cancel to verify clean shutdown
	time.Sleep(10 * time.Millisecond)
	cancel()
}
