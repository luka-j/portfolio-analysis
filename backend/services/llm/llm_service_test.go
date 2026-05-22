package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
	"gorm.io/gorm"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/portfolio"
)

type mockMarketProvider struct {
	history      func(symbol string, from, to time.Time) ([]models.PricePoint, error)
	tradingDates func(from, to time.Time) ([]time.Time, error)
	latestPrice  func(symbol string) (float64, error)
}

func (m *mockMarketProvider) GetHistory(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	if m.history != nil {
		return m.history(symbol, from, to)
	}
	return nil, nil
}

func (m *mockMarketProvider) TradingDates(from, to time.Time) ([]time.Time, error) {
	if m.tradingDates != nil {
		return m.tradingDates(from, to)
	}
	return nil, nil
}

func (m *mockMarketProvider) GetLatestPrice(symbol string) (float64, error) {
	if m.latestPrice != nil {
		return m.latestPrice(symbol)
	}
	return 100.0, nil
}

type mockLLMTransport struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (m *mockLLMTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.roundTrip != nil {
		return m.roundTrip(req)
	}
	return nil, fmt.Errorf("no roundTrip func configured")
}

// setupLLMTestDB initializes the test database with all models.
func setupLLMTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(
		&models.ChatThread{},
		&models.ChatMessage{},
		&models.AssetFundamental{},
		&models.EtfBreakdown{},
		&models.User{},
		&models.Transaction{},
	)
	require.NoError(t, err)
	return db
}

// TestNewServiceAndResolve checks NewService initialization and model resolution.
func TestNewServiceAndResolve(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)

	// Test flash default
	s1 := NewService("key1", "flash-model", "pro-model", "flash", db, ps)
	assert.True(t, s1.ResolveModel("flash") == "flash-model")
	assert.True(t, s1.ResolveModel("pro") == "pro-model")
	assert.True(t, s1.ResolveModel("unknown") == "pro-model")

	// Test pro default
	s2 := NewService("key2", "flash-model", "pro-model", "pro", db, ps)
	assert.True(t, s2.ResolveModel("flash") == "flash-model")
	assert.True(t, s2.ResolveModel("pro") == "pro-model")
}

// TestRealUserHash verifies RealUserHash extraction behavior on various prefixes.
func TestRealUserHash(t *testing.T) {
	assert.Equal(t, "user123", RealUserHash("user123"))
	assert.Equal(t, "user123", RealUserHash("scenario:12:34:user123"))
	assert.Equal(t, "", RealUserHash("scenario:12:34:"))
	assert.Equal(t, "scenario:12:user123", RealUserHash("scenario:12:user123")) // less than 3 colons
}

// TestLookupFundamentals verifies lookups on DB with and without user scoping.
func TestLookupFundamentals(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	// Setup users
	u1 := models.User{TokenHash: "user_a"}
	require.NoError(t, db.Create(&u1).Error)
	u2 := models.User{TokenHash: "user_b"}
	require.NoError(t, db.Create(&u2).Error)

	// Create fundamentals
	require.NoError(t, db.Create(&models.AssetFundamental{
		UserID:      u1.ID,
		Symbol:      "AAPL",
		Name:        "Apple A",
		ISIN:        "US0378331005",
		Currency:    "USD",
		LastUpdated: time.Now(),
	}).Error)
	require.NoError(t, db.Create(&models.AssetFundamental{
		UserID:      u2.ID,
		Symbol:      "AAPL",
		Name:        "Apple B",
		ISIN:        "US0378331005",
		Currency:    "USD",
		LastUpdated: time.Now(),
	}).Error)

	// Test scoped lookup (user_a)
	m1 := llmSvc.lookupFundamentals("user_a", []string{"AAPL"})
	require.Contains(t, m1, "AAPL")
	assert.Equal(t, "Apple A", m1["AAPL"].Name)

	// Test scoped lookup (user_b)
	m2 := llmSvc.lookupFundamentals("user_b", []string{"AAPL"})
	require.Contains(t, m2, "AAPL")
	assert.Equal(t, "Apple B", m2["AAPL"].Name)

	// Test lookupNames
	names := llmSvc.lookupNames("user_a", []string{"AAPL"})
	assert.Equal(t, "Apple A", names["AAPL"])
}

