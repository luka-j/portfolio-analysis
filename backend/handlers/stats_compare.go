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
		if strings.HasPrefix(sym, "scenario:") {
			idStr := strings.TrimPrefix(sym, "scenario:")
			scenarioID, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				benchmarks = append(benchmarks, models.BenchmarkResult{Symbol: sym, Error: "invalid scenario ID"})
				continue
			}

			realData, err := h.Repo.LoadSaved(userHash)
			if err != nil {
				benchmarks = append(benchmarks, models.BenchmarkResult{Symbol: sym, Error: "loading real data: " + err.Error()})
				continue
			}

			syntheticData, err := h.resolveScenarioBenchmark(userHash, scenarioID, realData)
			if err != nil {
				benchmarks = append(benchmarks, models.BenchmarkResult{Symbol: sym, Error: err.Error()})
				continue
			}

			sRet, _, sEnd, err := h.PortfolioService.GetDailyReturns(syntheticData, from, to, currency, acctModel, cachedOnly)
			if err != nil {
				benchmarks = append(benchmarks, models.BenchmarkResult{Symbol: sym, Error: "computing scenario returns: " + err.Error()})
				continue
			}

			sMap := make(map[string]float64)
			for i, endD := range sEnd {
				sMap[endD] = sRet[i]
			}

			var pRetAligned []float64
			var bRet []float64
			for i := 0; i < len(portfolioReturns); i++ {
				if val, ok := sMap[endDates[i]]; ok {
					bRet = append(bRet, val)
					pRetAligned = append(pRetAligned, portfolioReturns[i])
				}
			}

			if len(pRetAligned) == 0 {
				benchmarks = append(benchmarks, models.BenchmarkResult{Symbol: sym, Error: "no overlapping data between portfolio and scenario"})
				continue
			}

			metrics := stats.CalculateBenchmarkMetrics(pRetAligned, bRet, riskFreeRate)
			benchmarks = append(benchmarks, models.BenchmarkResult{
				Symbol:           sym,
				Alpha:            metrics.Alpha,
				Beta:             metrics.Beta,
				TreynorRatio:     metrics.TreynorRatio,
				TrackingError:    metrics.TrackingError,
				InformationRatio: metrics.InformationRatio,
				Correlation:      metrics.Correlation,
			})
			continue
		}

		// Market symbol — use shared helpers: fetch → FX → forward-fill → align.
		priceMap, err := buildBenchmarkPriceMap(h.MarketProvider, h.CurrencyGetter, sym, from, to, currency, acctModel, cachedOnly)
		if err != nil {
			benchmarks = append(benchmarks, models.BenchmarkResult{
				Symbol: sym,
				Error:  "could not fetch price data: " + err.Error(),
			})
			continue
		}

		pRet, bRet := alignBenchmarkReturnsPair(priceMap, portfolioReturns, startDates, endDates)

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

// alignBenchmarkReturnsPair aligns portfolio and benchmark returns over common date intervals,
// returning both slices with only the overlapping entries.
func alignBenchmarkReturnsPair(priceMap map[string]float64, portfolioReturns []float64, startDates, endDates []string) (pRet, bRet []float64) {
	for i := 0; i < len(portfolioReturns); i++ {
		prevPrice, ok1 := priceMap[startDates[i]]
		curPrice, ok2 := priceMap[endDates[i]]
		if ok1 && ok2 && prevPrice != 0 {
			bRet = append(bRet, (curPrice-prevPrice)/prevPrice)
			pRet = append(pRet, portfolioReturns[i])
		}
	}
	return
}
