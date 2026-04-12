package handlers

import (
	"net/http"
	"strconv"
	"strings"
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

	h.saveEtradeTransactions(c, txns, "etrade_benefits")
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

	h.saveEtradeTransactions(c, txns, "etrade_sales")
}

func (h *PortfolioHandler) saveEtradeTransactions(c *gin.Context, txns []models.Transaction, entryMethod string) {
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
			txn.EntryMethod = entryMethod
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
	currencies, ok := parseCurrencies(c, currenciesStr)
	if !ok {
		return
	}
	primaryCurrency := currencies[0]
	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
	cachedOnly := parseCachedOnly(c)

	resultsByCur, err := h.PortfolioService.GetCurrentValueMulti(data, currencies, acctModel, cachedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := resultsByCur[primaryCurrency]

	// Resolve user ID for scoped asset_fundamentals lookup.
	// A user that has never uploaded has no row yet — skip enrichment (no positions to enrich).
	var user models.User
	userFound := h.Repo.DB.Where("token_hash = ?", userHash).First(&user).Error == nil

	// Enrich positions with metadata from asset_fundamentals (scoped to this user).
	for i := range result.Positions {
		if !userFound {
			break
		}
		pos := &result.Positions[i]
		eff := pos.YahooSymbol
		if eff == "" {
			eff = pos.Symbol
		}
		var fund models.AssetFundamental
		if err := h.Repo.DB.Select("asset_type, duration, name, isin").
			Where("user_id = ? AND symbol = ?", user.ID, eff).First(&fund).Error; err == nil {
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

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
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

	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}
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
	acctModel, ok := parseAccountingModel(c)
	if !ok {
		return
	}

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

	// "Last" price per symbol: use the unified GetLatestPrice when `to` is today,
	// otherwise fall back to the latest historical close on or before `to`.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	toIsToday := !to.Before(today) && to.Before(today.AddDate(0, 0, 1))

	lastBySymbol := make(map[string]float64, len(yahooSymbols))
	if toIsToday {
		for _, ys := range yahooSymbols {
			if p, err := h.PortfolioService.MarketProvider.GetLatestPrice(ys, false); err == nil && p > 0 {
				lastBySymbol[ys] = p
			}
		}
	}
	// For symbols without a live price (or when to != today), use latest historical close.
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

// addTransactionRequest is the JSON body for POST /api/v1/portfolio/transactions.
type addTransactionRequest struct {
	TransactionType string  `json:"transaction_type"` // "buy", "sell", "espp_vest", "rsu_vest"
	Symbol          string  `json:"symbol"`
	Currency        string  `json:"currency"`
	ListingExchange string  `json:"listing_exchange"`
	Date            string  `json:"date"` // YYYY-MM-DD
	Quantity        float64 `json:"quantity"`
	Price           float64 `json:"price"`
	Commission      float64 `json:"commission"`
	TaxCostBasis    float64 `json:"tax_cost_basis"`
	Force           bool    `json:"force"`
}

// AddTransaction handles POST /api/v1/portfolio/transactions.
// It inserts a manually-entered trade and deduplicates against existing records
// using a float-tolerance match on (type, symbol, date_time, quantity, price).
func (h *PortfolioHandler) AddTransaction(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)

	var req addTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// Validate required fields.
	req.Symbol = strings.TrimSpace(strings.ToUpper(req.Symbol))
	if req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is required"})
		return
	}
	if req.Currency == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency is required"})
		return
	}
	req.ListingExchange = strings.TrimSpace(strings.ToUpper(req.ListingExchange))
	if req.ListingExchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "listing_exchange is required"})
		return
	}
	if req.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be greater than 0"})
		return
	}
	if req.Price <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price must be greater than 0"})
		return
	}

	dt, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date must be in YYYY-MM-DD format"})
		return
	}

	// Map transaction_type to DB fields.
	var txnType, buySell string
	var quantity, proceeds float64
	var taxCostBasis *float64

	switch req.TransactionType {
	case "buy":
		txnType = "Trade"
		buySell = "BUY"
		quantity = req.Quantity
		proceeds = -(req.Quantity * req.Price)
	case "sell":
		txnType = "Trade"
		buySell = "SELL"
		quantity = -req.Quantity
		proceeds = req.Quantity * req.Price
	case "espp_vest":
		if req.TaxCostBasis < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tax_cost_basis must be >= 0 for espp_vest"})
			return
		}
		txnType = "ESPP_VEST"
		buySell = "ESPP_VEST"
		quantity = req.Quantity
		proceeds = -(req.Quantity * req.Price)
		v := req.TaxCostBasis
		taxCostBasis = &v
	case "rsu_vest":
		txnType = "RSU_VEST"
		buySell = "RSU_VEST"
		quantity = req.Quantity
		proceeds = -(req.Quantity * req.Price)
		zero := 0.0
		taxCostBasis = &zero
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "transaction_type must be one of: buy, sell, espp_vest, rsu_vest"})
		return
	}

	var user models.User
	if err := h.Repo.DB.Where(&models.User{TokenHash: userHash}).FirstOrCreate(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "getting user: " + err.Error()})
		return
	}

	// Deduplication check (same float-match pattern as saveEtradeTransactions).
	if !req.Force {
		var existing models.Transaction
		err := h.Repo.DB.Where(
			"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ? AND price >= ? AND price <= ?",
			user.ID, txnType, req.Symbol, dt,
			quantity-1e-8, quantity+1e-8, req.Price-1e-8, req.Price+1e-8,
		).First(&existing).Error
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"status": "duplicate", "id": existing.PublicID})
			return
		}
	}

	txn := models.Transaction{
		UserID:          user.ID,
		Type:            txnType,
		Symbol:          req.Symbol,
		Currency:        req.Currency,
		ListingExchange: req.ListingExchange,
		DateTime:        dt,
		Quantity:        quantity,
		Price:           req.Price,
		Proceeds:        proceeds,
		Commission:      req.Commission,
		BuySell:         buySell,
		AssetCategory:   "STK",
		TaxCostBasis:    taxCostBasis,
		EntryMethod:     "manual",
	}
	if err := h.Repo.DB.Create(&txn).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "saving transaction: " + err.Error()})
		return
	}

	h.Repo.DB.Where("user_hash = ?", userHash).Delete(&models.LLMCache{})
	c.JSON(http.StatusCreated, gin.H{"status": "created", "id": txn.PublicID})
}

