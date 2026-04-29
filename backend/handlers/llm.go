package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	breakdownsvc "portfolio-analysis/services/breakdown"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	"portfolio-analysis/services/tax"
)

// LLMHandler manages LLM text generation and AI analysis endpoints.
type LLMHandler struct {
	PortfolioResolver
	Repo               *flexquery.Repository
	DB                 *gorm.DB
	LLM                *llm.Service
	PortfolioService   *portfolio.Service
	TaxSvc             *tax.Service
	MarketProvider     market.Provider
	CurrencyGetter     market.CurrencyGetter
	BreakdownSvc       *breakdownsvc.Service
	DefaultRiskFreeRate float64
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
	bs *breakdownsvc.Service,
	defaultRFR float64,
) *LLMHandler {
	return &LLMHandler{
		Repo:               repo,
		DB:                 db,
		LLM:                llmSvc,
		PortfolioService:   ps,
		TaxSvc:             ts,
		MarketProvider:     mp,
		CurrencyGetter:     cg,
		BreakdownSvc:       bs,
		DefaultRiskFreeRate: defaultRFR,
	}
}

// IsAvailable handles GET /api/v1/llm/available
func (h *LLMHandler) IsAvailable(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"available": h.LLM.APIKey != "", "canned_model": h.LLM.DefaultModelKey})
}

// GetSummary handles GET /api/v1/llm/summary?period=1d
func (h *LLMHandler) GetSummary(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	period := c.DefaultQuery("period", "1d")
	if period != "1d" && period != "1w" && period != "1m" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid period"})
		return
	}

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	forceRefresh := c.Query("force_refresh") == "true"
	promptType := "summary_" + period
	modelKey := h.LLM.FlashModel

	var cacheEntry models.LLMCache
	cacheFound := h.DB.Where("user_hash = ? AND prompt_type = ? AND model = ?", userHash, promptType, modelKey).First(&cacheEntry).Error == nil
	if !forceRefresh && cacheFound && time.Since(cacheEntry.CreatedAt) < 8*time.Hour {
		c.JSON(http.StatusOK, gin.H{"summary": cacheEntry.Response})
		return
	}

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
	PromptType string `json:"prompt_type"`
	Message    string `json:"message"`
	Currency   string `json:"currency"`
	Model      string `json:"model"` // "flash" | "pro" (default "pro")

	// Freeform-only fields (ignored for canned prompts).
	EnabledTools []string               `json:"enabled_tools"`
	History      []llm.ConversationTurn `json:"history"`

	// ticker_analysis
	Symbol string `json:"symbol"`

	// risk_metrics and benchmark_analysis
	From            string  `json:"from"`
	To              string  `json:"to"`
	AccountingModel string  `json:"accounting_model"`
	RiskFreeRate    float64 `json:"risk_free_rate"`
	ForceRefresh    bool    `json:"force_refresh"`

	// benchmark_analysis
	BenchmarkSymbol string `json:"benchmark_symbol"`

	// long_market_summary
	Period string `json:"period"`

	// risk_metrics_comparison / holdings_comparison
	ScenarioIDA *int `json:"scenario_id_a"`
	ScenarioIDB *int `json:"scenario_id_b"`
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
	llm.ToolSimulateScenario:         "Simulating scenario portfolio",
	llm.ToolGetPortfolioBreakdown:    "Computing portfolio breakdown",
	llm.ToolGetCorrelations:          "Computing portfolio correlations",
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

	effectiveKey := req.Model
	if effectiveKey != "flash" && effectiveKey != "pro" {
		if cannedType != "" {
			effectiveKey = h.LLM.DefaultModelKey
		} else {
			effectiveKey = "flash"
		}
	}
	modelKey := h.LLM.ResolveModel(effectiveKey)

	pctx, ok := h.loadPortfolioContext(c, h.Repo, userHash)
	if !ok {
		return
	}
	data := pctx.Data

	var contextNote string
	if pctx.Kind == "scenario" {
		contextNote = fmt.Sprintf("Conversation context: the user's active portfolio for this chat is a %s. This is a hypothetical counterfactual, NOT the user's real holdings. Scenario definition: %s. When the user asks about \"my portfolio\", they are referring to this active view unless they say otherwise.\n\n", pctx.Kind, pctx.ScenarioSummary)
	} else {
		contextNote = "Conversation context: the user's active portfolio for this chat is their real portfolio.\n\n"
	}
	req.Message = contextNote + req.Message

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

	message := req.Message
	if cannedType != "" {
		var renderErr error
		message, renderErr = h.renderCannedPrompt(req, data, userHash)
		if renderErr != nil {
			log.Printf("ERROR: renderCannedPrompt failed [user=%s type=%s]: %v", userHash[:8], cannedType, renderErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "preparing prompt: " + renderErr.Error()})
			return
		}
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	executor := h.buildExecutor(data, req, userHash)

	log.Printf("INFO: AnalyzePortfolio calling LLM [user=%s prompt_type=%s model=%s currency=%s enabledToolsCount=%d]",
		userHash[:8], req.PromptType, modelKey, req.Currency, len(req.EnabledTools))
	var history []llm.ConversationTurn
	if cannedType == "" {
		history = req.History
	}

	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()

	response, sections, err := h.LLM.AnalyzePortfolioStream(
		reqCtx, data, req.Currency, cannedType, message,
		modelKey, req.EnabledTools, history, req.AccountingModel,
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
func (h *LLMHandler) renderCannedPrompt(req ChatRequest, data *models.FlexQueryData, userHash string) (string, error) {
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

	case "risk_metrics_comparison", "holdings_comparison":
		return h.renderComparisonPrompt(req, data, userHash)

	default:
		// general_analysis, best_worst_scenarios, add_or_trim — no template vars.
		return cp.Message, nil
	}
}

// renderTickerAnalysis renders the ticker_analysis canned prompt.
func (h *LLMHandler) renderTickerAnalysis(cp *llm.CannedPrompt, req ChatRequest) (string, error) {
	if req.Symbol == "" {
		return "", fmt.Errorf("symbol is required for ticker_analysis")
	}
	var fund models.AssetFundamental
	err := h.DB.Select("name").Where("symbol = ?", req.Symbol).First(&fund).Error
	if err != nil && !isGormNotFound(err) {
		return "", fmt.Errorf("database err querying ticker %s: %w", req.Symbol, err)
	}

	label := req.Symbol
	if fund.Name != "" {
		label = req.Symbol + " (" + fund.Name + ")"
	}
	return cp.Render(map[string]string{"label": label}), nil
}

// llmCannedPrompts retrieves a canned prompt by type (used by llm_compare.go).
func llmCannedPrompts(promptType string) llm.CannedPrompt {
	return llm.CannedPrompts[promptType]
}

// isGormNotFound returns true when err is gorm.ErrRecordNotFound.
func isGormNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
