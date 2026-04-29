package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/stats"
)

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
