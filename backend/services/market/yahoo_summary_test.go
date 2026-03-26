package market

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSummaryTransport records requests and returns canned responses.
type mockSummaryTransport struct {
	calls     []*http.Request
	responses []mockResponse
	idx       int
}

type mockResponse struct {
	status  int
	body    string
	cookies []*http.Cookie // Set-Cookie headers to include in the response
}

func (m *mockSummaryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.calls = append(m.calls, req)
	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("unexpected request #%d to %s", m.idx+1, req.URL)
	}
	r := m.responses[m.idx]
	m.idx++

	header := http.Header{}
	header.Set("Content-Type", "application/json")
	for _, c := range r.cookies {
		header.Add("Set-Cookie", c.String())
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(bytes.NewBufferString(r.body)),
		Header:     header,
		Request:    req,
	}, nil
}

func buildSummaryService(transport http.RoundTripper) *YahooFinanceService {
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	svc := &YahooFinanceService{
		HTTPClient:     client,
		summaryLimiter: rate.NewLimiter(rate.Inf, 1),
		crumbMgr:       newCrumbManager(client),
	}
	return svc
}

// quoteSummaryJSON returns a minimal topHoldings response with given sector slugs.
func quoteSummaryJSON(sectors map[string]float64) string {
	sectorEntries := ""
	for k, v := range sectors {
		if sectorEntries != "" {
			sectorEntries += ","
		}
		sectorEntries += fmt.Sprintf(`{%q:{"raw":%f,"fmt":"%.2f%%"}}`, k, v, v*100)
	}
	return fmt.Sprintf(`{
		"quoteSummary": {
			"result": [{
				"topHoldings": {
					"sectorWeightings": [%s]
				}
			}],
			"error": null
		}
	}`, sectorEntries)
}

func TestSectorLabelMapping(t *testing.T) {
	cases := []struct {
		key      string
		expected string
	}{
		{"technology", "Technology"},
		{"realestate", "Real Estate"},
		{"consumer_cyclical", "Consumer Cyclical"},
		{"financial_services", "Financial Services"},
		{"communication_services", "Communication Services"},
		{"basic_materials", "Basic Materials"},
		{"consumer_defensive", "Consumer Defensive"},
		{"utilities", "Utilities"},
		{"industrials", "Industrials"},
		{"energy", "Energy"},
		{"healthcare", "Healthcare"},
		{"some_unknown_sector", "Some Unknown Sector"}, // fallback title-casing
	}
	for _, tc := range cases {
		assert.Equal(t, tc.expected, sectorLabel(tc.key), "key=%s", tc.key)
	}
}

func TestCrumbManagerFetchesCrumb(t *testing.T) {
	transport := &mockSummaryTransport{
		responses: []mockResponse{
			// Step 1: Yahoo seed page — returns cookie
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "testcookie"}}},
			// Step 2: crumb endpoint
			{status: 200, body: "testcrumb123"},
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	cm := newCrumbManager(client)

	crumb, err := cm.getCrumb()
	require.NoError(t, err)
	assert.Equal(t, "testcrumb123", crumb)
	assert.Equal(t, 2, len(transport.calls))

	// Second call should use cache — no new HTTP requests.
	crumb2, err2 := cm.getCrumb()
	require.NoError(t, err2)
	assert.Equal(t, "testcrumb123", crumb2)
	assert.Equal(t, 2, len(transport.calls), "should use cached crumb")
}

func TestCrumbManagerRefreshesAfterForce(t *testing.T) {
	transport := &mockSummaryTransport{
		responses: []mockResponse{
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "c1"}}},
			{status: 200, body: "crumb1"},
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "c2"}}},
			{status: 200, body: "crumb2"},
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	cm := newCrumbManager(client)

	crumb1, _ := cm.getCrumb()
	assert.Equal(t, "crumb1", crumb1)

	cm.forceRefresh()

	crumb2, _ := cm.getCrumb()
	assert.Equal(t, "crumb2", crumb2)
	assert.Equal(t, 4, len(transport.calls))
}

func TestGetETFBreakdownParsesSectorWeightings(t *testing.T) {
	sectors := map[string]float64{
		"technology":        0.25,
		"healthcare":        0.15,
		"financial_services": 0.20,
	}
	transport := &mockSummaryTransport{
		responses: []mockResponse{
			// crumb seed
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck"}}},
			{status: 200, body: "crumbXYZ"},
			// quoteSummary
			{status: 200, body: quoteSummaryJSON(sectors)},
		},
	}

	svc := buildSummaryService(transport)
	result, err := svc.GetETFBreakdown("VWCE.DE")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Breakdowns, 3)
	assert.False(t, result.IsBondETF)

	byLabel := make(map[string]float64)
	for _, r := range result.Breakdowns {
		assert.Equal(t, "sector", r.Dimension)
		byLabel[r.Label] = r.Weight
	}
	assert.InDelta(t, 0.25, byLabel["Technology"], 0.001)
	assert.InDelta(t, 0.15, byLabel["Healthcare"], 0.001)
	assert.InDelta(t, 0.20, byLabel["Financial Services"], 0.001)
}

