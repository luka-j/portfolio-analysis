package handlers

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"portfolio-analysis/middleware"
	"portfolio-analysis/models"
	"portfolio-analysis/services/flexquery"
	"portfolio-analysis/services/fx"
	"portfolio-analysis/services/parsers"
	"portfolio-analysis/services/portfolio"
)

// PortfolioHandler handles portfolio-related endpoints.
type PortfolioHandler struct {
	Repo             *flexquery.Repository
	PortfolioService *portfolio.Service
	FXService        *fx.Service
	activeUploads    sync.Map // key: userHash string, value: struct{}
}

// NewPortfolioHandler creates a new PortfolioHandler.
func NewPortfolioHandler(repo *flexquery.Repository, ps *portfolio.Service, fxSvc *fx.Service) *PortfolioHandler {
	return &PortfolioHandler{Repo: repo, PortfolioService: ps, FXService: fxSvc}
}

// claimUpload marks an upload as in-progress for the user. Returns false if one is already running.
func (h *PortfolioHandler) claimUpload(userHash string) bool {
	_, loaded := h.activeUploads.LoadOrStore(userHash, struct{}{})
	return !loaded
}

// releaseUpload clears the in-progress upload marker for the user.
func (h *PortfolioHandler) releaseUpload(userHash string) {
	h.activeUploads.Delete(userHash)
}

// Upload handles POST /api/v1/portfolio/upload
func (h *PortfolioHandler) Upload(c *gin.Context) {
	// Limit upload to 10 MB to prevent abuse.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)

	userHash := c.GetString(middleware.UserHashKey)

	if !h.claimUpload(userHash) {
		c.JSON(http.StatusConflict, gin.H{"error": "an upload is already in progress for this user"})
		return
	}
	defer h.releaseUpload(userHash)

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file field: " + err.Error()})
		return
	}
	defer file.Close()

	data, err := h.Repo.ParseAndSave(file, userHash)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse error: " + err.Error()})
		return
	}

	// Invalidate LLM cache since portfolio changed
	h.Repo.DB.Where("user_hash = ?", userHash).Delete(&models.LLMCache{})

	c.JSON(http.StatusOK, gin.H{
		"message":                 "upload successful",
		"positions_count":         len(data.OpenPositions),
		"trades_count":            len(data.Trades),
		"cash_transactions_count": len(data.CashTransactions),
	})
}

// UploadEtradeBenefits handles POST /api/v1/portfolio/upload/etrade/benefits
func (h *PortfolioHandler) UploadEtradeBenefits(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	if !h.claimUpload(userHash) {
		c.JSON(http.StatusConflict, gin.H{"error": "an upload is already in progress for this user"})
		return
	}
	defer h.releaseUpload(userHash)

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
	userHash := c.GetString(middleware.UserHashKey)
	if !h.claimUpload(userHash) {
		c.JSON(http.StatusConflict, gin.H{"error": "an upload is already in progress for this user"})
		return
	}
	defer h.releaseUpload(userHash)

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
	if err := h.Repo.DB.Where(&models.User{TokenHash: userHash}).FirstOrCreate(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "getting user: " + err.Error()})
		return
	}

	saved := 0
	for _, txn := range txns {
		var existing models.Transaction
		err := h.Repo.DB.Where(
			"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ?",
			user.ID, txn.Type, txn.Symbol, txn.DateTime, txn.Quantity-1e-8, txn.Quantity+1e-8,
		).First(&existing).Error

		if err == nil {
			if err := h.Repo.DB.Save(&existing).Error; err == nil {
				saved++
			}
		} else {
			txn.UserID = user.ID
			if err := h.Repo.DB.Create(&txn).Error; err == nil {
				saved++
			}
		}
	}

	// Invalidate LLM cache since portfolio changed
	h.Repo.DB.Where("user_hash = ?", userHash).Delete(&models.LLMCache{})

	c.JSON(http.StatusOK, gin.H{
		"message":      "upload successful",
		"saved_count":  saved,
		"parsed_count": len(txns),
	})
}

