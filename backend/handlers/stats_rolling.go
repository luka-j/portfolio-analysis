package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/stats"
)

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

		var pAligned, bAligned []float64
		var dAligned []string

		if strings.HasPrefix(benchSym, "scenario:") {
			idStr := strings.TrimPrefix(benchSym, "scenario:")
			scenarioID, parseErr := strconv.ParseUint(idStr, 10, 64)
			if parseErr != nil {
				rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: benchSym, Error: "invalid scenario ID"})
				goto computeBeta
			}

			realData, loadErr := h.Repo.LoadSaved(userHash)
			if loadErr != nil {
				rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: benchSym, Error: "loading real data: " + loadErr.Error()})
				goto computeBeta
			}

			syntheticData, resolveErr := h.resolveScenarioBenchmark(userHash, scenarioID, realData)
			if resolveErr != nil {
				rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: benchSym, Error: resolveErr.Error()})
				goto computeBeta
			}

			sRet, _, sEnd, retErr := h.PortfolioService.GetDailyReturns(syntheticData, from, to, currency, acctModel, cachedOnly)
			if retErr != nil {
				rollingResults = append(rollingResults, models.RollingSeriesResult{Symbol: benchSym, Error: "computing scenario returns: " + retErr.Error()})
				goto computeBeta
			}

			sMap := make(map[string]float64)
			for i, endD := range sEnd {
				sMap[endD] = sRet[i]
			}

			for i := 0; i < len(portfolioReturns); i++ {
				if val, ok := sMap[endDates[i]]; ok {
					bAligned = append(bAligned, val)
					pAligned = append(pAligned, portfolioReturns[i])
					dAligned = append(dAligned, endDates[i])
				}
			}
		} else {
			priceMap, priceErr := buildBenchmarkPriceMap(h.MarketProvider, h.CurrencyGetter, benchSym, from, to, currency, acctModel, cachedOnly)
			if priceErr != nil {
				rollingResults = append(rollingResults, models.RollingSeriesResult{
					Symbol: benchSym,
					Error:  "could not fetch price data: " + priceErr.Error(),
				})
				goto computeBeta
			}

			for i := 0; i < len(portfolioReturns); i++ {
				prevPrice, ok1 := priceMap[startDates[i]]
				curPrice, ok2 := priceMap[endDates[i]]
				if ok1 && ok2 && prevPrice != 0 {
					pAligned = append(pAligned, portfolioReturns[i])
					bAligned = append(bAligned, (curPrice-prevPrice)/prevPrice)
					dAligned = append(dAligned, endDates[i])
				}
			}
		}

	computeBeta:
		if len(pAligned) > 0 {
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
