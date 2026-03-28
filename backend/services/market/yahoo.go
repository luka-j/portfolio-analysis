package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	"golang.org/x/time/rate"

	"gofolio-analysis/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Provider is the interface for fetching historical market data.
// This allows swapping in a mock for testing.
type Provider interface {
	GetHistory(symbol string, from, to time.Time) ([]models.PricePoint, error)
}

// CurrencyGetter can report the native trading currency of a symbol.
type CurrencyGetter interface {
	GetCurrency(symbol string) (string, error)
}

// YahooFinanceService fetches historical OHLCV data from Yahoo Finance
// and caches results in the database.
type YahooFinanceService struct {
	DB             *gorm.DB
	HTTPClient     *http.Client
	limiter        *rate.Limiter // 3 requests/second for chart API
	summaryLimiter *rate.Limiter // 1 request/second for quoteSummary API (more restricted)
	crumbMgr       *CrumbManager
}

// NewYahooFinanceService creates a new Yahoo Finance service backed by DB caching.
func NewYahooFinanceService(db *gorm.DB) *YahooFinanceService {
	client := &http.Client{Timeout: 30 * time.Second}
	return &YahooFinanceService{
		DB:             db,
		HTTPClient:     client,
		limiter:        rate.NewLimiter(rate.Limit(3), 5), // 3 req/s, burst of 5
		summaryLimiter: rate.NewLimiter(rate.Limit(1), 2), // 1 req/s, burst of 2
		crumbMgr:       newCrumbManager(client),
	}
}

// NewYahooFinanceServiceWithTransport creates a YahooFinanceService with a custom
// HTTP transport. Intended for tests that need to inject a mock transport.
func NewYahooFinanceServiceWithTransport(transport http.RoundTripper) *YahooFinanceService {
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return &YahooFinanceService{
		HTTPClient:     client,
		limiter:        rate.NewLimiter(rate.Inf, 1),
		summaryLimiter: rate.NewLimiter(rate.Inf, 1),
		crumbMgr:       newCrumbManager(client),
	}
}

// GetHistory returns daily price data for the symbol in [from, to].
func (s *YahooFinanceService) GetHistory(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	// Truncate dates to midnight for consistency.
	fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toDate := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)

	// Fetch raw cache models to include dummy cache points
	dbData, err := s.loadRawCache(symbol, fromDate, toDate)
	if err != nil {
		return nil, err
	}

	var missingRanges [][2]time.Time

	if len(dbData) == 0 {
		missingRanges = append(missingRanges, [2]time.Time{fromDate, toDate})
	} else {
		first := dbData[0].Date
		last := dbData[len(dbData)-1].Date

		// If the first cached date is > from + 5 days (weekends/holidays leeway)
		if first.After(fromDate.AddDate(0, 0, 5)) {
			missingRanges = append(missingRanges, [2]time.Time{fromDate, first.AddDate(0, 0, -1)})
		}

		// Don't request future dates from Yahoo
		now := time.Now().UTC()
		end := toDate
		if end.After(now) {
			end = now
		}

		if last.Before(end.AddDate(0, 0, -5)) {
			missingRanges = append(missingRanges, [2]time.Time{last.AddDate(0, 0, 1), end})
		}
	}

	fetchedAny := false
	var lastErr error
	for _, rng := range missingRanges {
		points, err := s.fetchFromYahoo(symbol, rng[0], rng[1])
		if err != nil {
			lastErr = err
			// Insert a dummy marker at the start of the failing range to prevent future queries.
			// "No data returned" means Yahoo doesn't know the ticker or date (e.g. pre-IPO).
			if err.Error() == fmt.Sprintf("no data returned for symbol %s", symbol) ||
				err.Error() == "yahoo finance error: No data found, symbol may be delisted" {
				_ = s.saveCache(symbol, []models.PricePoint{
					{Date: rng[0], Volume: -1},
					{Date: rng[1], Volume: -1},
				})
			}
			continue
		}

		if len(points) > 0 {
			// If points start long after the requested start (e.g. IPO), add a dummy at the request start.
			if points[0].Date.After(rng[0].AddDate(0, 0, 5)) {
				points = append(points, models.PricePoint{Date: rng[0], Volume: -1})
			}
			_ = s.saveCache(symbol, points)
			fetchedAny = true
		} else {
			// Yahoo returned success but empty array
			_ = s.saveCache(symbol, []models.PricePoint{
				{Date: rng[0], Volume: -1},
				{Date: rng[1], Volume: -1},
			})
		}
	}

	// Read again to get the final merged data (filtered of dummies)
	if fetchedAny || len(dbData) == 0 {
		cachedData, err := s.loadCache(symbol, fromDate, toDate)
		if len(cachedData) > 0 {
			return cachedData, nil
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, err
	}

	// If we didn't fetch any new real data, just map the dbData we already queried
	var cachedData []models.PricePoint
	for _, m := range dbData {
		if m.Volume == -1 {
			continue // skip negative cache markers
		}
		cachedData = append(cachedData, models.PricePoint{
			Date:     m.Date,
			Open:     m.Open,
			High:     m.High,
			Low:      m.Low,
			Close:    m.Close,
			AdjClose: m.AdjClose,
			Volume:   m.Volume,
		})
	}

	if len(cachedData) > 0 {
		return cachedData, nil
	}
	return nil, lastErr
}

