package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/middleware"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/tax"
)

// TaxHandler processes tax report requests.
type TaxHandler struct {
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

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "failed to load data: " + err.Error()})
		return
	}

	report, err := h.TaxSvc.GetReport(data, req.Year, req.ExchangeRates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compute tax: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, report)
}
