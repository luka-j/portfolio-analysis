package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/stats"
)

// StatsHandler handles portfolio statistics and comparison endpoints.
type StatsHandler struct {
	ScenarioMiddleware
	Repo             *flexquery.Repository
	PortfolioService *portfolio.Service
	MarketProvider   market.Provider
	FXService        *fx.Service
	CurrencyGetter   market.CurrencyGetter
}

// NewStatsHandler creates a new StatsHandler.
func NewStatsHandler(repo *flexquery.Repository, ps *portfolio.Service, mp market.Provider, fxSvc *fx.Service, cg market.CurrencyGetter) *StatsHandler {
	return &StatsHandler{
		Repo:             repo,
		PortfolioService: ps,
		MarketProvider:   mp,
		FXService:        fxSvc,
		CurrencyGetter:   cg,
	}
}

// GetStats handles GET /api/v1/portfolio/stats
func (h *StatsHandler) GetStats(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Constrain 'from' date to the portfolio's inception date
	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	if from.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data to compute statistics"})
		return
	}

	// Get daily values and cash flows for the calculations.
	hist, err := h.PortfolioService.GetDailyValues(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cashFlows, err := h.PortfolioService.GetCashFlows(data, currency, acctModel, cachedOnly, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var twrCashFlows []models.CashFlow
	var mwrCashFlows []models.CashFlow

	if len(hist.Data) > 0 {
		startValue := hist.Data[0].Value
		if startValue > 0 {
			// A synthetic deposit represents incoming capital for the isolated start of the period for MWR only.
			mwrCashFlows = append(mwrCashFlows, models.CashFlow{
				Date:   from,
				Amount: -startValue,
			})
		}
	}

	actualFromStr := ""
	actualToStr := ""
	if len(hist.Data) > 0 {
		actualFromStr = hist.Data[0].Date
		actualToStr = hist.Data[len(hist.Data)-1].Date
	}

	for _, cf := range cashFlows {
		cfDateStr := cf.Date.Format("2006-01-02")
		// Real cashflows are scoped precisely to the time frame covered by the priced history data
		if actualToStr != "" {
			if cfDateStr > actualFromStr && cfDateStr <= actualToStr {
				twrCashFlows = append(twrCashFlows, cf)
				mwrCashFlows = append(mwrCashFlows, cf)
			}
		}
	}

	statistics := make(map[string]interface{})

	// TWR
	twr, err := stats.CalculateTWR(hist.Data, twrCashFlows)
	if err != nil {
		statistics["twr"] = map[string]string{"error": err.Error()}
	} else {
		statistics["twr"] = twr
	}

	// MWR — use the actual last priced date, not the raw query param `to`.
	// When `to` is in the future (e.g. a weekend or a date with no market data),
	// actualToStr is the last date for which we have a price. Using `to` instead
	// would stretch the IRR time-window past the end of real data, distorting MWR.
	endValue := 0.0
	mwrEndDate := to
	if len(hist.Data) > 0 {
		endValue = hist.Data[len(hist.Data)-1].Value
		if t, err2 := time.Parse("2006-01-02", actualToStr); err2 == nil {
			mwrEndDate = t
		}
	}
	mwr, err := stats.CalculateMWR(mwrCashFlows, endValue, mwrEndDate)
	if err != nil {
		statistics["mwr"] = map[string]string{"error": err.Error()}
	} else {
		statistics["mwr"] = mwr
	}

	// Run any additional registered calculators.
	registryResults := stats.CalculateAll(map[string]interface{}{
		"data":             data,
		"currency":         currency,
		"accounting_model": string(acctModel),
		"daily_values":     hist.Data,
		"cash_flows":       twrCashFlows,
	})
	for k, v := range registryResults {
		if _, exists := statistics[k]; !exists {
			statistics[k] = v
		}
	}

	c.JSON(http.StatusOK, models.StatsResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Statistics:      statistics,
	})
}

