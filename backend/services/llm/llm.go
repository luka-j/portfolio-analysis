package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"
	"gorm.io/gorm"

	"portfolio-analysis/models"
	"portfolio-analysis/services/portfolio"
)

// ErrNotConfigured is returned when the Gemini API key is not set.
var ErrNotConfigured = errors.New("LLM service unavailable: GEMINI_API_KEY not set")

// CustomWeight represents a symbol and its portfolio weight (percentage).
type CustomWeight struct {
	Symbol string  `json:"symbol"`
	Weight float64 `json:"weight"`
}

// PortfolioContextItem represents a single portfolio holding in the LLM context.
type PortfolioContextItem struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name,omitempty"`
	ISIN      string  `json:"isin,omitempty"`
	WeightPct float64 `json:"weight_pct"`
}

// ConversationTurn represents one turn in a multi-turn chat history.
type ConversationTurn struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// Service provides LLM interactions via Gemini.
type Service struct {
	APIKey           string
	FlashModel       string
	ProModel         string
	DefaultModelKey  string // "flash" | "pro" — model used for canned prompts not explicitly requesting a model
	DB               *gorm.DB
	PortfolioService *portfolio.Service
	HTTPClient       *http.Client
}

// NewService creates a new LLM Service.
func NewService(apiKey, flashModel, proModel, cannedModelKey string, db *gorm.DB, ps *portfolio.Service) *Service {
	transport := &http.Transport{
		IdleConnTimeout:     10 * time.Second,
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 5,
	}
	if cannedModelKey != "pro" {
		cannedModelKey = "flash"
	}
	return &Service{
		APIKey:           apiKey,
		FlashModel:       flashModel,
		ProModel:         proModel,
		DefaultModelKey:  cannedModelKey,
		DB:               db,
		PortfolioService: ps,
		HTTPClient:       &http.Client{Transport: transport},
	}
}

// isAvailable checks if the API key is set.
func (s *Service) isAvailable() bool {
	return s.APIKey != ""
}

// ResolveModel maps "flash"/"pro" to the configured model name. Empty string → ProModel.
func (s *Service) ResolveModel(override string) string {
	if override == "flash" {
		return s.FlashModel
	}
	return s.ProModel
}

// fundamentalsEntry holds the name and ISIN for a symbol.
type fundamentalsEntry struct {
	Name string
	ISIN string
}

// lookupFundamentals batch-queries asset_fundamentals for the given symbols and returns a symbol→entry map.
func (s *Service) lookupFundamentals(symbols []string) map[string]fundamentalsEntry {
	result := make(map[string]fundamentalsEntry, len(symbols))
	if s.DB == nil || len(symbols) == 0 {
		return result
	}
	var rows []models.AssetFundamental
	if err := s.DB.Select("symbol, name, isin").Where("symbol IN ?", symbols).Find(&rows).Error; err != nil {
		log.Printf("WARN: llm fundamentals lookup failed: %v", err)
		return result
	}
	for _, r := range rows {
		result[r.Symbol] = fundamentalsEntry{Name: r.Name, ISIN: r.ISIN}
	}
	return result
}

// lookupNames batch-queries asset_fundamentals for the given symbols and returns a symbol→name map.
func (s *Service) lookupNames(symbols []string) map[string]string {
	fd := s.lookupFundamentals(symbols)
	names := make(map[string]string, len(fd))
	for sym, e := range fd {
		if e.Name != "" {
			names[sym] = e.Name
		}
	}
	return names
}

