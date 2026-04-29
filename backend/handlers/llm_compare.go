package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	scenariosvc "portfolio-analysis/services/scenario"
	"portfolio-analysis/models"
)

// doSimulateScenario is the implementation for toolSimulateScenario, separated to avoid circular imports.
func (h *LLMHandler) doSimulateScenario(req ChatRequest, args map[string]any, userHash string) (map[string]any, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("encoding args: %w", err)
	}
	var spec scenariosvc.ScenarioSpec
	if err := json.Unmarshal(b, &spec); err != nil {
		return nil, fmt.Errorf("invalid scenario spec: %w", err)
	}

	realData, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		return nil, fmt.Errorf("loading real portfolio data: %w", err)
	}

	synthData, err := scenariosvc.Build(spec, realData, h.ScenarioMkt, h.ScenarioFX)
	if err != nil {
		return nil, fmt.Errorf("building scenario: %w", err)
	}

	result := map[string]any{
		"scenario_summary": scenariosvc.RenderSummary(spec),
		"note":           "This data is hypothetical (counterfactual). Discuss it as a 'what if' scenario.",
		"normalized_spec": spec,
	}

	metricsAny, ok := args["metrics"]
	var metrics []string
	if ok {
		if list, isArr := metricsAny.([]any); isArr {
			for _, item := range list {
				if s, isStr := item.(string); isStr {
					metrics = append(metrics, s)
				}
			}
		}
	}

	ctx := context.Background()

	subArgs := map[string]any{}
	if fd, ok := args["from_date"]; ok { subArgs["from_date"] = fd }
	if td, ok := args["to_date"]; ok { subArgs["to_date"] = td }
	if bs, ok := args["benchmark_symbol"]; ok { subArgs["benchmark_symbol"] = bs }

	for _, m := range metrics {
		if m == "allocations" {
			alloc, err := h.toolGetCurrentAllocations(ctx, synthData, req)
			if err == nil {
				result["allocations"] = alloc["holdings"]
			}
		} else if m == "holdings" {
			hl, err := h.toolGetPositionsWithCostBasis(ctx, synthData, req)
			if err == nil {
				result["holdings"] = hl["positions"]
			}
		} else if m == "risk" {
			rm, err := h.toolGetRiskMetrics(ctx, synthData, req, subArgs)
			if err == nil {
				result["risk_metrics"] = rm
			}
		} else if m == "historical_performance" {
			hp, err := h.toolGetHistoricalPerformance(ctx, synthData, req, subArgs)
			if err == nil {
				result["historical_performance"] = hp
			}
		} else if m == "benchmark" {
			bm, err := h.toolGetBenchmarkMetrics(ctx, synthData, req, subArgs)
			if err == nil {
				result["benchmark_metrics"] = bm
			}
		}
	}

	return result, nil
}

// renderComparisonPrompt builds the fully-rendered message for risk_metrics_comparison and
// holdings_comparison by pre-computing metrics for both scenarios server-side.
func (h *LLMHandler) renderComparisonPrompt(req ChatRequest, dataA *models.FlexQueryData, userHash string) (string, error) {
	cp := llmCannedPrompts(req.PromptType)

	from, to, err := parseDateStrings(req.From, req.To)
	if err != nil {
		return "", fmt.Errorf("invalid dates: %w", err)
	}
	rfr := req.RiskFreeRate
	if rfr == 0 {
		rfr = 0.05
	}
	acctModel, acctErr := models.ValidateAccountingModel(req.AccountingModel)
	if acctErr != nil {
		return "", acctErr
	}

	sidA := 0
	if req.ScenarioIDA != nil {
		sidA = *req.ScenarioIDA
	}
	sidB := 0
	if req.ScenarioIDB != nil {
		sidB = *req.ScenarioIDB
	}

	var realData *models.FlexQueryData
	if sidA == 0 {
		realData = dataA
	} else {
		realData, err = h.Repo.LoadSaved(userHash)
		if err != nil {
			return "", fmt.Errorf("loading real portfolio data: %w", err)
		}
	}

	dataB, nameB, err := h.loadComparisonScenarioData(sidB, realData, userHash)
	if err != nil {
		return "", fmt.Errorf("loading scenario B: %w", err)
	}

	nameA := "Real portfolio"
	if sidA > 0 {
		if n, nerr := h.scenarioDisplayName(sidA, userHash); nerr == nil {
			nameA = n
		} else {
			nameA = fmt.Sprintf("Scenario %d", sidA)
		}
	}

	fromFmt := from.Format("Jan 2, 2006")
	toFmt := to.Format("Jan 2, 2006")

	if req.PromptType == "risk_metrics_comparison" {
		mA, merr := computePortfolioMetrics(h.PortfolioService, dataA, from, to, req.Currency, acctModel, rfr, false)
		if merr != nil {
			return "", fmt.Errorf("computing metrics for A: %w", merr)
		}
		mB, merr := computePortfolioMetrics(h.PortfolioService, dataB, from, to, req.Currency, acctModel, rfr, false)
		if merr != nil {
			return "", fmt.Errorf("computing metrics for B: %w", merr)
		}
		fmtPct := func(v float64) string { return fmt.Sprintf("%.2f%%", v*100) }
		twrA, mwrA := fmtPct(mA.TWR), fmtPct(mA.MWR)
		if mA.TWRErr != "" { twrA = "N/A (" + mA.TWRErr + ")" }
		if mA.MWRErr != "" { mwrA = "N/A (" + mA.MWRErr + ")" }
		twrB, mwrB := fmtPct(mB.TWR), fmtPct(mB.MWR)
		if mB.TWRErr != "" { twrB = "N/A (" + mB.TWRErr + ")" }
		if mB.MWRErr != "" { mwrB = "N/A (" + mB.MWRErr + ")" }
		return cp.Render(map[string]string{
			"from": fromFmt, "to": toFmt,
			"rfr":       fmt.Sprintf("%.2f%%", rfr*100),
			"name_a":    nameA,
			"a_twr":     twrA,
			"a_mwr":     mwrA,
			"a_sharpe":  fmt.Sprintf("%.3f", mA.Standalone.SharpeRatio),
			"a_sortino": fmt.Sprintf("%.3f", mA.Standalone.SortinoRatio),
			"a_vami":    fmt.Sprintf("%.1f", mA.Standalone.VAMI),
			"a_vol":     fmtPct(mA.Standalone.Volatility),
			"a_dd":      fmtPct(mA.Standalone.MaxDrawdown),
			"name_b":    nameB,
			"b_twr":     twrB,
			"b_mwr":     mwrB,
			"b_sharpe":  fmt.Sprintf("%.3f", mB.Standalone.SharpeRatio),
			"b_sortino": fmt.Sprintf("%.3f", mB.Standalone.SortinoRatio),
			"b_vami":    fmt.Sprintf("%.1f", mB.Standalone.VAMI),
			"b_vol":     fmtPct(mB.Standalone.Volatility),
			"b_dd":      fmtPct(mB.Standalone.MaxDrawdown),
		}), nil
	}

	holdingsA := h.buildHoldingsSummary(dataA, req.Currency, acctModel)
	holdingsB := h.buildHoldingsSummary(dataB, req.Currency, acctModel)
	return cp.Render(map[string]string{
		"from": fromFmt, "to": toFmt,
		"name_a":     nameA,
		"holdings_a": holdingsA,
		"name_b":     nameB,
		"holdings_b": holdingsB,
	}), nil
}

