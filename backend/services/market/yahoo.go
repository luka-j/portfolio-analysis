package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"portfolio-analysis/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Provider is the interface for fetching historical market data.
// This allows swapping in a mock for testing.
type Provider interface {
	GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error)
}

// CurrentPriceProvider returns a live or recently-cached market price for a symbol.
// If the stored price is older than 5 minutes a fresh fetch is attempted.
type CurrentPriceProvider interface {
	GetCurrentPrice(symbol string, cachedOnly bool) (float64, error)
}

// CurrencyGetter can report the native trading currency of a symbol.
type CurrencyGetter interface {
	GetCurrency(symbol string) (string, error)
}

// PriceStatusChecker can report whether any cached market data exists for a symbol,
// used to distinguish "symbol not found" from "had data but now stale".
type PriceStatusChecker interface {
	HasCachedData(symbol string) bool
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
	transport := &http.Transport{
		IdleConnTimeout:     10 * time.Second,
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 5,
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
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

// HasCachedData reports whether any market data rows exist for the given symbol,
// regardless of date. Used to distinguish "never seen this symbol" from "had data but now stale".
func (s *YahooFinanceService) HasCachedData(symbol string) bool {
	if s.DB == nil {
		return false
	}
	var count int64
	s.DB.Model(&models.MarketData{}).Where("symbol = ? AND volume != -1", symbol).Count(&count)
	return count > 0
}

// GetHistory returns daily price data for the symbol in [from, to].
func (s *YahooFinanceService) GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error) {
	// Truncate dates to midnight for consistency.
	fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toDate := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)

	// Fetch raw cache models to include dummy cache points
	dbData, err := s.loadRawCache(symbol, fromDate, toDate)
	if err != nil {
		return nil, err
	}

	if cachedOnly {
		// Just return what we have in DB, no external fetch.
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
		return cachedData, nil
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
				err.Error() == "yahoo finance error: No data found, symbol may be delisted" ||
				err.Error() == fmt.Sprintf("yahoo returned HTTP 404 for %s", symbol) {
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

	start := time.Now()
	resp, err := s.HTTPClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		observeYahooRequest("chart", "quote_type", 0, elapsed)
		return "", "", fmt.Errorf("yahoo quoteType request: %w", err)
	}
	defer resp.Body.Close()
	observeYahooRequest("chart", "quote_type", resp.StatusCode, elapsed)

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
// It checks asset_fundamentals first; on a miss it fetches from Yahoo and persists the result.
// Returns an empty string when the symbol is not found on Yahoo.
func (s *YahooFinanceService) GetCurrency(symbol string) (string, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	// DB cache check — skip when DB is nil (e.g. in unit tests).
	if s.DB != nil {
		var af models.AssetFundamental
		if err := s.DB.Select("currency").Where("symbol = ? AND currency != ''", symbol).First(&af).Error; err == nil {
			return af.Currency, nil
		}
	}

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

	start := time.Now()
	resp, err := s.HTTPClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		observeYahooRequest("chart", "currency", 0, elapsed)
		return "", fmt.Errorf("yahoo currency request: %w", err)
	}
	defer resp.Body.Close()
	observeYahooRequest("chart", "currency", resp.StatusCode, elapsed)

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
	currency := yResp.Chart.Result[0].Meta.Currency

	// Persist so subsequent calls are served from DB.
	if s.DB != nil && currency != "" {
		s.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "symbol"}},
			DoUpdates: clause.Assignments(map[string]interface{}{"currency": currency}),
		}).Create(&models.AssetFundamental{
			Symbol:      symbol,
			Currency:    currency,
			LastUpdated: time.Now().UTC(),
		})
	}

	return currency, nil
}

// GetCurrentPrice returns the current market price for symbol.
// It checks the database first; if cachedOnly is false and stored price is older than 5 minutes
// it fetches fresh data from Yahoo.
func (s *YahooFinanceService) GetCurrentPrice(symbol string, cachedOnly bool) (float64, error) {
	const maxAge = 5 * time.Minute

	if s.DB != nil {
		var cp models.CurrentPrice
		if err := s.DB.Where("symbol = ?", symbol).First(&cp).Error; err == nil {
			if cachedOnly || time.Since(cp.FetchedAt) < maxAge {
				if cp.Price == -1 {
					return 0, fmt.Errorf("no current price fetched for %s (negative cache)", symbol)
				}
				return cp.Price, nil
			}
		}
	}

	if cachedOnly {
		// FALLBACK: if no current price entry, try to get the latest close from history.
		var md models.MarketData
		if err := s.DB.Where("symbol = ?", symbol).Order("date DESC").First(&md).Error; err == nil {
			return md.AdjClose, nil
		}
		return 0, fmt.Errorf("no cached price for %s", symbol)
	}

	if err := s.limiter.Wait(context.Background()); err != nil {
		return 0, fmt.Errorf("rate limiter: %w", err)
	}

	chartURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d",
		url.PathEscape(symbol),
	)
	req, err := http.NewRequest("GET", chartURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", yahooUserAgent)

	start := time.Now()
	resp, err := s.HTTPClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		observeYahooRequest("chart", "current_price", 0, elapsed)
		return 0, fmt.Errorf("yahoo current price request: %w", err)
	}
	defer resp.Body.Close()
	observeYahooRequest("chart", "current_price", resp.StatusCode, elapsed)

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			if s.DB != nil {
				s.DB.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "symbol"}},
					DoUpdates: clause.AssignmentColumns([]string{"price", "fetched_at"}),
				}).Create(&models.CurrentPrice{
					Symbol:    symbol,
					Price:     -1,
					FetchedAt: time.Now().UTC(),
				})
			}
			return 0, fmt.Errorf("yahoo current price HTTP 404 for %s", symbol)
		}
		return 0, fmt.Errorf("yahoo current price HTTP %d for %s", resp.StatusCode, symbol)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading yahoo current price response: %w", err)
	}

	var yResp yahooChartResponse
	if err := json.Unmarshal(body, &yResp); err != nil {
		return 0, fmt.Errorf("parsing yahoo current price: %w", err)
	}
	if yResp.Chart.Error != nil || len(yResp.Chart.Result) == 0 {
		return 0, fmt.Errorf("no data for symbol %s", symbol)
	}

	price := yResp.Chart.Result[0].Meta.RegularMarketPrice
	if price == 0 {
		return 0, fmt.Errorf("zero price returned for %s", symbol)
	}

	if s.DB != nil {
		s.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "symbol"}},
			DoUpdates: clause.AssignmentColumns([]string{"price", "fetched_at"}),
		}).Create(&models.CurrentPrice{
			Symbol:    symbol,
			Price:     price,
			FetchedAt: time.Now().UTC(),
		})
	}

	return price, nil
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

		start := time.Now()
		resp, err := s.HTTPClient.Do(req)
		elapsed := time.Since(start)
		if err != nil {
			observeYahooRequest("chart", "history", 0, elapsed)
			lastErr = fmt.Errorf("yahoo finance request: %w", err)
			continue
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		observeYahooRequest("chart", "history", resp.StatusCode, elapsed)
		if err != nil {
			lastErr = fmt.Errorf("reading yahoo response: %w", err)
			continue
		}

		// Retry on 429 (rate limited) or 5xx server errors.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("yahoo returned HTTP %d for %s", resp.StatusCode, symbol)
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			lastErr = fmt.Errorf("yahoo returned HTTP 404 for %s", symbol)
			break
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
