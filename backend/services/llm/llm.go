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

// ResponseSection is a single named section from a structured (schema-backed) LLM response.
type ResponseSection struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
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

// ResolveModel maps "flash" or "pro" to the configured model name.
// Any value other than "flash" resolves to ProModel.
func (s *Service) ResolveModel(key string) string {
	if key == "flash" {
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
		if pos.Value != 0 && pos.Symbol != "PENDING_CASH" {
			symbols = append(symbols, pos.Symbol)
		}
	}
	fd := s.lookupFundamentals(symbols)

	var items []PortfolioContextItem
	for _, pos := range result.Positions {
		if pos.Value == 0 || pos.Symbol == "PENDING_CASH" {
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

	// Task 5: supply portfolio data as a discrete Part separate from the behavioural instruction.
	systemParts := []*genai.Part{{Text: cp.SystemInstruction}}
	if portfolioJSON != "" {
		systemParts = append(systemParts, &genai.Part{Text: portfolioJSON})
	}

	cfg := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: systemParts,
		},
		Tools: []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		},
	}

	return s.callGemini(ctx, s.FlashModel, "summary", genai.Text(prompt), cfg)
}

// buildSystemParts constructs the SystemInstruction Parts slice, keeping behavioural rules
// and portfolio data as discrete elements (Task 5: system context segregation).
func buildSystemParts(instructionText, portfolioJSON string) []*genai.Part {
	parts := []*genai.Part{{Text: instructionText}}
	if portfolioJSON != "" {
		parts = append(parts, &genai.Part{Text: portfolioJSON})
	}
	return parts
}

