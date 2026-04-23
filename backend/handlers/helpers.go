package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/models"
	scenariosvc "portfolio-analysis/services/scenario"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/stats"
)

// ScenarioMiddleware holds the dependencies needed to resolve an optional scenario_id
// query parameter. Embed this in handler structs that should support scenarios.
type ScenarioMiddleware struct {
	ScenarioRepo *scenariosvc.Repository
	ScenarioMkt  market.Provider
	ScenarioFX   *fx.Service
}

// loadPortfolioData loads FlexQueryData for the authenticated user.
// If a scenario_id query parameter is present, the corresponding ScenarioSpec is
// applied via scenario.Build and the synthesised FlexQueryData is returned.
// The second return value is false when the handler should abort (error already written).
func (sm *ScenarioMiddleware) loadPortfolioData(
	c *gin.Context,
	flexRepo *flexquery.Repository,
	userHash string,
) (*models.FlexQueryData, bool) {
	realData, err := flexRepo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return nil, false
	}

	scenarioIDStr := c.Query("scenario_id")
	if scenarioIDStr == "" {
		return realData, true
	}

	scenarioID, err := strconv.ParseUint(scenarioIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scenario_id"})
		return nil, false
	}

	if sm.ScenarioRepo == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scenarios not available"})
		return nil, false
	}

	// Resolve user ID from the token hash.
	var user models.User
	if err := flexRepo.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return nil, false
	}

	row, err := sm.ScenarioRepo.Get(user.ID, uint(scenarioID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "loading scenario: " + err.Error()})
		return nil, false
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return nil, false
	}

	spec, err := scenariosvc.ParseSpec(row)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "parsing scenario: " + err.Error()})
		return nil, false
	}

	syntheticData, err := scenariosvc.Build(spec, realData, sm.ScenarioMkt, sm.ScenarioFX)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "building scenario: " + err.Error()})
		return nil, false
	}

	// Scenario-specific hash prevents singleflight cache collisions with the real portfolio.
	// Including UpdatedAt (ns) means editing the scenario invalidates any downstream caches
	// that key on UserHash (e.g. portfolio-service singleflight).
	syntheticData.UserHash = fmt.Sprintf("scenario:%d:%d:%s", scenarioID, row.UpdatedAt.UnixNano(), userHash)

	go sm.ScenarioRepo.TouchLastUsed(user.ID, uint(scenarioID))

	return syntheticData, true
}

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

// parseCurrencies parses a comma-separated currencies query parameter and
// validates each token. Returns a 400 and false when:
//   - all tokens are empty (defaults to ["USD"] are intentional via the caller)
//   - any token is the reserved sentinel "Original" (that is a mode flag, not a currency)
//   - any token contains non-alpha characters or is not 2–10 characters long
//   - the same code appears more than once
func parseCurrencies(c *gin.Context, s string) ([]string, bool) {
	var out []string
	seen := make(map[string]bool)
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		upper := strings.ToUpper(tok)
		if upper == "ORIGINAL" {
			c.JSON(http.StatusBadRequest, gin.H{"error": `"Original" is an accounting mode, not a currency — use accounting_model=original instead`})
			return nil, false
		}
		if len(tok) < 2 || len(tok) > 10 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid currency code %q: must be 2–10 characters", tok)})
			return nil, false
		}
		for _, r := range tok {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid currency code %q: only letters allowed", tok)})
				return nil, false
			}
		}
		if seen[upper] {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("duplicate currency code %q", tok)})
			return nil, false
		}
		seen[upper] = true
		out = append(out, tok)
	}
	if len(out) == 0 {
		return []string{"USD"}, true
	}
	return out, true
}

// parseAccountingModel extracts and validates the accounting_model query parameter.
// Writes a 400 and returns false when the value is set but not one of the known models.
func parseAccountingModel(c *gin.Context) (models.AccountingModel, bool) {
	raw := c.DefaultQuery("accounting_model", "historical")
	m, err := models.ValidateAccountingModel(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return m, false
	}
	return m, true
}

// parseCachedOnly extracts the cachedOnly query parameter.
func parseCachedOnly(c *gin.Context) bool {
	return c.Query("cachedOnly") == "true"
}

// buildBenchmarkPriceMap fetches the benchmark's historical prices, converts to the display
// currency (under historical accounting), and forward-fills over weekends/holidays so that
// the returned map can be indexed by any calendar date in [from, to].
//
// Returns an error only if the price fetch fails; an empty map is returned when the symbol
// has no data.
func buildBenchmarkPriceMap(
	mp market.Provider, cg market.CurrencyGetter,
	symbol string, from, to time.Time,
	currency string, acctModel models.AccountingModel,
	cachedOnly bool,
) (map[string]float64, error) {
	prices, err := mp.GetHistory(symbol, from.AddDate(0, 0, -7), to, cachedOnly)
	if err != nil {
		return nil, err
	}

	var fxRates map[string]float64
	if (acctModel == models.AccountingModelHistorical || acctModel == "") && cg != nil {
		if nativeCcy, err2 := cg.GetCurrency(symbol); err2 == nil {
			fxRates = buildFXRateMap(mp, nativeCcy, currency, from.AddDate(0, 0, -7), to)
		}
	}

	priceMap := make(map[string]float64)
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
		priceMap[ds] = adj
	}
	var last float64
	for d := from.AddDate(0, 0, -7); !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		if p, ok := priceMap[ds]; ok {
			last = p
		} else if last != 0 {
			priceMap[ds] = last
		}
	}
	return priceMap, nil
}

// alignBenchmarkReturns builds a return series for the benchmark aligned to the given
// (startDates[i], endDates[i]) intervals. Intervals with missing prices are skipped;
// the returned slice may be shorter than the input.
func alignBenchmarkReturns(priceMap map[string]float64, startDates, endDates []string) (returns []float64, dates []string) {
	for i := range startDates {
		prev, ok1 := priceMap[startDates[i]]
		cur, ok2 := priceMap[endDates[i]]
		if ok1 && ok2 && prev != 0 {
			returns = append(returns, cur/prev-1)
			dates = append(dates, endDates[i])
		}
	}
	return
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
	cachedOnly bool,
) (*portfolioMetricsResult, error) {
	// Constrain from to portfolio inception.
	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	res := &portfolioMetricsResult{}

	// Daily values for TWR/MWR.
	hist, err := ps.GetDailyValues(data, from, to, currency, acctModel, cachedOnly)
	if err != nil {
		return nil, fmt.Errorf("computing daily values: %w", err)
	}
	cashFlows, err := ps.GetCashFlows(data, currency, acctModel, cachedOnly, to)
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
	portfolioReturns, startDates, endDates, err := ps.GetDailyReturns(data, from, to, currency, acctModel, cachedOnly)
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
