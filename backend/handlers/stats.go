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

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel := parseAccountingModel(c)

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
	hist, err := h.PortfolioService.GetDailyValues(data, from, to, currency, acctModel, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cashFlows, err := h.PortfolioService.GetCashFlows(data, currency, acctModel, false)
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

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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

	acctModel := parseAccountingModel(c)

	// Get portfolio daily returns.
	portfolioReturns, startDates, endDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing portfolio returns: " + err.Error()})
		return
	}

	var benchmarks []models.BenchmarkResult
	for _, sym := range symbols {
		// Fetch with a 7-day lookback to ensure we have a starting price for forward-filling.
		// On error, record the problem in the result and continue with remaining symbols
		// so one bad ticker does not discard metrics already computed for others.
		prices, err := h.MarketProvider.GetHistory(sym, from.AddDate(0, 0, -7), to, false)
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

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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

	acctModel := parseAccountingModel(c)

	// Portfolio daily returns.
	portfolioReturns, startDates, endDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel)
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
			prices, err := h.MarketProvider.GetHistory(sym, from.AddDate(0, 0, -7), to, false)
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
