package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/stats"
)

// GetAnalysisDashboard handles GET /api/v1/portfolio/analysis-dashboard.
// Returns stats, twr_history, and mwr_history in a single concurrent pass.
func (h *StatsHandler) GetAnalysisDashboard(c *gin.Context) {
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

	if from.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data to compute statistics"})
		return
	}

	var wg sync.WaitGroup
	var statsRes models.StatsResponse
	var twrRes *models.PortfolioHistoryResponse
	var mwrRes *models.PortfolioHistoryResponse
	var errStats, errTwr, errMwr error

	wg.Add(3)

	go func() {
		defer wg.Done()
		hist, err := h.PortfolioService.GetDailyValues(data, from, to, currency, acctModel, cachedOnly)
		if err != nil {
			errStats = err
			return
		}

		cashFlows, err := h.PortfolioService.GetCashFlows(data, currency, acctModel, cachedOnly, to)
		if err != nil {
			errStats = err
			return
		}

		var twrCashFlows []models.CashFlow
		var mwrCashFlows []models.CashFlow

		if len(hist.Data) > 0 {
			startValue := hist.Data[0].Value
			if startValue > 0 {
				mwrCashFlows = append(mwrCashFlows, models.CashFlow{
					Date:   from,
					Amount: -startValue,
				})
			}
		}

		var actualFrom, actualTo time.Time
		actualFromStr := ""
		actualToStr := ""
		if len(hist.Data) > 0 {
			actualFromStr = hist.Data[0].Date
			actualToStr = hist.Data[len(hist.Data)-1].Date
			actualFrom, _ = time.Parse("2006-01-02", actualFromStr)
			actualTo, _ = time.Parse("2006-01-02", actualToStr)
		}

		for _, cf := range cashFlows {
			if actualToStr != "" {
				cfDate := time.Date(cf.Date.Year(), cf.Date.Month(), cf.Date.Day(), 0, 0, 0, 0, time.UTC)
				if cfDate.After(actualFrom) && !cfDate.After(actualTo) {
					twrCashFlows = append(twrCashFlows, cf)
					mwrCashFlows = append(mwrCashFlows, cf)
				}
			}
		}

		statistics := make(map[string]interface{})

		twr, err := stats.CalculateTWR(hist.Data, twrCashFlows)
		if err != nil {
			statistics["twr"] = map[string]string{"error": err.Error()}
		} else {
			statistics["twr"] = twr
		}

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

		statsRes = models.StatsResponse{
			Currency:        currency,
			AccountingModel: string(acctModel),
			Statistics:      statistics,
		}
	}()

	go func() {
		defer wg.Done()
		twrRes, errTwr = h.PortfolioService.GetCumulativeTWR(data, from, to, currency, acctModel, cachedOnly)
	}()

	go func() {
		defer wg.Done()
		mwrRes, errMwr = h.PortfolioService.GetCumulativeMWR(data, from, to, currency, acctModel, cachedOnly)
	}()

	wg.Wait()

	if errStats != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stats: " + errStats.Error()})
		return
	}
	if errTwr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "twr: " + errTwr.Error()})
		return
	}
	if errMwr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "mwr: " + errMwr.Error()})
		return
	}

	c.JSON(http.StatusOK, models.AnalysisDashboardResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Stats:           statsRes.Statistics,
		TWRHistory:      twrRes.Data,
		MWRHistory:      mwrRes.Data,
	})
}

// GetAnalysisHoldings handles GET /api/v1/portfolio/analysis-holdings.
// Returns attribution and correlations in a single concurrent pass.
func (h *StatsHandler) GetAnalysisHoldings(c *gin.Context) {
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

	var wg sync.WaitGroup
	var attrRes models.AttributionResponse
	var corrRes models.CorrelationMatrixResponse

	wg.Add(2)

	go func() {
		defer wg.Done()
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

		// Calculate totalTWR
		portfolioReturns, _, _, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
		totalTWR := 0.0
		if err == nil {
			cum := 1.0
			for _, r := range portfolioReturns {
				cum *= (1 + r)
			}
			totalTWR = cum - 1
		}

		attrRes = models.AttributionResponse{
			Currency:        currency,
			AccountingModel: string(acctModel),
			TotalTWR:        totalTWR,
			Positions:       positions,
		}
	}()

	go func() {
		defer wg.Done()
		perSymbolReturns := make(map[string][]float64, len(perPos.BySymbol))
		perSymbolMask := make(map[string][]bool, len(perPos.BySymbol))
		for sym, vals := range perPos.BySymbol {
			if len(vals) < 2 {
				continue
			}
			cfs := perPos.CashFlowsBySymbol[sym]
			rets := make([]float64, len(vals)-1)
			mask := make([]bool, len(vals)-1)
			for i := 1; i < len(vals); i++ {
				prev := vals[i-1]
				if prev > 1e-8 {
					cfAmount := 0.0
					if i < len(cfs) {
						cfAmount = cfs[i]
					}
					rets[i-1] = (vals[i] - cfAmount - prev) / prev
					mask[i-1] = true
				}
			}
			perSymbolReturns[sym] = rets
			perSymbolMask[sym] = mask
		}

		result := stats.CalculateCorrelationMatrix(perSymbolReturns, perSymbolMask, 10)

		corrRes = models.CorrelationMatrixResponse{
			Currency:        currency,
			AccountingModel: string(acctModel),
			Symbols:         result.Symbols,
			Matrix:          result.Matrix,
		}
	}()

	wg.Wait()

	c.JSON(http.StatusOK, models.AnalysisHoldingsResponse{
		Currency:        currency,
		AccountingModel: string(acctModel),
		Attribution:     attrRes,
		Correlations:    corrRes,
	})
}