// Compare handles GET /api/v1/portfolio/compare
func (h *StatsHandler) Compare(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	symbolsStr := c.Query("symbols")
	if symbolsStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbols parameter is required"})
		return
	}
	symbols := strings.Split(symbolsStr, ",")
	for i := range symbols {
		symbols[i] = strings.TrimSpace(symbols[i])
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Constrain 'from' date to the portfolio's inception date
	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	riskFreeRate := 0.05
	if rfStr := c.Query("risk_free_rate"); rfStr != "" {
		if rf, err := strconv.ParseFloat(rfStr, 64); err == nil {
			riskFreeRate = rf
		}
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	// Get portfolio daily returns.
	portfolioReturns, startDates, endDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing portfolio returns: " + err.Error()})
		return
	}

	var benchmarks []models.BenchmarkResult
	for _, sym := range symbols {
		// Fetch with a 7-day lookback to ensure we have a starting price for forward-filling.
		// On error, record the problem in the result and continue with remaining symbols
		// so one bad ticker does not discard metrics already computed for others.
		prices, err := h.MarketProvider.GetHistory(sym, from.AddDate(0, 0, -7), to, cachedOnly)
		if err != nil {
			log.Printf("Warning: fetching benchmark %s: %v", sym, err)
			benchmarks = append(benchmarks, models.BenchmarkResult{
				Symbol: sym,
				Error:  "could not fetch price data: " + err.Error(),
			})
			continue
		}

		// For historical accounting, convert benchmark prices to the display currency so
		// that the benchmark return series is in the same currency as the portfolio returns.
		// Under spot accounting a constant multiplier cancels out in returns; under original
		// there is no conversion at all — so FX adjustment is only needed for historical.
		var fxRates map[string]float64 // date string → rate (nativeCcy → displayCcy)
		if acctModel == models.AccountingModelHistorical || acctModel == "" {
			if h.CurrencyGetter != nil {
				nativeCcy, err := h.CurrencyGetter.GetCurrency(sym)
				if err != nil {
					log.Printf("Warning: could not determine currency for %s: %v", sym, err)
				} else {
					fxRates = buildFXRateMap(h.MarketProvider, nativeCcy, currency, from.AddDate(0, 0, -7), to)
				}
			}
		}

		// Build a daily price map that matches the portfolio's pricing method
		// (forward-filled over weekends/holidays).
		benchPriceMap := make(map[string]float64)
		var lastBenchPrice float64
		for _, p := range prices {
			adj := p.AdjClose
			if adj == 0 {
				adj = p.Close
			}
			ds := p.Date.Format("2006-01-02")
			if fxRates != nil {
				if r, ok := fxRates[ds]; ok && r != 0 {
					adj *= r
				}
			}
			benchPriceMap[ds] = adj
		}

		// Forward-fill prices
		for d := from.AddDate(0, 0, -7); !d.After(to); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			if p, ok := benchPriceMap[ds]; ok {
				lastBenchPrice = p
			} else if lastBenchPrice != 0 {
				benchPriceMap[ds] = lastBenchPrice
			}
		}

		// Align lengths using the exact date intervals from the portfolio.
		var pRet []float64
		var bRet []float64
		// startDates and endDates are synchronised with portfolioReturns.
		for i := 0; i < len(portfolioReturns); i++ {
			prevDateStr := startDates[i]
			curDateStr := endDates[i]

			prevPrice, ok1 := benchPriceMap[prevDateStr]
			curPrice, ok2 := benchPriceMap[curDateStr]

			if ok1 && ok2 && prevPrice != 0 {
				bRet = append(bRet, (curPrice-prevPrice)/prevPrice)
				pRet = append(pRet, portfolioReturns[i])
			}
		}

		// No aligned pairs means the portfolio has no holdings in this date range
		// (or the benchmark has no price data that overlaps). Return an explicit
		// error so callers are not silently given all-zero metrics.
		if len(pRet) == 0 {
			benchmarks = append(benchmarks, models.BenchmarkResult{
				Symbol: sym,
				Error:  "no data: portfolio has no holdings in this date range or benchmark prices do not overlap",
			})
			continue
		}

		metrics := stats.CalculateBenchmarkMetrics(pRet, bRet, riskFreeRate)
		benchmarks = append(benchmarks, models.BenchmarkResult{
			Symbol:           sym,
			Alpha:            metrics.Alpha,
			Beta:             metrics.Beta,
			TreynorRatio:     metrics.TreynorRatio,
			TrackingError:    metrics.TrackingError,
			InformationRatio: metrics.InformationRatio,
			Correlation:      metrics.Correlation,
		})
	}

	c.JSON(http.StatusOK, models.CompareResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Benchmarks:      benchmarks,
	})
}