// ---------- Yahoo Finance v8 chart API ----------

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				QuoteType string `json:"quoteType"`
				LongName  string `json:"longName"`
				Currency  string `json:"currency"`
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
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// GetQuoteType returns the Yahoo Finance asset class and display name for a symbol.
// The asset type is mapped to our internal type string ("Stock", "ETF", "Commodity", or "" if unknown).
// The name is taken from meta.longName and may be empty. Uses a minimal 1-day request — no premium quota.
func (s *YahooFinanceService) GetQuoteType(symbol string) (string, string, error) {
	if err := s.limiter.Wait(context.Background()); err != nil {
		return "", "", fmt.Errorf("rate limiter: %w", err)
	}

	chartURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d",
		url.PathEscape(symbol),
	)
	req, err := http.NewRequest("GET", chartURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", yahooUserAgent)

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("yahoo quoteType request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("yahoo quoteType HTTP %d for %s", resp.StatusCode, symbol)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading yahoo quoteType response: %w", err)
	}

	var yResp yahooChartResponse
	if err := json.Unmarshal(body, &yResp); err != nil {
		return "", "", fmt.Errorf("parsing yahoo quoteType: %w", err)
	}
	if yResp.Chart.Error != nil || len(yResp.Chart.Result) == 0 {
		return "", "", nil // symbol not found on Yahoo
	}
	meta := yResp.Chart.Result[0].Meta
	return yahooQuoteTypeToAssetType(meta.QuoteType), meta.LongName, nil
}

// yahooQuoteTypeToAssetType maps Yahoo's quoteType field to our internal AssetType strings.
func yahooQuoteTypeToAssetType(qt string) string {
	switch qt {
	case "EQUITY":
		return "Stock"
	case "ETF", "MUTUALFUND":
		return "ETF"
	case "FUTURE":
		return "Commodity"
	default:
		return ""
	}
}

// GetCurrency returns the native trading currency for a symbol (e.g. "USD", "EUR").
// Returns an empty string when the symbol is not found on Yahoo.
func (s *YahooFinanceService) GetCurrency(symbol string) (string, error) {
	if err := s.limiter.Wait(context.Background()); err != nil {
		return "", fmt.Errorf("rate limiter: %w", err)
	}

	chartURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d",
		url.PathEscape(symbol),
	)
	req, err := http.NewRequest("GET", chartURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", yahooUserAgent)

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("yahoo currency request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("yahoo currency HTTP %d for %s", resp.StatusCode, symbol)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading yahoo currency response: %w", err)
	}

	var yResp yahooChartResponse
	if err := json.Unmarshal(body, &yResp); err != nil {
		return "", fmt.Errorf("parsing yahoo currency: %w", err)
	}
	if yResp.Chart.Error != nil || len(yResp.Chart.Result) == 0 {
		return "", nil
	}
	return yResp.Chart.Result[0].Meta.Currency, nil
}

