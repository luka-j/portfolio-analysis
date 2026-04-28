package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/llm"
	"portfolio-analysis/services/market"
	"portfolio-analysis/services/portfolio"
	scenariosvc "portfolio-analysis/services/scenario"
	"portfolio-analysis/services/stats"
	"portfolio-analysis/services/tax"
)

// ScenarioHandler handles scenario CRUD and LLM-comparison endpoints.
type ScenarioHandler struct {
	Repo             *flexquery.Repository
	DB               *gorm.DB
	ScenarioRepo     *scenariosvc.Repository
	PortfolioService *portfolio.Service
	TaxSvc           *tax.Service
	MarketProvider   market.Provider
	CurrencyGetter   market.CurrencyGetter
	FXService        *fx.Service
	LLM              *llm.Service
}

// NewScenarioHandler creates a ScenarioHandler.
func NewScenarioHandler(
	repo *flexquery.Repository,
	db *gorm.DB,
	scenarioRepo *scenariosvc.Repository,
	ps *portfolio.Service,
	ts *tax.Service,
	mp market.Provider,
	cg market.CurrencyGetter,
	fxSvc *fx.Service,
	llmSvc *llm.Service,
) *ScenarioHandler {
	return &ScenarioHandler{
		Repo:             repo,
		DB:               db,
		ScenarioRepo:     scenarioRepo,
		PortfolioService: ps,
		TaxSvc:           ts,
		MarketProvider:   mp,
		CurrencyGetter:   cg,
		FXService:        fxSvc,
		LLM:              llmSvc,
	}
}

type createScenarioRequest struct {
	Spec   scenariosvc.ScenarioSpec `json:"spec"`
	Name   string                   `json:"name"`
	Pinned bool                     `json:"pinned"`
}

type updateScenarioRequest struct {
	Name   *string                   `json:"name,omitempty"`
	Pinned *bool                     `json:"pinned,omitempty"`
	Spec   *scenariosvc.ScenarioSpec `json:"spec,omitempty"`
}

// resolveUser looks up the User row for the given token hash.
// Writes a 401 and returns false on failure.
func (h *ScenarioHandler) resolveUser(c *gin.Context, userHash string) (models.User, bool) {
	var user models.User
	if err := h.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return user, false
	}
	return user, true
}

// List handles GET /api/v1/scenarios
func (h *ScenarioHandler) List(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	user, ok := h.resolveUser(c, userHash)
	if !ok {
		return
	}
	summaries, err := h.ScenarioRepo.List(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, summaries)
}

// Get handles GET /api/v1/scenarios/:id
func (h *ScenarioHandler) Get(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	user, ok := h.resolveUser(c, userHash)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	row, err := h.ScenarioRepo.Get(user.ID, uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}
	spec, err := scenariosvc.ParseSpec(row)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":           row.ID,
		"name":         row.Name,
		"pinned":       row.Pinned,
		"spec":         spec,
		"created_at":   row.CreatedAt,
		"last_used_at": row.LastUsedAt,
	})
}

// Create handles POST /api/v1/scenarios
func (h *ScenarioHandler) Create(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	user, ok := h.resolveUser(c, userHash)
	if !ok {
		return
	}
	var req createScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	row, err := h.ScenarioRepo.Create(user.ID, req.Spec, req.Name, req.Pinned)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": row.ID, "name": row.Name, "pinned": row.Pinned, "created_at": row.CreatedAt})
}

// Update handles PATCH /api/v1/scenarios/:id
func (h *ScenarioHandler) Update(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	user, ok := h.resolveUser(c, userHash)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req updateScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	patch := scenariosvc.ScenarioPatch{Name: req.Name, Pinned: req.Pinned, Spec: req.Spec}
	row, err := h.ScenarioRepo.Update(user.ID, uint(id), patch)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scenario not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": row.ID, "name": row.Name, "pinned": row.Pinned, "updated_at": row.UpdatedAt})
}

