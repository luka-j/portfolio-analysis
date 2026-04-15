package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/genai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/tax"
)

// LLMHandler manages LLM text generation and AI analysis endpoints.
type LLMHandler struct {
	Repo             *flexquery.Repository
	DB               *gorm.DB
	LLM              *llm.Service
	PortfolioService *portfolio.Service
	TaxSvc           *tax.Service
	MarketProvider   market.Provider
	CurrencyGetter   market.CurrencyGetter
}

// NewLLMHandler creates a new handler.
func NewLLMHandler(
	repo *flexquery.Repository,
	db *gorm.DB,
	llmSvc *llm.Service,
	ps *portfolio.Service,
	ts *tax.Service,
	mp market.Provider,
	cg market.CurrencyGetter,
) *LLMHandler {
	return &LLMHandler{
		Repo:             repo,
		DB:               db,
		LLM:              llmSvc,
		PortfolioService: ps,
		TaxSvc:           ts,
		MarketProvider:   mp,
		CurrencyGetter:   cg,
	}
}

// IsAvailable handles GET /api/v1/llm/available
func (h *LLMHandler) IsAvailable(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"available": h.LLM.APIKey != "", "canned_model": h.LLM.DefaultModelKey})
}

// GetSummary handles GET /api/v1/llm/summary?period=1d
func (h *LLMHandler) GetSummary(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	// Validate period
	period := c.DefaultQuery("period", "1d")
	if period != "1d" && period != "1w" && period != "1m" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid period"})
		return
	}

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		log.Printf("ERROR: GetSummary LoadSaved failed [user=%s]: %v", userHash[:8], err)
		c.JSON(http.StatusNotFound, gin.H{"error": "user data not found"})
		return
	}

	forceRefresh := c.Query("force_refresh") == "true"
	promptType := "summary_" + period
	modelKey := h.LLM.FlashModel

	// Check Cache (valid for 8h, skipped when force_refresh=true)
	var cacheEntry models.LLMCache
	cacheFound := h.DB.Where("user_hash = ? AND prompt_type = ? AND model = ?", userHash, promptType, modelKey).First(&cacheEntry).Error == nil
	if !forceRefresh && cacheFound && time.Since(cacheEntry.CreatedAt) < 8*time.Hour {
		c.JSON(http.StatusOK, gin.H{"summary": cacheEntry.Response})
		return
	}

	// Call LLM
	log.Printf("INFO: GetMarketSummary calling LLM [user=%s period=%s]", userHash[:8], period)
	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 130*time.Second)
	defer cancel()
	summary, err := h.LLM.GetMarketSummary(reqCtx, data, period)
	if err != nil {
		if errors.Is(err, llm.ErrNotConfigured) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": "NOT_CONFIGURED"})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			log.Printf("WARN: GetMarketSummary timed out [user=%s period=%s]", userHash[:8], period)
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "Model timed out. The servers may be overloaded, try again later or with a different model."})
			return
		}
		log.Printf("ERROR: GetMarketSummary failed [user=%s period=%s]: %v", userHash[:8], period, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "generating summary: " + err.Error()})
		return
	}

	// Upsert Cache entry
	cacheEntry.UserHash = userHash
	cacheEntry.PromptType = promptType
	cacheEntry.Model = modelKey
	cacheEntry.Response = summary
	cacheEntry.CreatedAt = time.Now()

	err = h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_hash"}, {Name: "prompt_type"}, {Name: "model"}},
		DoUpdates: clause.AssignmentColumns([]string{"response", "created_at"}),
	}).Create(&cacheEntry).Error

	if err != nil {
		log.Printf("WARN: GetSummary failed to save cache [user=%s period=%s]: %v", userHash[:8], period, err)
	}

	c.JSON(http.StatusOK, gin.H{"summary": summary})
}

