package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/models"
	"gofolio-analysis/services/market"
)

// MarketHandler handles market data endpoints.
type MarketHandler struct {
	MarketProvider market.Provider
	CurrencyGetter market.CurrencyGetter
}

// NewMarketHandler creates a new MarketHandler.
func NewMarketHandler(mp market.Provider, cg market.CurrencyGetter) *MarketHandler {
	return &MarketHandler{MarketProvider: mp, CurrencyGetter: cg}
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

	points, err := h.MarketProvider.GetHistory(symbol, from, to, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert prices to the requested display currency using historical FX rates.
	// Spot accounting uses a constant multiplier which cancels out in % return charts,
	// so conversion is only meaningful for the historical model.
	currency := c.Query("currency")
	acctModel := parseAccountingModel(c)
	if currency != "" && (acctModel == models.AccountingModelHistorical || acctModel == "") && h.CurrencyGetter != nil {
		nativeCcy, err := h.CurrencyGetter.GetCurrency(symbol)
		if err != nil {
			log.Printf("Warning: could not determine currency for %s: %v", symbol, err)
		} else if fxRates := buildFXRateMap(h.MarketProvider, nativeCcy, currency, from, to); fxRates != nil {
			for i := range points {
				if r, ok := fxRates[points[i].Date.Format("2006-01-02")]; ok && r != 0 {
					points[i].Open *= r
					points[i].High *= r
					points[i].Low *= r
					points[i].Close *= r
					points[i].AdjClose *= r
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol": symbol,
		"data":   points,
	})
}
