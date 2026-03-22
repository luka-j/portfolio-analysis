package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/middleware"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/tax"
)

// TaxHandler processes tax report requests.
type TaxHandler struct {
	Parser  *flexquery.Parser
	TaxSvc  *tax.Service
}

// GetReport handles GET /api/v1/tax/report
func (h *TaxHandler) GetReport(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	yearStr := c.Query("year")
	year, err := strconv.Atoi(yearStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid year parameter"})
		return
	}

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "failed to load data: " + err.Error()})
		return
	}

	report, err := h.TaxSvc.GetReport(data, year)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compute tax: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, report)
}
