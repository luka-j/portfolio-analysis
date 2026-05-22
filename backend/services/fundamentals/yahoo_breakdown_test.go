package fundamentals_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"portfolio-analysis/services/fundamentals"
	"portfolio-analysis/services/market"
)

type mockBreakdownTransport struct {
	responses []mockBreakdownResp
	idx       int
}

type mockBreakdownResp struct {
	status  int
	body    string
	cookies []*http.Cookie
}

func (m *mockBreakdownTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

func buildBreakdownProvider(responses []mockBreakdownResp, rpm, rpd int) *fundamentals.YahooBreakdownProvider {
	transport := &mockBreakdownTransport{responses: responses}
	svc := market.NewYahooFinanceServiceWithTransport(transport)
	return fundamentals.NewYahooBreakdownProvider(svc, rpm, rpd)
}

func mockCrumbResponses() []mockBreakdownResp {
	return []mockBreakdownResp{
		{status: 200, body: "<html></html>", cookies: []*http.Cookie{{Name: "A3", Value: "ck"}}},
		{status: 200, body: "testcrumb"},
	}
}

func TestYahooBreakdownProvider_Metadata(t *testing.T) {
	provider := fundamentals.NewYahooBreakdownProvider(nil, 10, 100)
	assert.Equal(t, "Yahoo", provider.Name())
	assert.Equal(t, 10, provider.RateLimit().RequestsPerMinute)
	assert.Equal(t, 100, provider.RateLimit().RequestsPerDay)
}

func TestYahooBreakdownProvider_FetchETFBreakdown_Success(t *testing.T) {
	responseBody := `{
		"quoteSummary": {
			"result": [{
				"topHoldings": {
					"sectorWeightings": [
						{"technology":{"raw":0.25,"fmt":"25.00%"}},
						{"healthcare":{"raw":0.15,"fmt":"15.00%"}}
					],
					"countryWeightings": [
						{"us":{"raw":0.60,"fmt":"60.00%"}}
					]
				}
			}],
			"error": null
		}
	}`

	responses := append(mockCrumbResponses(), mockBreakdownResp{status: 200, body: responseBody})
	provider := buildBreakdownProvider(responses, 10, 100)

	data, err := provider.FetchETFBreakdown("URTH")
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.False(t, data.IsBondETF)
	assert.Nil(t, data.Duration)
	assert.Len(t, data.Rows, 3)

	byDimLabel := make(map[string]float64)
	for _, row := range data.Rows {
		assert.Equal(t, "URTH", row.FundSymbol)
		assert.Equal(t, "Yahoo", row.DataSource)
		byDimLabel[row.Dimension+":"+row.Label] = row.Weight
	}

	assert.Equal(t, 0.25, byDimLabel["sector:Technology"])
	assert.Equal(t, 0.15, byDimLabel["sector:Healthcare"])
	assert.Equal(t, 0.60, byDimLabel["country:United States"])
}

func TestYahooBreakdownProvider_FetchETFBreakdown_BondETF(t *testing.T) {
	responseBody := `{
		"quoteSummary": {
			"result": [{
				"topHoldings": {
					"bondPosition": {"raw":0.99},
					"stockPosition": {"raw":0.0},
					"bondHoldings": {"duration":{"raw":3.5}},
					"bondRatings": [
						{"aaa":{"raw":0.40}}
					]
				}
			}],
			"error": null
		}
	}`

	responses := append(mockCrumbResponses(), mockBreakdownResp{status: 200, body: responseBody})
	provider := buildBreakdownProvider(responses, 10, 100)

	data, err := provider.FetchETFBreakdown("BND")
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.True(t, data.IsBondETF)
	require.NotNil(t, data.Duration)
	assert.Equal(t, 3.5, *data.Duration)
	assert.Len(t, data.Rows, 1)
	assert.Equal(t, "bond_rating", data.Rows[0].Dimension)
	assert.Equal(t, "AAA", data.Rows[0].Label)
	assert.Equal(t, 0.40, data.Rows[0].Weight)
}

func TestYahooBreakdownProvider_FetchETFBreakdown_Empty(t *testing.T) {
	responseBody := `{"quoteSummary":{"result":[],"error":null}}`
	responses := append(mockCrumbResponses(), mockBreakdownResp{status: 200, body: responseBody})
	provider := buildBreakdownProvider(responses, 10, 100)

	data, err := provider.FetchETFBreakdown("EMPTY")
	require.NoError(t, err)
	assert.Nil(t, data)
}