// Delete handles DELETE /api/v1/scenarios/:id
func (h *ScenarioHandler) Delete(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	user, ok := h.resolveUser(c, userHash)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.ScenarioRepo.Delete(user.ID, uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// compareLLMRequest is the body for POST /api/v1/scenarios/compare-llm.
type compareLLMRequest struct {
	AID      uint   `json:"a_id"`
	BID      uint   `json:"b_id"`
	Question string `json:"question,omitempty"`
	Currency string `json:"currency"`
	Model    string `json:"model"`
}

// CompareLLM handles POST /api/v1/scenarios/compare-llm.
// Builds both scenarios, computes a metrics bundle for each, and streams a Gemini Pro
// qualitative comparison. Responses are cached in llm_caches.
func (h *ScenarioHandler) CompareLLM(c *gin.Context) {
	if h.LLM == nil || h.LLM.APIKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM service not configured"})
		return
	}

	userHash := c.GetString(middleware.UserHashKey)
	user, ok := h.resolveUser(c, userHash)
	if !ok {
		return
	}

	var req compareLLMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	effectiveKey := req.Model
	if effectiveKey != "flash" && effectiveKey != "pro" {
		effectiveKey = "pro" // default to pro
	}
	modelKey := h.LLM.ResolveModel(effectiveKey)

	// Load real portfolio data once; reused as base for both scenarios.
	realData, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "portfolio data not found"})
		return
	}

	buildScenarioData := func(id uint) (*models.FlexQueryData, string, error) {
		if id == 0 {
			return realData, "Real portfolio", nil
		}
		row, err := h.ScenarioRepo.Get(user.ID, id)
		if err != nil {
			return nil, "", err
		}
		if row == nil {
			return nil, "", fmt.Errorf("scenario %d not found", id)
		}
		spec, err := scenariosvc.ParseSpec(row)
		if err != nil {
			return nil, "", err
		}
		data, err := scenariosvc.Build(spec, realData, h.MarketProvider, h.FXService)
		if err != nil {
			return nil, "", err
		}
		data.UserHash = fmt.Sprintf("scenario:%d:%d:%s", id, row.UpdatedAt.UnixNano(), userHash)
		name := row.Name
		if name == "" {
			name = fmt.Sprintf("Scenario %d", id)
		}
		return data, name, nil
	}

	dataA, nameA, err := buildScenarioData(req.AID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "loading scenario A: " + err.Error()})
		return
	}
	dataB, nameB, err := buildScenarioData(req.BID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "loading scenario B: " + err.Error()})
		return
	}

	// Compute analytics bundles for both scenarios.
	bundleA, err := h.buildMetricsBundle(dataA, req.Currency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing metrics for A: " + err.Error()})
		return
	}
	bundleB, err := h.buildMetricsBundle(dataB, req.Currency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "computing metrics for B: " + err.Error()})
		return
	}

	prompt := buildComparePrompt(nameA, bundleA, nameB, bundleB, req.Question)

	// Cache key is deterministic: hash of both spec JSONs + question + currency. Including the
	// JSON (not just the IDs) means edits to a scenario invalidate the cached comparison.
	specA, specB := specJSONForID(h.ScenarioRepo, user.ID, req.AID), specJSONForID(h.ScenarioRepo, user.ID, req.BID)
	cacheKey := scenarioCompareCacheKey(specA, specB, req.Question, req.Currency)
	var cached models.LLMCache
	if h.DB.Where("user_hash = ? AND prompt_type = ? AND model = ?", userHash, cacheKey, modelKey).First(&cached).Error == nil {
		c.JSON(http.StatusOK, gin.H{"response": cached.Response, "cached": true})
		return
	}

	response, err := h.LLM.GenerateSimple(c.Request.Context(), prompt, modelKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "LLM error: " + err.Error()})
		return
	}

	// Cache the response.
	h.DB.Create(&models.LLMCache{
		UserHash:   userHash,
		PromptType: cacheKey,
		Model:      modelKey,
		Response:   response,
	})

	c.JSON(http.StatusOK, gin.H{"response": response, "cached": false})
}

// metricsBundle is a compact JSON-serialisable analytics summary for one scenario.
type metricsBundle struct {
	Holdings    string  `json:"holdings"`    // compact JSON of top positions
	Sharpe      float64 `json:"sharpe"`
	Sortino     float64 `json:"sortino"`
	Volatility  float64 `json:"volatility"`
	MaxDrawdown float64 `json:"max_drawdown"`
	VAMI        float64 `json:"vami"`
}

// buildMetricsBundle computes a compact analytics summary for the given FlexQueryData.
func (h *ScenarioHandler) buildMetricsBundle(data *models.FlexQueryData, currency string) (*metricsBundle, error) {
	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(-1, 0, 0) // 1-year window

	returns, _, _, err := h.PortfolioService.GetDailyReturns(data, from, to, currency, models.AccountingModelHistorical, false)
	if err != nil || len(returns) == 0 {
		// Graceful degradation: return empty bundle if data is insufficient.
		return &metricsBundle{}, nil
	}

	m := stats.CalculateStandaloneMetrics(returns, 0.04)

	// Compact holdings summary.
	holdingsSummary := ""
	if result, err := h.PortfolioService.GetCurrentValue(data, currency, models.AccountingModelSpot, false); err == nil {
		type pos struct {
			Symbol string  `json:"symbol"`
			Value  float64 `json:"value"`
		}
		var positions []pos
		for _, p := range result.Positions {
			positions = append(positions, pos{Symbol: p.Symbol, Value: p.Value})
		}
		if b, err := json.Marshal(positions); err == nil {
			holdingsSummary = string(b)
		}
	}

	return &metricsBundle{
		Holdings:    holdingsSummary,
		Sharpe:      m.SharpeRatio,
		Sortino:     m.SortinoRatio,
		Volatility:  m.Volatility,
		MaxDrawdown: m.MaxDrawdown,
		VAMI:        m.VAMI,
	}, nil
}

