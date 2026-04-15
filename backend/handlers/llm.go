package handlers

import (
	"context"
	"encoding/json"
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
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
)

// LLMHandler manages LLM text generation and AI analysis endpoints.
type LLMHandler struct {
	Repo             *flexquery.Repository
	DB               *gorm.DB
	LLM              *llm.Service
	PortfolioService *portfolio.Service
	MarketProvider   market.Provider
	CurrencyGetter   market.CurrencyGetter
}

// NewLLMHandler creates a new handler.
func NewLLMHandler(
	repo *flexquery.Repository,
	db *gorm.DB,
	llmSvc *llm.Service,
	ps *portfolio.Service,
	mp market.Provider,
	cg market.CurrencyGetter,
) *LLMHandler {
	return &LLMHandler{
		Repo:             repo,
		DB:               db,
		LLM:              llmSvc,
		PortfolioService: ps,
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
	IncludePortfolio         *bool                  `json:"include_portfolio"`          // nil = default true
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

	// includePortfolio and override weights only apply to freeform.
	includePortfolio := true
	if req.IncludePortfolio != nil && cannedType == "" {
		includePortfolio = *req.IncludePortfolio
	}
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

	// Pre-render message: data-driven canned prompts compute their metrics here;
	// simple canned prompts expand their template; freeform uses req.Message verbatim.
	message := req.Message
	if cannedType != "" {
		var err error
		message, err = h.renderCannedPrompt(req, data)
		if err != nil {
			log.Printf("ERROR: renderCannedPrompt failed [user=%s type=%s]: %v", userHash[:8], cannedType, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "preparing prompt: " + err.Error()})
			return
		}
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	// Call LLM
	log.Printf("INFO: AnalyzePortfolio calling LLM [user=%s prompt_type=%s model=%s currency=%s includePortfolio=%v overrideWeights=%v]",
		userHash[:8], req.PromptType, modelKey, req.Currency, includePortfolio, overrideWeights != nil)
	var history []llm.ConversationTurn
	if cannedType == "" {
		history = req.History
	}

	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 130*time.Second)
	defer cancel()
	response, err := h.LLM.AnalyzePortfolioStream(
		reqCtx, data, req.Currency, cannedType, message,
		modelKey, includePortfolio, overrideWeights, history, req.AccountingModel,
		func(chunk string) error {
			c.SSEvent("chunk", chunk)
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

	c.SSEvent("done", gin.H{"response": response})
	c.Writer.Flush()
}

// renderCannedPrompt builds the fully-rendered message for a canned prompt type.
// Data-driven prompts (ticker_analysis, risk_metrics, benchmark_analysis) compute
// their metrics from the portfolio data; simple prompts expand their template as-is.
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
		return h.renderRiskMetrics(&cp, req, data)

	case "benchmark_analysis":
		return h.renderBenchmarkAnalysis(&cp, req, data)

	case "upcoming_events":
		return cp.Render(map[string]string{
			"current_date": time.Now().Format("Jan 2, 2006"),
		}), nil

	default:
		// general_analysis, best_worst_scenarios — no template vars.
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

func (h *LLMHandler) renderRiskMetrics(cp *llm.CannedPrompt, req ChatRequest, data *models.FlexQueryData) (string, error) {
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
	metrics, err := computePortfolioMetrics(
		h.PortfolioService, data, from, to,
		req.Currency, acctModel, rfr, false,
	)
	if err != nil {
		return "", fmt.Errorf("computing portfolio metrics: %w", err)
	}
	twr, mwr := "N/A", "N/A"
	if metrics.TWRErr == "" {
		twr = fmt.Sprintf("%.2f%%", metrics.TWR*100)
	}
	if metrics.MWRErr == "" {
		mwr = fmt.Sprintf("%.2f%%", metrics.MWR*100)
	}
	payload := map[string]interface{}{
		"period":       fmt.Sprintf("%s – %s", from.Format("Jan 2, 2006"), to.Format("Jan 2, 2006")),
		"twr":          twr,
		"mwr":          mwr,
		"sharpe":       fmt.Sprintf("%.3f", metrics.Standalone.SharpeRatio),
		"sortino":      fmt.Sprintf("%.3f", metrics.Standalone.SortinoRatio),
		"vami":         fmt.Sprintf("%.1f", metrics.Standalone.VAMI),
		"volatility":   fmt.Sprintf("%.2f%%", metrics.Standalone.Volatility*100),
		"max_drawdown": fmt.Sprintf("-%.2f%%", metrics.Standalone.MaxDrawdown*100),
	}
	jsonBytes, _ := json.MarshalIndent(payload, "", "  ")

	return cp.Render(map[string]string{
		"data_json": string(jsonBytes),
	}), nil
}

func (h *LLMHandler) renderBenchmarkAnalysis(cp *llm.CannedPrompt, req ChatRequest, data *models.FlexQueryData) (string, error) {
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
	acctModel, acctErr := models.ValidateAccountingModel(req.AccountingModel)
	if acctErr != nil {
		return "", acctErr
	}
	metrics, err := computePortfolioMetrics(h.PortfolioService, data, from, to, req.Currency, acctModel, rfr, false)
	if err != nil {
		return "", fmt.Errorf("computing portfolio metrics: %w", err)
	}
	bmMetrics, err := computeBenchmarkComparison(
		h.MarketProvider, h.CurrencyGetter,
		metrics.Returns, metrics.StartDates, metrics.EndDates,
		req.BenchmarkSymbol, from, to, req.Currency, acctModel, rfr,
	)
	if err != nil {
		return "", fmt.Errorf("computing benchmark metrics: %w", err)
	}
	payload := map[string]interface{}{
		"period":         fmt.Sprintf("%s – %s", from.Format("Jan 2, 2006"), to.Format("Jan 2, 2006")),
		"alpha":          fmt.Sprintf("%.2f%%", bmMetrics.Alpha*100),
		"beta":           fmt.Sprintf("%.3f", bmMetrics.Beta),
		"treynor":        fmt.Sprintf("%.4f", bmMetrics.TreynorRatio),
		"tracking_error": fmt.Sprintf("%.2f%%", bmMetrics.TrackingError*100),
		"info_ratio":     fmt.Sprintf("%.3f", bmMetrics.InformationRatio),
		"correlation":    fmt.Sprintf("%.3f", bmMetrics.Correlation),
		"sharpe":         fmt.Sprintf("%.3f", metrics.Standalone.SharpeRatio),
		"sortino":        fmt.Sprintf("%.3f", metrics.Standalone.SortinoRatio),
		"vami":           fmt.Sprintf("%.1f", metrics.Standalone.VAMI),
		"volatility":     fmt.Sprintf("%.2f%%", metrics.Standalone.Volatility*100),
		"max_drawdown":   fmt.Sprintf("-%.2f%%", metrics.Standalone.MaxDrawdown*100),
	}
	jsonBytes, _ := json.MarshalIndent(payload, "", "  ")

	return cp.Render(map[string]string{
		"benchmark": req.BenchmarkSymbol,
		"data_json": string(jsonBytes),
	}), nil
}
