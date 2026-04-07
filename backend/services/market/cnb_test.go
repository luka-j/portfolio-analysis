package market

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"portfolio-analysis/models"
)

func TestCNBProvider(t *testing.T) {
	// Setup in-memory sqlite
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.AutoMigrate(&models.MarketData{})

	// Mock CNB API server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/cnbapi/exrates/daily", r.URL.Path)
		assert.Contains(t, r.URL.Query().Get("date"), "2025-01-02")

		response := `{
			"rates": [
				{"validFor": "2025-01-02", "amount": 1, "currencyCode": "EUR", "rate": 25.50},
				{"validFor": "2025-01-02", "amount": 1, "currencyCode": "USD", "rate": 23.50},
				{"validFor": "2025-01-02", "amount": 100, "currencyCode": "JPY", "rate": 15.00}
			]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	}))
	defer ts.Close()

	provider := NewCNBProvider(db)

	// Override the HTTPClient to redirect all API calls to the mock server
	// We do this by swapping out the Transport
	origTransport := http.DefaultTransport
	provider.HTTPClient.Transport = &rewriteTransport{targetURL: ts.URL}
	defer func() { provider.HTTPClient.Transport = origTransport }()

	t.Run("Fetching USD correctly queries server and persists", func(t *testing.T) {
		date := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
		rate, err := provider.GetRate("USD", date)
		require.NoError(t, err)
		assert.Equal(t, 23.50, rate)

		// Verify it was cached under Provider=CNB
		var md models.MarketData
		err = db.Where("symbol = ? AND date = ? AND provider = ?", "USDCZK=X", date, "CNB").First(&md).Error
		require.NoError(t, err)
		assert.Equal(t, 23.50, md.Close)
	})

	t.Run("Fetching EUR pulls from cache directly", func(t *testing.T) {
		// Because the first query saved ALL currencies from the payload, EUR should be cached!
		// Let's break the mock server to enforce cache-only logic.
		ts.Close() // Turn off server

		date := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
		rate, err := provider.GetRate("EUR", date)
		require.NoError(t, err)
		assert.Equal(t, 25.50, rate)
	})

	t.Run("Amounts logic (JPY example) corrects ratios", func(t *testing.T) {
		date := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
		// 100 JPY = 15.00 CZK -> 1 JPY = 0.15 CZK
		rate, err := provider.GetRate("JPY", date)
		require.NoError(t, err)
		assert.InDelta(t, 0.15, rate, 0.0001)
	})

	t.Run("CZK inherently returns 1.0", func(t *testing.T) {
		date := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
		rate, err := provider.GetRate("CZK", date)
		require.NoError(t, err)
		assert.Equal(t, 1.0, rate)
	})
}

// rewriteTransport intercepts requests and modifies the scheme/host to point to the mock test server
type rewriteTransport struct {
	targetURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL, _ := http.NewRequest(req.Method, t.targetURL+req.URL.Path+"?"+req.URL.RawQuery, req.Body)
	newURL.Header = req.Header
	return http.DefaultClient.Do(newURL)
}
