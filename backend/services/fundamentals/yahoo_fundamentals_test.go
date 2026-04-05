package fundamentals_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"portfolio-analysis/services/fundamentals"
	"portfolio-analysis/services/market"
)

// mockTransport is a minimal http.RoundTripper that returns canned responses.
type mockTransport struct {
	responses []mockResp
	idx       int
}

type mockResp struct {
	status  int
	body    string
	cookies []*http.Cookie
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("unexpected request #%d to %s", m.idx+1, req.URL)
	}
	r := m.responses[m.idx]
	m.idx++
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	for _, c := range r.cookies {
		h.Add("Set-Cookie", c.String())
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(bytes.NewBufferString(r.body)),
		Header:     h,
		Request:    req,
	}, nil
}

// profileJSON builds a minimal quoteSummary JSON response for profile modules.
func profileJSON(assetLongName, country, sector, fundLongName, exchange string) string {
	return fmt.Sprintf(`{
		"quoteSummary": {
			"result": [{
				"assetProfile": {"longName":%q,"country":%q,"sector":%q},
				"fundProfile":  {"longName":%q},
				"price":        {"longName":"","exchangeName":%q}
			}],
			"error": null
		}
	}`, assetLongName, country, sector, fundLongName, exchange)
}

// crumbResponses returns the two HTTP responses needed to seed the crumb manager.
func crumbResponses() []mockResp {
	return []mockResp{
		{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck"}}},
		{status: 200, body: "testcrumb"},
	}
}

func buildProvider(responses []mockResp, rpm int) *fundamentals.YahooFundamentalsProvider {
	transport := &mockTransport{responses: responses}
	svc := market.NewYahooFinanceServiceWithTransport(transport)
	return fundamentals.NewYahooFundamentalsProvider(svc, rpm)
}

// TestYahooFundamentalsProviderStock verifies that a stock response maps correctly.
func TestYahooFundamentalsProviderStock(t *testing.T) {
	responses := append(crumbResponses(),
		mockResp{status: 200, body: profileJSON("Apple Inc.", "United States", "Technology", "", "NMS")},
	)
	provider := buildProvider(responses, 0)

	fund, err := provider.FetchFundamentals("AAPL")
	require.NoError(t, err)
	require.NotNil(t, fund)
	assert.Equal(t, "AAPL", fund.Symbol)
	assert.Equal(t, "Apple Inc.", fund.Name)
	assert.Equal(t, "United States", fund.Country)
	assert.Equal(t, "Technology", fund.Sector)
	assert.Equal(t, "NMS", fund.Exchange)
	assert.Equal(t, "Yahoo", fund.DataSource)
	assert.WithinDuration(t, time.Now().UTC(), fund.LastUpdated, 5*time.Second)
}

// TestYahooFundamentalsProviderETF verifies that fundProfile.longName is used for
// ETFs (assetProfile.longName is empty for funds).
func TestYahooFundamentalsProviderETF(t *testing.T) {
	responses := append(crumbResponses(),
		mockResp{status: 200, body: profileJSON("", "", "", "iShares Core MSCI World UCITS ETF", "LSE")},
	)
	provider := buildProvider(responses, 0)

	fund, err := provider.FetchFundamentals("IWDA.L")
	require.NoError(t, err)
	require.NotNil(t, fund)
	assert.Equal(t, "iShares Core MSCI World UCITS ETF", fund.Name)
	// ETFs have no country/sector in assetProfile — emptyToUnknown applies.
	assert.Equal(t, "Unknown", fund.Country)
	assert.Equal(t, "Unknown", fund.Sector)
	assert.Equal(t, "LSE", fund.Exchange)
}

// TestYahooFundamentalsProviderNotFound verifies nil, nil when no profile data exists.
func TestYahooFundamentalsProviderNotFound(t *testing.T) {
	responses := append(crumbResponses(),
		mockResp{status: 200, body: profileJSON("", "", "", "", "")},
	)
	provider := buildProvider(responses, 0)

	fund, err := provider.FetchFundamentals("UNKNOWN")
	require.NoError(t, err)
	assert.Nil(t, fund, "symbol with no data should return nil, nil")
}

// TestYahooFundamentalsProviderEmptyFieldsUnknown verifies that empty country/sector
// are replaced with "Unknown" even when a name is present.
func TestYahooFundamentalsProviderEmptyFieldsUnknown(t *testing.T) {
	responses := append(crumbResponses(),
		mockResp{status: 200, body: profileJSON("Some Corp", "", "", "", "")},
	)
	provider := buildProvider(responses, 0)

	fund, err := provider.FetchFundamentals("XYZ")
	require.NoError(t, err)
	require.NotNil(t, fund)
	assert.Equal(t, "Some Corp", fund.Name)
	assert.Equal(t, "Unknown", fund.Country)
	assert.Equal(t, "Unknown", fund.Sector)
}

// TestYahooFundamentalsProviderMetadata verifies Name() and RateLimit().
func TestYahooFundamentalsProviderMetadata(t *testing.T) {
	provider := fundamentals.NewYahooFundamentalsProvider(nil, 20)
	assert.Equal(t, "Yahoo", provider.Name())
	assert.Equal(t, 20, provider.RateLimit().RequestsPerMinute)
}