// GetValue handles GET /api/v1/portfolio/value
func (h *PortfolioHandler) GetValue(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	currenciesStr := c.DefaultQuery("currencies", "USD")
	currencies := splitCurrencies(currenciesStr)
	primaryCurrency := currencies[0]
	acctModel := parseAccountingModel(c)
	cachedOnly := parseCachedOnly(c)

	result, err := h.PortfolioService.GetCurrentValue(data, primaryCurrency, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// For additional currencies, compute and merge into the per-currency maps.
	for _, cur := range currencies[1:] {
		extra, err := h.PortfolioService.GetCurrentValue(data, cur, acctModel, cachedOnly)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Index extra positions by (symbol, exchange) to match with primary results.
		type posKey struct{ symbol, exchange string }
		extraByKey := make(map[posKey]*models.PositionValue, len(extra.Positions))
		for j := range extra.Positions {
			p := &extra.Positions[j]
			extraByKey[posKey{p.Symbol, p.ListingExchange}] = p
		}
		for i := range result.Positions {
			p := &result.Positions[i]
			if ep, ok := extraByKey[posKey{p.Symbol, p.ListingExchange}]; ok {
				p.Prices[cur] = ep.Price
				p.CostBases[cur] = ep.CostBasis
				p.Values[cur] = ep.Value
			}
		}
	}

	// Enrich bond ETF positions with effective duration from asset_fundamentals.
	for i := range result.Positions {
		pos := &result.Positions[i]
		eff := pos.YahooSymbol
		if eff == "" {
			eff = pos.Symbol
		}
		var fund models.AssetFundamental
		if err := h.Repo.DB.Select("asset_type, duration, name, isin").Where("symbol = ?", eff).First(&fund).Error; err == nil {
			if fund.AssetType == "Bond ETF" && fund.Duration != nil {
				result.Positions[i].BondDuration = fund.Duration
			}
			if fund.Name != "" {
				result.Positions[i].Name = fund.Name
			}
			if fund.ISIN != "" {
				result.Positions[i].ISIN = fund.ISIN
			}
			if fund.AssetType != "" {
				result.Positions[i].AssetType = fund.AssetType
			}
		}
	}

	result.HasTransactions = len(data.Trades) > 0 || len(data.CashTransactions) > 0
	c.JSON(http.StatusOK, result)
}

// GetHistory handles GET /api/v1/portfolio/history
func (h *PortfolioHandler) GetHistory(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Repo.LoadSaved(userHash)
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
	cachedOnly := parseCachedOnly(c)
	result, err := h.PortfolioService.GetDailyValues(data, from, to, currency, acctModel, cachedOnly)
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

	data, err := h.Repo.LoadSaved(userHash)
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
// Returns the daily cumulative return series (TWR or MWR in %) so the frontend can
// plot a true return chart, uncontaminated by cash flows.
func (h *PortfolioHandler) GetReturns(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Repo.LoadSaved(userHash)
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
	cachedOnly := parseCachedOnly(c)
	returnType := c.DefaultQuery("type", "twr")

	var result *models.PortfolioHistoryResponse
	var calcErr error

	if returnType == "mwr" {
		result, calcErr = h.PortfolioService.GetCumulativeMWR(data, from, to, currency, acctModel, cachedOnly)
	} else {
		result, calcErr = h.PortfolioService.GetCumulativeTWR(data, from, to, currency, acctModel, cachedOnly)
	}

	if calcErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": calcErr.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// SymbolPriceHistory holds per-symbol price change and average price over a date range.
type SymbolPriceHistory struct {
	Symbol    string   `json:"symbol"`
	Exchange  string   `json:"exchange,omitempty"`
	ChangePct *float64 `json:"change_pct"`
	AvgPrice  *float64 `json:"avg_price"`
	Currency  string   `json:"currency"`
}

// GetPriceHistory handles GET /api/v1/portfolio/price-history
// Returns change % and average price for each portfolio position over the requested date range.
func (h *PortfolioHandler) GetPriceHistory(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	data, err := h.Repo.LoadSaved(userHash)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	from, to, err := parseDateRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	currency := c.DefaultQuery("currency", "USD")
	acctModel := parseAccountingModel(c)

	// Resolve positions to get yahoo symbols and native currencies.
	val, err := h.PortfolioService.GetCurrentValue(data, currency, acctModel, false) // price history usually wants fresh data
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(val.Positions) == 0 {
		c.JSON(http.StatusOK, gin.H{"items": []SymbolPriceHistory{}})
		return
	}

	// Build a slice of unique yahoo symbols and map them back to positions.
	type posInfo struct {
		symbol         string
		exchange       string
		yahooSymbol    string
		nativeCurrency string
	}
	positions := make([]posInfo, 0, len(val.Positions))
	yahooSymbols := make([]string, 0, len(val.Positions))
	seen := make(map[string]bool)
	for _, pos := range val.Positions {
		ys := pos.YahooSymbol
		if ys == "" {
			ys = pos.Symbol
		}
		positions = append(positions, posInfo{
			symbol:         pos.Symbol,
			exchange:       pos.ListingExchange,
			yahooSymbol:    ys,
			nativeCurrency: pos.NativeCurrency,
		})
		if !seen[ys] {
			seen[ys] = true
			yahooSymbols = append(yahooSymbols, ys)
		}
	}

	// "First" price per symbol: latest close on or before `from`.
	// Using a subquery join so a single round-trip handles all symbols and is
	// compatible with both PostgreSQL and SQLite.
	subFirst := h.Repo.DB.Model(&models.MarketData{}).
		Select("symbol, MAX(date) as max_date").
		Where("symbol IN ? AND date <= ? AND volume != -1", yahooSymbols, from).
		Group("symbol")
	var firstRows []models.MarketData
	if err := h.Repo.DB.
		Joins("JOIN (?) sub ON market_data.symbol = sub.symbol AND market_data.date = sub.max_date", subFirst).
		Where("market_data.symbol IN ?", yahooSymbols).
		Select("market_data.symbol, market_data.adj_close").
		Find(&firstRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "querying first prices: " + err.Error()})
		return
	}
	firstBySymbol := make(map[string]float64, len(firstRows))
	for _, r := range firstRows {
		firstBySymbol[r.Symbol] = r.AdjClose
	}

	// "Last" price per symbol: live current price when `to` is today, otherwise
	// the latest close on or before `to`.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	toIsToday := !to.Before(today) && to.Before(today.AddDate(0, 0, 1))

	lastBySymbol := make(map[string]float64, len(yahooSymbols))
	if toIsToday && h.PortfolioService.CurrentPriceProvider != nil {
		for _, ys := range yahooSymbols {
			if p, err := h.PortfolioService.CurrentPriceProvider.GetCurrentPrice(ys, false); err == nil && p != 0 {
				lastBySymbol[ys] = p
			}
		}
	}
	// Fall back to latest historical close ≤ to for any symbol without a live price.
	var needHistLast []string
	for _, ys := range yahooSymbols {
		if lastBySymbol[ys] == 0 {
			needHistLast = append(needHistLast, ys)
		}
	}
	if len(needHistLast) > 0 {
		subLast := h.Repo.DB.Model(&models.MarketData{}).
			Select("symbol, MAX(date) as max_date").
			Where("symbol IN ? AND date <= ? AND volume != -1", needHistLast, to).
			Group("symbol")
		var lastRows []models.MarketData
		if err := h.Repo.DB.
			Joins("JOIN (?) sub ON market_data.symbol = sub.symbol AND market_data.date = sub.max_date", subLast).
			Where("market_data.symbol IN ?", needHistLast).
			Select("market_data.symbol, market_data.adj_close").
			Find(&lastRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "querying last prices: " + err.Error()})
			return
		}
		for _, r := range lastRows {
			lastBySymbol[r.Symbol] = r.AdjClose
		}
	}

	// Range rows for avg_price (all closes in [from, to]).
	var rows []models.MarketData
	if err := h.Repo.DB.
		Where("symbol IN ? AND date >= ? AND date <= ? AND volume != -1", yahooSymbols, from, to).
		Order("symbol, date").
		Select("symbol, date, adj_close").
		Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "querying market data: " + err.Error()})
		return
	}
	bySymbol := make(map[string][]models.MarketData, len(yahooSymbols))
	for _, r := range rows {
		bySymbol[r.Symbol] = append(bySymbol[r.Symbol], r)
	}

	// For each position compute change_pct and avg_price.
	items := make([]SymbolPriceHistory, 0, len(positions))
	for _, pos := range positions {
		item := SymbolPriceHistory{
			Symbol:   pos.symbol,
			Exchange: pos.exchange,
			Currency: currency,
		}

		firstPrice := firstBySymbol[pos.yahooSymbol]
		lastPrice := lastBySymbol[pos.yahooSymbol]
		if firstPrice != 0 && lastPrice != 0 {
			pct := (lastPrice - firstPrice) / firstPrice * 100
			item.ChangePct = &pct
		}

		pts := bySymbol[pos.yahooSymbol]
		if len(pts) >= 1 {
			var sum float64
			for _, p := range pts {
				sum += p.AdjClose
			}
			avg := sum / float64(len(pts))
			// Convert native avg price to display currency using spot rate.
			if currency != "Original" && pos.nativeCurrency != "" && pos.nativeCurrency != currency {
				converted, fxErr := h.FXService.ConvertSpot(avg, pos.nativeCurrency, currency, false)
				if fxErr == nil {
					avg = converted
				}
			}
			item.AvgPrice = &avg
		}

		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
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

	if err := h.Repo.UpdateSymbolMapping(userHash, symbol, exchange, req.YahooSymbol); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "symbol mapped successfully"})
}