// loadComparisonScenarioData loads FlexQueryData for a given scenario ID.
// id=0 returns realData with label "Real portfolio".
func (h *LLMHandler) loadComparisonScenarioData(id int, realData *models.FlexQueryData, userHash string) (*models.FlexQueryData, string, error) {
	if id == 0 {
		return realData, "Real portfolio", nil
	}
	if h.ScenarioRepo == nil {
		return nil, "", fmt.Errorf("scenarios not available")
	}
	var user models.User
	if err := h.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		return nil, "", fmt.Errorf("user not found")
	}
	row, err := h.ScenarioRepo.Get(user.ID, uint(id))
	if err != nil {
		return nil, "", fmt.Errorf("fetching scenario: %w", err)
	}
	if row == nil {
		return nil, "", fmt.Errorf("scenario %d not found", id)
	}
	spec, err := scenariosvc.ParseSpec(row)
	if err != nil {
		return nil, "", fmt.Errorf("parsing scenario spec: %w", err)
	}
	data, err := scenariosvc.Build(spec, realData, h.ScenarioMkt, h.ScenarioFX)
	if err != nil {
		return nil, "", fmt.Errorf("building scenario: %w", err)
	}
	data.UserHash = fmt.Sprintf("scenario:%d:%d:%s", id, row.UpdatedAt.UnixNano(), userHash)
	name := row.Name
	if name == "" {
		name = fmt.Sprintf("Scenario %d", id)
	}
	return data, name, nil
}

// scenarioDisplayName looks up the display name for a scenario.
func (h *LLMHandler) scenarioDisplayName(id int, userHash string) (string, error) {
	if h.ScenarioRepo == nil || h.DB == nil {
		return fmt.Sprintf("Scenario %d", id), nil
	}
	var user models.User
	if err := h.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		return fmt.Sprintf("Scenario %d", id), fmt.Errorf("user not found")
	}
	row, err := h.ScenarioRepo.Get(user.ID, uint(id))
	if err != nil || row == nil {
		return fmt.Sprintf("Scenario %d", id), err
	}
	if row.Name != "" {
		return row.Name, nil
	}
	return fmt.Sprintf("Scenario %d", id), nil
}

// buildHoldingsSummary returns a compact text list of top holdings by weight percentage.
func (h *LLMHandler) buildHoldingsSummary(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) string {
	result, err := h.PortfolioService.GetCurrentValue(data, currency, acctModel, false)
	if err != nil || result.Value == 0 {
		return "(no holdings data)"
	}
	var sb fmt.Stringer
	_ = sb
	out := ""
	for _, p := range result.Positions {
		if p.Value == 0 || p.Symbol == "PENDING_CASH" {
			continue
		}
		out += fmt.Sprintf("- %s: %.1f%%\n", p.Symbol, p.Value/result.Value*100)
	}
	if out == "" {
		return "(no holdings data)"
	}
	// Trim trailing newline
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out
}