// ChatRequest defines the payload for LLM chatting.
type ChatRequest struct {
	PromptType string `json:"prompt_type"` // "general_analysis", "best_worst_scenarios", "ticker_analysis", "risk_metrics", "benchmark_analysis", "upcoming_events", "freeform"
	Message    string `json:"message"`
	Currency   string `json:"currency"`
	Model      string `json:"model"` // "flash" | "pro" (default "pro")

	// Freeform-only fields (ignored for canned prompts).
	EnabledTools             []string               `json:"enabled_tools"`              // dynamically allowed tool names
	OverridePortfolioWeights []llm.CustomWeight     `json:"override_portfolio_weights"` // overrides live portfolio weights
	History                  []llm.ConversationTurn `json:"history"`                    // prior conversation turns

	// ticker_analysis
	Symbol string `json:"symbol"` // ticker symbol to analyse

	// risk_metrics and benchmark_analysis
	From            string  `json:"from"`             // ISO date YYYY-MM-DD
	To              string  `json:"to"`               // ISO date YYYY-MM-DD
	AccountingModel string  `json:"accounting_model"` // "historical" | "spot" (default "historical")
	RiskFreeRate    float64 `json:"risk_free_rate"`   // annualised; 0 → defaults to 0.05
	ForceRefresh    bool    `json:"force_refresh"`

	// benchmark_analysis
	BenchmarkSymbol string `json:"benchmark_symbol"`

	// long_market_summary
	Period string `json:"period"` // "1d" | "1w" | "1m"
}

// toolCallLabel maps internal tool names to user-friendly display strings.
var toolCallLabel = map[string]string{
	llm.ToolGetCurrentAllocations:    "Fetching portfolio allocations",
	llm.ToolGetRiskMetrics:           "Computing risk & return metrics",
	llm.ToolGetBenchmarkMetrics:      "Computing benchmark comparison",
	llm.ToolGetAssetFundamentals:     "Looking up asset fundamentals",
	llm.ToolGetTaxImpact:             "Calculating tax impact",
	llm.ToolGetPositionsWithCostBasis: "Fetching positions and cost bases",
	llm.ToolGetRecentTransactions:    "Fetching recent transactions",
	llm.ToolGetFXImpact:              "Calculating FX impact",
	llm.ToolGetHistoricalPerformance: "Fetching historical performance",
}