// reconstructMarkdown rebuilds a markdown string from a parsed structured response.
// The thinking field is wrapped in <thinking> tags; other sections use ### headers.
func reconstructMarkdown(cp CannedPrompt, fields map[string]string) string {
	var sb strings.Builder
	if thinking := fields["thinking"]; thinking != "" {
		sb.WriteString("<thinking>\n")
		sb.WriteString(thinking)
		sb.WriteString("\n</thinking>\n\n")
	}
	for _, key := range cp.SectionOrder {
		if key == "thinking" {
			continue
		}
		content := fields[key]
		if content == "" {
			continue
		}
		title := cp.SectionTitles[key]
		if title != "" {
			sb.WriteString("### ")
			sb.WriteString(title)
			sb.WriteString("\n")
		}
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// maxToolRounds is the maximum number of tool-call/response cycles per request.
const maxToolRounds = 8

// AnalyzePortfolioStream executes portfolio analysis with optional multi-turn history and autonomous tool calling.
// It streams text chunks to onChunk, emits tool_call events via onToolCall, and returns the final response
// along with parsed sections (non-nil only for schema-backed canned prompts).
func (s *Service) AnalyzePortfolioStream(
	ctx context.Context,
	data *models.FlexQueryData,
	currency, cannedType, message, model string,
	enabledTools []string,
	customWeights []CustomWeight,
	history []ConversationTurn,
	acctModel string,
	executor ToolExecutor,
	onChunk func(string) error,
	onToolCall func(toolName string) error,
) (response string, sections []ResponseSection, err error) {
	if !s.isAvailable() {
		return "", nil, ErrNotConfigured
	}

	isCanned := cannedType != ""

	// Resolve portfolio data (either custom or live) and serialize to JSON.
	// For the agentic chat (non-canned), we no longer inject raw portfolio JSON into the system prompt —
	// the model will call get_current_allocations() if it needs the data.
	// For canned prompts that historically needed the portfolio context in the system instruction,
	// we retain the injection (they are single-turn and use ForcedTool to prime the tool call anyway).
	var portfolioJSON string
	if isCanned {
		// Canned prompts still get the portfolio JSON injected as a system part for backward compatibility.
		if customWeights != nil {
			portfolioJSON = s.buildPortfolioJSONFromCustom(customWeights)
		} else {
			parsedAcct := models.ParseAccountingModel(acctModel)
			portfolioJSON = s.getPortfolioJSON(data, currency, parsedAcct)
		}
	}
	// For freeform, includePortfolio == true means we tell the model about its available tools
	// to call get_current_allocations on its own, rather than injecting the full JSON.

	// Build system instruction.
	var defaultSystemInstruction = "You are an expert, professional financial analyst.\n" +
		"Focus on macroeconomic factors that affect the specific assets in the user's portfolio. " +
		"Notice specifically if a ticker they hold has seen significant news lately."

	sysBase := defaultSystemInstruction
	if isCanned && CannedPrompts[cannedType].SystemInstruction != "" {
		sysBase = CannedPrompts[cannedType].SystemInstruction
	}

	// For agentic freeform: add tool-use guidance to the system instruction.
	toolHint := ""
	if !isCanned && len(enabledTools) > 0 && executor != nil {
		toolHint = fmt.Sprintf("\n\nYou have access to the following dynamic tools: %s.\n"+
			"Always call the relevant tool(s) before answering quantitative questions about the portfolio. Do not guess metrics.",
			strings.Join(enabledTools, ", "))
	}

	instructionText := sysBase + toolHint + "\n\n" + defaultConstraints
	systemParts := buildSystemParts(instructionText, portfolioJSON)

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
		Parts: []*genai.Part{{Text: message}},
	})

	log.Printf("DEBUG: AnalyzePortfolioStream [model=%s cannedType=%s historyTurns=%d enabledToolsCount=%d executor=%v]",
		model, cannedType, len(history), len(enabledTools), executor != nil)

	// Build tool list: dynamically filter portfolio tools + Google Search
	var tools []*genai.Tool
	hasFuncs := false
	if executor != nil && len(enabledTools) > 0 {
		pt := PortfolioTools()
		var activeFuncs []*genai.FunctionDeclaration
		for _, tf := range pt.FunctionDeclarations {
			for _, allowed := range enabledTools {
				if tf.Name == allowed {
					activeFuncs = append(activeFuncs, tf)
					break
				}
			}
		}
		if len(activeFuncs) > 0 {
			tools = append(tools, &genai.Tool{FunctionDeclarations: activeFuncs})
			hasFuncs = true
		}
	}

	hasGoogleSearch := false
	if cannedType == "upcoming_events" || cannedType == "ticker_analysis" || cannedType == "long_market_summary" || cannedType == "add_or_trim" || cannedType == "biggest_drag_on_performance" {
		tools = append(tools, &genai.Tool{GoogleSearch: &genai.GoogleSearch{}})
		hasGoogleSearch = true
	}

	// For canned prompts with a ForcedTool, configure ToolChoice to force that tool first.
	var toolConfig *genai.ToolConfig
	if isCanned {
		if cp := CannedPrompts[cannedType]; cp.ForcedTool != "" && executor != nil {
			toolConfig = &genai.ToolConfig{
				FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode:                  genai.FunctionCallingConfigModeAny,
					AllowedFunctionNames:  []string{cp.ForcedTool},
				},
			}
		}
	}

	if hasFuncs && hasGoogleSearch {
		if toolConfig == nil {
			toolConfig = &genai.ToolConfig{}
		}
		t := true
		toolConfig.IncludeServerSideToolInvocations = &t
	}

	cfg := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: systemParts},
		Tools:             tools,
		ToolConfig:        toolConfig,
	}

	// Task 4: for schema-backed canned prompts, use structured JSON output.
	// Streaming chunks are suppressed; the full response is parsed after accumulation.
	isStructured := isCanned && CannedPrompts[cannedType].Schema != nil && CannedPrompts[cannedType].ForcedTool == ""
	if isStructured {
		cfg.ResponseSchema = CannedPrompts[cannedType].Schema
		cfg.ResponseMIMEType = "application/json"
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: s.APIKey, HTTPClient: s.HTTPClient})
	if err != nil {
		return "", nil, fmt.Errorf("creating genai client: %w", err)
	}

	// --- Multi-turn agentic tool loop ---
	var fullResponse strings.Builder

	for round := 0; round < maxToolRounds; round++ {
		iter := client.Models.GenerateContentStream(ctx, model, contents, cfg)

		// Collect the complete response for this round.
		var roundText strings.Builder
		var functionCalls []*genai.FunctionCall
		var functionCallParts []*genai.Part
		finishedWithToolCall := false

		for resp, iterErr := range iter {
			if iterErr != nil {
				return fullResponse.String(), nil, fmt.Errorf("generating content stream: %w", iterErr)
			}
			if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
				continue
			}
			for _, pt := range resp.Candidates[0].Content.Parts {
				if pt.Text != "" {
					roundText.WriteString(pt.Text)
				}
				if pt.FunctionCall != nil {
					functionCalls = append(functionCalls, pt.FunctionCall)
					functionCallParts = append(functionCallParts, pt)
					finishedWithToolCall = true
				}
			}
		}

		// Stream any text the model produced before the tool call.
		if chunk := roundText.String(); chunk != "" {
			fullResponse.WriteString(chunk)
			if !isStructured && onChunk != nil {
				if cbErr := onChunk(fullResponse.String()); cbErr != nil {
					log.Printf("WARN: stream chunk callback failed, client disconnected: %v", cbErr)
					return fullResponse.String(), nil, nil
				}
			}
		}

		if !finishedWithToolCall || len(functionCalls) == 0 {
			// No more tool calls — model has finished.
			break
		}

		// Append the model's tool-call turn to the conversation.
		var modelTurnParts []*genai.Part
		if roundText.Len() > 0 {
			modelTurnParts = append(modelTurnParts, &genai.Part{Text: roundText.String()})
		}
		modelTurnParts = append(modelTurnParts, functionCallParts...)
		contents = append(contents, &genai.Content{Role: genai.RoleModel, Parts: modelTurnParts})

		// After the first tool call round, clear any ForcedTool so the model can proceed freely.
		if cfg.ToolConfig != nil && cfg.ToolConfig.FunctionCallingConfig != nil {
			cfg.ToolConfig.FunctionCallingConfig = nil
		}

		// Execute each function call and collect responses.
		responseParts := make([]*genai.Part, 0, len(functionCalls))
		for _, fc := range functionCalls {
			log.Printf("INFO: tool_call [name=%s args=%v]", fc.Name, fc.Args)

			// Notify the frontend a tool is running.
			if onToolCall != nil {
				if cbErr := onToolCall(fc.Name); cbErr != nil {
					log.Printf("WARN: tool_call SSE callback failed: %v", cbErr)
				}
			}

			var result map[string]any
			var toolErr error
			if executor != nil {
				result, toolErr = executor(ctx, fc)
			} else {
				toolErr = fmt.Errorf("no executor configured")
			}

			if toolErr != nil {
				log.Printf("WARN: tool %s execution error: %v", fc.Name, toolErr)
				result = map[string]any{"error": toolErr.Error()}
			}

			responseParts = append(responseParts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     fc.Name,
					Response: result,
				},
			})
		}

		// Append all tool responses as a single user turn.
		contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: responseParts})
	}

	rawText := fullResponse.String()

	if !isStructured {
		return rawText, nil, nil
	}

	// Parse the JSON response and build the sections slice.
	cp := CannedPrompts[cannedType]
	var fields map[string]string
	if jsonErr := json.Unmarshal([]byte(rawText), &fields); jsonErr != nil {
		log.Printf("WARN: structured response JSON parse failed [cannedType=%s]: %v — falling back to raw text", cannedType, jsonErr)
		return rawText, nil, nil
	}

	// Build ordered sections (exclude "thinking" — it becomes the collapsible disclosure).
	for _, key := range cp.SectionOrder {
		if key == "thinking" {
			continue
		}
		content := fields[key]
		sections = append(sections, ResponseSection{
			Key:     key,
			Title:   cp.SectionTitles[key],
			Content: content,
		})
	}

	// Prepend thinking section if present (frontend renders it as a collapsible).
	if thinking := fields["thinking"]; thinking != "" {
		sections = append([]ResponseSection{{Key: "thinking", Title: "Thinking", Content: thinking}}, sections...)
	}

	// Reconstruct markdown for cache storage and fallback rendering.
	reconstructed := reconstructMarkdown(cp, fields)

	return reconstructed, sections, nil
}
