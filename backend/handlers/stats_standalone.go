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
			if strings.HasPrefix(sym, "scenario:") {
				idStr := strings.TrimPrefix(sym, "scenario:")
				scenarioID, err := strconv.ParseUint(idStr, 10, 64)
				if err != nil {
					results = append(results, models.StandaloneResult{Symbol: sym, Error: "invalid scenario ID"})
					continue
				}

				realData, err := h.Repo.LoadSaved(userHash)
				if err != nil {
					results = append(results, models.StandaloneResult{Symbol: sym, Error: "loading real data: " + err.Error()})
					continue
				}

				syntheticData, err := h.resolveScenarioBenchmark(userHash, scenarioID, realData)
				if err != nil {
					results = append(results, models.StandaloneResult{Symbol: sym, Error: err.Error()})
					continue
				}

				sRet, _, sEnd, err := h.PortfolioService.GetDailyReturns(syntheticData, from, to, currency, acctModel, cachedOnly)
				if err != nil {
					results = append(results, models.StandaloneResult{Symbol: sym, Error: "computing scenario returns: " + err.Error()})
					continue
				}

				sMap := make(map[string]float64)
				for i, endD := range sEnd {
					sMap[endD] = sRet[i]
				}

				var symReturns []float64
				for i := 0; i < len(portfolioReturns); i++ {
					if val, ok := sMap[endDates[i]]; ok {
						symReturns = append(symReturns, val)
					}
				}

				if len(symReturns) == 0 {
					results = append(results, models.StandaloneResult{Symbol: sym, Error: "no data: portfolio has no holdings in this date range or scenario returns do not overlap"})
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
				continue
			}

			// Market symbol — use shared helpers.
			priceMap, err := buildBenchmarkPriceMap(h.MarketProvider, h.CurrencyGetter, sym, from, to, currency, acctModel, cachedOnly)
			if err != nil {
				results = append(results, models.StandaloneResult{
					Symbol: sym,
					Error:  "could not fetch price data: " + err.Error(),
				})
				continue
			}

			symReturns, _ := alignBenchmarkReturns(priceMap, startDates, endDates)

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