// getPortfolioJSON creates a compact JSON string of the latest portfolio weights with security names.
func (s *Service) getPortfolioJSON(data *models.FlexQueryData, currency string, acctModel models.AccountingModel) string {
	result, err := s.PortfolioService.GetCurrentValue(data, currency, acctModel, false)
	if err != nil {
		log.Printf("WARN: getPortfolioJSON failed to get current value: %v", err)
		return ""
	}

	if result.Value == 0 {
		return ""
	}

	symbols := make([]string, 0, len(result.Positions))
	for _, pos := range result.Positions {
		if pos.Value != 0 {
			symbols = append(symbols, pos.Symbol)
		}
	}
	fd := s.lookupFundamentals(symbols)

	var items []PortfolioContextItem
	for _, pos := range result.Positions {
		if pos.Value == 0 {
			continue
		}
		e := fd[pos.Symbol]
		items = append(items, PortfolioContextItem{
			Symbol:    pos.Symbol,
			Name:      e.Name,
			ISIN:      e.ISIN,
			WeightPct: math.Round((pos.Value/result.Value)*1000) / 10,
		})
	}

	if len(items) == 0 {
		return ""
	}

	b, err := json.Marshal(items)
	if err != nil {
		log.Printf("WARN: getPortfolioJSON failed to marshal JSON: %v", err)
		return ""
	}
	return string(b)
}

// buildPortfolioJSONFromCustom builds a compact JSON string from caller-provided weights with name lookup.
func (s *Service) buildPortfolioJSONFromCustom(weights []CustomWeight) string {
	symbols := make([]string, len(weights))
	for i, w := range weights {
		symbols[i] = w.Symbol
	}
	fd := s.lookupFundamentals(symbols)

	var items []PortfolioContextItem
	for _, w := range weights {
		e := fd[w.Symbol]
		items = append(items, PortfolioContextItem{
			Symbol:    w.Symbol,
			Name:      e.Name,
			ISIN:      e.ISIN,
			WeightPct: math.Round(w.Weight*10) / 10,
		})
	}

	if len(items) == 0 {
		return ""
	}

	b, err := json.Marshal(items)
	if err != nil {
		log.Printf("WARN: buildPortfolioJSONFromCustom failed to marshal JSON: %v", err)
		return ""
	}
	return string(b)
}

// callGemini sends a multi-turn content slice to Gemini and returns the text response.
// callType labels the metric ("summary" or "analysis").
func (s *Service) callGemini(ctx context.Context, model, callType string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (string, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: s.APIKey, HTTPClient: s.HTTPClient})
	if err != nil {
		return "", fmt.Errorf("creating genai client: %w", err)
	}

	log.Printf("callGemini [model=%s callType=%s turns=%d]", model, callType, len(contents))
	start := time.Now()
	resp, err := client.Models.GenerateContent(ctx, model, contents, cfg)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("ERROR: Gemini GenerateContent failed [model=%s]: %v", model, err)
		geminiRequests.WithLabelValues(model, callType, "error").Inc()
		geminiRequestDuration.WithLabelValues(model, callType).Observe(elapsed.Seconds())
		return "", fmt.Errorf("generating content: %w", err)
	}

	geminiRequests.WithLabelValues(model, callType, "ok").Inc()
	geminiRequestDuration.WithLabelValues(model, callType).Observe(elapsed.Seconds())
	if resp.UsageMetadata != nil {
		geminiInputTokens.WithLabelValues(model, callType).Add(float64(resp.UsageMetadata.PromptTokenCount))
		geminiOutputTokens.WithLabelValues(model, callType).Add(float64(resp.UsageMetadata.CandidatesTokenCount))
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		reason := "unknown"
		if len(resp.Candidates) > 0 {
			reason = string(resp.Candidates[0].FinishReason)
		}
		log.Printf("WARN: Gemini returned no content [model=%s finishReason=%s]", model, reason)
		return "", fmt.Errorf("no response generated (finish reason: %s)", reason)
	}

	if fr := resp.Candidates[0].FinishReason; fr != "STOP" && fr != "" {
		log.Printf("WARN: Gemini finished with non-STOP reason [model=%s finishReason=%s]", model, fr)
	}

	var parts []string
	for _, pt := range resp.Candidates[0].Content.Parts {
		if pt.Text != "" {
			parts = append(parts, pt.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// GetMarketSummary generates a short market summary for the requested period.
// This is always single-turn and is not affected by chat history.
func (s *Service) GetMarketSummary(ctx context.Context, data *models.FlexQueryData, period string) (string, error) {
	if !s.isAvailable() {
		return "", ErrNotConfigured
	}

	// Currency is irrelevant here — only percentage weights are sent to the LLM,
	// and weights are currency-independent ratios.
	portfolioJSON := s.getPortfolioJSON(data, "USD", models.AccountingModelSpot)

	periodMap := map[string]string{
		"1d": "the past day",
		"1w": "the past week",
		"1m": "the past month",
	}
	periodText, ok := periodMap[period]
	if !ok {
		periodText = "the past day"
	}

	cp := CannedPrompts["market_summary"]
	prompt := cp.Render(map[string]string{"period": periodText})
	if portfolioJSON != "" {
		prompt += fmt.Sprintf("\n\nHere is the user's current portfolio data:\n<portfolio_data>\n%s\n</portfolio_data>", portfolioJSON)
	}

	cfg := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: cp.SystemInstruction},
			},
		},
		Tools: []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		},
	}

	return s.callGemini(ctx, s.FlashModel, "summary", genai.Text(prompt), cfg)
}