// TestGetPortfolioJSON verifies portfolio data serialization.
func TestGetPortfolioJSON(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{
		latestPrice: func(symbol string) (float64, error) {
			return 100.0, nil
		},
	}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	u1 := models.User{TokenHash: "user_a"}
	require.NoError(t, db.Create(&u1).Error)

	// Create fundamentals
	require.NoError(t, db.Create(&models.AssetFundamental{
		UserID:   u1.ID,
		Symbol:   "AAPL",
		Name:     "Apple Inc.",
		ISIN:     "US0378331005",
		Currency: "USD",
	}).Error)

	data := &models.FlexQueryData{
		UserHash: "user_a",
		Trades: []models.Trade{
			{
				Symbol:        "AAPL",
				AssetCategory: "STK",
				Currency:      "USD",
				Quantity:      10,
				Price:         100,
				BuySell:       "BUY",
				DateTime:      time.Now().AddDate(-1, 0, 0),
			},
		},
	}

	jsonStr := llmSvc.getPortfolioJSON(data, "USD", models.AccountingModelSpot)
	assert.NotEmpty(t, jsonStr)

	var items []PortfolioContextItem
	err := json.Unmarshal([]byte(jsonStr), &items)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "AAPL", items[0].Symbol)
	assert.Equal(t, "Apple Inc.", items[0].Name)
	assert.Equal(t, 100.0, items[0].WeightPct)
}

// TestGetMarketSummary checks the market summary request construction and mock response.
func TestGetMarketSummary(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "POST", req.Method)
				assert.Contains(t, req.URL.Path, "generateContent")

				respJSON := `{
					"candidates": [{
						"content": {
							"parts": [{"text": "Market is green."}],
							"role": "model"
						},
						"finishReason": "STOP"
					}]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(respJSON)),
				}, nil
			},
		},
	}

	data := &models.FlexQueryData{UserHash: "user_a"}
	summary, err := llmSvc.GetMarketSummary(context.Background(), data, "1w")
	require.NoError(t, err)
	assert.Equal(t, "Market is green.", summary)
}

// TestGenerateTitle verifies formatting and cleaning of async titles.
func TestGenerateTitle(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				respJSON := `{
					"candidates": [{
						"content": {
							"parts": [{"text": "\"My Cleaned Title\""}],
							"role": "model"
						},
						"finishReason": "STOP"
					}]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(respJSON)),
				}, nil
			},
		},
	}

	messages := []models.ChatMessage{
		{Role: "user", Content: "Hello"},
	}
	title, err := llmSvc.GenerateTitle(context.Background(), messages)
	require.NoError(t, err)
	// Quotes should be stripped
	assert.Equal(t, "My Cleaned Title", title)
}

// TestAnalyzePortfolioStream_Freeform verifies multi-turn agentic loop with custom tool execution.
func TestAnalyzePortfolioStream_Freeform(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	roundCount := 0
	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				roundCount++
				var ssePayload string

				if roundCount == 1 {
					// Round 1 SSE stream element
					ssePayload = `data: {"candidates": [{"content": {"parts": [{"text": "<thinking>I need to check the risk metrics first.</thinking>"}, {"functionCall": {"name": "get_risk_metrics", "args": {"from_date": "2024-01-01", "to_date": "2024-12-31"}}}]}, "finishReason": "STOP"}]}` + "\n\n"
				} else {
					// Round 2 SSE stream element
					ssePayload = `data: {"candidates": [{"content": {"parts": [{"text": "Your risk looks fine."}]}, "finishReason": "STOP"}]}` + "\n\n"
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(ssePayload)),
				}, nil
			},
		},
	}

	data := &models.FlexQueryData{UserHash: "user_a"}
	var chunkCount int
	var toolsCalled []string

	onChunk := func(c string) error {
		chunkCount++
		return nil
	}
	onToolCall := func(name string) error {
		toolsCalled = append(toolsCalled, name)
		return nil
	}

	executor := func(ctx context.Context, call *genai.FunctionCall) (map[string]any, error) {
		assert.Equal(t, "get_risk_metrics", call.Name)
		return map[string]any{"sharpe": 1.5}, nil
	}

	response, sections, extras, err := llmSvc.AnalyzePortfolioStream(
		context.Background(),
		data,
		"USD",
		"", // freeform
		"What is my risk?",
		"pro",
		[]string{"get_risk_metrics"},
		nil,
		"spot",
		executor,
		onChunk,
		onToolCall,
	)

	require.NoError(t, err)
	assert.Contains(t, response, "Your risk looks fine.")
	assert.Nil(t, sections)
	assert.Nil(t, extras)
	assert.Len(t, toolsCalled, 1)
	assert.Equal(t, "get_risk_metrics", toolsCalled[0])
	assert.True(t, chunkCount > 0)
}