func (s *YahooFinanceService) fetchFromYahoo(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	// Rate-limit: wait for a token before making the request.
	if err := s.limiter.Wait(context.Background()); err != nil {
		return nil, fmt.Errorf("rate limiter cancelled: %w", err)
	}

	chartURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d&includeAdjustedClose=true",
		url.PathEscape(symbol), from.Unix(), to.Add(24*time.Hour).Unix(),
	)

	var body []byte
	var lastErr error

	// Retry up to 3 times with exponential back-off on transient errors.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * 2 * time.Second
			time.Sleep(backoff)
			// Consume another rate-limit token for the retry.
			if err := s.limiter.Wait(context.Background()); err != nil {
				return nil, fmt.Errorf("rate limiter cancelled: %w", err)
			}
		}

		req, err := http.NewRequest("GET", chartURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", yahooUserAgent)

		resp, err := s.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("yahoo finance request: %w", err)
			continue
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("reading yahoo response: %w", err)
			continue
		}

		// Retry on 429 (rate limited) or 5xx server errors.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("yahoo returned HTTP %d for %s", resp.StatusCode, symbol)
			continue
		}

		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, lastErr
	}

	var yResp yahooChartResponse
	if err := json.Unmarshal(body, &yResp); err != nil {
		return nil, fmt.Errorf("parsing yahoo response: %w", err)
	}

	if yResp.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo finance error: %s", yResp.Chart.Error.Description)
	}

	if len(yResp.Chart.Result) == 0 {
		return nil, fmt.Errorf("no data returned for symbol %s", symbol)
	}

	result := yResp.Chart.Result[0]
	quotes := result.Indicators.Quote
	if len(quotes) == 0 {
		return nil, fmt.Errorf("no quotes for %s", symbol)
	}

	q := quotes[0]
	var adjCloses []float64
	if len(result.Indicators.AdjClose) > 0 {
		adjCloses = result.Indicators.AdjClose[0].AdjClose
	}

	var points []models.PricePoint
	for i, ts := range result.Timestamp {
		t := time.Unix(ts, 0).UTC()
		pp := models.PricePoint{
			Date: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC),
		}
		if i < len(q.Open) {
			pp.Open = q.Open[i]
		}
		if i < len(q.High) {
			pp.High = q.High[i]
		}
		if i < len(q.Low) {
			pp.Low = q.Low[i]
		}
		if i < len(q.Close) {
			pp.Close = q.Close[i]
		}
		if i < len(q.Volume) {
			pp.Volume = q.Volume[i]
		}
		if i < len(adjCloses) {
			pp.AdjClose = adjCloses[i]
		} else {
			pp.AdjClose = pp.Close
		}

		// Yahoo finance sometimes includes timestamps with null arrays right before holidays or when trading is halted
		// Since Go's encoding/json parses float nulls in an array as 0, this causes our portfolio code to see a massive zero drop.
		if pp.Close == 0 {
			continue
		}

		points = append(points, pp)
	}

	sort.Slice(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) })
	return points, nil
}

// ---------- Database cache ----------

func (s *YahooFinanceService) loadRawCache(symbol string, from, to time.Time) ([]models.MarketData, error) {
	var dbData []models.MarketData
	err := s.DB.Where("symbol = ? AND date >= ? AND date <= ?", symbol, from, to).Order("date ASC").Find(&dbData).Error
	return dbData, err
}

func (s *YahooFinanceService) loadCache(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	dbData, err := s.loadRawCache(symbol, from, to)
	if err != nil {
		return nil, err
	}

	var points []models.PricePoint
	for _, m := range dbData {
		if m.Volume == -1 {
			continue // skip dummy records used for negative caching
		}
		points = append(points, models.PricePoint{
			Date:     m.Date,
			Open:     m.Open,
			High:     m.High,
			Low:      m.Low,
			Close:    m.Close,
			AdjClose: m.AdjClose,
			Volume:   m.Volume,
		})
	}
	return points, nil
}

func (s *YahooFinanceService) saveCache(symbol string, points []models.PricePoint) error {
	if len(points) == 0 {
		return nil
	}

	var batch []models.MarketData
	seen := make(map[string]bool)

	for _, p := range points {
		ds := p.Date.Format("2006-01-02")
		if seen[ds] {
			continue
		}
		seen[ds] = true

		batch = append(batch, models.MarketData{
			Symbol:   symbol,
			Date:     p.Date,
			Open:     p.Open,
			High:     p.High,
			Low:      p.Low,
			Close:    p.Close,
			AdjClose: p.AdjClose,
			Volume:   p.Volume,
		})
	}

	// Use GORM Clauses for a batch UPSERT.
	// This prevents "duplicate key value violates unique constraint" by updating the row if it exists.
	err := s.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "symbol"}, {Name: "date"}},
		DoUpdates: clause.AssignmentColumns([]string{"open", "high", "low", "close", "adj_close", "volume"}),
	}).Create(&batch).Error

	return err
}
