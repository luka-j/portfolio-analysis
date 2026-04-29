package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/stats"
)

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

	// Also compute the MWR cumulative series so the frontend can overlay it on the MWR chart
	// for hypothetical/scenario portfolios (the MWR chart needs this from GetCumulative since
	// addScenarioBenchmark calls getCumulativeSeries to obtain both TWR and MWR series).
	if mwrResp, err := h.PortfolioService.GetCumulativeMWR(data, from, to, currency, acctModel, cachedOnly); err == nil && mwrResp != nil {
		mwrSeries := make([]models.CumulativePoint, len(mwrResp.Data))
		for i, pt := range mwrResp.Data {
			mwrSeries[i] = models.CumulativePoint{Date: pt.Date, Value: pt.Value}
		}
		results = append(results, models.CumulativeSeriesResult{Symbol: "Portfolio-MWR", Series: mwrSeries})
	}

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
