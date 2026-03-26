package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/middleware"
	breakdownsvc "gofolio-analysis/services/breakdown"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/fundamentals"
	"gofolio-analysis/models"
	"gofolio-analysis/services/portfolio"
)

// BreakdownHandler handles portfolio breakdown endpoints.
type BreakdownHandler struct {
	Parser          *flexquery.Parser
	PortfolioSvc    *portfolio.Service
	BreakdownSvc    *breakdownsvc.Service
	FundamentalsSvc *fundamentals.Service
}

// NewBreakdownHandler creates a new BreakdownHandler.
func NewBreakdownHandler(parser *flexquery.Parser, ps *portfolio.Service, bs *breakdownsvc.Service, fs *fundamentals.Service) *BreakdownHandler {
	return &BreakdownHandler{
		Parser:          parser,
		PortfolioSvc:    ps,
		BreakdownSvc:    bs,
		FundamentalsSvc: fs,
	}
}

// GetBreakdown handles GET /api/v1/portfolio/breakdown.
// Query params:
//
//	currency — display currency, default "USD"
func (h *BreakdownHandler) GetBreakdown(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	currency := strings.ToUpper(c.DefaultQuery("currency", "USD"))

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// GetCurrentValue uses spot accounting model for breakdown (we only need current weights).
	result, err := h.PortfolioSvc.GetCurrentValue(data, currency, models.AccountingModelSpot)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "fetching portfolio: " + err.Error()})
		return
	}

	breakdown, err := h.BreakdownSvc.Calculate(result.Positions, currency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "calculating breakdown: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, breakdown)
}
