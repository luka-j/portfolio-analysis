package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	scenariosvc "portfolio-analysis/services/scenario"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/tax"
)

// TaxHandler processes tax report requests.
type TaxHandler struct {
	ScenarioMiddleware
	Repo   *flexquery.Repository
	TaxSvc *tax.Service
}

type taxReportRequest struct {
	Year          int                `json:"year"`
	ExchangeRates map[string]float64 `json:"exchange_rates,omitempty"`
}

// GetReport handles POST /api/v1/tax/report
func (h *TaxHandler) GetReport(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	var req taxReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.Year == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "year is required"})
		return
	}

	// Backtest scenarios contain only synthetic Trade entries with no cost-basis lineage,
	// no corporate actions, and no dividend/ESPP/RSU records — the Czech tax report would
	// be meaningless. Refuse up front.
	if sidStr := c.Query("scenario_id"); sidStr != "" {
		if sid, perr := strconv.ParseUint(sidStr, 10, 64); perr == nil && h.ScenarioRepo != nil {
			var user models.User
			if err := h.Repo.DB.Where("token_hash = ?", userHash).First(&user).Error; err == nil {
				if row, gerr := h.ScenarioRepo.Get(user.ID, uint(sid)); gerr == nil && row != nil {
					if spec, perr2 := scenariosvc.ParseSpec(row); perr2 == nil && spec.Backtest != nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "tax reports are not available for backtest scenarios (no cost-basis or tax lineage)"})
						return
					}
				}
			}
		}
	}

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	report, err := h.TaxSvc.GetReport(data, req.Year, req.ExchangeRates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compute tax: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, report)
}
