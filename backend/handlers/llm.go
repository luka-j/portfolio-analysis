package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
	"portfolio-analysis/services/sandbox"
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
	SandboxSvc         *sandbox.Service
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
	sb *sandbox.Service,
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
		SandboxSvc:         sb,
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

	slog.Info("llm: GetMarketSummary calling LLM", "user", userHash[:8], "period", period)
	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 130*time.Second)
	defer cancel()
	summary, err := h.LLM.GetMarketSummary(reqCtx, data, period)
	if err != nil {
		if errors.Is(err, llm.ErrNotConfigured) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": "NOT_CONFIGURED"})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("llm: GetMarketSummary timed out", "user", userHash[:8], "period", period)
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "Model timed out. The servers may be overloaded, try again later or with a different model."})
			return
		}
		slog.Error("llm: GetMarketSummary failed", "user", userHash[:8], "period", period, "err", err)
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
		slog.Warn("llm: GetSummary failed to save cache", "user", userHash[:8], "period", period, "err", err)
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

	// thread_id for conversation history continuation
	ThreadID *uint `json:"thread_id"`
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
	llm.ToolRunPortfolioAnalysis:     "Running Python analysis",
	llm.ToolSubmitThinking:           "Structuring analysis & reasoning",
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
			
			payload := gin.H{"response": cacheEntry.Response, "cached": true}
			if cacheEntry.SectionsJSON != "" {
				var sections []llm.ResponseSection
				if err := json.Unmarshal([]byte(cacheEntry.SectionsJSON), &sections); err == nil {
					payload["sections"] = sections
				}
			}
			if cacheEntry.ExtrasJSON != "" {
				var extras map[string]any
				if err := json.Unmarshal([]byte(cacheEntry.ExtrasJSON), &extras); err == nil {
					for k, v := range extras {
						payload[k] = v
					}
				}
			}
			
			c.SSEvent("done", payload)
			c.Writer.Flush()
			return
		}
	}

	message := req.Message
	if cannedType != "" {
		var renderErr error
		message, renderErr = h.renderCannedPrompt(req, data, userHash)
		if renderErr != nil {
			slog.Error("llm: renderCannedPrompt failed", "user", userHash[:8], "type", cannedType, "err", renderErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": "preparing prompt: " + renderErr.Error()})
			return
		}
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	executor := h.buildExecutor(data, req, userHash)

	slog.Info("llm: Chat calling LLM", "user", userHash[:8], "prompt_type", req.PromptType, "model", modelKey, "currency", req.Currency, "tools", len(req.EnabledTools))
	var history []llm.ConversationTurn
	if cannedType == "" {
		if req.ThreadID != nil {
			// Load conversation history from DB if continuing a thread
			_, messages, err := h.LLM.GetThread(*req.ThreadID, llm.RealUserHash(userHash))
			if err != nil {
				slog.Warn("llm: failed to load thread history", "thread_id", *req.ThreadID, "err", err)
			} else {
				for _, msg := range messages {
					history = append(history, llm.ConversationTurn{
						Role:    msg.Role,
						Content: msg.Content,
					})
				}
			}
		} else {
			history = req.History
		}
	}

	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()

	response, sections, extras, err := h.LLM.AnalyzePortfolioStream(
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
			slog.Warn("llm: Chat timed out", "user", userHash[:8], "prompt_type", req.PromptType)
			c.SSEvent("error", gin.H{"error": "Model timed out. The servers may be overloaded, try again later or with a different model."})
			return
		}
		slog.Error("llm: Chat failed", "user", userHash[:8], "prompt_type", req.PromptType, "currency", req.Currency, "err", err)
		c.SSEvent("error", gin.H{"error": "generating analysis: " + err.Error()})
		return
	}

	if cannedType != "" && llm.CannedPrompts[cannedType].Cacheable {
		cacheEntry.UserHash = userHash
		cacheEntry.PromptType = cacheKey
		cacheEntry.Model = modelKey
		cacheEntry.Response = response
		if sections != nil {
			if b, err := json.Marshal(sections); err == nil {
				cacheEntry.SectionsJSON = string(b)
			}
		}
		if extras != nil {
			if b, err := json.Marshal(extras); err == nil {
				cacheEntry.ExtrasJSON = string(b)
			}
		}
		cacheEntry.CreatedAt = time.Now()

		err = h.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_hash"}, {Name: "prompt_type"}, {Name: "model"}},
			DoUpdates: clause.AssignmentColumns([]string{"response", "sections_json", "extras_json", "created_at"}),
		}).Create(&cacheEntry).Error

		if err != nil {
			slog.Warn("llm: Chat failed to save cache", "user", userHash[:8], "prompt_type", req.PromptType, "err", err)
		}
	}

	var finalThreadID uint
	if cannedType == "" {
		finalThreadID = h.persistThread(req, userHash, message, response)
	}

	donePayload := gin.H{"response": response}
	if finalThreadID != 0 {
		donePayload["thread_id"] = finalThreadID
	}
	if sections != nil {
		donePayload["sections"] = sections
	}
	if extras != nil {
		for k, v := range extras {
			donePayload[k] = v
		}
	}
	c.SSEvent("done", donePayload)
	c.Writer.Flush()
}