func TestGetETFBreakdownParsesCountryWeightings(t *testing.T) {
	response := `{
		"quoteSummary": {
			"result": [{
				"topHoldings": {
					"sectorWeightings": [{"technology":{"raw":0.30,"fmt":"30.00%"}}],
					"countryWeightings": [
						{"us":{"raw":0.60,"fmt":"60.00%"}},
						{"gb":{"raw":0.15,"fmt":"15.00%"}},
						{"jp":{"raw":0.10,"fmt":"10.00%"}}
					]
				}
			}],
			"error": null
		}
	}`
	transport := &mockSummaryTransport{
		responses: []mockResponse{
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck"}}},
			{status: 200, body: "crumbABC"},
			{status: 200, body: response},
		},
	}
	svc := buildSummaryService(transport)
	result, err := svc.GetETFBreakdown("VWCE.DE")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Breakdowns, 4) // 1 sector + 3 countries
	assert.False(t, result.IsBondETF)

	byDimLabel := make(map[string]float64)
	for _, r := range result.Breakdowns {
		byDimLabel[r.Dimension+":"+r.Label] = r.Weight
	}
	assert.InDelta(t, 0.30, byDimLabel["sector:Technology"], 0.001)
	assert.InDelta(t, 0.60, byDimLabel["country:United States"], 0.001)
	assert.InDelta(t, 0.15, byDimLabel["country:United Kingdom"], 0.001)
	assert.InDelta(t, 0.10, byDimLabel["country:Japan"], 0.001)
}

func TestCountryLabelMapping(t *testing.T) {
	assert.Equal(t, "United States", countryLabel("us"))
	assert.Equal(t, "United Kingdom", countryLabel("gb"))
	assert.Equal(t, "Japan", countryLabel("jp"))
	assert.Equal(t, "Other", countryLabel("other"))
	assert.Equal(t, "Some Unknown", countryLabel("some_unknown")) // fallback
}

func TestGetETFBreakdownReturnsNilWhenNoData(t *testing.T) {
	emptyResponse := `{"quoteSummary":{"result":[{"topHoldings":{"sectorWeightings":[]}}],"error":null}}`
	transport := &mockSummaryTransport{
		responses: []mockResponse{
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck"}}},
			{status: 200, body: "crumb1"},
			{status: 200, body: emptyResponse},
		},
	}
	svc := buildSummaryService(transport)
	result, err := svc.GetETFBreakdown("UNKNOWN")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetETFBreakdownRefreshesOn401(t *testing.T) {
	sectors := map[string]float64{"technology": 0.30}
	transport := &mockSummaryTransport{
		responses: []mockResponse{
			// initial crumb
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck1"}}},
			{status: 200, body: "crumb1"},
			// first quoteSummary attempt → 401
			{status: 401, body: "Unauthorized"},
			// crumb refresh
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck2"}}},
			{status: 200, body: "crumb2"},
			// retry quoteSummary → success
			{status: 200, body: quoteSummaryJSON(sectors)},
		},
	}
	svc := buildSummaryService(transport)
	result, err := svc.GetETFBreakdown("SPY")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Breakdowns, 1)
	assert.Equal(t, "Technology", result.Breakdowns[0].Label)
	assert.InDelta(t, 0.30, result.Breakdowns[0].Weight, 0.001)
}

func TestGetETFBreakdownDetectsBondETF(t *testing.T) {
	bondResponse := `{"quoteSummary":{"result":[{"topHoldings":{
		"bondPosition":{"raw":0.9951,"fmt":"99.51%"},
		"stockPosition":{"raw":0.0,"fmt":"0.00%"},
		"bondHoldings":{"duration":{"raw":2.66,"fmt":"2.66"}},
		"bondRatings":[
			{"aaa":{"raw":0.0018,"fmt":"0.18%"}},
			{"aa":{"raw":0.0695,"fmt":"6.95%"}},
			{"a":{"raw":0.4173,"fmt":"41.73%"}},
			{"bbb":{"raw":0.5101,"fmt":"51.01%"}}
		],
		"sectorWeightings":[]
	}}],"error":null}}`
	transport := &mockSummaryTransport{
		responses: []mockResponse{
			{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck"}}},
			{status: 200, body: "crumbBOND"},
			{status: 200, body: bondResponse},
		},
	}
	svc := buildSummaryService(transport)
	result, err := svc.GetETFBreakdown("IBCI.AS")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsBondETF)
	assert.InDelta(t, 2.66, result.Duration, 0.001)

	byLabel := make(map[string]float64)
	for _, r := range result.Breakdowns {
		assert.Equal(t, "bond_rating", r.Dimension)
		byLabel[r.Label] = r.Weight
	}
	assert.InDelta(t, 0.0018, byLabel["AAA"], 0.0001)
	assert.InDelta(t, 0.0695, byLabel["AA"], 0.0001)
	assert.InDelta(t, 0.4173, byLabel["A"], 0.0001)
	assert.InDelta(t, 0.5101, byLabel["BBB"], 0.0001)
	// Sector entries must NOT be present for bond ETFs.
	for _, r := range result.Breakdowns {
		assert.NotEqual(t, "sector", r.Dimension)
	}
}