// Chat handles POST /api/v1/llm/chat
func (h *LLMHandler) Chat(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request format"})
		return
	}

	if req.Currency == "" {
		req.Currency = "USD"
	}

	cannedType := ""
	if llm.IsValidCannedType(req.PromptType) {
		cannedType = req.PromptType
	} else if req.PromptType != "freeform" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown prompt_type"})
		return
	}

	// Resolve the effective model key then the full model name once, so the same
	// value is used for the cache key, the log, and the LLM call.
	// Canned prompts fall back to the service's configured default when the caller
	// does not specify; freeform defaults to "flash".
	effectiveKey := req.Model
	if effectiveKey != "flash" && effectiveKey != "pro" {
		if cannedType != "" {
			effectiveKey = h.LLM.DefaultModelKey
		} else {
			effectiveKey = "flash"
		}
	}
	modelKey := h.LLM.ResolveModel(effectiveKey)

	// override weights only apply to freeform.
	var overrideWeights []llm.CustomWeight
	if cannedType == "" && req.OverridePortfolioWeights != nil {
		overrideWeights = req.OverridePortfolioWeights
	}

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		log.Printf("ERROR: Chat LoadSaved failed [user=%s]: %v", userHash[:8], err)
		c.JSON(http.StatusNotFound, gin.H{"error": "user data not found"})
		return
	}

	// Cache retrieval for cacheable canned prompts (keyed by user, prompt type, and model).
	var cacheEntry models.LLMCache
	cacheKey := req.PromptType
	if req.Symbol != "" {
		cacheKey += ":" + req.Symbol
	}
	if req.BenchmarkSymbol != "" {
		cacheKey += ":" + req.BenchmarkSymbol
	}

	if cannedType != "" && llm.CannedPrompts[cannedType].Cacheable {
		cacheFound := h.DB.Where("user_hash = ? AND prompt_type = ? AND model = ?", userHash, cacheKey, modelKey).First(&cacheEntry).Error == nil
		if cacheFound && !req.ForceRefresh && time.Since(cacheEntry.CreatedAt) < 8*time.Hour {
			c.Writer.Header().Set("Content-Type", "text/event-stream")
			c.Writer.Header().Set("Cache-Control", "no-cache")
			c.Writer.Header().Set("Connection", "keep-alive")
			c.SSEvent("message", gin.H{"response": cacheEntry.Response, "cached": true})
			c.Writer.Flush()
			return
		}
	}

	// Pre-render message for canned prompts that still use template vars (ticker_analysis, etc.).
	// Tool-first canned prompts (ForcedTool != "") no longer carry {data_json}; their messages are
	// sent verbatim after minimal template expansion.
	message := req.Message
	if cannedType != "" {
		var renderErr error
		message, renderErr = h.renderCannedPrompt(req, data)
		if renderErr != nil {
			log.Printf("ERROR: renderCannedPrompt failed [user=%s type=%s]: %v", userHash[:8], cannedType, renderErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "preparing prompt: " + renderErr.Error()})
			return
		}
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	// Build the ToolExecutor bound to this request's data and portfolio context.
	executor := h.buildExecutor(data, req)

	// Call LLM
	log.Printf("INFO: AnalyzePortfolio calling LLM [user=%s prompt_type=%s model=%s currency=%s enabledToolsCount=%d overrideWeights=%v]",
		userHash[:8], req.PromptType, modelKey, req.Currency, len(req.EnabledTools), overrideWeights != nil)
	var history []llm.ConversationTurn
	if cannedType == "" {
		history = req.History
	}

	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()

	response, sections, err := h.LLM.AnalyzePortfolioStream(
		reqCtx, data, req.Currency, cannedType, message,
		modelKey, req.EnabledTools, overrideWeights, history, req.AccountingModel,
		executor,
		func(chunk string) error {
			c.SSEvent("chunk", chunk)
			c.Writer.Flush()
			return nil
		},
		func(toolName string) error {
			label := toolCallLabel[toolName]
			if label == "" {
				label = toolName
			}
			c.SSEvent("tool_call", gin.H{"tool": toolName, "label": label})
			c.Writer.Flush()
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, llm.ErrNotConfigured) {
			c.SSEvent("error", gin.H{"error": err.Error(), "code": "NOT_CONFIGURED"})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			log.Printf("WARN: AnalyzePortfolio timed out [user=%s prompt_type=%s]", userHash[:8], req.PromptType)
			c.SSEvent("error", gin.H{"error": "Model timed out. The servers may be overloaded, try again later or with a different model."})
			return
		}
		log.Printf("ERROR: AnalyzePortfolio failed [user=%s prompt_type=%s currency=%s]: %v", userHash[:8], req.PromptType, req.Currency, err)
		c.SSEvent("error", gin.H{"error": "generating analysis: " + err.Error()})
		return
	}

	// Upsert Cache for cacheable canned prompts
	if cannedType != "" && llm.CannedPrompts[cannedType].Cacheable {
		cacheEntry.UserHash = userHash
		cacheEntry.PromptType = cacheKey
		cacheEntry.Model = modelKey
		cacheEntry.Response = response
		cacheEntry.CreatedAt = time.Now()

		err = h.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_hash"}, {Name: "prompt_type"}, {Name: "model"}},
			DoUpdates: clause.AssignmentColumns([]string{"response", "created_at"}),
		}).Create(&cacheEntry).Error

		if err != nil {
			log.Printf("WARN: Chat failed to save cache [user=%s prompt_type=%s]: %v", userHash[:8], req.PromptType, err)
		}
	}

	donePayload := gin.H{"response": response}
	if sections != nil {
		donePayload["sections"] = sections
	}
	c.SSEvent("done", donePayload)
	c.Writer.Flush()
}

