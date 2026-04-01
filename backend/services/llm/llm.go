package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"

	"google.golang.org/genai"
	"gorm.io/gorm"

	"gofolio-analysis/models"
	"gofolio-analysis/services/portfolio"
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
	WeightPct float64 `json:"weight_pct"`
}

// CannedPrompt holds the configuration for a predefined system prompt.
type CannedPrompt struct {
	Message string
}

// CannedPrompts is the registry of available predefined prompts.
var CannedPrompts = map[string]CannedPrompt{
	"general_analysis": {
		Message: "Analyze my current portfolio given current market conditions. What am I effectively betting on?\nStructure your response using these exact markdown headers:\n### 🌍 Macro Environment\n### 📊 Sector Concentration\n### 🎯 Implicit Bets",
	},
	"best_worst_scenarios": {
		Message: "Analyze my current portfolio.\nFirst, use a <thinking> block to list out major macroeconomic tailwinds and headwinds for these specific tickers.\nThen, outside of the block, explain the best and worst realistic scenarios for my portfolio in the short-to-medium term.",
	},
}

// IsValidCannedType returns true if the specified type is registered.
func IsValidCannedType(promptType string) bool {
	_, ok := CannedPrompts[promptType]
	return ok
}

// ConversationTurn represents one turn in a multi-turn chat history.
type ConversationTurn struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"`
}

// Service provides LLM interactions via Gemini.
type Service struct {
	APIKey           string
	SummaryModel     string
	ChatModel        string
	DB               *gorm.DB
	PortfolioService *portfolio.Service
}

// NewService creates a new LLM Service.
func NewService(apiKey, summaryModel, chatModel string, db *gorm.DB, ps *portfolio.Service) *Service {
	return &Service{
		APIKey:           apiKey,
		SummaryModel:     summaryModel,
		ChatModel:        chatModel,
		DB:               db,
		PortfolioService: ps,
	}
}

// isAvailable checks if the API key is set.
func (s *Service) isAvailable() bool {
	return s.APIKey != ""
}

// resolveModel maps "flash"/"pro" to the configured model name. Empty string → ChatModel.
func (s *Service) resolveModel(override string) string {
	if override == "flash" {
		return s.SummaryModel
	}
	return s.ChatModel
}

// lookupNames batch-queries asset_fundamentals for the given symbols and returns a symbol→name map.
func (s *Service) lookupNames(symbols []string) map[string]string {
	names := make(map[string]string, len(symbols))
	if s.DB == nil || len(symbols) == 0 {
		return names
	}
	var rows []models.AssetFundamental
	if err := s.DB.Select("symbol, name").Where("symbol IN ?", symbols).Find(&rows).Error; err != nil {
		log.Printf("WARN: llm name lookup failed: %v", err)
		return names
	}
	for _, r := range rows {
		if r.Name != "" {
			names[r.Symbol] = r.Name
		}
	}
	return names
}

// getPortfolioJSON creates a compact JSON string of the latest portfolio weights with security names.
func (s *Service) getPortfolioJSON(data *models.FlexQueryData, currency string) string {
	result, err := s.PortfolioService.GetCurrentValue(data, currency, models.AccountingModelSpot, false)
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
	names := s.lookupNames(symbols)

	var items []PortfolioContextItem
	for _, pos := range result.Positions {
		if pos.Value == 0 {
			continue
		}
		items = append(items, PortfolioContextItem{
			Symbol:    pos.Symbol,
			Name:      names[pos.Symbol],
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
	names := s.lookupNames(symbols)

	var items []PortfolioContextItem
	for _, w := range weights {
		items = append(items, PortfolioContextItem{
			Symbol:    w.Symbol,
			Name:      names[w.Symbol],
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
func (s *Service) callGemini(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (string, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: s.APIKey})
	if err != nil {
		return "", fmt.Errorf("creating genai client: %w", err)
	}

	log.Printf("DEBUG: callGemini [model=%s turns=%d]", model, len(contents))
	resp, err := client.Models.GenerateContent(ctx, model, contents, cfg)
	if err != nil {
		log.Printf("ERROR: Gemini GenerateContent failed [model=%s]: %v", model, err)
		return "", fmt.Errorf("generating content: %w", err)
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
func (s *Service) GetMarketSummary(ctx context.Context, data *models.FlexQueryData, currency, period string) (string, error) {
	if !s.isAvailable() {
		return "", ErrNotConfigured
	}

	portfolioJSON := s.getPortfolioJSON(data, currency)

	periodMap := map[string]string{
		"1d": "the past day",
		"1w": "the past week",
		"1m": "the past month",
	}
	periodText, ok := periodMap[period]
	if !ok {
		periodText = "the past day"
	}

	prompt := fmt.Sprintf(`Provide a brief market summary formatted as two short bullet points covering %s:
- **Macro:** Focus on macroeconomic factors that had the biggest impact on the market.
- **Portfolio:** Very briefly explain the biggest market movements in the <portfolio_data> provided, citing specific tickers.

Keep each bullet point under 30 words.`, periodText)

	if portfolioJSON != "" {
		prompt += fmt.Sprintf("\n\nHere is the user's current portfolio data:\n<portfolio_data>\n%s\n</portfolio_data>", portfolioJSON)
	}

	cfg := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: "You are an expert, professional financial analyst. Provide concise, impactful summaries. Use short information-filled sentences. Avoid overly long compound sentences. Respect the length requirement."},
			},
		},
		Tools: []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		},
	}

	return s.callGemini(ctx, s.SummaryModel, genai.Text(prompt), cfg)
}

// AnalyzePortfolio executes portfolio analysis with optional multi-turn history.
// modelOverride: "flash" or "pro" (empty → ChatModel).
// includePortfolio: if false, omit portfolio context (freeform only; canned always includes).
// customWeights: if non-nil, use instead of live portfolio (freeform only).
// history: prior conversation turns (freeform only; canned is always single-turn).
func (s *Service) AnalyzePortfolio(
	ctx context.Context,
	data *models.FlexQueryData,
	currency, cannedType, message, modelOverride string,
	includePortfolio bool,
	customWeights []CustomWeight,
	history []ConversationTurn,
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
			portfolioJSON = s.getPortfolioJSON(data, currency)
		}
	}

	// Build system instruction — persona + portfolio context baked in once,
	// so it does not need to be repeated across conversation turns.
	sysText := "You are an expert, professional financial analyst.\n" +
		"Focus on macroeconomic factors that affect the specific assets in the user's portfolio. " +
		"Notice specifically if a ticker they hold has seen significant news lately.\n\n" +
		"<constraints>\n" +
		"- DO NOT provide personalized financial advice (e.g., never say \"You should sell X\").\n" +
		"- DO NOT invent or hallucinate news events. If you are unsure about recent news for a ticker, state that explicitly.\n" +
		"- DO NOT speculate on exact future price targets.\n" +
		"</constraints>"
	if portfolioJSON != "" {
		sysText += "\n\nHere is the user's current portfolio data:\n<portfolio_data>\n" + portfolioJSON + "\n</portfolio_data>"
	}

	// Resolve the user-facing question for this turn.
	var currentMessage string
	if isCanned {
		if cp, ok := CannedPrompts[cannedType]; ok {
			currentMessage = cp.Message
		} else {
			currentMessage = message // Fallback
		}
	} else {
		currentMessage = message
	}

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

	model := s.resolveModel(modelOverride)
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

	return s.callGemini(ctx, model, contents, cfg)
}