// GetStandalone handles GET /api/v1/portfolio/standalone.
// It always returns standalone metrics for the portfolio as the first result.
// If the optional `symbols` query parameter is provided (comma-separated),
// metrics for each symbol are appended as additional results.
func (h *StatsHandler) GetStandalone(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Constrain 'from' date to the portfolio's inception date.
	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	riskFreeRate := 0.05
	if rfStr := c.Query("risk_free_rate"); rfStr != "" {
		if rf, err := strconv.ParseFloat(rfStr, 64); err == nil {
			riskFreeRate = rf
		}
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	// Portfolio daily returns.
	portfolioReturns, startDates, endDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing portfolio returns: " + err.Error()})
		return
	}

	portfolioMetrics := stats.CalculateStandaloneMetrics(portfolioReturns, riskFreeRate)
	results := []models.StandaloneResult{
		{
			Symbol:       "Portfolio",
			SharpeRatio:  portfolioMetrics.SharpeRatio,
			VAMI:         portfolioMetrics.VAMI,
			Volatility:   portfolioMetrics.Volatility,
			SortinoRatio: portfolioMetrics.SortinoRatio,
			MaxDrawdown:  portfolioMetrics.MaxDrawdown,
		},
	}

	// Optional symbol results.
	if symbolsStr := c.Query("symbols"); symbolsStr != "" {
		symbols := strings.Split(symbolsStr, ",")
		for i := range symbols {
			symbols[i] = strings.TrimSpace(symbols[i])
		}

		for _, sym := range symbols {
			prices, err := h.MarketProvider.GetHistory(sym, from.AddDate(0, 0, -7), to, cachedOnly)
			if err != nil {
				log.Printf("Warning: fetching standalone %s: %v", sym, err)
				results = append(results, models.StandaloneResult{
					Symbol: sym,
					Error:  "could not fetch price data: " + err.Error(),
				})
				continue
			}

			var fxRates map[string]float64
			if acctModel == models.AccountingModelHistorical || acctModel == "" {
				if h.CurrencyGetter != nil {
					nativeCcy, err := h.CurrencyGetter.GetCurrency(sym)
					if err != nil {
						log.Printf("Warning: could not determine currency for %s: %v", sym, err)
					} else {
						fxRates = buildFXRateMap(h.MarketProvider, nativeCcy, currency, from.AddDate(0, 0, -7), to)
					}
				}
			}

			benchPriceMap := make(map[string]float64)
			var lastBenchPrice float64
			for _, p := range prices {
				adj := p.AdjClose
				if adj == 0 {
					adj = p.Close
				}
				ds := p.Date.Format("2006-01-02")
				if fxRates != nil {
					if r, ok := fxRates[ds]; ok && r != 0 {
						adj *= r
					}
				}
				benchPriceMap[ds] = adj
			}

			// Forward-fill prices over weekends/holidays.
			for d := from.AddDate(0, 0, -7); !d.After(to); d = d.AddDate(0, 0, 1) {
				ds := d.Format("2006-01-02")
				if p, ok := benchPriceMap[ds]; ok {
					lastBenchPrice = p
				} else if lastBenchPrice != 0 {
					benchPriceMap[ds] = lastBenchPrice
				}
			}

			// Build symbol return series aligned to the portfolio's date intervals.
			var symReturns []float64
			for i := 0; i < len(portfolioReturns); i++ {
				prevPrice, ok1 := benchPriceMap[startDates[i]]
				curPrice, ok2 := benchPriceMap[endDates[i]]
				if ok1 && ok2 && prevPrice != 0 {
					symReturns = append(symReturns, (curPrice-prevPrice)/prevPrice)
				}
			}

			if len(symReturns) == 0 {
				results = append(results, models.StandaloneResult{
					Symbol: sym,
					Error:  "no data: portfolio has no holdings in this date range or benchmark prices do not overlap",
				})
				continue
			}

			m := stats.CalculateStandaloneMetrics(symReturns, riskFreeRate)
			results = append(results, models.StandaloneResult{
				Symbol:       sym,
				SharpeRatio:  m.SharpeRatio,
				VAMI:         m.VAMI,
				Volatility:   m.Volatility,
				SortinoRatio: m.SortinoRatio,
				MaxDrawdown:  m.MaxDrawdown,
			})
		}
	}

	c.JSON(http.StatusOK, models.StandaloneResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Results:         results,
	})
}

