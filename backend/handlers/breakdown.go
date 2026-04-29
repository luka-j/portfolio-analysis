package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	breakdownsvc "portfolio-analysis/services/breakdown"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fundamentals"
	"portfolio-analysis/services/portfolio"
)

// BreakdownHandler handles portfolio breakdown endpoints.
type BreakdownHandler struct {
	PortfolioResolver
	Repo            *flexquery.Repository
	PortfolioSvc    *portfolio.Service
	BreakdownSvc    *breakdownsvc.Service
	FundamentalsSvc *fundamentals.Service
}

// NewBreakdownHandler creates a new BreakdownHandler.
func NewBreakdownHandler(repo *flexquery.Repository, ps *portfolio.Service, bs *breakdownsvc.Service, fs *fundamentals.Service) *BreakdownHandler {
	return &BreakdownHandler{
		Repo:            repo,
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
	cachedOnly := parseCachedOnly(c)

	data, ok := h.loadPortfolioData(c, h.Repo, userHash)
	if !ok {
		return
	}

	// Resolve user ID for scoped asset_fundamentals lookup.
	var user models.User
	if err := h.Repo.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	// GetCurrentValue uses spot accounting model for breakdown (we only need current weights).
	result, err := h.PortfolioSvc.GetCurrentValue(data, currency, models.AccountingModelSpot, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "fetching portfolio: " + err.Error()})
		return
	}

	breakdown, err := h.BreakdownSvc.Calculate(result.Positions, currency, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "calculating breakdown: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, breakdown)
}
