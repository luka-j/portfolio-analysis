package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/middleware"
	"gofolio-analysis/models"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/market"
	"gofolio-analysis/services/portfolio"
	"gofolio-analysis/services/stats"
)

// StatsHandler handles portfolio statistics and comparison endpoints.
type StatsHandler struct {
	Parser           *flexquery.Parser
	PortfolioService *portfolio.Service
	MarketProvider   market.Provider
}

// NewStatsHandler creates a new StatsHandler.
func NewStatsHandler(parser *flexquery.Parser, ps *portfolio.Service, mp market.Provider) *StatsHandler {
	return &StatsHandler{
		Parser:           parser,
		PortfolioService: ps,
		MarketProvider:   mp,
	}
}

// GetStats handles GET /api/v1/portfolio/stats
func (h *StatsHandler) GetStats(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Parser.LoadSaved(userHash)
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
	hist, err := h.PortfolioService.GetDailyValues(data, from, to, currency, acctModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cashFlows, err := h.PortfolioService.GetCashFlows(data, currency, acctModel)
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

	fromDateStr := from.Format("2006-01-02")
	toDateStr := to.Format("2006-01-02")

	for _, cf := range cashFlows {
		cfDateStr := cf.Date.Format("2006-01-02")
		// Real cashflows are only tracked strictly AFTER the from date
		if cfDateStr > fromDateStr && cfDateStr <= toDateStr {
			twrCashFlows = append(twrCashFlows, cf)
			mwrCashFlows = append(mwrCashFlows, cf)
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

	// MWR
	endValue := 0.0
	if len(hist.Data) > 0 {
		endValue = hist.Data[len(hist.Data)-1].Value
	}
	mwr, err := stats.CalculateMWR(mwrCashFlows, endValue, to)
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

	data, err := h.Parser.LoadSaved(userHash)
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
	portfolioReturns, pDates, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing portfolio returns: " + err.Error()})
		return
	}

	var benchmarks []models.BenchmarkResult
	for _, sym := range symbols {
		prices, err := h.MarketProvider.GetHistory(sym, from, to)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "fetching benchmark " + sym + ": " + err.Error()})
			return
		}

		// Compute daily returns for the benchmark using AdjClose.
		benchMap := make(map[string]float64)
		for i := 1; i < len(prices); i++ {
			adjPrev := prices[i-1].AdjClose
			if adjPrev == 0 {
				adjPrev = prices[i-1].Close
			}
			adjCur := prices[i].AdjClose
			if adjCur == 0 {
				adjCur = prices[i].Close
			}
			if adjPrev != 0 {
				benchMap[prices[i].Date.Format("2006-01-02")] = (adjCur - adjPrev) / adjPrev
			}
		}

		// Align lengths.
		var pRet []float64
		var bRet []float64
		for i, date := range pDates {
			dStr := date.Format("2006-01-02")
			if br, ok := benchMap[dStr]; ok {
				pRet = append(pRet, portfolioReturns[i])
				bRet = append(bRet, br)
			}
		}

		metrics := stats.CalculateBenchmarkMetrics(pRet, bRet, riskFreeRate)
		benchmarks = append(benchmarks, models.BenchmarkResult{
			Symbol:           sym,
			Alpha:            metrics.Alpha,
			Beta:             metrics.Beta,
			SharpeRatio:      metrics.SharpeRatio,
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