// GetDrawdown handles GET /api/v1/portfolio/drawdown.
// Returns a per-day drawdown series for the portfolio over the requested period.
func (h *StatsHandler) GetDrawdown(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	portfolioReturns, startDates, endDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing portfolio returns: " + err.Error()})
		return
	}

	ddSeries := stats.CalculateDrawdownSeries(portfolioReturns, endDates)
	series := make([]models.DrawdownPoint, len(ddSeries))
	for i, pt := range ddSeries {
		series[i] = models.DrawdownPoint{
			Date:        pt.Date,
			DrawdownPct: pt.DrawdownPct,
			Peak:        pt.Peak,
			Wealth:      pt.Wealth,
		}
	}

	results := []models.DrawdownResult{{Symbol: "Portfolio", Series: series}}

	if symStr := strings.TrimSpace(c.Query("symbols")); symStr != "" {
		for _, sym := range strings.Split(symStr, ",") {
			sym = strings.TrimSpace(sym)
			if sym == "" {
				continue
			}
			priceMap, err := buildBenchmarkPriceMap(h.MarketProvider, h.CurrencyGetter, sym, from, to, currency, acctModel, cachedOnly)
			if err != nil {
				results = append(results, models.DrawdownResult{Symbol: sym, Error: "could not fetch price data: " + err.Error()})
				continue
			}
			rets, dts := alignBenchmarkReturns(priceMap, startDates, endDates)
			if len(rets) == 0 {
				results = append(results, models.DrawdownResult{Symbol: sym, Error: "no overlapping price data"})
				continue
			}
			dd := stats.CalculateDrawdownSeries(rets, dts)
			ss := make([]models.DrawdownPoint, len(dd))
			for i, pt := range dd {
				ss[i] = models.DrawdownPoint{Date: pt.Date, DrawdownPct: pt.DrawdownPct, Peak: pt.Peak, Wealth: pt.Wealth}
			}
			results = append(results, models.DrawdownResult{Symbol: sym, Series: ss})
		}
	}

	c.JSON(http.StatusOK, models.DrawdownResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Series:          series, // backward-compat: portfolio-only series
		Results:         results,
	})
}