// TestAnalyzePortfolioStream_StructuredCanned verifies canned prompts structured logic.
func TestAnalyzePortfolioStream_StructuredCanned(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				// Canned structured JSON response payload wrapped in SSE data format
				ssePayload := `data: {"candidates": [{"content": {"parts": [{"text": "{\"thinking\": \"Thinking about the portfolio. I analysed this.\", \"confidence_score_value\": \"85%\", \"missing_data_context\": \"No cash details\", \"macro_environment\": \"All good\", \"sector_geographic_concentration\": \"Stocks mostly\", \"fama_french_factor_tilts\": \"Growth\", \"implicit_bets\": \"Bull market\", \"blind_spots\": \"Bonds\"}"}]}, "finishReason": "STOP"}]}` + "\n\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(ssePayload)),
				}, nil
			},
		},
	}

	data := &models.FlexQueryData{UserHash: "user_a"}
	response, sections, extras, err := llmSvc.AnalyzePortfolioStream(
		context.Background(),
		data,
		"USD",
		"general_analysis", // structured canned prompt
		"Analyze my portfolio.",
		"flash",
		nil,
		nil,
		"spot",
		nil,
		nil,
		nil,
	)

	require.NoError(t, err)
	assert.Contains(t, response, "All good")
	assert.Contains(t, response, "Thinking")

	require.NotEmpty(t, sections)
	assert.Equal(t, "thinking", sections[0].Key)
	assert.Equal(t, "Thinking about the portfolio. I analysed this.", sections[0].Content)

	require.NotNil(t, extras)
	assert.Equal(t, 85, extras["confidence_score_value"])
	assert.Equal(t, "No cash details", extras["missing_data_context"])
}

// TestAnalyzePortfolioStream_SubmitThinking verifies canned prompt with SubmitThinkingDecl.
func TestAnalyzePortfolioStream_SubmitThinking(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	round := 0
	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				round++
				var ssePayload string
				if round == 1 {
					// SubmitThinking function call wrapped in SSE data format
					ssePayload = `data: {"candidates": [{"content": {"parts": [{"functionCall": {"name": "submit_thinking", "args": {"thinking": "We are thinking", "self_critique": "A critique", "confidence_score_value": 90, "missing_data_context": "None"}}}]}, "finishReason": "STOP"}]}` + "\n\n"
				} else {
					// Round 2 structured JSON matching general_analysis schema wrapped in SSE
					ssePayload = `data: {"candidates": [{"content": {"parts": [{"text": "{\"macro_environment\": \"Plan working\", \"sector_geographic_concentration\": \"Details\", \"fama_french_factor_tilts\": \"Growth\", \"implicit_bets\": \"Bull market\", \"blind_spots\": \"Bonds\"}"}]}, "finishReason": "STOP"}]}` + "\n\n"
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(ssePayload)),
				}, nil
			},
		},
	}

	data := &models.FlexQueryData{UserHash: "user_a"}
	response, sections, extras, err := llmSvc.AnalyzePortfolioStream(
		context.Background(),
		data,
		"USD",
		"general_analysis",
		"Analyze my portfolio.",
		"flash",
		nil,
		nil,
		"spot",
		nil,
		nil,
		nil,
	)

	require.NoError(t, err)
	assert.Contains(t, response, "Plan working")
	require.NotEmpty(t, sections)
	assert.Equal(t, "thinking", sections[0].Key)
	assert.Contains(t, sections[0].Content, "We are thinking")
	assert.Contains(t, sections[0].Content, "Devil's Advocate")

	require.NotNil(t, extras)
	assert.Equal(t, 90, extras["confidence_score_value"])
}