// buildComparePrompt assembles the LLM prompt for comparing two scenarios head-to-head.
// Mirrors the structured-section style of the canned prompts in services/llm/prompts.go.
func buildComparePrompt(nameA string, a *metricsBundle, nameB string, b *metricsBundle, question string) string {
	extraQuestion := ""
	if question != "" {
		extraQuestion = fmt.Sprintf("\n\n### ❓ User Question\nThe user asked the following specific question — answer it directly after the sections above, in its own paragraph, drawing on the data already presented rather than repeating it: %q", question)
	}
	return fmt.Sprintf(`You are a senior portfolio analyst conducting a head-to-head comparison of two portfolio configurations for a single investor who is deciding which to hold. Both portfolios were evaluated over the same trailing 1-year window, in the same currency, using the same methodology. Your goal is to explain the meaningful differences and characterise the trade-off — not to rank one as "correct".

## Portfolio A: %s
Holdings (symbol → value in base currency): %s
Sharpe: %.3f | Sortino: %.3f | Volatility (annualised): %.2f%% | Max Drawdown: %.2f%% | VAMI (1y, base=1000): %.2f

## Portfolio B: %s
Holdings (symbol → value in base currency): %s
Sharpe: %.3f | Sortino: %.3f | Volatility (annualised): %.2f%% | Max Drawdown: %.2f%% | VAMI (1y, base=1000): %.2f

Before writing, think step-by-step inside a <thinking> block: compute the absolute and relative deltas for each metric (e.g. "Sharpe: A 1.24 vs B 0.91 → A +0.33, +36%%"), identify which positions are unique to each side or differ materially in weight, and form a hypothesis linking those composition differences to the metric deltas. Flag any metric where the difference is marginal (<10%% relative) so you don't overstate it downstream.

Then produce the final report using EXACTLY these markdown headers in this order:

### 📊 Risk-Adjusted Return
Compare Sharpe and Sortino directly, citing both values and the delta. State which portfolio delivers more return per unit of risk and whether the edge is meaningful or marginal. If Sortino's gap is notably wider than Sharpe's, explain what that asymmetry says about downside behaviour; if they agree, say so.

### 🎢 Volatility & Drawdown
Compare annualised volatility and max drawdown side-by-side. Quantify the difference in both absolute percentage points AND relative terms. Say which portfolio offers the smoother ride, and whether the bumpier one is actually being compensated for that ride via higher wealth growth (VAMI) — if not, call that out as an inefficiency.

### 💰 Wealth Growth (VAMI)
Compare the 1-year VAMI figures (base = 1000). Translate the difference into plain language (e.g. "A ended at 1120 vs B at 1080 — A grew $1000 of starting capital ~$40 more"). Attribute the winner's edge: is it offence (capturing more upside) or defence (avoiding more downside)? Reason from the drawdown and volatility deltas already established.

### 🧩 Composition Differences
Identify concrete holdings-level differences: positions unique to A, positions unique to B, and shared positions whose weights differ meaningfully. Group by theme where possible (e.g. "A leans harder into large-cap US tech; B adds a bond sleeve and emerging-markets exposure"). Connect these composition differences causally to the metric deltas above — this is the "why" behind the numbers.

### ⚖️ Trade-offs & Suitability
Summarise each portfolio's primary trade-off in one or two sentences (e.g. "A maximises growth at the cost of deeper drawdowns; B sacrifices ~X%% of return for a materially smoother path"). Close with a direct statement about which investor type each portfolio suits better — accumulation vs. preservation, high vs. low risk tolerance, long vs. shorter horizon. Do NOT recommend that the user switch or act; just characterise the fit.%s

<constraints>
- Every numeric claim must come directly from the metrics provided above. Do NOT invent figures, project forward returns, or introduce outside data.
- Ticker symbols in the holdings JSON are authoritative — never silently rename or reinterpret them.
- If a metric is zero or missing for one side (e.g. a scenario with insufficient history), state that the comparison is unavailable on that dimension rather than guessing.
- Treat differences under ~10%% relative as "marginal" and avoid overclaiming a winner on those dimensions.
- Do NOT give specific buy/sell/hold advice. Your job is characterisation and trade-off analysis, not recommendation.
- Omit filler. If a section has no meaningful differentiator (e.g. the two portfolios are near-identical on that dimension), say so in one sentence instead of padding.
</constraints>`,
		nameA, a.Holdings, a.Sharpe, a.Sortino, a.Volatility*100, a.MaxDrawdown*100, a.VAMI,
		nameB, b.Holdings, b.Sharpe, b.Sortino, b.Volatility*100, b.MaxDrawdown*100, b.VAMI,
		extraQuestion,
	)
}

// scenarioCompareCacheKey produces a stable cache key for a compare-llm request.
// The key incorporates both spec JSONs so that editing a scenario invalidates any cached
// comparison that referenced the previous version.
func scenarioCompareCacheKey(specA, specB, question, currency string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s", specA, specB, question, currency)
	return fmt.Sprintf("scenario_compare_%x", h.Sum(nil)[:8])
}

// specJSONForID returns the raw SpecJSON for a scenario, or "real" for id==0,
// or "missing:<id>" when the row is gone — the latter still produces a stable key.
func specJSONForID(repo *scenariosvc.Repository, userID, id uint) string {
	if id == 0 {
		return "real"
	}
	row, err := repo.Get(userID, id)
	if err != nil || row == nil {
		return fmt.Sprintf("missing:%d", id)
	}
	return row.SpecJSON
}
