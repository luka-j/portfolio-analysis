package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"
)

const yahooUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.3856.72"

// ETFBreakdownResult holds one dimension/label/weight triple from Yahoo quoteSummary.
type ETFBreakdownResult struct {
	Dimension string  // "sector" or "country"
	Label     string  // e.g. "Technology", "United States"
	Weight    float64 // fraction 0.0–1.0
}

// CrumbManager handles Yahoo Finance cookie+crumb authentication required by the
// v10 quoteSummary endpoint. It maintains its own http.Client with a cookie jar so
// that cookies are automatically captured across redirects (e.g. consent pages) and
// sent on subsequent requests without any manual cookie passing.
type CrumbManager struct {
	mu     sync.Mutex
	client *http.Client // dedicated client with cookie jar
	crumb  string
	expiry time.Time
}

func newCrumbManager(base *http.Client) *CrumbManager {
	jar, _ := cookiejar.New(nil)
	return &CrumbManager{
		client: &http.Client{
			Timeout:   base.Timeout,
			Transport: base.Transport,
			Jar:       jar,
		},
	}
}

// getCrumb returns a valid crumb, refreshing cookies and crumb if stale or expired.
func (cm *CrumbManager) getCrumb() (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if time.Now().Before(cm.expiry) && cm.crumb != "" {
		return cm.crumb, nil
	}
	if err := cm.fetchCrumb(); err != nil {
		return "", err
	}
	return cm.crumb, nil
}

// forceRefresh invalidates the cached crumb, forcing the next call to re-fetch.
func (cm *CrumbManager) forceRefresh() {
	cm.mu.Lock()
	// Reset the cookie jar so stale session cookies are discarded.
	jar, _ := cookiejar.New(nil)
	cm.client.Jar = jar
	cm.expiry = time.Time{}
	cm.mu.Unlock()
}

// fetchCrumb seeds the cookie jar and retrieves the crumb. Must be called with mu held.
//
// Seeding from fc.yahoo.com sets .yahoo.com-scoped cookies without triggering the
// GDPR/consent redirect that finance.yahoo.com uses in some regions. Cookies set
// during a consent redirect are scoped to consent.yahoo.com and are rejected by the
// crumb endpoint on query2.finance.yahoo.com.
func (cm *CrumbManager) fetchCrumb() error {
	// Step 1: Seed the cookie jar from fc.yahoo.com. This sets .yahoo.com cookies
	// without a consent redirect, so the jar is populated with the right domain scope.
	seedReq, err := http.NewRequest("GET", "https://fc.yahoo.com/", nil)
	if err != nil {
		return fmt.Errorf("crumb seed request: %w", err)
	}
	seedReq.Header.Set("User-Agent", yahooUserAgent)
	seedReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	seedReq.Header.Set("Accept-Language", "en-US,en;q=0.5")

	seedResp, err := cm.client.Do(seedReq)
	if err != nil {
		return fmt.Errorf("crumb seed fetch: %w", err)
	}
	io.Copy(io.Discard, seedResp.Body)
	seedResp.Body.Close()

	// Step 2: Fetch the crumb. The jar sends .yahoo.com cookies automatically.
	// Use query2 which is slightly more permissive than query1 for crumb issuance.
	crumbReq, err := http.NewRequest("GET", "https://query2.finance.yahoo.com/v1/test/getcrumb", nil)
	if err != nil {
		return fmt.Errorf("crumb request: %w", err)
	}
	crumbReq.Header.Set("User-Agent", yahooUserAgent)

	crumbResp, err := cm.client.Do(crumbReq)
	if err != nil {
		return fmt.Errorf("crumb fetch: %w", err)
	}
	defer crumbResp.Body.Close()

	body, err := io.ReadAll(crumbResp.Body)
	if err != nil {
		return fmt.Errorf("reading crumb: %w", err)
	}

	crumb := strings.TrimSpace(string(body))
	if crumb == "" || crumbResp.StatusCode != http.StatusOK {
		return fmt.Errorf("crumb endpoint returned status %d with body %q", crumbResp.StatusCode, crumb)
	}

	cm.crumb = crumb
	cm.expiry = time.Now().Add(50 * time.Minute) // crumbs last ~1h; refresh early
	return nil
}

