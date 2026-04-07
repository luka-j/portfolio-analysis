package handlers

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/models"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/stats"
)

// parseDateRange extracts and validates from/to query parameters.
func parseDateRange(c *gin.Context) (time.Time, time.Time, error) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr == "" || toStr == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("from and to parameters are required (YYYY-MM-DD)")
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid from date: %w", err)
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid to date: %w", err)
	}

	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("to date must be on or after from date")
	}

	return from, to, nil
}

// splitCurrencies parses a comma-separated currencies string into a non-empty slice.
// Returns ["USD"] if the input is empty.
func splitCurrencies(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []string{"USD"}
	}
	return out
}

// parseAccountingModel extracts the accounting_model query parameter.
func parseAccountingModel(c *gin.Context) models.AccountingModel {
	return models.ParseAccountingModel(c.DefaultQuery("accounting_model", "historical"))
}

// parseCachedOnly extracts the cachedOnly query parameter.
func parseCachedOnly(c *gin.Context) bool {
	return c.Query("cachedOnly") == "true"
}

// buildFXRateMap pre-fetches historical exchange rates for nativeCcy→displayCcy and returns
// a date-keyed map of rates, forward-filled over weekends/holidays. Returns nil when no
// conversion is needed (same currency) or when the fetch fails.
func buildFXRateMap(mp market.Provider, nativeCcy, displayCcy string, from, to time.Time) map[string]float64 {
	if nativeCcy == "" || nativeCcy == displayCcy {
		return nil
	}
	fxSymbol := fmt.Sprintf("%s%s=X", nativeCcy, displayCcy)
	points, err := mp.GetHistory(fxSymbol, from.AddDate(0, 0, -5), to, false)
	if err != nil {
		log.Printf("Warning: pre-fetching FX %s: %v", fxSymbol, err)
		return nil
	}
	pc := make(map[string]float64)
	for _, p := range points {
		pc[p.Date.Format("2006-01-02")] = p.Close
	}
	var lastRate float64
	if len(points) > 0 {
		lastRate = points[0].Close
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		if r, ok := pc[ds]; ok {
			lastRate = r
		} else if lastRate != 0 {
			pc[ds] = lastRate
		}
	}
	return pc
}

// DateRangeFromData determines the logical start and end dates from the portfolio data.
func DateRangeFromData(data *models.FlexQueryData) (time.Time, time.Time) {
	var earliest, latest time.Time

	for _, t := range data.Trades {
		// Skip trades that don't create holdings — they must not drive the
		// inception date, otherwise GetDailyValues returns zero-value rows
		// before any securities are actually held.
		if models.IsFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		if earliest.IsZero() || t.DateTime.Before(earliest) {
			earliest = t.DateTime
		}
		if t.DateTime.After(latest) {
			latest = t.DateTime
		}
	}
	for _, ct := range data.CashTransactions {
		if earliest.IsZero() || ct.DateTime.Before(earliest) {
			earliest = ct.DateTime
		}
		if ct.DateTime.After(latest) {
			latest = ct.DateTime
		}
	}

	if latest.IsZero() {
		return earliest, time.Now().UTC()
	}

	// Extend to today if latest is in the past.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.After(latest) {
		latest = today
	}

	return earliest, latest
}

// parseDateStrings parses two ISO date strings into time.Time values.
func parseDateStrings(from, to string) (time.Time, time.Time, error) {
	if from == "" || to == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("from and to dates are required (YYYY-MM-DD)")
	}
	fromT, err := time.Parse("2006-01-02", from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid from date: %w", err)
	}
	toT, err := time.Parse("2006-01-02", to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid to date: %w", err)
	}
	if toT.Before(fromT) {
		return time.Time{}, time.Time{}, fmt.Errorf("to date must be on or after from date")
	}
	return fromT, toT, nil
}

// portfolioMetricsResult holds computed TWR, MWR, standalone risk metrics, and the aligned return series.
type portfolioMetricsResult struct {
	TWR        float64
	TWRErr     string
	MWR        float64
	MWRErr     string
	Standalone stats.StandaloneMetrics
	Returns    []float64
	StartDates []string
	EndDates   []string
}

