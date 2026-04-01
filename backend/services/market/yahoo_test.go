package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gofolio-analysis/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type mockTransport struct {
	calls []*http.Request
	resp  func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.calls = append(m.calls, req)
	return m.resp(req)
}

func setupTestDB(t *testing.T) *gorm.DB {
	dbName := fmt.Sprintf("file:memdb_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(&models.MarketData{}, &models.AssetFundamental{})
	require.NoError(t, err)
	return db
}

func mockYahooResponse(symbol string, from, to time.Time) []byte {
	// Generate dummy data points within the range
	var timestamps []int64
	var quotes struct {
		Open   []float64 `json:"open"`
		High   []float64 `json:"high"`
		Low    []float64 `json:"low"`
		Close  []float64 `json:"close"`
		Volume []int64   `json:"volume"`
	}
	var adjCloses struct {
		AdjClose []float64 `json:"adjclose"`
	}

	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		// Skip weekends
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		timestamps = append(timestamps, d.Unix())
		quotes.Open = append(quotes.Open, 100)
		quotes.High = append(quotes.High, 105)
		quotes.Low = append(quotes.Low, 95)
		quotes.Close = append(quotes.Close, 102)
		quotes.Volume = append(quotes.Volume, 1000)
		adjCloses.AdjClose = append(adjCloses.AdjClose, 102)
	}

	// Build JSON
	resp := yahooChartResponse{}
	if len(timestamps) > 0 {
		resp.Chart.Result = append(resp.Chart.Result, struct {
			Meta struct {
				QuoteType          string  `json:"quoteType"`
				LongName           string  `json:"longName"`
				Currency           string  `json:"currency"`
				RegularMarketPrice float64 `json:"regularMarketPrice"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []int64   `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		}{
			Timestamp: timestamps,
			Indicators: struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []int64   `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []float64 `json:"adjclose"`
				} `json:"adjclose"`
			}{
				Quote: []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []int64   `json:"volume"`
				}{{
					Open:   quotes.Open,
					High:   quotes.High,
					Low:    quotes.Low,
					Close:  quotes.Close,
					Volume: quotes.Volume,
				}},
				AdjClose: []struct {
					AdjClose []float64 `json:"adjclose"`
				}{{
					AdjClose: adjCloses.AdjClose,
				}},
			},
		})
	} else {
		// Empty result, Yahoo return code varies, but typically result is nil if no data
		resp.Chart.Error = &struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		}{Code: "Not Found", Description: "No data found, symbol may be delisted"}
	}

	data, _ := json.Marshal(resp)
	return data
}

func TestSmartCachingBehavior(t *testing.T) {
	db := setupTestDB(t)

	transport := &mockTransport{}
	transport.resp = func(req *http.Request) (*http.Response, error) {
		// Return dummy response that says "found some data" mapped to the request's bounds
		req.ParseForm()
		p1 := req.Form.Get("period1")
		p2 := req.Form.Get("period2")
		// parse
		var t1, t2 time.Time
		if p1 != "" {
			var sec int64
			fmt.Sscanf(p1, "%d", &sec)
			t1 = time.Unix(sec, 0)
		}
		if p2 != "" {
			var sec int64
			fmt.Sscanf(p2, "%d", &sec)
			t2 = time.Unix(sec, 0)
		}
		body := mockYahooResponse("AAPL", t1, t2)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBuffer(body)),
		}, nil
	}

	svc := NewYahooFinanceService(db)
	svc.HTTPClient.Transport = transport

	// Scene 1: Completely empty cache (Obvious Case)
	// User asks for 10 days of history
	t.Run("Empty cache fetches entire range", func(t *testing.T) {
		transport.calls = nil
		from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

		pts, err := svc.GetHistory("AAPL", from, to, false)
		require.NoError(t, err)
		assert.NotEmpty(t, pts)
		assert.Len(t, transport.calls, 1, "Should make exactly 1 network request to Yahoo")
	})

	// Scene 2: Fully cached (Obvious Case)
	// Asking for the exact same bounds again should result in 0 network calls.
	t.Run("Fully cached avoids network calls", func(t *testing.T) {
		transport.calls = nil
		from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

		pts, err := svc.GetHistory("AAPL", from, to, false)
		require.NoError(t, err)
		assert.NotEmpty(t, pts)
		assert.Len(t, transport.calls, 0, "Should make 0 network requests because data is cached")
	})

	// Scene 3: Missing Prefix (Smart behavior)
	// Database has [Jan 1 -> Jan 10]. We ask for [Dec 25 -> Jan 10].
	// Expected: It should fetch ONLY Dec 25 -> Dec 31
	t.Run("Missing prefix makes targeted call", func(t *testing.T) {
		transport.calls = nil
		from := time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)
		to := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

		pts, err := svc.GetHistory("AAPL", from, to, false)
		require.NoError(t, err)
		assert.NotEmpty(t, pts)

		require.Len(t, transport.calls, 1, "Should fetch the prefix gap")
		// The URL period2 should be Dec 31, 2024 (the day before first cache hit)
		url := transport.calls[0].URL.String()
		expectedEnd := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1).Unix()
		assert.Contains(t, url, fmt.Sprintf("period2=%d", expectedEnd))
	})

	// Scene 4: Missing Suffix (Smart behavior)
	// Database has [Dec 25 -> Jan 10]. We ask for [Dec 25 -> Jan 20].
	// Expected: It should fetch ONLY Jan 11 -> Jan 20
	t.Run("Missing suffix makes targeted call", func(t *testing.T) {
		transport.calls = nil
		from := time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)
		to := time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)

		pts, err := svc.GetHistory("AAPL", from, to, false)
		require.NoError(t, err)
		assert.NotEmpty(t, pts)

		require.Len(t, transport.calls, 1, "Should fetch the suffix gap")
		url := transport.calls[0].URL.String()
		expectedStart := time.Date(2025, 1, 11, 0, 0, 0, 0, time.UTC).Unix()
		assert.Contains(t, url, fmt.Sprintf("period1=%d", expectedStart))
	})
}

func TestNegativeCaching(t *testing.T) {
	db := setupTestDB(t)

	transport := &mockTransport{}
	transport.resp = func(req *http.Request) (*http.Response, error) {
		// Yahoo returns empty set for pre-IPO
		resp := yahooChartResponse{}
		resp.Chart.Error = &struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		}{Code: "Not Found", Description: "No data found, symbol may be delisted"}

		data, _ := json.Marshal(resp)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBuffer(data)),
		}, nil
	}

	svc := NewYahooFinanceService(db)
	svc.HTTPClient.Transport = transport

	from := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(1980, 1, 10, 0, 0, 0, 0, time.UTC)

	// First call should reach out and get empty results, then store negative cache bounds
	pts, err := svc.GetHistory("PRE_IPO_TICKER", from, to, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "yahoo finance error: No data found")
	assert.Empty(t, pts)
	require.Len(t, transport.calls, 1)

	transport.calls = nil

	// Second call for the EXACT same pre-IPO dates shouldn't reach out to the network at all!
	pts2, _ := svc.GetHistory("PRE_IPO_TICKER", from, to, false)
	// It relies on dummy volume markers in the DB to skip hitting Yahoo again
	assert.Empty(t, pts2)
	assert.Len(t, transport.calls, 0, "Negative cache prevents identical failing fetches")
}

func TestGetCurrency(t *testing.T) {
	db := setupTestDB(t)
	svc := NewYahooFinanceService(db)

	t.Run("returns currency from Yahoo meta", func(t *testing.T) {
		body := `{"chart":{"result":[{"meta":{"quoteType":"ETF","currency":"EUR"}}],"error":null}}`
		transport := &mockTransport{
			resp: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		}
		svc.HTTPClient.Transport = transport

		ccy, err := svc.GetCurrency("SXR8.DE")
		require.NoError(t, err)
		assert.Equal(t, "EUR", ccy)
		require.Len(t, transport.calls, 1)
		assert.Contains(t, transport.calls[0].URL.Path, "SXR8.DE")
	})

	t.Run("returns USD for a US-listed stock", func(t *testing.T) {
		body := `{"chart":{"result":[{"meta":{"quoteType":"EQUITY","currency":"USD"}}],"error":null}}`
		transport := &mockTransport{
			resp: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		}
		svc.HTTPClient.Transport = transport

		ccy, err := svc.GetCurrency("SPY")
		require.NoError(t, err)
		assert.Equal(t, "USD", ccy)
	})

	t.Run("returns empty string when symbol not found", func(t *testing.T) {
		body := `{"chart":{"result":[],"error":{"code":"Not Found","description":"No data found"}}}`
		transport := &mockTransport{
			resp: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		}
		svc.HTTPClient.Transport = transport

		ccy, err := svc.GetCurrency("UNKNOWN")
		require.NoError(t, err)
		assert.Equal(t, "", ccy)
	})
}
