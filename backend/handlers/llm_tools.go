package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"google.golang.org/genai"

	"portfolio-analysis/models"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/stats"
)

// toolGetCurrentAllocations returns portfolio holdings with percentage weights (no absolute values).
func (h *LLMHandler) toolGetCurrentAllocations(_ context.Context, data *models.FlexQueryData, req ChatRequest) (map[string]any, error) {
	acctModel := models.ParseAccountingModel(req.AccountingModel)
	result, err := h.PortfolioService.GetCurrentValue(data, req.Currency, acctModel)
	if err != nil {
		return nil, fmt.Errorf("computing portfolio value: %w", err)
	}
	if result.Value == 0 {
		return map[string]any{"holdings": []any{}, "note": "Portfolio has no current value."}, nil
	}

	symbols := make([]string, 0, len(result.Positions))
	for _, pos := range result.Positions {
		if pos.Value != 0 && pos.Symbol != "PENDING_CASH" {
			symbols = append(symbols, pos.Symbol)
		}
	}
	var rows []models.AssetFundamental
	nameMap := make(map[string]string, len(symbols))
	if h.DB != nil && len(symbols) > 0 {
		h.DB.Select("symbol, name").Where("symbol IN ?", symbols).Find(&rows)
		for _, r := range rows {
			if r.Name != "" {
				nameMap[r.Symbol] = r.Name
			}
		}
	}

	type holding struct {
		Symbol    string  `json:"symbol"`
		Name      string  `json:"name,omitempty"`
		WeightPct float64 `json:"weight_pct"`
	}
	holdings := make([]holding, 0, len(result.Positions))
	for _, pos := range result.Positions {
		if pos.Value == 0 || pos.Symbol == "PENDING_CASH" {
			continue
		}
		holdings = append(holdings, holding{
			Symbol:    pos.Symbol,
			Name:      nameMap[pos.Symbol],
			WeightPct: math.Round((pos.Value/result.Value)*1000) / 10,
		})
	}

	return map[string]any{"holdings": holdings, "currency": req.Currency}, nil
}

// toolGetRiskMetrics computes risk and return metrics for the requested date range.
func (h *LLMHandler) toolGetRiskMetrics(_ context.Context, data *models.FlexQueryData, req ChatRequest, args map[string]any) (map[string]any, error) {
	fromStr, _ := args["from_date"].(string)
	toStr, _ := args["to_date"].(string)
	rfrRaw, _ := args["risk_free_rate"].(float64)
	if fromStr == "" {
		fromStr = req.From
	}
	if toStr == "" {
		toStr = req.To
	}
	if toStr == "" {
		toStr = time.Now().Format("2006-01-02")
	}
	rfr := rfrRaw
	if rfr == 0 {
		rfr = req.RiskFreeRate
	}
	if rfr == 0 {
		rfr = h.DefaultRiskFreeRate
	}

	from, to, err := parseDateStrings(fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("invalid dates: %w", err)
	}

	acctModel, acctErr := models.ValidateAccountingModel(req.AccountingModel)
	if acctErr != nil {
		return nil, acctErr
	}

	metrics, err := computePortfolioMetrics(h.PortfolioService, data, from, to, req.Currency, acctModel, rfr)
	if err != nil {
		return nil, fmt.Errorf("computing portfolio metrics: %w", err)
	}

	result := map[string]any{
		"period":         fmt.Sprintf("%s – %s", from.Format("Jan 2, 2006"), to.Format("Jan 2, 2006")),
		"risk_free_rate": fmt.Sprintf("%.2f%%", rfr*100),
		"sharpe_ratio":  fmt.Sprintf("%.3f", metrics.Standalone.SharpeRatio),
		"sortino_ratio": fmt.Sprintf("%.3f", metrics.Standalone.SortinoRatio),
		"vami":          fmt.Sprintf("%.1f", metrics.Standalone.VAMI),
		"volatility":    fmt.Sprintf("%.2f%%", metrics.Standalone.Volatility*100),
		"max_drawdown":  fmt.Sprintf("-%.2f%%", metrics.Standalone.MaxDrawdown*100),
	}
	if metrics.TWRErr == "" {
		result["twr"] = fmt.Sprintf("%.2f%%", metrics.TWR*100)
	} else {
		result["twr"] = "N/A (" + metrics.TWRErr + ")"
	}
	if metrics.MWRErr == "" {
		result["mwr"] = fmt.Sprintf("%.2f%%", metrics.MWR*100)
	} else {
		result["mwr"] = "N/A (" + metrics.MWRErr + ")"
	}
	return result, nil
}

