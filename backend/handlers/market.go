package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/services/market"
)

// MarketHandler handles market data endpoints.
type MarketHandler struct {
	MarketProvider market.Provider
}

// NewMarketHandler creates a new MarketHandler.
func NewMarketHandler(mp market.Provider) *MarketHandler {
	return &MarketHandler{MarketProvider: mp}
}

// GetHistory handles GET /api/v1/market/history
func (h *MarketHandler) GetHistory(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	points, err := h.MarketProvider.GetHistory(symbol, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol": symbol,
		"data":   points,
	})
}