// renderCannedPrompt builds the fully-rendered message for a canned prompt type.
// Tool-first prompts (ForcedTool set) only need minimal template variable expansion.
// Data-injection prompts (ticker_analysis, long_market_summary, upcoming_events) compute their vars here.
func (h *LLMHandler) renderCannedPrompt(req ChatRequest, data *models.FlexQueryData) (string, error) {
	cp := llm.CannedPrompts[req.PromptType]

	switch req.PromptType {

	case "long_market_summary":
		periodMap := map[string]string{
			"1d": "the past day",
			"1w": "the past week",
			"1m": "the past month",
		}
		periodText, ok := periodMap[req.Period]
		if !ok {
			periodText = "the past day"
		}
		return cp.Render(map[string]string{"period": periodText}), nil

	case "ticker_analysis":
		return h.renderTickerAnalysis(&cp, req)

	case "risk_metrics":
		// Tool-first: message only needs the hint about which date range to use.
		// The actual metrics are fetched by the model calling get_risk_metrics().
		from, to, err := parseDateStrings(req.From, req.To)
		if err != nil {
			return "", fmt.Errorf("invalid dates: %w", err)
		}
		rfr := req.RiskFreeRate
		if rfr == 0 {
			rfr = 0.05
		}
		return cp.Render(map[string]string{
			"from":           from.Format("Jan 2, 2006"),
			"to":             to.Format("Jan 2, 2006"),
			"risk_free_rate": fmt.Sprintf("%.4f", rfr),
		}), nil

	case "benchmark_analysis":
		if req.BenchmarkSymbol == "" {
			return "", fmt.Errorf("benchmark_symbol is required for benchmark_analysis")
		}
		from, to, err := parseDateStrings(req.From, req.To)
		if err != nil {
			return "", fmt.Errorf("invalid dates: %w", err)
		}
		rfr := req.RiskFreeRate
		if rfr == 0 {
			rfr = 0.05
		}
		return cp.Render(map[string]string{
			"benchmark":      req.BenchmarkSymbol,
			"from":           from.Format("Jan 2, 2006"),
			"to":             to.Format("Jan 2, 2006"),
			"risk_free_rate": fmt.Sprintf("%.4f", rfr),
		}), nil

	case "upcoming_events":
		return cp.Render(map[string]string{
			"current_date": time.Now().Format("Jan 2, 2006"),
		}), nil

	default:
		// general_analysis, best_worst_scenarios, add_or_trim — no template vars.
		return cp.Message, nil
	}
}

func (h *LLMHandler) renderTickerAnalysis(cp *llm.CannedPrompt, req ChatRequest) (string, error) {
	if req.Symbol == "" {
		return "", fmt.Errorf("symbol is required for ticker_analysis")
	}
	var fund models.AssetFundamental
	err := h.DB.Select("name").Where("symbol = ?", req.Symbol).First(&fund).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("database err querying ticker %s: %w", req.Symbol, err)
	}

	label := req.Symbol
	if fund.Name != "" {
		label = req.Symbol + " (" + fund.Name + ")"
	}
	return cp.Render(map[string]string{"label": label}), nil
}

// buildExecutor creates a ToolExecutor closure bound to the current request's data and HTTP context.
// It routes each named function call to the appropriate backend method and returns a JSON-serialisable map.
func (h *LLMHandler) buildExecutor(data *models.FlexQueryData, req ChatRequest) llm.ToolExecutor {
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
			return h.toolGetPositionsWithCostBasis(ctx, data, req)

		case llm.ToolGetRecentTransactions:
			return h.toolGetRecentTransactions(ctx, data, req, call.Args)

		case llm.ToolGetFXImpact:
			return h.toolGetFXImpact(ctx, data, req)

		case llm.ToolGetHistoricalPerformance:
			return h.toolGetHistoricalPerformance(ctx, data, req, call.Args)

		default:
			return nil, fmt.Errorf("unknown tool: %s", call.Name)
		}
	}
}

// toolGetCurrentAllocations returns portfolio holdings with percentage weights (no absolute values).
func (h *LLMHandler) toolGetCurrentAllocations(_ context.Context, data *models.FlexQueryData, req ChatRequest) (map[string]any, error) {
	acctModel := models.ParseAccountingModel(req.AccountingModel)
	result, err := h.PortfolioService.GetCurrentValue(data, req.Currency, acctModel, false)
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
	// Batch name lookup.
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

	metrics, err := computePortfolioMetrics(h.PortfolioService, data, from, to, req.Currency, acctModel, rfr, false)
	if err != nil {
		return nil, fmt.Errorf("computing portfolio metrics: %w", err)
	}

	result := map[string]any{
		"period":        fmt.Sprintf("%s – %s", from.Format("Jan 2, 2006"), to.Format("Jan 2, 2006")),
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

	metrics, err := computePortfolioMetrics(h.PortfolioService, data, from, to, req.Currency, acctModel, rfr, false)
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

	// First: local DB lookup.
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
			// For ETFs, include breakdown weights if available.
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
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("WARN: toolGetAssetFundamentals DB error for %s: %v", symbol, err)
		}
	}

	// Fallback: MarketProvider doesn't expose a profile interface; return a minimal record
	// telling the model to use Google Search for additional information.
	return map[string]any{
		"symbol": symbol,
		"note":   "No local data found for this ticker. Use Google Search to look up asset class, country, sector, and other fundamentals.",
		"source": "not_found",
	}, nil
}

