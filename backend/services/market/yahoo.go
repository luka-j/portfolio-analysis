package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"portfolio-analysis/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Provider is the interface for fetching historical market data.
// This allows swapping in a mock for testing.
type Provider interface {
	GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error)
	// TradingDates returns the set of market-open dates in [from, to], ordered ascending.
	// Used by the portfolio service to skip non-trading days without forward-filling
	// price data across every calendar day.
	TradingDates(from, to time.Time) ([]time.Time, error)
	// GetLatestPrice returns the most recent price for symbol, trying a live/intraday
	// fetch first and falling back to the latest historical close.
	GetLatestPrice(symbol string, cachedOnly bool) (float64, error)
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
	limiter        *rate.Limiter // 5 requests/second for chart API
	summaryLimiter *rate.Limiter // 1 request/second for quoteSummary API (more restricted)
	crumbMgr       *CrumbManager

	// sfHistory, sfCurrent, sfQuoteType, sfCurrency collapse concurrent calls for
	// the same underlying request so that N parallel callers issue only one upstream
	// Yahoo fetch and share the result.
	sfHistory   singleflight.Group
	sfCurrent   singleflight.Group
	sfQuoteType singleflight.Group
	sfCurrency  singleflight.Group
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
		limiter:        rate.NewLimiter(rate.Limit(5), 8), // 5 req/s, burst of 8
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

// TradingDates returns all dates in [from, to] that have at least one real (non-dummy)
// market-data row, ordered ascending. Used by the portfolio service to avoid iterating
// every calendar day when computing daily portfolio values.
func (s *YahooFinanceService) TradingDates(from, to time.Time) ([]time.Time, error) {
	var dates []time.Time
	err := s.DB.Model(&models.MarketData{}).
		Where("date >= ? AND date <= ? AND volume != -1", from, to).
		Distinct("date").
		Order("date ASC").
		Pluck("date", &dates).Error
	return dates, err
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
// Concurrent callers for the same (symbol, from, to) are collapsed via singleflight
// so that only one upstream Yahoo fetch is issued and all callers share the result.
func (s *YahooFinanceService) GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error) {
	// Truncate dates to midnight for consistency.
	fromDate := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toDate := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)

	if cachedOnly {
		return s.loadCache(symbol, fromDate, toDate)
	}

	key := fmt.Sprintf("%s|%d|%d", symbol, fromDate.Unix(), toDate.Unix())
	v, err, _ := s.sfHistory.Do(key, func() (interface{}, error) {
		points, fetchErr := s.getHistoryUncached(symbol, fromDate, toDate)
		if fetchErr != nil {
			return nil, fetchErr
		}
		return points, nil
	})
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	return v.([]models.PricePoint), nil
}

