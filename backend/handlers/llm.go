package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"gofolio-analysis/middleware"
	"gofolio-analysis/models"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/llm"
)

// LLMHandler manages LLM text generation and AI analysis endpoints.
type LLMHandler struct {
	Parser *flexquery.Parser
	DB     *gorm.DB
	LLM    *llm.Service
}

// NewLLMHandler creates a new handler.
func NewLLMHandler(parser *flexquery.Parser, db *gorm.DB, llmSvc *llm.Service) *LLMHandler {
	return &LLMHandler{
		Parser: parser,
		DB:     db,
		LLM:    llmSvc,
	}
}

// normalizeModel maps client model strings to canonical cache keys.
// Anything other than "flash" is treated as "pro".
func normalizeModel(m string) string {
	if m == "flash" {
		return "flash"
	}
	return "pro"
}

// IsAvailable handles GET /api/v1/llm/available
func (h *LLMHandler) IsAvailable(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"available": h.LLM.APIKey != ""})
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

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		log.Printf("ERROR: GetSummary LoadSaved failed [user=%s]: %v", userHash[:8], err)
		c.JSON(http.StatusNotFound, gin.H{"error": "user data not found"})
		return
	}

	currency := c.DefaultQuery("currency", "USD")
	forceRefresh := c.Query("force_refresh") == "true"
	promptType := "summary_" + period
	model := "pro" // summary always uses the flash model internally; "pro" is just the cache key default

	// Check Cache (valid for 8h, skipped when force_refresh=true)
	var cacheEntry models.LLMCache
	cacheFound := h.DB.Where("user_hash = ? AND prompt_type = ? AND model = ?", userHash, promptType, model).First(&cacheEntry).Error == nil
	if !forceRefresh && cacheFound && time.Since(cacheEntry.CreatedAt) < 8*time.Hour {
		c.JSON(http.StatusOK, gin.H{"summary": cacheEntry.Response})
		return
	}

	// Call LLM
	log.Printf("INFO: GetMarketSummary calling LLM [user=%s period=%s currency=%s]", userHash[:8], period, currency)
	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 130*time.Second)
	defer cancel()
	summary, err := h.LLM.GetMarketSummary(reqCtx, data, currency, period)
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
		log.Printf("ERROR: GetMarketSummary failed [user=%s period=%s currency=%s]: %v", userHash[:8], period, currency, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "generating summary: " + err.Error()})
		return
	}

	// Save Cache — Save performs UPDATE when ID is set (cache hit), INSERT otherwise.
	cacheEntry.UserHash = userHash
	cacheEntry.PromptType = promptType
	cacheEntry.Model = model
	cacheEntry.Response = summary
	cacheEntry.CreatedAt = time.Now()
	if err := h.DB.Save(&cacheEntry).Error; err != nil {
		log.Printf("WARN: GetSummary failed to save cache [user=%s period=%s]: %v", userHash[:8], period, err)
	}

	c.JSON(http.StatusOK, gin.H{"summary": summary})
}

// ChatRequest defines the payload for LLM chatting.
type ChatRequest struct {
	PromptType       string                  `json:"prompt_type"`       // "general_analysis", "best_worst_scenarios", "freeform"
	Message          string                  `json:"message"`
	Currency         string                  `json:"currency"`
	Model            string                  `json:"model"`             // "flash" | "pro" (default "pro")
	IncludePortfolio *bool                   `json:"include_portfolio"` // nil = default true; ignored for canned
	CustomWeights    []llm.CustomWeight      `json:"custom_weights"`    // nil = live portfolio; ignored for canned
	History          []llm.ConversationTurn  `json:"history"`           // prior turns; ignored for canned
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

	model := normalizeModel(req.Model)

	// includePortfolio and customWeights only apply to freeform
	includePortfolio := true
	if req.IncludePortfolio != nil && cannedType == "" {
		includePortfolio = *req.IncludePortfolio
	}
	var customWeights []llm.CustomWeight
	if cannedType == "" && req.CustomWeights != nil {
		customWeights = req.CustomWeights
	}

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		log.Printf("ERROR: Chat LoadSaved failed [user=%s]: %v", userHash[:8], err)
		c.JSON(http.StatusNotFound, gin.H{"error": "user data not found"})
		return
	}

	// Cache retrieval for canned (keyed by user, prompt type, and model)
	var cacheEntry models.LLMCache
	if cannedType != "" {
		cacheFound := h.DB.Where("user_hash = ? AND prompt_type = ? AND model = ?", userHash, req.PromptType, model).First(&cacheEntry).Error == nil
		if cacheFound && time.Since(cacheEntry.CreatedAt) < 24*time.Hour {
			c.JSON(http.StatusOK, gin.H{"response": cacheEntry.Response})
			return
		}
	}

	// Call LLM
	log.Printf("INFO: AnalyzePortfolio calling LLM [user=%s prompt_type=%s model=%s currency=%s includePortfolio=%v customWeights=%v]",
		userHash[:8], req.PromptType, model, req.Currency, includePortfolio, customWeights != nil)
	var history []llm.ConversationTurn
	if cannedType == "" {
		history = req.History
	}

	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 130*time.Second)
	defer cancel()
	response, err := h.LLM.AnalyzePortfolio(
		reqCtx, data, req.Currency, cannedType, req.Message,
		req.Model, includePortfolio, customWeights, history,
	)
	if err != nil {
		if errors.Is(err, llm.ErrNotConfigured) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": "NOT_CONFIGURED"})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			log.Printf("WARN: AnalyzePortfolio timed out [user=%s prompt_type=%s]", userHash[:8], req.PromptType)
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "Model timed out. The servers may be overloaded, try again later or with a different model."})
			return
		}
		log.Printf("ERROR: AnalyzePortfolio failed [user=%s prompt_type=%s currency=%s]: %v", userHash[:8], req.PromptType, req.Currency, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "generating analysis: " + err.Error()})
		return
	}

	// Save Cache for canned — Save performs UPDATE when ID is set (cache hit), INSERT otherwise.
	if cannedType != "" {
		cacheEntry.UserHash = userHash
		cacheEntry.PromptType = req.PromptType
		cacheEntry.Model = model
		cacheEntry.Response = response
		cacheEntry.CreatedAt = time.Now()
		if err := h.DB.Save(&cacheEntry).Error; err != nil {
			log.Printf("WARN: Chat failed to save cache [user=%s prompt_type=%s]: %v", userHash[:8], req.PromptType, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"response": response})
}