// GetRolling handles GET /api/v1/portfolio/rolling.
// Query params: metric=sharpe|volatility|beta, window=21|63|126, benchmark=SYM (required for beta).
func (h *StatsHandler) GetRolling(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	metric := c.DefaultQuery("metric", "sharpe")
	if metric != "sharpe" && metric != "volatility" && metric != "beta" && metric != "sortino" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "metric must be sharpe, volatility, beta, or sortino"})
		return
	}

	window := 63
	if wStr := c.Query("window"); wStr != "" {
		if w, err := strconv.Atoi(wStr); err == nil && w > 0 {
			window = w
		}
	}

	riskFreeRate := 0.05
	if rfStr := c.Query("risk_free_rate"); rfStr != "" {
		if rf, err := strconv.ParseFloat(rfStr, 64); err == nil {
			riskFreeRate = rf
		}
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	portfolioReturns, startDates, endDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing portfolio returns: " + err.Error()})
		return
	}

	var rollingResults []models.RollingSeriesResult

	if metric == "beta" {
		benchSym := strings.TrimSpace(c.Query("benchmark"))
		if benchSym == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "benchmark parameter is required for metric=beta"})
			return
		}

		// We only need the aligned price data — recompute rolling below from raw prices.

		// Re-align portfolio and benchmark returns for rolling.
		prices, priceErr := h.MarketProvider.GetHistory(benchSym, from.AddDate(0, 0, -7), to, cachedOnly)
		if priceErr != nil {
			rollingResults = append(rollingResults, models.RollingSeriesResult{
				Symbol: benchSym,
				Error:  "could not fetch price data: " + priceErr.Error(),
			})
		} else {
			var fxRates map[string]float64
			if (acctModel == models.AccountingModelHistorical || acctModel == "") && h.CurrencyGetter != nil {
				if nativeCcy, err := h.CurrencyGetter.GetCurrency(benchSym); err == nil {
					fxRates = buildFXRateMap(h.MarketProvider, nativeCcy, currency, from.AddDate(0, 0, -7), to)
				}
			}

			benchPriceMap := make(map[string]float64)
			var lastBenchPrice float64
			for _, p := range prices {
				adj := p.AdjClose
				if adj == 0 {
					adj = p.Close
				}
				ds := p.Date.Format("2006-01-02")
				if fxRates != nil {
					if r, ok2 := fxRates[ds]; ok2 && r != 0 {
						adj *= r
					}
				}
				benchPriceMap[ds] = adj
			}
			for d := from.AddDate(0, 0, -7); !d.After(to); d = d.AddDate(0, 0, 1) {
				ds := d.Format("2006-01-02")
				if p, ok2 := benchPriceMap[ds]; ok2 {
					lastBenchPrice = p
				} else if lastBenchPrice != 0 {
					benchPriceMap[ds] = lastBenchPrice
				}
			}

			// Build aligned return series (portfolio, benchmark, dates).
			type alignedEntry struct {
				date string
				pRet float64
				bRet float64
			}
			var aligned []alignedEntry
			for i := 0; i < len(portfolioReturns); i++ {
				prevPrice, ok1 := benchPriceMap[startDates[i]]
				curPrice, ok2 := benchPriceMap[endDates[i]]
				if ok1 && ok2 && prevPrice != 0 {
					aligned = append(aligned, alignedEntry{
						date: endDates[i],
						pRet: portfolioReturns[i],
						bRet: (curPrice - prevPrice) / prevPrice,
					})
				}
			}

			pAligned := make([]float64, len(aligned))
			bAligned := make([]float64, len(aligned))
			dAligned := make([]string, len(aligned))
			for i, a := range aligned {
				pAligned[i] = a.pRet
				bAligned[i] = a.bRet
				dAligned[i] = a.date
			}

			pts := stats.CalculateRollingBeta(pAligned, bAligned, dAligned, window, riskFreeRate)
			series := make([]models.RollingPoint, len(pts))
			for i, pt := range pts {
				series[i] = models.RollingPoint{Date: pt.Date, Value: pt.Value}
			}
			rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: benchSym, Series: series})
		}
	} else {
		pts := stats.CalculateRollingStandalone(portfolioReturns, endDates, window, metric, riskFreeRate)
		series := make([]models.RollingPoint, len(pts))
		for i, pt := range pts {
			series[i] = models.RollingPoint{Date: pt.Date, Value: pt.Value}
		}
		rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: "Portfolio", Series: series})

		// Optional: per-symbol rolling series for benchmarks.
		if symStr := strings.TrimSpace(c.Query("symbols")); symStr != "" {
			for _, sym := range strings.Split(symStr, ",") {
				sym = strings.TrimSpace(sym)
				if sym == "" {
					continue
				}
				priceMap, err := buildBenchmarkPriceMap(h.MarketProvider, h.CurrencyGetter, sym, from, to, currency, acctModel, cachedOnly)
				if err != nil {
					rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: sym, Error: "could not fetch price data: " + err.Error()})
					continue
				}
				rets, dts := alignBenchmarkReturns(priceMap, startDates, endDates)
				if len(rets) == 0 {
					rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: sym, Error: "no overlapping price data"})
					continue
				}
				pts := stats.CalculateRollingStandalone(rets, dts, window, metric, riskFreeRate)
				series := make([]models.RollingPoint, len(pts))
				for i, pt := range pts {
					series[i] = models.RollingPoint{Date: pt.Date, Value: pt.Value}
				}
				rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: sym, Series: series})
			}
		}
	}

	c.JSON(http.StatusOK, models.RollingResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Metric:          metric,
		Window:          window,
		Results:         rollingResults,
	})
}

// GetAttribution handles GET /api/v1/portfolio/attribution.
// Returns per-position contribution to portfolio return over the period.
func (h *StatsHandler) GetAttribution(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	riskFreeRate := 0.05
	if rfStr := c.Query("risk_free_rate"); rfStr != "" {
		if rf, err := strconv.ParseFloat(rfStr, 64); err == nil {
			riskFreeRate = rf
		}
	}

	perPos, err := h.PortfolioService.GetDailyValuesPerPosition(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing per-position values: " + err.Error()})
		return
	}

	attrResults := stats.CalculateAttribution(perPos.BySymbol, perPos.CashFlowsBySymbol, perPos.Totals)

	positions := make([]models.AttributionResult, len(attrResults))
	for i, r := range attrResults {
		positions[i] = models.AttributionResult{
			Symbol:       r.Symbol,
			AvgWeight:    r.AvgWeight,
			Return:       r.Return,
			Contribution: r.Contribution,
		}
	}

	// Portfolio TWR for reconciliation.
	portfolioReturns, _, _, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
	totalTWR := 0.0
	if err == nil {
		if sm := stats.CalculateStandaloneMetrics(portfolioReturns, riskFreeRate); true {
			_ = sm
		}
		// Simple TWR: product of (1+r) - 1
		cum := 1.0
		for _, r := range portfolioReturns {
			cum *= (1 + r)
		}
		totalTWR = cum - 1
	}

	c.JSON(http.StatusOK, models.AttributionResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		TotalTWR:        totalTWR,
		Positions:       positions,
	})
}