// getHistoryUncached is the non-deduplicated body of GetHistory. It is only
// invoked via singleflight; callers must go through GetHistory.
func (s *YahooFinanceService) getHistoryUncached(symbol string, fromDate, toDate time.Time) ([]models.PricePoint, error) {

	firstDate, lastDate, hasCache, err := s.loadCacheBounds(symbol, fromDate, toDate)
	if err != nil {
		return nil, err
	}

	var missingRanges [][2]time.Time

	if !hasCache {
		missingRanges = append(missingRanges, [2]time.Time{fromDate, toDate})
	} else {
		// If the first cached date is > from + 5 days (weekends/holidays leeway)
		if firstDate.After(fromDate.AddDate(0, 0, 5)) {
			missingRanges = append(missingRanges, [2]time.Time{fromDate, firstDate.AddDate(0, 0, -1)})
		}

		// Don't request future dates from Yahoo
		now := time.Now().UTC()
		end := toDate
		if end.After(now) {
			end = now
		}

		// Trailing edge: optimistically fetch whenever the cache doesn't cover the
		// requested end date. If end falls on a weekend/holiday Yahoo will simply
		// return no new data, which is harmless — we keep what's already cached.
		if lastDate.Before(end) {
			missingRanges = append(missingRanges, [2]time.Time{lastDate.AddDate(0, 0, 1), end})
		}
	}

	var lastErr error
	for _, rng := range missingRanges {
		points, err := s.fetchFromYahoo(symbol, rng[0], rng[1])
		if err != nil {
			lastErr = err
			log.Printf("Fetching %s history [%s..%s]: %v",
				symbol, rng[0].Format("2006-01-02"), rng[1].Format("2006-01-02"), err)
			// Write negative-cache markers for genuinely unknown/delisted symbols so
			// we don't re-query the same hopeless range on every request.
			// Only do this when we have no prior data for this symbol — if we already
			// have cached history, a failing trailing-edge fetch is likely transient
			// (weekend, holiday, market not yet closed) and should not be cached.
			if !hasCache {
				if err.Error() == fmt.Sprintf("no data returned for symbol %s", symbol) ||
					err.Error() == "yahoo finance error: No data found, symbol may be delisted" ||
					err.Error() == fmt.Sprintf("yahoo returned HTTP 404 for %s", symbol) {
					if saveErr := s.saveCache(symbol, []models.PricePoint{
						{Date: rng[0], Volume: -1},
						{Date: rng[1], Volume: -1},
					}); saveErr != nil {
						log.Printf("Warning: saving negative cache for %s: %v", symbol, saveErr)
					}
				}
			}
			continue
		}

		if len(points) > 0 {
			// If points start long after the requested start (e.g. IPO), add a dummy at the request start.
			if points[0].Date.After(rng[0].AddDate(0, 0, 5)) {
				points = append(points, models.PricePoint{Date: rng[0], Volume: -1})
			}
			if saveErr := s.saveCache(symbol, points); saveErr != nil {
				log.Printf("Warning: saving %d price points for %s: %v", len(points), symbol, saveErr)
			}
		}
		// Empty successful response (no trading days in range) — skip silently.
		// This is normal for trailing-edge fetches that land on weekends/holidays.
	}

	cachedData, err := s.loadCache(symbol, fromDate, toDate)
	if len(cachedData) > 0 {
		return cachedData, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, err
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
// Concurrent calls for the same symbol are deduplicated via singleflight.
func (s *YahooFinanceService) GetQuoteType(symbol string) (string, string, error) {
	type qtResult struct {
		assetType string
		name      string
	}
	v, err, _ := s.sfQuoteType.Do(symbol, func() (interface{}, error) {
		at, name, err := s.getQuoteTypeUncached(symbol)
		if err != nil {
			return nil, err
		}
		return qtResult{at, name}, nil
	})
	if err != nil {
		return "", "", err
	}
	r := v.(qtResult)
	return r.assetType, r.name, nil
}

func (s *YahooFinanceService) getQuoteTypeUncached(symbol string) (string, string, error) {
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
// Concurrent calls for the same symbol are deduplicated via singleflight.
func (s *YahooFinanceService) GetCurrency(symbol string) (string, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	// DB cache check — skip when DB is nil (e.g. in unit tests).
	if s.DB != nil {
		var af models.AssetFundamental
		if err := s.DB.Select("currency").Where("symbol = ? AND currency != ''", symbol).First(&af).Error; err == nil {
			return af.Currency, nil
		}
	}

	v, err, _ := s.sfCurrency.Do(symbol, func() (interface{}, error) {
		cur, fetchErr := s.getCurrencyFromYahoo(symbol)
		if fetchErr != nil {
			return "", fetchErr
		}
		return cur, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (s *YahooFinanceService) getCurrencyFromYahoo(symbol string) (string, error) {
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
// it fetches fresh data from Yahoo. Concurrent fresh-fetches for the same symbol are
// collapsed via singleflight so that N parallel callers issue only one Yahoo request.
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

	v, err, _ := s.sfCurrent.Do(symbol, func() (interface{}, error) {
		// Re-check DB inside the flight in case a prior duplicate in-flight
		// call already populated it while we were queued.
		if s.DB != nil {
			var cp models.CurrentPrice
			if dbErr := s.DB.Where("symbol = ?", symbol).First(&cp).Error; dbErr == nil {
				if time.Since(cp.FetchedAt) < maxAge {
					if cp.Price == -1 {
						return float64(0), fmt.Errorf("no current price fetched for %s (negative cache)", symbol)
					}
					return cp.Price, nil
				}
			}
		}
		price, fetchErr := s.fetchCurrentPriceFromYahoo(symbol)
		if fetchErr != nil {
			return float64(0), fetchErr
		}
		return price, nil
	})
	if err != nil {
		return 0, err
	}
	return v.(float64), nil
}

func (s *YahooFinanceService) fetchCurrentPriceFromYahoo(symbol string) (float64, error) {
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

// GetLatestPrice returns the most recent price for symbol by trying the live
// intraday price first (via GetCurrentPrice) and falling back to the latest
// historical close from the last 5 days.
func (s *YahooFinanceService) GetLatestPrice(symbol string, cachedOnly bool) (float64, error) {
	p, err := s.GetCurrentPrice(symbol, cachedOnly)
	if err == nil && p > 0 {
		return p, nil
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	lookback := today.AddDate(0, 0, -5)

	prices, histErr := s.GetHistory(symbol, lookback, today, cachedOnly)
	if histErr == nil && len(prices) > 0 {
		last := prices[len(prices)-1]
		if last.AdjClose != 0 {
			return last.AdjClose, nil
		}
		if last.Close != 0 {
			return last.Close, nil
		}
	}

	// Return the original current-price error if history also failed.
	if err != nil {
		return 0, err
	}
	if histErr != nil {
		return 0, histErr
	}
	return 0, fmt.Errorf("no price data for %s", symbol)
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

// loadCacheBounds returns the first and last dates of cached rows (including dummy
// negative-cache markers) for symbol in [from, to]. Returns hasData=false when no
// rows exist. The dates guide missing-range detection without loading full OHLCV data.
func (s *YahooFinanceService) loadCacheBounds(symbol string, from, to time.Time) (first, last time.Time, hasData bool, err error) {
	var dates []time.Time
	err = s.DB.Model(&models.MarketData{}).
		Select("date").
		Where("symbol = ? AND date >= ? AND date <= ?", symbol, from, to).
		Order("date ASC").
		Pluck("date", &dates).Error
	if err != nil || len(dates) == 0 {
		return
	}
	return dates[0], dates[len(dates)-1], true, nil
}

// loadCache returns real (non-dummy) price points for symbol in [from, to],
// scanning directly into []models.PricePoint without an intermediate MarketData allocation.
func (s *YahooFinanceService) loadCache(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	var points []models.PricePoint
	err := s.DB.Model(&models.MarketData{}).
		Select("date, open, high, low, close, adj_close, volume").
		Where("symbol = ? AND date >= ? AND date <= ? AND volume != -1", symbol, from, to).
		Order("date ASC").
		Scan(&points).Error
	return points, err
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