// persistThread saves the chat interaction to the database and optionally triggers title generation.
func (h *LLMHandler) persistThread(req ChatRequest, userHash, userMessage, assistantResponse string) uint {
	realHash := llm.RealUserHash(userHash)
	threadID := uint(0)

	if req.ThreadID == nil {
		// Create new thread
		thread, err := h.LLM.CreateThread(realHash, "")
		if err != nil {
			slog.Error("llm: failed to create thread", "err", err)
			return 0
		}
		threadID = thread.ID

		// Append initial messages
		_ = h.LLM.AppendMessage(threadID, "user", userMessage)
		_ = h.LLM.AppendMessage(threadID, "assistant", assistantResponse)

		// Async generate title
		go h.generateTitleAsync(threadID, realHash)
	} else {
		threadID = *req.ThreadID
		// Append messages
		_ = h.LLM.AppendMessage(threadID, "user", userMessage)
		_ = h.LLM.AppendMessage(threadID, "assistant", assistantResponse)

		// Regenerate title every 4 turns
		if thread, _, err := h.LLM.GetThread(threadID, realHash); err == nil && thread.TurnCount > 0 && thread.TurnCount%4 == 0 {
			go h.generateTitleAsync(threadID, realHash)
		}
	}
	return threadID
}

// generateTitleAsync generates a title for a thread in a background goroutine.
func (h *LLMHandler) generateTitleAsync(threadID uint, realHash string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, messages, err := h.LLM.GetThread(threadID, realHash)
	if err != nil {
		slog.Error("llm: generateTitle failed to load thread", "err", err)
		return
	}

	title, err := h.LLM.GenerateTitle(ctx, messages)
	if err != nil || title == "" {
		slog.Warn("llm: failed to generate title, using fallback", "err", err)
		// Fallback to first few words of user prompt
		if len(messages) > 0 {
			fallback := messages[0].Content
			if len(fallback) > 50 {
				fallback = fallback[:47] + "..."
			}
			// Clean up context prefixes from the fallback
			if idx := strings.Index(fallback, "Conversation context:"); idx != -1 {
				nlIdx := strings.Index(fallback, "\n\n")
				if nlIdx != -1 && len(fallback) > nlIdx+2 {
					fallback = fallback[nlIdx+2:]
				}
			}
			if len(fallback) > 50 {
				fallback = fallback[:47] + "..."
			}
			title = strings.TrimSpace(fallback)
		} else {
			title = "New Conversation"
		}
	}

	if err := h.LLM.UpdateTitle(threadID, title); err != nil {
		slog.Error("llm: failed to update thread title", "err", err)
	}
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
