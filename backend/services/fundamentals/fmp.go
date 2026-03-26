package fundamentals

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gofolio-analysis/models"
)

// FMPProvider fetches asset fundamentals from FinancialModelingPrep's /stable/profile endpoint.
// Supports global equities. Rate limit defaults to free-tier (250/day, 10/min).
type FMPProvider struct {
	APIKey     string
	HTTPClient *http.Client
	rateLimit  RateLimitConfig
}

// NewFMPProvider creates a new FMP fundamentals provider.
func NewFMPProvider(apiKey string, rpm, rpd int) *FMPProvider {
	return &FMPProvider{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		rateLimit: RateLimitConfig{
			RequestsPerMinute: rpm,
			RequestsPerDay:    rpd,
			CooldownDuration:  5 * time.Minute,
		},
	}
}

// Name returns the provider identifier.
func (p *FMPProvider) Name() string { return "FMP" }

// RateLimit returns the configured rate limit.
func (p *FMPProvider) RateLimit() RateLimitConfig { return p.rateLimit }

type fmpProfileResponse struct {
	Symbol      string `json:"symbol"`
	CompanyName string `json:"companyName"`
	Exchange    string `json:"exchangeShortName"`
	Sector      string `json:"sector"`
	Country     string `json:"country"`
	IsEtf       bool   `json:"isEtf"`
	IsFund      bool   `json:"isFund"`
}

// FetchFundamentals fetches fundamentals for the given ticker from FMP /stable/profile.
// Returns nil, nil when the symbol is not found or the API key is absent.
func (p *FMPProvider) FetchFundamentals(symbol string) (*models.AssetFundamental, error) {
	if p.APIKey == "" {
		return nil, nil // no key configured; skip gracefully
	}

	url := fmt.Sprintf("https://financialmodelingprep.com/stable/profile?symbol=%s&apikey=%s", symbol, p.APIKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("fmp build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fmp request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("fmp: rate limit reached (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fmp: HTTP %d for %s", resp.StatusCode, symbol)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fmp read body: %w", err)
	}

	// FMP /stable/profile returns either an object or an array with one element.
	// Try array first, then single object.
	var profiles []fmpProfileResponse
	if err := json.Unmarshal(body, &profiles); err != nil || len(profiles) == 0 {
		var single fmpProfileResponse
		if jerr := json.Unmarshal(body, &single); jerr != nil || single.Symbol == "" {
			// Symbol not found — not an error, just nothing to store.
			return nil, nil
		}
		profiles = []fmpProfileResponse{single}
	}

	if len(profiles) == 0 || profiles[0].Symbol == "" {
		return nil, nil
	}

	prof := profiles[0]
	now := time.Now().UTC()

	assetType := "Stock"
	if prof.IsEtf {
		assetType = "ETF"
	} else if prof.IsFund {
		assetType = "Mutual Fund"
	}

	return &models.AssetFundamental{
		Symbol:      symbol,
		Name:        prof.CompanyName,
		AssetType:   assetType,
		Country:     emptyToUnknown(prof.Country),
		Sector:      emptyToUnknown(prof.Sector),
		Exchange:    prof.Exchange,
		DataSource:  "FMP",
		LastUpdated: now,
	}, nil
}

func emptyToUnknown(s string) string {
	if s == "" {
		return "Unknown"
	}
	return s
}