// toolGetBenchmarkMetrics computes benchmark comparison metrics for the portfolio.
func (h *LLMHandler) toolGetBenchmarkMetrics(_ context.Context, data *models.FlexQueryData, req ChatRequest, args map[string]any) (map[string]any, error) {
	benchmarkSymbol, _ := args["benchmark_symbol"].(string)
	fromStr, _ := args["from_date"].(string)
	toStr, _ := args["to_date"].(string)
	rfrRaw, _ := args["risk_free_rate"].(float64)

	if benchmarkSymbol == "" {
		benchmarkSymbol = req.BenchmarkSymbol
	}
	if benchmarkSymbol == "" {
		return nil, fmt.Errorf("benchmark_symbol is required")
	}
	if fromStr == "" {
		fromStr = req.From
	}
	if toStr == "" {
		toStr = req.To
	}
	if toStr == "" {
		toStr = time.Now().Format("2006-01-02")
	}
	rfr := rfrRaw
	if rfr == 0 {
		rfr = req.RiskFreeRate
	}
	if rfr == 0 {
		rfr = 0.05
	}

	from, to, err := parseDateStrings(fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("invalid dates: %w", err)
	}
	acctModel, acctErr := models.ValidateAccountingModel(req.AccountingModel)
	if acctErr != nil {
		return nil, acctErr
	}

	metrics, err := computePortfolioMetrics(h.PortfolioService, data, from, to, req.Currency, acctModel, rfr)
	if err != nil {
		return nil, fmt.Errorf("computing portfolio metrics: %w", err)
	}
	bmMetrics, err := computeBenchmarkComparison(
		h.MarketProvider, h.CurrencyGetter,
		metrics.Returns, metrics.StartDates, metrics.EndDates,
		benchmarkSymbol, from, to, req.Currency, acctModel, rfr,
	)
	if err != nil {
		return nil, fmt.Errorf("computing benchmark metrics: %w", err)
	}

	return map[string]any{
		"benchmark":      benchmarkSymbol,
		"period":         fmt.Sprintf("%s – %s", from.Format("Jan 2, 2006"), to.Format("Jan 2, 2006")),
		"alpha":          fmt.Sprintf("%.2f%%", bmMetrics.Alpha*100),
		"beta":           fmt.Sprintf("%.3f", bmMetrics.Beta),
		"treynor":        fmt.Sprintf("%.4f", bmMetrics.TreynorRatio),
		"tracking_error": fmt.Sprintf("%.2f%%", bmMetrics.TrackingError*100),
		"info_ratio":     fmt.Sprintf("%.3f", bmMetrics.InformationRatio),
		"correlation":    fmt.Sprintf("%.3f", bmMetrics.Correlation),
		"sharpe":         fmt.Sprintf("%.3f", metrics.Standalone.SharpeRatio),
		"sortino":        fmt.Sprintf("%.3f", metrics.Standalone.SortinoRatio),
		"volatility":     fmt.Sprintf("%.2f%%", metrics.Standalone.Volatility*100),
		"max_drawdown":   fmt.Sprintf("-%.2f%%", metrics.Standalone.MaxDrawdown*100),
	}, nil
}