// toolGetPositionsWithCostBasis returns open positions with qty, price, average cost basis, and unrealized gl.
func (h *LLMHandler) toolGetPositionsWithCostBasis(_ context.Context, data *models.FlexQueryData, req ChatRequest) (map[string]any, error) {
	acctModel := models.ParseAccountingModel(req.AccountingModel)
	result, err := h.PortfolioService.GetCurrentValue(data, req.Currency, acctModel, false)
	if err != nil {
		return nil, fmt.Errorf("computing portfolio value: %w", err)
	}

	type posDetail struct {
		Symbol       string  `json:"symbol"`
		Quantity     float64 `json:"quantity"`
		Price        float64 `json:"price"`
		CostBasis    float64 `json:"cost_basis"`
		Value        float64 `json:"value"`
		UnrealizedGL float64 `json:"unrealized_gl"`
	}

	positions := make([]posDetail, 0, len(result.Positions))
	for _, p := range result.Positions {
		if p.Value == 0 || p.Symbol == "PENDING_CASH" {
			continue
		}
		positions = append(positions, posDetail{
			Symbol:       p.Symbol,
			Quantity:     p.Quantity,
			Price:        p.Price,
			CostBasis:    p.CostBasis,
			Value:        p.Value,
			UnrealizedGL: p.Value - (p.CostBasis * p.Quantity),
		})
	}
	return map[string]any{"positions": positions, "currency": req.Currency}, nil
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
	if limitF > 50 {
		limitF = 50 // cap
	}

	tradesResp, err := h.PortfolioService.GetTradesForSymbol(data, sym, "", req.Currency)
	if err != nil {
		return nil, fmt.Errorf("fetching trades: %w", err)
	}

	limit := int(limitF)
	if len(tradesResp.Trades) > limit {
		tradesResp.Trades = tradesResp.Trades[:limit]
	}
	return map[string]any{
		"symbol":           sym,
		"display_currency": req.Currency,
		"recent_trades":    tradesResp.Trades,
	}, nil
}

// toolGetFXImpact evaluates value difference between Spot and Historical FX.
func (h *LLMHandler) toolGetFXImpact(_ context.Context, data *models.FlexQueryData, req ChatRequest) (map[string]any, error) {
	spotVal, err := h.PortfolioService.GetCurrentValue(data, req.Currency, models.AccountingModelSpot, false)
	if err != nil {
		return nil, fmt.Errorf("spot err: %w", err)
	}
	histVal, err := h.PortfolioService.GetCurrentValue(data, req.Currency, models.AccountingModelHistorical, false)
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

// toolGetHistoricalPerformance gets portfolio daily value array
func (h *LLMHandler) toolGetHistoricalPerformance(_ context.Context, data *models.FlexQueryData, req ChatRequest, args map[string]any) (map[string]any, error) {
	fromStr, _ := args["from_date"].(string)
	toStr, _ := args["to_date"].(string)
	if toStr == "" {
		toStr = time.Now().Format("2006-01-02")
	}
	if fromStr == "" {
		fromStr = "2000-01-01" // dummy fallback
	}

	from, to, err := parseDateStrings(fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("invalid dates: %w", err)
	}

	resp, err := h.PortfolioService.GetDailyValues(data, from, to, req.Currency, models.ParseAccountingModel(req.AccountingModel), false)
	if err != nil {
		return nil, fmt.Errorf("get daily values: %w", err)
	}

	if len(resp.Data) > 60 {
		var sampled []models.DailyValue
		for i := 0; i < len(resp.Data); i += 21 {
			sampled = append(sampled, resp.Data[i])
		}
		if len(sampled) > 0 && sampled[len(sampled)-1].Date != resp.Data[len(resp.Data)-1].Date {
			sampled = append(sampled, resp.Data[len(resp.Data)-1])
		}
		return map[string]any{
			"sampled_frequency": "monthly",
			"data":              sampled,
		}, nil
	}
	return map[string]any{"sampled_frequency": "daily", "data": resp.Data}, nil
}

