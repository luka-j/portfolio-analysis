package flexquery

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"portfolio-analysis/models"
)

// Repository handles persisting and loading FlexQuery data in the database.
type Repository struct {
	DB     *gorm.DB
	parser *Parser
}

// NewRepository creates a new Repository backed by a GORM database.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{DB: db, parser: &Parser{}}
}

// splitRatioRe matches IB split descriptions like "SPLIT 10 FOR 1" or "1 FOR 25 REVERSE SPLIT".
var splitRatioRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s+FOR\s+(\d+(?:\.\d+)?)`)

// parseSplitRatio extracts the split multiplier from an IB CorporateAction description.
// For "SPLIT 4 FOR 1" it returns 4.0; for "1 FOR 25 REVERSE SPLIT" it returns 0.04.
func parseSplitRatio(desc string) (float64, bool) {
	m := splitRatioRe.FindStringSubmatch(desc)
	if m == nil {
		return 0, false
	}
	num, err1 := strconv.ParseFloat(m[1], 64)
	den, err2 := strconv.ParseFloat(m[2], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0, false
	}
	return num / den, true
}

// icSymbolRe matches IB IC descriptions like "FB(US...) CHANGE TO: META(US...)" → "META".
var icSymbolRe = regexp.MustCompile(`(?i)(?:CHANGE TO|RENAMED TO|SYMBOL CHANGE TO):\s*([A-Z0-9.\-]+)`)

// parseNewSymbol extracts the post-rename ticker from an IC CorporateAction description.
func parseNewSymbol(desc string) string {
	m := icSymbolRe.FindStringSubmatch(desc)
	if m == nil {
		return ""
	}
	// IB often appends the ISIN in parentheses: "META(US30303M1027)" — strip it.
	sym := m[1]
	if idx := strings.Index(sym, "("); idx != -1 {
		sym = sym[:idx]
	}
	return sym
}

// ParseAndSave reads an IB FlexQuery XML from r, parses it,
// and saves new transactions into the database, skipping duplicates.
// It returns the parsed data (for counts), an ImportResult containing per-trade and
// per-corporate-action summaries for the upload response, and any error.
func (r *Repository) ParseAndSave(reader io.Reader, userHash string) (*models.FlexQueryData, *models.ImportResult, error) {
	data, err := r.parser.Parse(reader)
	if err != nil {
		return nil, nil, err
	}

	// 1. Get or create the User
	var user models.User
	if err := r.DB.Where(&models.User{TokenHash: userHash}).FirstOrCreate(&user).Error; err != nil {
		return nil, nil, fmt.Errorf("getting user: %w", err)
	}

	result := &models.ImportResult{}

	// 2. Insert Trades (with deduplication via IB's TransactionID)
	for _, t := range data.Trades {
		// Filter out FX conversions (AssetCategory == "CASH" or XXX.YYY symbols)
		if models.IsFXTrade(t) {
			continue
		}

		// If IB provided a stable ID, use it for O(1) dedup.
		// Fallback to float-matching for reports that omit TransactionID.
		if t.TransactionID != "" {
			var existing models.Transaction
			err := r.DB.Where("user_id = ? AND transaction_id = ?", user.ID, t.TransactionID).
				First(&existing).Error
			if err == nil {
				backfillMissingTradeData(r.DB, &existing, t)
				log.Printf("Duplicate Trade skipped (id=%s): %s", t.TransactionID, t.Symbol)
				result.Transactions = append(result.Transactions, models.ImportedTransaction{
					ID:             existing.PublicID,
					Symbol:         t.Symbol,
					Date:           t.DateTime.Format("2006-01-02"),
					Side:           t.BuySell,
					Quantity:       t.Quantity,
					Price:          t.Price,
					Currency:       t.Currency,
					TotalCost:      math.Abs(t.Quantity * t.Price),
					IsDuplicate:    true,
					ConfidentDedup: true,
				})
				continue
			}
		} else {
			var existing models.Transaction
			err := r.DB.Where(
				"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ? AND price >= ? AND price <= ?",
				user.ID, "Trade", t.Symbol, t.DateTime, t.Quantity-1e-8, t.Quantity+1e-8, t.Price-1e-8, t.Price+1e-8,
			).First(&existing).Error
			if err == nil {
				backfillMissingTradeData(r.DB, &existing, t)
				log.Printf("Duplicate Trade skipped: %s %v qty=%v price=%v", t.Symbol, t.DateTime, t.Quantity, t.Price)
				result.Transactions = append(result.Transactions, models.ImportedTransaction{
					ID:             existing.PublicID,
					Symbol:         t.Symbol,
					Date:           t.DateTime.Format("2006-01-02"),
					Side:           t.BuySell,
					Quantity:       t.Quantity,
					Price:          t.Price,
					Currency:       t.Currency,
					TotalCost:      math.Abs(t.Quantity * t.Price),
					IsDuplicate:    true,
					ConfidentDedup: false,
				})
				continue
			}
		}

		txn := models.Transaction{
			UserID:          user.ID,
			Type:            "Trade",
			TransactionID:   t.TransactionID,
			Conid:           t.Conid,
			Symbol:          t.Symbol,
			ISIN:            t.ISIN,
			AssetCategory:   t.AssetCategory,
			Currency:        t.Currency,
			ListingExchange: t.ListingExchange,
			DateTime:        t.DateTime,
			Quantity:        t.Quantity,
			Price:           t.Price,
			Proceeds:        t.Proceeds,
			Commission:      t.Commission,
			BuySell:         t.BuySell,
			EntryMethod:     "flexquery",
		}
		if err := r.DB.Create(&txn).Error; err != nil {
			return nil, nil, fmt.Errorf("inserting trade: %w", err)
		}

		it := models.ImportedTransaction{
			ID:        txn.PublicID,
			Symbol:    t.Symbol,
			Date:      t.DateTime.Format("2006-01-02"),
			Side:      t.BuySell,
			Quantity:  t.Quantity,
			Price:     t.Price,
			Currency:  t.Currency,
			TotalCost: math.Abs(t.Quantity * t.Price),
		}

		// When a TransactionID-based dedup found nothing but a manual entry matches
		// on float-tolerance, both rows now coexist as a duplicate. Surface the manual
		// entry as a "suspected duplicate" so the user can resolve it in the UI.
		if t.TransactionID != "" {
			// Use a looser price tolerance (±0.02) than the strict dedup gate above.
			// Manual entries may differ from broker-reported prices by a few cents due
			// to rounding, and commission is intentionally excluded from this check.
			// Date is matched at day granularity (not exact timestamp) because manual
			// entries carry only a date, while FlexQuery timestamps include a time-of-day.
			const suspectedPriceTol = 0.02
			dayStart := time.Date(t.DateTime.Year(), t.DateTime.Month(), t.DateTime.Day(), 0, 0, 0, 0, t.DateTime.Location())
			dayEnd := dayStart.Add(24 * time.Hour)
			var manual models.Transaction
			err2 := r.DB.Where(
				"user_id = ? AND type = ? AND symbol = ? AND date_time >= ? AND date_time < ? "+
					"AND quantity >= ? AND quantity <= ? AND price >= ? AND price <= ? "+
					"AND entry_method = 'manual'",
				user.ID, "Trade", t.Symbol, dayStart, dayEnd,
				t.Quantity-1e-8, t.Quantity+1e-8, t.Price-suspectedPriceTol, t.Price+suspectedPriceTol,
			).First(&manual).Error
			if err2 == nil {
				it.SuspectedDuplicateID = &manual.PublicID
			}
		}

		result.Transactions = append(result.Transactions, it)
	}

	// 3. Insert Cash Transactions (with deduplication via IB's TransactionID)
	for _, ct := range data.CashTransactions {
		if ct.TransactionID != "" {
			var count int64
			r.DB.Model(&models.Transaction{}).Where(
				"user_id = ? AND transaction_id = ?",
				user.ID, ct.TransactionID,
			).Count(&count)
			if count > 0 {
				log.Printf("Duplicate CashTxn skipped (id=%s)", ct.TransactionID)
				continue
			}
		} else {
			var count int64
			r.DB.Model(&models.Transaction{}).Where(
				"user_id = ? AND type = ? AND currency = ? AND date_time = ? AND amount = ? AND symbol = ?",
				user.ID, ct.Type, ct.Currency, ct.DateTime, ct.Amount, ct.Symbol,
			).Count(&count)
			if count > 0 {
				log.Printf("Duplicate CashTxn skipped: %s %v amount=%v", ct.Type, ct.DateTime, ct.Amount)
				continue
			}
		}

		txn := models.Transaction{
			UserID:        user.ID,
			Type:          ct.Type,
			TransactionID: ct.TransactionID,
			Symbol:        ct.Symbol,
			Currency:      ct.Currency,
			DateTime:      ct.DateTime,
			Amount:        ct.Amount,
			Description:   ct.Description,
			EntryMethod:   "flexquery",
		}
		if err := r.DB.Create(&txn).Error; err != nil {
			return nil, nil, fmt.Errorf("inserting cash txn: %w", err)
		}
	}

	// 4. Apply corporate actions (IC, FS, RS, SD, CD) with idempotency.
	result.CorporateActions = r.applyCorporateActions(user.ID, data.ParsedCorporateActions)

	data.UserHash = userHash
	return data, result, nil
}

// applyCorporateActions processes each parsed corporate action for a user, applying DB mutations
// and recording the action for idempotency. Already-applied actions are returned with IsNew=false.
func (r *Repository) applyCorporateActions(userID uint, actions []models.ParsedCorporateAction) []models.ImportedCorporateAction {
	// Process in chronological order so renames precede splits on the same security.
	sorted := make([]models.ParsedCorporateAction, len(actions))
	copy(sorted, actions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].DateTime.Before(sorted[j].DateTime)
	})

	var results []models.ImportedCorporateAction
	for _, a := range sorted {
		// Idempotency check: skip if this action was already applied.
		var existing models.CorporateActionRecord
		if err := r.DB.Where("user_id = ? AND action_id = ?", userID, a.ActionID).First(&existing).Error; err == nil {
			results = append(results, models.ImportedCorporateAction{
				ActionID:    a.ActionID,
				Type:        a.Type,
				Symbol:      a.Symbol,
				NewSymbol:   existing.NewSymbol,
				Date:        a.DateTime.Format("2006-01-02"),
				Description: a.Description,
				SplitRatio:  existing.SplitRatio,
				Quantity:    a.Quantity,
				Amount:      a.Amount,
				Currency:    a.Currency,
				IsNew:       false,
			})
			continue
		}

		rec := models.CorporateActionRecord{
			UserID:      userID,
			ActionID:    a.ActionID,
			Type:        a.Type,
			Symbol:      a.Symbol,
			Conid:       a.Conid,
			Currency:    a.Currency,
			Quantity:    a.Quantity,
			Amount:      a.Amount,
			DateTime:    a.DateTime,
			Description: a.Description,
		}

		imported := models.ImportedCorporateAction{
			ActionID:    a.ActionID,
			Type:        a.Type,
			Symbol:      a.Symbol,
			Date:        a.DateTime.Format("2006-01-02"),
			Description: a.Description,
			Quantity:    a.Quantity,
			Amount:      a.Amount,
			Currency:    a.Currency,
			IsNew:       true,
		}

		switch a.Type {
		case "IC":
			newSym := parseNewSymbol(a.Description)
			if newSym != "" {
				// Only rename transactions that have no conid — conid-bearing rows are
				// already normalised dynamically by LoadSaved.
				r.DB.Model(&models.Transaction{}).
					Where("user_id = ? AND symbol = ? AND conid = '' AND date_time <= ?", userID, a.Symbol, a.DateTime).
					Update("symbol", newSym)
				rec.NewSymbol = newSym
				imported.NewSymbol = newSym
			} else {
				log.Printf("corporate action IC: could not parse new symbol from description %q", a.Description)
			}

		case "FS", "RS":
			ratio, ok := parseSplitRatio(a.Description)
			if !ok {
				log.Printf("corporate action %s: could not parse split ratio from description %q", a.Type, a.Description)
			} else {
				// Update quantity and price for all transactions of this security dated before the split.
				// Match by conid when available; fall back to symbol for non-IB transactions.
				query := r.DB.Model(&models.Transaction{}).
					Where("user_id = ? AND date_time < ?", userID, a.DateTime)
				if a.Conid != "" {
					query = query.Where("conid = ? OR (conid = '' AND symbol = ?)", a.Conid, a.Symbol)
				} else {
					query = query.Where("symbol = ?", a.Symbol)
				}
				query.Updates(map[string]interface{}{
					"quantity": gorm.Expr("quantity * ?", ratio),
					"price":    gorm.Expr("price / ?", ratio),
				})
				rec.SplitRatio = ratio
				imported.SplitRatio = ratio
			}

		case "SD":
			// Insert a stock dividend transaction, deduped by actionID as transaction_id.
			var count int64
			r.DB.Model(&models.Transaction{}).Where("user_id = ? AND transaction_id = ?", userID, a.ActionID).Count(&count)
			if count == 0 {
				txn := models.Transaction{
					UserID:        userID,
					Type:          "Trade",
					BuySell:       "STOCK_DIVIDEND",
					TransactionID: a.ActionID,
					Symbol:        a.Symbol,
					Conid:         a.Conid,
					Currency:      a.Currency,
					DateTime:      a.DateTime,
					Quantity:      a.Quantity,
					Price:         0,
					Proceeds:      0,
					Commission:    0,
					Description:   a.Description,
					EntryMethod:   "flexquery",
				}
				if err := r.DB.Create(&txn).Error; err != nil {
					log.Printf("corporate action SD: failed to insert transaction: %v", err)
				}
			}

		case "CD":
			// Store in the separate cash_dividends table.
			var count int64
			r.DB.Model(&models.CashDividendRecord{}).Where("user_id = ? AND action_id = ?", userID, a.ActionID).Count(&count)
			if count == 0 {
				cd := models.CashDividendRecord{
					UserID:      userID,
					ActionID:    a.ActionID,
					Symbol:      a.Symbol,
					Currency:    a.Currency,
					Amount:      a.Amount,
					DateTime:    a.DateTime,
					Description: a.Description,
				}
				if err := r.DB.Create(&cd).Error; err != nil {
					log.Printf("corporate action CD: failed to insert cash dividend: %v", err)
				}
			}
		}

		if err := r.DB.Create(&rec).Error; err != nil {
			log.Printf("corporate action %s: failed to record: %v", a.Type, err)
		}

		results = append(results, imported)
	}
	return results
}

// backfillMissingTradeData updates an existing duplicate transaction's metadata
// when the incoming trade carries data that the stored row is missing.
// This lets a re-upload of a FlexQuery retroactively enrich old rows that were
// imported before certain fields (like conid, isin, listing_exchange) were supported.
func backfillMissingTradeData(db *gorm.DB, existing *models.Transaction, incoming models.Trade) {
	updates := map[string]interface{}{}
	if incoming.Conid != "" && existing.Conid != incoming.Conid {
		updates["conid"] = incoming.Conid
		if existing.Conid == "" {
			// First time we see a conid for this trade — also refresh the symbol
			// in case IBKR renamed the ticker since the original import.
			updates["symbol"] = incoming.Symbol
		}
	}
	if incoming.ISIN != "" && existing.ISIN == "" {
		updates["isin"] = incoming.ISIN
	}
	if incoming.ListingExchange != "" && existing.ListingExchange == "" {
		updates["listing_exchange"] = incoming.ListingExchange
	}
	if len(updates) > 0 {
		db.Model(existing).Updates(updates)
	}
}

// LoadSaved loads all historical transactions from the DB for a user.
// Trades are symbol-normalised: all transactions sharing the same conid use
// the symbol from that conid's most-recent trade.  eTrade transactions (no
// conid) are enriched with the canonical symbol when the symbol maps to
// exactly one conid; ambiguous cases are left unchanged.
func (r *Repository) LoadSaved(userHash string) (*models.FlexQueryData, error) {
	var user models.User
	if err := r.DB.Where(models.User{TokenHash: userHash}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// User has never uploaded data — return an empty portfolio instead of an error.
			return &models.FlexQueryData{UserHash: userHash}, nil
		}
		return nil, fmt.Errorf("fetching user: %w", err)
	}

	var dbTxns []models.Transaction
	if err := r.DB.Where("user_id = ?", user.ID).Find(&dbTxns).Error; err != nil {
		return nil, fmt.Errorf("fetching transactions: %w", err)
	}

	// --- Symbol normalisation via conid ---
	//
	// Step 1: for each conid, find the symbol of the most-recent trade.
	conidLatest := make(map[string]time.Time)
	conidSymbol := make(map[string]string) // conid → canonical symbol
	for _, txn := range dbTxns {
		if txn.Conid == "" {
			continue
		}
		if txn.DateTime.After(conidLatest[txn.Conid]) {
			conidLatest[txn.Conid] = txn.DateTime
			conidSymbol[txn.Conid] = txn.Symbol
		}
	}

	// Step 2: build a reverse map symbol(uppercase) → conid for eTrade enrichment.
	// Only keep entries where exactly one conid owns a given symbol (ambiguous → skip).
	symbolToConid := make(map[string]string) // uppercased symbol → conid
	symbolConidCount := make(map[string]int) // number of distinct conids per symbol
	for _, txn := range dbTxns {
		if txn.Conid == "" {
			continue
		}
		key := strings.ToUpper(txn.Symbol)
		if existing, seen := symbolToConid[key]; !seen {
			symbolToConid[key] = txn.Conid
			symbolConidCount[key] = 1
		} else if existing != txn.Conid {
			symbolConidCount[key]++ // mark ambiguous
		}
	}

	// getCanonical returns the canonical symbol for a transaction.
	getCanonical := func(txn models.Transaction) string {
		if txn.Conid != "" {
			if s, ok := conidSymbol[txn.Conid]; ok {
				return s
			}
		} else {
			// eTrade / no-conid: enrich only when unambiguous
			key := strings.ToUpper(txn.Symbol)
			if symbolConidCount[key] == 1 {
				if conid, ok := symbolToConid[key]; ok {
					if s, ok := conidSymbol[conid]; ok {
						return s
					}
				}
			}
		}
		return txn.Symbol
	}
	// --- end normalisation setup ---

	data := &models.FlexQueryData{}

	exchangeMap := make(map[string]string) // canonical symbol → first-seen exchange
	for _, txn := range dbTxns {
		if (txn.Type == "Trade" || txn.Type == "ESPP_VEST" || txn.Type == "RSU_VEST") && txn.ListingExchange != "" {
			sym := getCanonical(txn)
			if _, exists := exchangeMap[sym]; !exists {
				exchangeMap[sym] = txn.ListingExchange
			}
		}
	}

	for _, txn := range dbTxns {
		if txn.Type == "Trade" || txn.Type == "ESPP_VEST" || txn.Type == "RSU_VEST" {
			sym := getCanonical(txn)

			listingExchange := txn.ListingExchange
			if listingExchange == "" {
				if ex, ok := exchangeMap[sym]; ok {
					listingExchange = ex
				}
			}

			buySell := txn.BuySell
			if txn.Type == "ESPP_VEST" || txn.Type == "RSU_VEST" {
				buySell = txn.Type
			}

			data.Trades = append(data.Trades, models.Trade{
				TransactionID:   txn.TransactionID,
				Conid:           txn.Conid,
				Symbol:          sym,
				AssetCategory:   txn.AssetCategory,
				Currency:        txn.Currency,
				ListingExchange: listingExchange,
				DateTime:        txn.DateTime,
				Quantity:        txn.Quantity,
				Price:           txn.Price,
				Proceeds:        txn.Proceeds,
				Commission:      txn.Commission,
				BuySell:         buySell,
				TaxCostBasis:    txn.TaxCostBasis,
				YahooSymbol:     txn.YahooSymbol,
				PublicID:        txn.PublicID,
				EntryMethod:     txn.EntryMethod,
			})
		} else {
			data.CashTransactions = append(data.CashTransactions, models.CashTransaction{
				TransactionID: txn.TransactionID,
				Type:          txn.Type,
				Currency:      txn.Currency,
				Amount:        txn.Amount,
				DateTime:      txn.DateTime,
				Description:   txn.Description,
				Symbol:        txn.Symbol,
			})
		}
	}

	// OpenPositions array is intentionally left empty. The portfolio service
	// will automatically fallback to reconstructing holdings and cost bases from the trades dataset!

	// Load cash dividends for use in the pending-cash bucket calculation.
	var cdRows []models.CashDividendRecord
	if err := r.DB.Where("user_id = ?", user.ID).Find(&cdRows).Error; err == nil {
		for _, cd := range cdRows {
			data.CashDividends = append(data.CashDividends, models.CashDividend{
				ActionID:    cd.ActionID,
				Symbol:      cd.Symbol,
				Currency:    cd.Currency,
				Amount:      cd.Amount,
				DateTime:    cd.DateTime,
				Description: cd.Description,
			})
		}
	}

	data.UserHash = userHash
	return data, nil
}

// UpdateSymbolMapping updates the YahooSymbol for all trades of a given symbol and exchange for a user.
// When the symbol belongs to transactions that have a conid, all transactions sharing those conids
// are updated (covering historical rows where the symbol may differ due to a ticker rename).
// If the effective Yahoo ticker changes, all cached market data fetched under the old ticker is purged
// so the next request fetches fresh data under the correct symbol.
func (r *Repository) UpdateSymbolMapping(userHash, symbol, exchange, yahooSymbol string) error {
	var user models.User
	if err := r.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		return fmt.Errorf("user not found")
	}

	// Capture the currently-used effective symbol before the update so we can purge
	// cached data that was fetched under the wrong ticker.
	var oldYahooSymbol string
	oldSymQuery := r.DB.Model(&models.Transaction{}).
		Where("user_id = ? AND symbol = ? AND yahoo_symbol != ''", user.ID, symbol)
	if exchange != "" {
		oldSymQuery = oldSymQuery.Where("listing_exchange = ?", exchange)
	}
	oldSymQuery.Limit(1).Pluck("yahoo_symbol", &oldYahooSymbol)

	oldEffective := oldYahooSymbol
	if oldEffective == "" {
		oldEffective = symbol
	}
	newEffective := yahooSymbol
	if newEffective == "" {
		newEffective = symbol
	}

	// Find all conids associated with the given (canonical) symbol and exchange for this user.
	// Scoping by exchange prevents a mapping intended for one exchange from matching
	// conids of the same ticker on a different exchange.
	conidsQuery := r.DB.Model(&models.Transaction{}).
		Where("user_id = ? AND symbol = ? AND conid != ''", user.ID, symbol)
	if exchange != "" {
		conidsQuery = conidsQuery.Where("listing_exchange = ?", exchange)
	}
	var conids []string
	conidsQuery.Distinct().Pluck("conid", &conids)

	query := r.DB.Model(&models.Transaction{}).
		Where("user_id = ? AND type IN ?", user.ID, []string{"Trade", "ESPP_VEST", "RSU_VEST"})

	if len(conids) > 0 {
		// Update every transaction that shares a conid with the canonical symbol
		// (covers old rows with a previous ticker name), plus any zero-conid rows
		// for this symbol restricted to the given exchange.
		if exchange != "" {
			query = query.Where(
				"conid IN ? OR (conid = '' AND symbol = ? AND (listing_exchange = ? OR listing_exchange = ''))",
				conids, symbol, exchange,
			)
		} else {
			query = query.Where(
				"conid IN ? OR (conid = '' AND symbol = ? AND listing_exchange = '')",
				conids, symbol,
			)
		}
	} else {
		// No conid available — original exchange-scoped update.
		if exchange != "" {
			query = query.Where("symbol = ? AND (listing_exchange = ? OR listing_exchange = '')", symbol, exchange)
		} else {
			query = query.Where("symbol = ? AND listing_exchange = ''", symbol)
		}
	}

	if err := query.Update("yahoo_symbol", yahooSymbol).Error; err != nil {
		return err
	}

	// Purge all cached data fetched under the old effective ticker so the next
	// request fetches fresh, correct data under the new Yahoo symbol.
	if oldEffective != newEffective {
		r.DB.Where("symbol = ?", oldEffective).Delete(&models.MarketData{})
		r.DB.Where("symbol = ?", oldEffective).Delete(&models.CurrentPrice{})
		r.DB.Where("user_id = ? AND symbol = ?", user.ID, oldEffective).Delete(&models.AssetFundamental{})
		r.DB.Where("fund_symbol = ?", oldEffective).Delete(&models.EtfBreakdown{})
	}

	return nil
}