// toolGetAssetFundamentals looks up stored fundamentals for a symbol with DB→MarketProvider fallback.
func (h *LLMHandler) toolGetAssetFundamentals(ctx context.Context, args map[string]any) (map[string]any, error) {
	symbol, _ := args["symbol"].(string)
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	if h.DB != nil {
		var fund models.AssetFundamental
		err := h.DB.Where("symbol = ?", symbol).First(&fund).Error
		if err == nil {
			result := map[string]any{
				"symbol":     fund.Symbol,
				"name":       fund.Name,
				"asset_type": fund.AssetType,
				"country":    fund.Country,
				"sector":     fund.Sector,
				"isin":       fund.ISIN,
				"source":     "local_db",
			}
			var breakdowns []models.EtfBreakdown
			if h.DB.Where("fund_symbol = ?", symbol).Limit(30).Find(&breakdowns).Error == nil && len(breakdowns) > 0 {
				byDim := make(map[string][]map[string]any)
				for _, b := range breakdowns {
					byDim[b.Dimension] = append(byDim[b.Dimension], map[string]any{
						"label":      b.Label,
						"weight_pct": math.Round(b.Weight*1000) / 10,
					})
				}
				result["etf_breakdowns"] = byDim
			}
			return result, nil
		}
		if !isNotFound(err) {
			slog.Warn("llm: toolGetAssetFundamentals DB lookup error", "symbol", symbol, "err", err)
		}
	}

	return map[string]any{
		"symbol": symbol,
		"note":   "No local data found for this ticker. Use Google Search to look up asset class, country, sector, and other fundamentals.",
		"source": "not_found",
	}, nil
}

// toolGetPortfolioBreakdown returns the aggregate portfolio breakdown by asset type, country, and sector.
func (h *LLMHandler) toolGetPortfolioBreakdown(_ context.Context, data *models.FlexQueryData, req ChatRequest) (map[string]any, error) {
	if h.BreakdownSvc == nil {
		return map[string]any{"error": "breakdown service unavailable"}, nil
	}

	acctModel := models.ParseAccountingModel(req.AccountingModel)
	result, err := h.PortfolioService.GetCurrentValue(data, req.Currency, acctModel)
	if err != nil {
		return nil, fmt.Errorf("fetching portfolio: %w", err)
	}

	var userID uint
	if h.DB != nil {
		var user models.User
		if err := h.DB.Where("token_hash = ?", data.UserHash).First(&user).Error; err == nil {
			userID = user.ID
		}
	}

	breakdown, err := h.BreakdownSvc.Calculate(result.Positions, req.Currency, userID)
	if err != nil {
		return nil, fmt.Errorf("calculating breakdown: %w", err)
	}

	sections := make(map[string]any, len(breakdown.Sections))
	for _, s := range breakdown.Sections {
		entries := make([]map[string]any, len(s.Entries))
		for i, e := range s.Entries {
			entries[i] = map[string]any{
				"label":      e.Label,
				"percentage": math.Round(e.Percentage*10) / 10,
			}
		}
		sections[s.Title] = entries
	}
	return map[string]any{"currency": req.Currency, "breakdown": sections}, nil
}

// toolGetCorrelations computes pairwise Pearson correlations for all current holdings.
func (h *LLMHandler) toolGetCorrelations(_ context.Context, data *models.FlexQueryData, req ChatRequest, args map[string]any) (map[string]any, error) {
	fromStr, _ := args["from_date"].(string)
	toStr, _ := args["to_date"].(string)

	if fromStr == "" {
		fromStr = req.From
	}
	if toStr == "" {
		toStr = req.To
	}
	if toStr == "" {
		toStr = time.Now().Format("2006-01-02")
	}

	from, to, err := parseDateStrings(fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("invalid dates: %w", err)
	}

	acctModel := models.ParseAccountingModel(req.AccountingModel)
	perPos, err := h.PortfolioService.GetDailyValuesPerPosition(data, from, to, req.Currency, acctModel)
	if err != nil {
		return nil, fmt.Errorf("computing per-position values: %w", err)
	}

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

	type pair struct {
		A, B        string  `json:"a,omitempty"`
		Correlation float64 `json:"correlation"`
	}
	var highCorr, lowCorr []pair
	for i := 0; i < len(result.Symbols); i++ {
		for j := i + 1; j < len(result.Symbols); j++ {
			c := math.Round(result.Matrix[i][j]*1000) / 1000
			if c > 0.7 {
				highCorr = append(highCorr, pair{result.Symbols[i], result.Symbols[j], c})
			}
			if c < 0.1 {
				lowCorr = append(lowCorr, pair{result.Symbols[i], result.Symbols[j], c})
			}
		}
	}

	return map[string]any{
		"symbols":             result.Symbols,
		"matrix":              result.Matrix,
		"highly_correlated":   highCorr,
		"low_or_uncorrelated": lowCorr,
		"period":              fmt.Sprintf("%s – %s", from.Format("Jan 2, 2006"), to.Format("Jan 2, 2006")),
	}, nil
}