// TestAnalyzePortfolioStream_ClientDisconnect verifies client disconnection (onChunk returns error).
func TestAnalyzePortfolioStream_ClientDisconnect(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				ssePayload := `data: {"candidates": [{"content": {"parts": [{"text": "Part 1"}]}, "finishReason": "STOP"}]}` + "\n\n"
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(ssePayload)),
				}, nil
			},
		},
	}

	data := &models.FlexQueryData{UserHash: "user_a"}
	onChunk := func(c string) error {
		return fmt.Errorf("client disconnected")
	}

	response, _, _, err := llmSvc.AnalyzePortfolioStream(
		context.Background(),
		data,
		"USD",
		"",
		"Analyze",
		"flash",
		nil,
		nil,
		"spot",
		nil,
		onChunk,
		nil,
	)

	// Should return early with nil error
	require.NoError(t, err)
	assert.Contains(t, response, "Part 1")
}

// TestGenerateSimple verifies the GenerateSimple API method.
func TestGenerateSimple(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				respJSON := `{
					"candidates": [{
						"content": {
							"parts": [{"text": "Simple Output"}],
							"role": "model"
						},
						"finishReason": "STOP"
					}]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(respJSON)),
				}, nil
			},
		},
	}

	res, err := llmSvc.GenerateSimple(context.Background(), "Hello", "flash")
	require.NoError(t, err)
	assert.Equal(t, "Simple Output", res)

	// Test ErrNotConfigured
	llmSvc.APIKey = ""
	_, err = llmSvc.GenerateSimple(context.Background(), "Hello", "flash")
	assert.ErrorIs(t, err, ErrNotConfigured)
}

// TestCallGemini_ErrorsAndEdgeCases verifies error/edge case branches inside callGemini.
func TestCallGemini_ErrorsAndEdgeCases(t *testing.T) {
	db := setupLLMTestDB(t)
	mp := &mockMarketProvider{}
	fxSvc := fx.NewService(mp, nil)
	ps := portfolio.NewService(mp, fxSvc, 0)
	llmSvc := NewService("dummy-key", "gemini-2.5-flash", "gemini-2.5-pro", "flash", db, ps)

	// 1. resp.UsageMetadata != nil
	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				respJSON := `{
					"candidates": [{
						"content": {
							"parts": [{"text": "Success with usage"}],
							"role": "model"
						},
						"finishReason": "STOP"
					}],
					"usageMetadata": {
						"promptTokenCount": 12,
						"candidatesTokenCount": 34
					}
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(respJSON)),
				}, nil
			},
		},
	}
	res, err := llmSvc.GenerateSimple(context.Background(), "Hello", "flash")
	require.NoError(t, err)
	assert.Equal(t, "Success with usage", res)

	// 2. len(candidates) == 0
	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				respJSON := `{"candidates": []}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(respJSON)),
				}, nil
			},
		},
	}
	_, err = llmSvc.GenerateSimple(context.Background(), "Hello", "flash")
	assert.ErrorContains(t, err, "no response generated")

	// 3. finishReason non-STOP with no parts generated
	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				respJSON := `{
					"candidates": [{
						"finishReason": "SAFETY"
					}]
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(respJSON)),
				}, nil
			},
		},
	}
	_, err = llmSvc.GenerateSimple(context.Background(), "Hello", "flash")
	assert.ErrorContains(t, err, "no response generated (finish reason: SAFETY)")

	// 4. Transport Error
	llmSvc.HTTPClient = &http.Client{
		Transport: &mockLLMTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("connection failure")
			},
		},
	}
	_, err = llmSvc.GenerateSimple(context.Background(), "Hello", "flash")
	assert.ErrorContains(t, err, "connection failure")
}

// TestLookupFundamentals_Empty DB checks empty cases in lookupFundamentals.
func TestLookupFundamentals_Empty(t *testing.T) {
	svc := &Service{}
	res := svc.lookupFundamentals("", nil)
	assert.Empty(t, res)

	res2 := svc.lookupFundamentals("", []string{"AAPL"})
	assert.Empty(t, res2)
}