// GetCumulative handles GET /api/v1/portfolio/cumulative.
// Returns cumulative-return series (in percent) for the portfolio and each requested benchmark symbol.
// Portfolio series is chained from daily TWR returns (cash-flow-adjusted). Benchmarks are chained from
// their (FX-converted) daily price returns aligned to the portfolio's return dates.
func (h *StatsHandler) GetCumulative(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	portfolioReturns, startDates, endDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing portfolio returns: " + err.Error()})
		return
	}

	firstDate := ""
	if len(startDates) > 0 {
		firstDate = startDates[0]
	}

	pPts := stats.CalculateCumulativeReturnSeries(portfolioReturns, endDates, firstDate)
	pSeries := make([]models.CumulativePoint, len(pPts))
	for i, pt := range pPts {
		pSeries[i] = models.CumulativePoint{Date: pt.Date, Value: pt.Value}
	}

	results := []models.CumulativeSeriesResult{{Symbol: "Portfolio", Series: pSeries}}

	if symStr := strings.TrimSpace(c.Query("symbols")); symStr != "" {
		for _, sym := range strings.Split(symStr, ",") {
			sym = strings.TrimSpace(sym)
			if sym == "" {
				continue
			}
			priceMap, err := buildBenchmarkPriceMap(h.MarketProvider, h.CurrencyGetter, sym, from, to, currency, acctModel, cachedOnly)
			if err != nil {
				results = append(results, models.CumulativeSeriesResult{Symbol: sym, Error: "could not fetch price data: " + err.Error()})
				continue
			}
			rets, dts := alignBenchmarkReturns(priceMap, startDates, endDates)
			if len(rets) == 0 {
				results = append(results, models.CumulativeSeriesResult{Symbol: sym, Error: "no overlapping price data"})
				continue
			}
			cPts := stats.CalculateCumulativeReturnSeries(rets, dts, firstDate)
			series := make([]models.CumulativePoint, len(cPts))
			for i, pt := range cPts {
				series[i] = models.CumulativePoint{Date: pt.Date, Value: pt.Value}
			}
			results = append(results, models.CumulativeSeriesResult{Symbol: sym, Series: series})
		}
	}

	c.JSON(http.StatusOK, models.CumulativeResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Results:         results,
	})
}

// GetCorrelations handles GET /api/v1/portfolio/correlations.
// Returns pairwise Pearson correlations for all current holdings.
func (h *StatsHandler) GetCorrelations(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	perPos, err := h.PortfolioService.GetDailyValuesPerPosition(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing per-position values: " + err.Error()})
		return
	}

	// Convert per-position daily values → cash-flow-adjusted daily returns and active-day masks.
	//
	// A day is "active" (mask == true) when the position was held at the start of that day
	// (prev > 1e-8). The mask feeds CalculateCorrelationMatrix so pairwise Pearson is computed
	// only over the overlapping window where both symbols were simultaneously held.
	//
	// CashFlowsBySymbol[sym][i] holds the signed cost of trades executed on validDates[i]:
	// positive for buys (capital injected), negative for sells (capital removed). On a trade
	// day the raw delta (vals[i] - vals[i-1]) blends price movement with a quantity change.
	// We strip the cash impact from vals[i] before dividing so the return reflects pure price
	// movement — the same TWR adjustment GetDailyReturns applies at the portfolio level.
	perSymbolReturns := make(map[string][]float64, len(perPos.BySymbol))
	perSymbolMask := make(map[string][]bool, len(perPos.BySymbol))
	for sym, vals := range perPos.BySymbol {
		if len(vals) < 2 {
			continue
		}
		cfs := perPos.CashFlowsBySymbol[sym] // length n, indexed same as vals
		rets := make([]float64, len(vals)-1)
		mask := make([]bool, len(vals)-1)
		for i := 1; i < len(vals); i++ {
			prev := vals[i-1]
			if prev > 1e-8 {
				cfAmount := 0.0
				if i < len(cfs) {
					cfAmount = cfs[i]
				}
				// (vals[i] - cfAmount) removes the quantity-change contribution,
				// leaving only the price-return component of the position's value move.
				rets[i-1] = (vals[i] - cfAmount - prev) / prev
				mask[i-1] = true
			}
		}
		perSymbolReturns[sym] = rets
		perSymbolMask[sym] = mask
	}

	result := stats.CalculateCorrelationMatrix(perSymbolReturns, perSymbolMask, 10)

	c.JSON(http.StatusOK, models.CorrelationMatrixResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Symbols:         result.Symbols,
		Matrix:          result.Matrix,
	})
}