// toolGetPositionsWithCostBasis returns open positions with qty, price, average cost basis, and unrealized gl.
func (h *LLMHandler) toolGetPositionsWithCostBasis(_ context.Context, data *models.FlexQueryData, req ChatRequest, args map[string]any) (map[string]any, error) {
	groupBy, _ := args["group_by"].(string)
	limitF, _ := args["limit"].(float64)

	acctModel := models.ParseAccountingModel(req.AccountingModel)
	result, err := h.PortfolioService.GetCurrentValue(data, req.Currency, acctModel)
	if err != nil {
		return nil, fmt.Errorf("computing portfolio value: %w", err)
	}

	symbols := make([]string, 0, len(result.Positions))
	for _, p := range result.Positions {
		if p.Value != 0 && p.Symbol != "PENDING_CASH" {
			symbols = append(symbols, p.Symbol)
		}
	}
	var fundamentals []models.AssetFundamental
	fundMap := make(map[string]models.AssetFundamental, len(symbols))
	if h.DB != nil && len(symbols) > 0 {
		h.DB.Where("symbol IN ?", symbols).Find(&fundamentals)
		for _, f := range fundamentals {
			fundMap[f.Symbol] = f
		}
	}

	type posDetail struct {
		Symbol       string  `json:"symbol,omitempty"`
		Name         string  `json:"name,omitempty"`
		Group        string  `json:"group,omitempty"`
		Quantity     float64 `json:"quantity,omitempty"`
		CostBasis    float64 `json:"cost_basis"`
		Value        float64 `json:"value"`
		UnrealizedGL float64 `json:"unrealized_gl"`
	}

	var items []posDetail

	if groupBy != "" {
		grouped := make(map[string]*posDetail)
		for _, p := range result.Positions {
			if p.Value == 0 || p.Symbol == "PENDING_CASH" {
				continue
			}
			f := fundMap[p.Symbol]
			g := "Unknown"
			if groupBy == "sector" && f.Sector != "" {
				g = f.Sector
			} else if groupBy == "country" && f.Country != "" {
				g = f.Country
			} else if groupBy == "asset_type" && f.AssetType != "" {
				g = f.AssetType
			}
			if _, ok := grouped[g]; !ok {
				grouped[g] = &posDetail{Group: g}
			}
			grouped[g].CostBasis += p.CostBasis * p.Quantity
			grouped[g].Value += p.Value
			grouped[g].UnrealizedGL += p.Value - (p.CostBasis * p.Quantity)
		}
		for _, g := range grouped {
			items = append(items, *g)
		}
	} else {
		for _, p := range result.Positions {
			if p.Value == 0 || p.Symbol == "PENDING_CASH" {
				continue
			}
			items = append(items, posDetail{
				Symbol:       p.Symbol,
				Name:         fundMap[p.Symbol].Name,
				Quantity:     math.Round(p.Quantity*1000) / 1000,
				CostBasis:    math.Round(p.CostBasis*100) / 100,
				Value:        math.Round(p.Value*100) / 100,
				UnrealizedGL: math.Round((p.Value-(p.CostBasis*p.Quantity))*100) / 100,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return math.Abs(items[i].Value) > math.Abs(items[j].Value)
	})

	if limitF > 0 && int(limitF) < len(items) {
		items = items[:int(limitF)]
	}

	return map[string]any{"positions": items, "currency": req.Currency}, nil
}

// toolGetTaxImpact evaluates tax figures for a given year.
func (h *LLMHandler) toolGetTaxImpact(_ context.Context, data *models.FlexQueryData, args map[string]any) (map[string]any, error) {
	yearF, ok := args["year"].(float64)
	if !ok || yearF == 0 {
		return nil, fmt.Errorf("year is required")
	}
	if h.TaxSvc == nil {
		return nil, fmt.Errorf("tax service is not configured")
	}

	report, err := h.TaxSvc.GetReport(data, int(yearF), nil)
	if err != nil {
		return nil, fmt.Errorf("calculating tax report: %w", err)
	}

	return map[string]any{
		"year":                             report.Year,
		"employment_income_cost_czk":       report.EmploymentIncome.TotalCostCZK,
		"employment_income_benefit_czk":    report.EmploymentIncome.TotalBenefitCZK,
		"employment_income_net_profit_czk": report.EmploymentIncome.TotalBenefitCZK - report.EmploymentIncome.TotalCostCZK,
		"investment_income_cost_czk":       report.InvestmentIncome.TotalCostCZK,
		"investment_income_benefit_czk":    report.InvestmentIncome.TotalBenefitCZK,
		"investment_income_net_profit_czk": report.InvestmentIncome.TotalBenefitCZK - report.InvestmentIncome.TotalCostCZK,
		"note":                             "Values are strictly in CZK per Czech tax rules.",
	}, nil
}

// toolGetRecentTransactions returns top N trades for a symbol.
func (h *LLMHandler) toolGetRecentTransactions(_ context.Context, data *models.FlexQueryData, req ChatRequest, args map[string]any) (map[string]any, error) {
	sym, _ := args["symbol"].(string)
	if sym == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	limitF, ok := args["limit"].(float64)
	if !ok || limitF <= 0 {
		limitF = 10
	}
	if limitF > 100 {
		limitF = 100
	}
	groupBy, _ := args["group_by"].(string)

	tradesResp, err := h.PortfolioService.GetTradesForSymbol(data, sym, "", req.Currency, models.AccountingModelHistorical)
	if err != nil {
		return nil, fmt.Errorf("fetching trades: %w", err)
	}

	limit := int(limitF)
	if len(tradesResp.Trades) > limit {
		tradesResp.Trades = tradesResp.Trades[:limit]
	}

	if groupBy != "" {
		type groupedTrade struct {
			Group    string  `json:"group"`
			Quantity float64 `json:"total_quantity"`
			Proceeds float64 `json:"total_proceeds"`
			Count    int     `json:"trade_count"`
		}
		grouped := make(map[string]*groupedTrade)
		for _, t := range tradesResp.Trades {
			g := "Unknown"
			if groupBy == "side" {
				g = t.Side
			} else if groupBy == "year" && len(t.Date) >= 4 {
				g = t.Date[:4]
			}
			if _, ok := grouped[g]; !ok {
				grouped[g] = &groupedTrade{Group: g}
			}
			grouped[g].Quantity += t.Quantity
			grouped[g].Proceeds += t.Proceeds
			grouped[g].Count++
		}
		var items []groupedTrade
		for _, v := range grouped {
			items = append(items, *v)
		}
		return map[string]any{
			"symbol":           sym,
			"display_currency": req.Currency,
			"grouped_trades":   items,
		}, nil
	}

	return map[string]any{
		"symbol":           sym,
		"display_currency": req.Currency,
		"recent_trades":    tradesResp.Trades,
	}, nil
}

// toolGetFXImpact evaluates value difference between Spot and Historical FX.
func (h *LLMHandler) toolGetFXImpact(_ context.Context, data *models.FlexQueryData, req ChatRequest) (map[string]any, error) {
	spotVal, err := h.PortfolioService.GetCurrentValue(data, req.Currency, models.AccountingModelSpot)
	if err != nil {
		return nil, fmt.Errorf("spot err: %w", err)
	}
	histVal, err := h.PortfolioService.GetCurrentValue(data, req.Currency, models.AccountingModelHistorical)
	if err != nil {
		return nil, fmt.Errorf("historical err: %w", err)
	}

	return map[string]any{
		"currency":                req.Currency,
		"spot_portfolio_value":    spotVal.Value,
		"history_portfolio_value": histVal.Value,
		"fx_impact_value":         spotVal.Value - histVal.Value,
		"fx_impact_pct":           (spotVal.Value - histVal.Value) / histVal.Value * 100,
	}, nil
}

// toolGetHistoricalPerformance gets portfolio daily value array with pre-computed analytics.
func (h *LLMHandler) toolGetHistoricalPerformance(_ context.Context, data *models.FlexQueryData, req ChatRequest, args map[string]any) (map[string]any, error) {
	fromStr, _ := args["from_date"].(string)
	toStr, _ := args["to_date"].(string)
	if toStr == "" {
		toStr = time.Now().Format("2006-01-02")
	}
	if fromStr == "" {
		fromStr = "2000-01-01"
	}

	from, to, err := parseDateStrings(fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("invalid dates: %w", err)
	}

	acctModel := models.ParseAccountingModel(req.AccountingModel)

	resp, err := h.PortfolioService.GetDailyValues(data, from, to, req.Currency, acctModel)
	if err != nil {
		return nil, fmt.Errorf("get daily values: %w", err)
	}

	portfolioReturns, _, endDates, retErr := h.PortfolioService.GetDailyReturns(data, from, to, req.Currency, acctModel)

	analytics := map[string]any{}
	if retErr == nil && len(portfolioReturns) > 0 {
		ddSeries := stats.CalculateDrawdownSeries(portfolioReturns, endDates)

		type drawdownEvent struct {
			TroughDate string  `json:"trough_date"`
			Drawdown   float64 `json:"drawdown_pct"`
		}
		var events []drawdownEvent
		for _, pt := range ddSeries {
			if pt.DrawdownPct < -0.01 {
				events = append(events, drawdownEvent{pt.Date, math.Round(pt.DrawdownPct*10000) / 100})
			}
		}
		sort.Slice(events, func(i, j int) bool { return events[i].Drawdown < events[j].Drawdown })
		if len(events) > 3 {
			events = events[:3]
		}
		analytics["top_drawdowns"] = events

		type monthReturn struct {
			Month  string  `json:"month"`
			Return float64 `json:"return_pct"`
		}
		monthlyReturns := map[string]float64{}
		for i, r := range portfolioReturns {
			if i < len(endDates) {
				month := endDates[i][:7]
				prev := monthlyReturns[month]
				monthlyReturns[month] = (1+prev)*(1+r) - 1
			}
		}
		var bestMonth, worstMonth monthReturn
		first := true
		for m, r := range monthlyReturns {
			pct := math.Round(r * 10000) / 100
			if first || pct > bestMonth.Return {
				bestMonth = monthReturn{m, pct}
			}
			if first || pct < worstMonth.Return {
				worstMonth = monthReturn{m, pct}
			}
			first = false
		}
		analytics["best_month"] = bestMonth
		analytics["worst_month"] = worstMonth
	}

	seriesData := resp.Data
	freq := "daily"
	if len(resp.Data) > 60 {
		var sampled []models.DailyValue
		for i := 0; i < len(resp.Data); i += 21 {
			sampled = append(sampled, resp.Data[i])
		}
		if len(sampled) > 0 && sampled[len(sampled)-1].Date != resp.Data[len(resp.Data)-1].Date {
			sampled = append(sampled, resp.Data[len(resp.Data)-1])
		}
		seriesData = sampled
		freq = "monthly"
	}

	return map[string]any{
		"sampled_frequency": freq,
		"data":              seriesData,
		"analytics":         analytics,
	}, nil
}

// toolSimulateScenario handles the 'simulate_scenario' tool call to build and analyze hypothetical counterfactuals.
func (h *LLMHandler) toolSimulateScenario(_ context.Context, req ChatRequest, args map[string]any, userHash string) (map[string]any, error) {
	// Import is needed for scenariosvc — it's in llm.go via existing import
	return h.doSimulateScenario(req, args, userHash)
}

// toolRunPortfolioAnalysis runs the Python sandbox with the portfolio trades dataframe.
func (h *LLMHandler) toolRunPortfolioAnalysis(ctx context.Context, data *models.FlexQueryData, args map[string]any) (map[string]any, error) {
	if h.SandboxSvc == nil || !h.SandboxSvc.IsEnabled() {
		return nil, fmt.Errorf("Python sandbox is not available")
	}
	code, _ := args["code"].(string)
	if code == "" {
		return nil, fmt.Errorf("code is required")
	}
	desc, _ := args["description"].(string)
	slog.Info("llm: running Python analysis", "description", desc, "code_len", len(code))
	
	result, err := h.SandboxSvc.Execute(ctx, code, data.Trades, data.CashTransactions)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	resp := map[string]any{"output": result.Output}
	if result.Error != "" {
		resp["python_error"] = result.Error
	}
	return resp, nil
}

// isNotFound checks if the error is a GORM "record not found" error.
func isNotFound(err error) bool {
	// Use errors.Is with a sentinel — import gorm ErrRecordNotFound in file that calls this.
	// This helper avoids importing gorm in llm_tools.go directly.
	return err != nil && strings.Contains(err.Error(), "record not found")
}

// buildExecutor creates a ToolExecutor closure bound to the current request's data and HTTP context.
// It routes each named function call to the appropriate backend method and returns a JSON-serialisable map.
func (h *LLMHandler) buildExecutor(data *models.FlexQueryData, req ChatRequest, userHash string) llm.ToolExecutor {
	return func(ctx context.Context, call *genai.FunctionCall) (map[string]any, error) {
		switch call.Name {

		case llm.ToolGetCurrentAllocations:
			return h.toolGetCurrentAllocations(ctx, data, req)

		case llm.ToolGetRiskMetrics:
			return h.toolGetRiskMetrics(ctx, data, req, call.Args)

		case llm.ToolGetBenchmarkMetrics:
			return h.toolGetBenchmarkMetrics(ctx, data, req, call.Args)

		case llm.ToolGetAssetFundamentals:
			return h.toolGetAssetFundamentals(ctx, call.Args)

		case llm.ToolGetTaxImpact:
			return h.toolGetTaxImpact(ctx, data, call.Args)

		case llm.ToolGetPositionsWithCostBasis:
			return h.toolGetPositionsWithCostBasis(ctx, data, req, call.Args)

		case llm.ToolGetRecentTransactions:
			return h.toolGetRecentTransactions(ctx, data, req, call.Args)

		case llm.ToolGetFXImpact:
			return h.toolGetFXImpact(ctx, data, req)

		case llm.ToolGetHistoricalPerformance:
			return h.toolGetHistoricalPerformance(ctx, data, req, call.Args)

		case llm.ToolSimulateScenario:
			return h.toolSimulateScenario(ctx, req, call.Args, userHash)

		case llm.ToolGetPortfolioBreakdown:
			return h.toolGetPortfolioBreakdown(ctx, data, req)

		case llm.ToolGetCorrelations:
			return h.toolGetCorrelations(ctx, data, req, call.Args)

		case llm.ToolRunPortfolioAnalysis:
			return h.toolRunPortfolioAnalysis(ctx, data, call.Args)

		default:
			return nil, fmt.Errorf("unknown tool: %s", call.Name)
		}
	}
}