// DeleteTransaction handles DELETE /api/v1/portfolio/transactions/:id.
// The :id parameter is the UUID PublicID of the transaction.
// Only transactions belonging to the authenticated user can be deleted.
func (h *PortfolioHandler) DeleteTransaction(c *gin.Context) {
	publicID := c.Param("id")
	userHash := c.GetString(middleware.UserHashKey)

	var user models.User
	if err := h.Repo.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	result := h.Repo.DB.Where("public_id = ? AND user_id = ?", publicID, user.ID).
		Delete(&models.Transaction{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "transaction not found"})
		return
	}

	h.Repo.DB.Where("user_hash = ?", userHash).Delete(&models.LLMCache{})
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// editAssetRequest is the JSON body for PUT /api/v1/portfolio/assets/:symbol.
type editAssetRequest struct {
	Name            *string `json:"name"`
	AssetType       *string `json:"asset_type"`
	Country         *string `json:"country"`
	Sector          *string `json:"sector"`
	YahooSymbol     *string `json:"yahoo_symbol"`
	ListingExchange *string `json:"listing_exchange"`
}

// EditAsset handles PUT /api/v1/portfolio/assets/:symbol.
// Updates name, asset_type, country, sector in asset_fundamentals and/or
// yahoo_symbol / listing_exchange in transactions — all scoped to the authenticated user.
// Sets DataSource="User" when any background-managed field (name/asset_type/country/sector) is provided,
// preventing the periodic job from overwriting manual edits.
func (h *PortfolioHandler) EditAsset(c *gin.Context) {
	userHash := c.GetString(middleware.UserHashKey)
	symbol := c.Param("symbol")

	var req editAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	var user models.User
	if err := h.Repo.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	// Resolve the effective symbol: the background fundamentals service and GetValue both key
	// asset_fundamentals rows to YahooSymbol (when set), not the raw broker symbol.
	// Using the wrong key here would create a phantom row that GetValue never reads.
	var yahooSym string
	h.Repo.DB.Model(&models.Transaction{}).
		Where("user_id = ? AND symbol = ? AND yahoo_symbol != ''", user.ID, symbol).
		Limit(1).Pluck("yahoo_symbol", &yahooSym)
	fundSymbol := symbol
	if yahooSym != "" {
		fundSymbol = yahooSym
	}

	// ── Update asset_fundamentals (upsert per user) ──────────────────────────
	fundamentalFields := req.Name != nil || req.AssetType != nil || req.Country != nil || req.Sector != nil
	if fundamentalFields {
		updates := map[string]interface{}{
			"data_source":  "User",
			"last_updated": time.Now().UTC(),
		}
		if req.Name != nil {
			updates["name"] = *req.Name
		}
		if req.AssetType != nil {
			updates["asset_type"] = *req.AssetType
		}
		if req.Country != nil {
			updates["country"] = *req.Country
		}
		if req.Sector != nil {
			updates["sector"] = *req.Sector
		}

		// Try update first; if no row exists, create one.
		result := h.Repo.DB.Model(&models.AssetFundamental{}).
			Where("user_id = ? AND symbol = ?", user.ID, fundSymbol).
			Updates(updates)
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "updating fundamentals: " + result.Error.Error()})
			return
		}
		if result.RowsAffected == 0 {
			// No existing row — insert a new one.
			now := time.Now().UTC()
			newRec := models.AssetFundamental{
				UserID:      user.ID,
				Symbol:      fundSymbol,
				DataSource:  "User",
				LastUpdated: now,
			}
			if req.Name != nil {
				newRec.Name = *req.Name
			}
			if req.AssetType != nil {
				newRec.AssetType = *req.AssetType
			}
			if req.Country != nil {
				newRec.Country = *req.Country
			}
			if req.Sector != nil {
				newRec.Sector = *req.Sector
			}
			if err := h.Repo.DB.Create(&newRec).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "creating fundamentals: " + err.Error()})
				return
			}
		}
	}

	// ── Update yahoo_symbol in transactions ──────────────────────────────────
	if req.YahooSymbol != nil {
		exchange := c.Query("exchange")
		if err := h.Repo.UpdateSymbolMapping(userHash, symbol, exchange, *req.YahooSymbol); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "updating yahoo symbol: " + err.Error()})
			return
		}
	}

	// ── Update listing_exchange in transactions (only where currently empty) ─
	if req.ListingExchange != nil && *req.ListingExchange != "" {
		exchange := c.Query("exchange")
		base := h.Repo.DB.Model(&models.Transaction{}).Where("user_id = ? AND symbol = ?", user.ID, symbol)
		if exchange != "" {
			// Scope to the specific exchange group so an unrelated same-ticker entry on
			// a different exchange is not affected.
			base = base.Where("listing_exchange = ?", exchange)
		} else {
			base = base.Where("listing_exchange IS NULL OR listing_exchange = ''")
		}
		if err := base.Update("listing_exchange", *req.ListingExchange).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "updating listing exchange: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "asset updated"})
}