// computePortfolioMetrics calculates TWR, MWR, and standalone risk metrics for the given date range.
// The from date is automatically constrained to the portfolio's inception date.
func computePortfolioMetrics(
	ps *portfolio.Service,
	data *models.FlexQueryData,
	from, to time.Time,
	currency string,
	acctModel models.AccountingModel,
	riskFreeRate float64,
) (*portfolioMetricsResult, error) {
	// Constrain from to portfolio inception.
	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	res := &portfolioMetricsResult{}

	// Daily values for TWR/MWR.
	hist, err := ps.GetDailyValues(data, from, to, currency, acctModel, false)
	if err != nil {
		return nil, fmt.Errorf("computing daily values: %w", err)
	}
	cashFlows, err := ps.GetCashFlows(data, currency, acctModel, false)
	if err != nil {
		return nil, fmt.Errorf("computing cash flows: %w", err)
	}

	var twrCashFlows, mwrCashFlows []models.CashFlow
	actualFromStr, actualToStr := "", ""
	if len(hist.Data) > 0 {
		actualFromStr = hist.Data[0].Date
		actualToStr = hist.Data[len(hist.Data)-1].Date
		if hist.Data[0].Value > 0 {
			mwrCashFlows = append(mwrCashFlows, models.CashFlow{Date: from, Amount: -hist.Data[0].Value})
		}
	}
	for _, cf := range cashFlows {
		cfDateStr := cf.Date.Format("2006-01-02")
		if actualToStr != "" && cfDateStr > actualFromStr && cfDateStr <= actualToStr {
			twrCashFlows = append(twrCashFlows, cf)
			mwrCashFlows = append(mwrCashFlows, cf)
		}
	}

	twr, err := stats.CalculateTWR(hist.Data, twrCashFlows)
	if err != nil {
		res.TWRErr = err.Error()
	} else {
		res.TWR = twr
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
		res.MWRErr = err.Error()
	} else {
		res.MWR = mwr
	}

	// Daily returns for standalone metrics.
	portfolioReturns, startDates, endDates, err := ps.GetDailyReturns(data, from, to, currency, acctModel)
	if err != nil {
		return nil, fmt.Errorf("computing portfolio returns: %w", err)
	}
	res.Standalone = stats.CalculateStandaloneMetrics(portfolioReturns, riskFreeRate)
	res.Returns = portfolioReturns
	res.StartDates = startDates
	res.EndDates = endDates

	return res, nil
}

// computeBenchmarkComparison calculates benchmark comparison metrics aligned to the portfolio return series.
func computeBenchmarkComparison(
	mp market.Provider,
	cg market.CurrencyGetter,
	portfolioReturns []float64,
	startDates, endDates []string,
	benchmarkSymbol string,
	from, to time.Time,
	currency string,
	acctModel models.AccountingModel,
	riskFreeRate float64,
) (stats.BenchmarkMetrics, error) {
	prices, err := mp.GetHistory(benchmarkSymbol, from.AddDate(0, 0, -7), to, false)
	if err != nil {
		return stats.BenchmarkMetrics{}, fmt.Errorf("fetching benchmark prices: %w", err)
	}

	var fxRates map[string]float64
	if (acctModel == models.AccountingModelHistorical || acctModel == "") && cg != nil {
		if nativeCcy, err := cg.GetCurrency(benchmarkSymbol); err == nil {
			fxRates = buildFXRateMap(mp, nativeCcy, currency, from.AddDate(0, 0, -7), to)
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
	for d := from.AddDate(0, 0, -7); !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		if p, ok := benchPriceMap[ds]; ok {
			lastBenchPrice = p
		} else if lastBenchPrice != 0 {
			benchPriceMap[ds] = lastBenchPrice
		}
	}

	var pRet, bRet []float64
	for i := 0; i < len(portfolioReturns); i++ {
		prevPrice, ok1 := benchPriceMap[startDates[i]]
		curPrice, ok2 := benchPriceMap[endDates[i]]
		if ok1 && ok2 && prevPrice != 0 {
			bRet = append(bRet, (curPrice-prevPrice)/prevPrice)
			pRet = append(pRet, portfolioReturns[i])
		}
	}

	if len(pRet) == 0 {
		return stats.BenchmarkMetrics{}, fmt.Errorf("no overlapping data between portfolio and benchmark")
	}
	return stats.CalculateBenchmarkMetrics(pRet, bRet, riskFreeRate), nil
}