// AnalyzePortfolio executes portfolio analysis with optional multi-turn history.
// modelOverride: "flash" or "pro" (empty → ProModel).
// includePortfolio: if false, omit portfolio context (freeform only; canned always includes).
// customWeights: if non-nil, use instead of live portfolio (freeform only).
// history: prior conversation turns (freeform only; canned is always single-turn).
// message must already be the fully rendered prompt text (the handler pre-renders canned prompts).
func (s *Service) AnalyzePortfolio(
	ctx context.Context,
	data *models.FlexQueryData,
	currency, cannedType, message, modelOverride string,
	includePortfolio bool,
	customWeights []CustomWeight,
	history []ConversationTurn,
	acctModel string,
) (string, error) {
	if !s.isAvailable() {
		return "", ErrNotConfigured
	}

	isCanned := cannedType != ""

	// Resolve portfolio data (either custom or live) and serialize to JSON
	var portfolioJSON string
	if isCanned || includePortfolio {
		if !isCanned && customWeights != nil {
			portfolioJSON = s.buildPortfolioJSONFromCustom(customWeights)
		} else {
			parsedAcct := models.ParseAccountingModel(acctModel)
			portfolioJSON = s.getPortfolioJSON(data, currency, parsedAcct)
		}
	}

	// Build system instruction — persona + portfolio context baked in once,
	// so it does not need to be repeated across conversation turns.
	var defaultSystemInstruction = "You are an expert, professional financial analyst.\n" +
		"Focus on macroeconomic factors that affect the specific assets in the user's portfolio. " +
		"Notice specifically if a ticker they hold has seen significant news lately."

	sysBase := defaultSystemInstruction
	if isCanned && CannedPrompts[cannedType].SystemInstruction != "" {
		sysBase = CannedPrompts[cannedType].SystemInstruction
	}

	sysText := sysBase + "\n\n" + defaultConstraints

	if portfolioJSON != "" {
		sysText += "\n\nHere is the user's current portfolio data:\n<portfolio_data>\n" + portfolioJSON + "\n</portfolio_data>"
	}

	// message is already fully rendered by the handler (canned or freeform).
	currentMessage := message

	// Build contents: prior history turns + current user message.
	// Canned prompts are always single-turn (history ignored).
	contents := make([]*genai.Content, 0, len(history)+1)
	if !isCanned {
		for _, turn := range history {
			role := genai.RoleUser
			if turn.Role == "assistant" {
				role = genai.RoleModel
			}
			contents = append(contents, &genai.Content{
				Role:  role,
				Parts: []*genai.Part{{Text: turn.Content}},
			})
		}
	}
	contents = append(contents, &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: currentMessage}},
	})

	modelKey := modelOverride
	if isCanned && modelKey != "flash" && modelKey != "pro" {
		modelKey = s.DefaultModelKey
	}
	model := s.ResolveModel(modelKey)
	log.Printf("DEBUG: AnalyzePortfolio [model=%s cannedType=%s historyTurns=%d includePortfolio=%v]",
		model, cannedType, len(history), includePortfolio)

	cfg := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: sysText}},
		},
		Tools: []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		},
	}

	return s.callGemini(ctx, model, "analysis", contents, cfg)
}
