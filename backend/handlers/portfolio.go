package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/middleware"
	"gofolio-analysis/models"
	"gofolio-analysis/services/flexquery"
	"gofolio-analysis/services/parsers"
	"gofolio-analysis/services/portfolio"
)

// PortfolioHandler handles portfolio-related endpoints.
type PortfolioHandler struct {
	Parser           *flexquery.Parser
	PortfolioService *portfolio.Service
}

// NewPortfolioHandler creates a new PortfolioHandler.
func NewPortfolioHandler(parser *flexquery.Parser, ps *portfolio.Service) *PortfolioHandler {
	return &PortfolioHandler{Parser: parser, PortfolioService: ps}
}

// Upload handles POST /api/v1/portfolio/upload
func (h *PortfolioHandler) Upload(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file field: " + err.Error()})
		return
	}
	defer file.Close()

	data, err := h.Parser.ParseAndSave(file, userHash)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":                "upload successful",
		"positions_count":        len(data.OpenPositions),
		"trades_count":           len(data.Trades),
		"cash_transactions_count": len(data.CashTransactions),
	})
}

// UploadEtradeBenefits handles POST /api/v1/portfolio/upload/etrade/benefits
func (h *PortfolioHandler) UploadEtradeBenefits(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file field: " + err.Error()})
		return
	}
	defer file.Close()

	txns, err := parsers.ParseBenefitHistory(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse error: " + err.Error()})
		return
	}

	h.saveEtradeTransactions(c, txns)
}

// UploadEtradeSales handles POST /api/v1/portfolio/upload/etrade/sales
func (h *PortfolioHandler) UploadEtradeSales(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file field: " + err.Error()})
		return
	}
	defer file.Close()

	txns, err := parsers.ParseGainsLosses(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse error: " + err.Error()})
		return
	}

	h.saveEtradeTransactions(c, txns)
}

func (h *PortfolioHandler) saveEtradeTransactions(c *gin.Context, txns []models.Transaction) {
	userHash := c.GetString(middleware.UserHashKey)

	var user models.User
	if err := h.Parser.DB.Where(&models.User{TokenHash: userHash}).FirstOrCreate(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "getting user: " + err.Error()})
		return
	}

	// Cleanup pass for previously inserted $0 placeholders that caused the charting artifacts
	h.Parser.DB.Where("user_id = ? AND type = ? AND price = 0", user.ID, "RSU_VEST").Delete(&models.Transaction{})

	saved := 0
	for _, txn := range txns {
		var existing models.Transaction
		err := h.Parser.DB.Where(
			"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ?",
			user.ID, txn.Type, txn.Symbol, txn.DateTime, txn.Quantity-1e-8, txn.Quantity+1e-8,
		).First(&existing).Error

		if err == nil {
			// Update in place to repair historical records that were incorrectly priced
			existing.Price = txn.Price
			existing.Proceeds = txn.Proceeds
			existing.TaxCostBasis = txn.TaxCostBasis
			if err := h.Parser.DB.Save(&existing).Error; err == nil {
				saved++
			}
		} else {
			txn.UserID = user.ID
			if err := h.Parser.DB.Create(&txn).Error; err == nil {
				saved++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "upload successful",
		"saved_count":  saved,
		"parsed_count": len(txns),
	})
}

// GetValue handles GET /api/v1/portfolio/value
func (h *PortfolioHandler) GetValue(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	currStr := c.DefaultQuery("currencies", "USD,EUR,CZK")
	currencies := strings.Split(currStr, ",")
	for i := range currencies {
		currencies[i] = strings.TrimSpace(currencies[i])
	}

	acctModel := parseAccountingModel(c)
	result, err := h.PortfolioService.GetCurrentValue(data, currencies, acctModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetHistory handles GET /api/v1/portfolio/history
func (h *PortfolioHandler) GetHistory(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Constrain 'from' date to the portfolio's inception date
	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel := parseAccountingModel(c)
	result, err := h.PortfolioService.GetDailyValues(data, from, to, currency, acctModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetTrades handles GET /api/v1/portfolio/trades
// Supports ?limit=N&offset=M pagination (default: limit=200, offset=0).
func (h *PortfolioHandler) GetTrades(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}
	exchange := c.Query("exchange") // optional; empty means all exchanges for that symbol

	limit := 200
	offset := 0
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	displayCurrency := c.DefaultQuery("currency", "CZK")

	result, err := h.PortfolioService.GetTradesForSymbol(data, symbol, exchange, displayCurrency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Apply pagination to the trades slice.
	total := len(result.Trades)
	if offset >= total {
		result.Trades = nil
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		result.Trades = result.Trades[offset:end]
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":           result.Symbol,
		"currency":         result.Currency,
		"display_currency": result.DisplayCurrency,
		"trades":           result.Trades,
		"total":            total,
		"limit":            limit,
		"offset":           offset,
	})
}

// GetReturns handles GET /api/v1/portfolio/history/returns
// Returns the daily cumulative TWR series (in %) so the frontend can
// plot a true Time-Weighted Return chart, uncontaminated by cash flows.
func (h *PortfolioHandler) GetReturns(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Parser.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Constrain 'from' to portfolio inception.
	earliest, _ := DateRangeFromData(data)
	if !earliest.IsZero() && from.Before(earliest) {
		from = time.Date(earliest.Year(), earliest.Month(), earliest.Day(), 0, 0, 0, 0, time.UTC)
	}

	currency := c.Query("currency")
	if currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency parameter is required"})
		return
	}

	acctModel := parseAccountingModel(c)
	result, err := h.PortfolioService.GetCumulativeTWR(data, from, to, currency, acctModel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// MapSymbol handles PUT /api/v1/portfolio/symbols/:symbol/mapping
func (h *PortfolioHandler) MapSymbol(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	symbol := c.Param("symbol")
	exchange := c.Query("exchange") // optional

	var req struct {
		YahooSymbol string `json:"yahoo_symbol"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if err := h.Parser.UpdateSymbolMapping(userHash, symbol, exchange, req.YahooSymbol); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "symbol mapped successfully"})
}