// ETFSummaryResult is the full result from a Yahoo quoteSummary fetch.
type ETFSummaryResult struct {
	Breakdowns []ETFBreakdownResult // sector, country, and bond_rating entries
	IsBondETF  bool                 // true when bondPosition >= 50 %
	Duration   float64              // effective duration in years; 0 if not available
}

// ---------- Yahoo quoteSummary response types ----------

type yahooRaw struct{ Raw float64 `json:"raw"` }

type yahooSummaryResponse struct {
	QuoteSummary struct {
		Result []struct {
			TopHoldings struct {
				BondPosition  yahooRaw `json:"bondPosition"`
				StockPosition yahooRaw `json:"stockPosition"`
				BondHoldings  struct {
					Duration yahooRaw `json:"duration"`
				} `json:"bondHoldings"`
				BondRatings []map[string]yahooRaw `json:"bondRatings"`
				SectorWeightings []map[string]yahooRaw `json:"sectorWeightings"`
				CountryWeightings []map[string]yahooRaw `json:"countryWeightings"`
			} `json:"topHoldings"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

// yahooSectorLabels maps Yahoo's sector slug keys to display names.
var yahooSectorLabels = map[string]string{
	"realestate":             "Real Estate",
	"consumer_cyclical":      "Consumer Cyclical",
	"basic_materials":        "Basic Materials",
	"consumer_defensive":     "Consumer Defensive",
	"technology":             "Technology",
	"communication_services": "Communication Services",
	"financial_services":     "Financial Services",
	"utilities":              "Utilities",
	"industrials":            "Industrials",
	"energy":                 "Energy",
	"healthcare":             "Healthcare",
}

// sectorLabel converts a Yahoo sector slug to a display label.
func sectorLabel(key string) string {
	if label, ok := yahooSectorLabels[key]; ok {
		return label
	}
	// Fallback: replace underscores, title-case each word.
	words := strings.Split(strings.ReplaceAll(key, "_", " "), " ")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// countryLabel converts a Yahoo country key to a display label.
func countryLabel(key string) string {
	if label, ok := yahooCountryLabels[key]; ok {
		return label
	}
	words := strings.Split(strings.ReplaceAll(key, "_", " "), " ")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// yahooCountryLabels maps Yahoo's country keys to display names.
var yahooCountryLabels = map[string]string{
	"us":    "United States",
	"gb":    "United Kingdom",
	"jp":    "Japan",
	"de":    "Germany",
	"fr":    "France",
	"ca":    "Canada",
	"ch":    "Switzerland",
	"au":    "Australia",
	"cn":    "China",
	"hk":    "Hong Kong",
	"kr":    "South Korea",
	"in":    "India",
	"nl":    "Netherlands",
	"se":    "Sweden",
	"dk":    "Denmark",
	"no":    "Norway",
	"fi":    "Finland",
	"es":    "Spain",
	"it":    "Italy",
	"br":    "Brazil",
	"tw":    "Taiwan",
	"sg":    "Singapore",
	"be":    "Belgium",
	"at":    "Austria",
	"ie":    "Ireland",
	"nz":    "New Zealand",
	"mx":    "Mexico",
	"za":    "South Africa",
	"il":    "Israel",
	"pt":    "Portugal",
	"pl":    "Poland",
	"cz":    "Czech Republic",
	"hu":    "Hungary",
	"ru":    "Russia",
	"sa":    "Saudi Arabia",
	"ae":    "United Arab Emirates",
	"th":    "Thailand",
	"id":    "Indonesia",
	"my":    "Malaysia",
	"ph":    "Philippines",
	"cl":    "Chile",
	"co":    "Colombia",
	"pe":    "Peru",
	"eg":    "Egypt",
	"tr":    "Turkey",
	"gr":    "Greece",
	"ro":    "Romania",
	"lu":    "Luxembourg",
	"other": "Other",
}

// yahooRatingLabels maps Yahoo bond rating keys to display labels in descending quality order.
var yahooRatingLabels = map[string]string{
	"aaa":          "AAA",
	"aa":           "AA",
	"a":            "A",
	"bbb":          "BBB",
	"bb":           "BB",
	"b":            "B",
	"below_b":      "Below B",
	"us_government": "US Government",
	"other":        "Other",
}

func bondRatingLabel(key string) string {
	if label, ok := yahooRatingLabels[key]; ok {
		return label
	}
	return strings.ToUpper(key)
}

// GetETFBreakdown fetches aggregate sector/country/bond-rating weights and bond metadata
// for an ETF from Yahoo Finance's quoteSummary endpoint.
// Returns nil, nil when no data is available.
func (s *YahooFinanceService) GetETFBreakdown(symbol string) (*ETFSummaryResult, error) {
	if err := s.summaryLimiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("summary rate limiter: %w", err)
	}

	crumb, err := s.crumbMgr.getCrumb()
	if err != nil {
		return nil, fmt.Errorf("yahoo crumb: %w", err)
	}

	result, err := s.doQuoteSummary(symbol, crumb)
	if err != nil {
		// On auth failure, refresh crumb and retry once.
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			s.crumbMgr.forceRefresh()
			if err2 := s.summaryLimiter.Wait(context.Background()); err2 != nil {
				return nil, fmt.Errorf("summary rate limiter retry: %w", err2)
			}
			crumb, err2 := s.crumbMgr.getCrumb()
			if err2 != nil {
				return nil, fmt.Errorf("yahoo crumb refresh: %w", err2)
			}
			result, err = s.doQuoteSummary(symbol, crumb)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return result, nil
}

func (s *YahooFinanceService) doQuoteSummary(symbol, crumb string) (*ETFSummaryResult, error) {
	url := fmt.Sprintf(
		"https://query2.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=topHoldings,fundProfile&crumb=%s",
		symbol, crumb,
	)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", yahooUserAgent)
	req.Header.Set("Accept", "application/json")

	// Use the crumb manager's jar-enabled client so session cookies are sent automatically.
	resp, err := s.crumbMgr.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("quoteSummary request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("quoteSummary rate limit 429 for %s", symbol)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("quoteSummary auth error %d for %s", resp.StatusCode, symbol)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("quoteSummary HTTP %d for %s", resp.StatusCode, symbol)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading quoteSummary: %w", err)
	}

	var yResp yahooSummaryResponse
	if err := json.Unmarshal(body, &yResp); err != nil {
		return nil, fmt.Errorf("parsing quoteSummary: %w", err)
	}

	if yResp.QuoteSummary.Error != nil {
		return nil, nil // symbol not found or no data — not an error
	}
	if len(yResp.QuoteSummary.Result) == 0 {
		return nil, nil
	}

	topHoldings := yResp.QuoteSummary.Result[0].TopHoldings

	isBondETF := topHoldings.BondPosition.Raw >= 0.5
	duration := topHoldings.BondHoldings.Duration.Raw

	var breakdowns []ETFBreakdownResult

	if isBondETF {
		for _, ratingMap := range topHoldings.BondRatings {
			for key, val := range ratingMap {
				if val.Raw <= 0 {
					continue
				}
				breakdowns = append(breakdowns, ETFBreakdownResult{
					Dimension: "bond_rating",
					Label:     bondRatingLabel(key),
					Weight:    val.Raw,
				})
			}
		}
	} else {
		for _, sectorMap := range topHoldings.SectorWeightings {
			for key, val := range sectorMap {
				if val.Raw <= 0 {
					continue
				}
				breakdowns = append(breakdowns, ETFBreakdownResult{
					Dimension: "sector",
					Label:     sectorLabel(key),
					Weight:    val.Raw,
				})
			}
		}
		for _, countryMap := range topHoldings.CountryWeightings {
			for key, val := range countryMap {
				if val.Raw <= 0 {
					continue
				}
				breakdowns = append(breakdowns, ETFBreakdownResult{
					Dimension: "country",
					Label:     countryLabel(key),
					Weight:    val.Raw,
				})
			}
		}
	}

	if len(breakdowns) == 0 {
		return nil, nil
	}
	return &ETFSummaryResult{
		Breakdowns: breakdowns,
		IsBondETF:  isBondETF,
		Duration:   duration,
	}, nil
}
