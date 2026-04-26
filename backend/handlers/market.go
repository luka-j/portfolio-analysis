package handlers

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"portfolio-analysis/models"
	"portfolio-analysis/services/market"
)

// MarketHandler handles market data endpoints.
type MarketHandler struct {
	MarketProvider market.Provider
	CurrencyGetter market.CurrencyGetter
	DB             *gorm.DB
}

// NewMarketHandler creates a new MarketHandler.
func NewMarketHandler(mp market.Provider, cg market.CurrencyGetter, db *gorm.DB) *MarketHandler {
	return &MarketHandler{MarketProvider: mp, CurrencyGetter: cg, DB: db}
}

// resolveYahooSymbol returns the effective Yahoo Finance symbol for a broker symbol.
// It checks the transactions table for a stored mapping; if none is found, the
// broker symbol itself is returned unchanged.
func (h *MarketHandler) resolveYahooSymbol(brokerSymbol string) string {
	if h.DB == nil {
		return brokerSymbol
	}
	var yahooSym string
	h.DB.Model(&models.Transaction{}).
		Where("symbol = ? AND yahoo_symbol != ''", brokerSymbol).
		Limit(1).
		Pluck("yahoo_symbol", &yahooSym)
	if yahooSym != "" {
		return yahooSym
	}
	return brokerSymbol
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
	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
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

// GetSymbols handles GET /api/v1/market/symbols
func (h *MarketHandler) GetSymbols(c *gin.Context) {
	var symbols []string
	if err := h.DB.Model(&models.MarketData{}).Where("symbol NOT LIKE '%=X'").Distinct("symbol").Pluck("symbol", &symbols).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"symbols": symbols})
}

// SecurityChartPoint is one data point in the security price chart response.
type SecurityChartPoint struct {
	Date  string   `json:"date"`
	Close float64  `json:"close"`
	MA    *float64 `json:"ma"`
}

// GetSecurityChart handles GET /api/v1/market/security-chart
// Query params: symbol (required), from (required, YYYY-MM-DD), to (optional, YYYY-MM-DD, default today),
// ma_days (int, default 30, range 2-365), currency (optional), accounting_model (optional;
// "original" or omitting currency keeps native prices).
func (h *MarketHandler) GetSecurityChart(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	fromStr := c.Query("from")
	if fromStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from parameter is required (YYYY-MM-DD)"})
		return
	}
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date: " + err.Error()})
		return
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	to := today
	if toStr := c.Query("to"); toStr != "" {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date: " + err.Error()})
			return
		}
	}
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to date must be on or after from date"})
		return
	}

	maDays := 30
	if maDaysStr := c.Query("ma_days"); maDaysStr != "" {
		maDays, err = strconv.Atoi(maDaysStr)
		if err != nil || maDays < 2 || maDays > 365 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ma_days must be an integer between 2 and 365"})
			return
		}
	}

	// Resolve the effective Yahoo Finance symbol via the stored transaction mapping.
	effectiveSym := h.resolveYahooSymbol(symbol)

	// Fetch with warm-up window so the MA can fill before the requested period starts.
	warmupFrom := from.AddDate(0, 0, -maDays*2)
	points, err := h.MarketProvider.GetHistory(effectiveSym, warmupFrom, to, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Apply FX conversion when a real display currency is requested.
	// accounting_model=original (or an absent currency) means keep native prices.
	currency := c.Query("currency")
	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	if currency != "" && acctModel != models.AccountingModelOriginal && h.CurrencyGetter != nil {
		nativeCcy, ccyErr := h.CurrencyGetter.GetCurrency(effectiveSym)
		if ccyErr != nil {
			log.Printf("Warning: could not determine currency for %s: %v", effectiveSym, ccyErr)
		} else if nativeCcy != currency {
			// For spot model use today's rate for all points; historical model uses per-day rates.
			var fxRates map[string]float64
			if acctModel == models.AccountingModelSpot {
				if todayRate, spotErr := h.MarketProvider.GetHistory(
					nativeCcy+currency+"=X",
					today.AddDate(0, 0, -5), today, false,
				); spotErr == nil && len(todayRate) > 0 {
					r := todayRate[len(todayRate)-1].Close
					// Fill a constant map so the loop below works uniformly.
					fxRates = make(map[string]float64, len(points))
					for _, p := range points {
						fxRates[p.Date.Format("2006-01-02")] = r
					}
				}
			} else {
				// Historical: per-day FX rate including warm-up window.
				fxRates = buildFXRateMap(h.MarketProvider, nativeCcy, currency, warmupFrom, to)
			}
			if fxRates != nil {
				for i := range points {
					if r, ok := fxRates[points[i].Date.Format("2006-01-02")]; ok && r != 0 {
						points[i].Close *= r
						points[i].AdjClose *= r
					}
				}
			}
		}
	}

	// Compute MA over all points (including warm-up), then filter to [from, to].
	// MA is computed after FX conversion so it is in the display currency.
	all := computeMA(points, maDays)
	var data []SecurityChartPoint
	for _, p := range all {
		if p.Date >= fromStr {
			data = append(data, p)
		}
	}
	if data == nil {
		data = []SecurityChartPoint{}
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":  symbol,
		"from":    fromStr,
		"to":      to.Format("2006-01-02"),
		"ma_days": maDays,
		"data":    data,
	})
}

// computeMA computes a trailing moving average over the given sorted price points.
// Returns one SecurityChartPoint per input point; MA is nil until the window fills.
func computeMA(points []models.PricePoint, days int) []SecurityChartPoint {
	result := make([]SecurityChartPoint, 0, len(points))
	window := make([]float64, 0, days)
	var sum float64

	for _, p := range points {
		window = append(window, p.Close)
		sum += p.Close
		if len(window) > days {
			sum -= window[0]
			window = window[1:]
		}

		pt := SecurityChartPoint{
			Date:  p.Date.Format("2006-01-02"),
			Close: p.Close,
		}
		if len(window) == days {
			avg := sum / float64(days)
			pt.MA = &avg
		}
		result = append(result, pt)
	}

	return result
}
