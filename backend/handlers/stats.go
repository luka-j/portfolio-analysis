package handlers

import (
	"net/http"
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
	PortfolioResolver
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
		// Real cashflows are scoped precisely to the time frame covered by the priced history data
		if actualToStr != "" {
			// Strip time from cf.Date for accurate boundary comparison
			cfDate := time.Date(cf.Date.Year(), cf.Date.Month(), cf.Date.Day(), 0, 0, 0, 0, time.UTC)
			if cfDate.After(actualFrom) && !cfDate.After(actualTo) {
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
