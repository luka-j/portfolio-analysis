package flexquery

import (
	"errors"
	"fmt"
	"io"
	"log"
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

// ParseAndSave reads an IB FlexQuery XML from r, parses it,
// and saves new transactions into the database, skipping duplicates.
func (r *Repository) ParseAndSave(reader io.Reader, userHash string) (*models.FlexQueryData, error) {
	data, err := r.parser.Parse(reader)
	if err != nil {
		return nil, err
	}

	// 1. Get or create the User
	var user models.User
	if err := r.DB.Where(&models.User{TokenHash: userHash}).FirstOrCreate(&user).Error; err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}

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
				backfillConid(r.DB, &existing, t)
				log.Printf("Duplicate Trade skipped (id=%s): %s", t.TransactionID, t.Symbol)
				continue
			}
		} else {
			var existing models.Transaction
			err := r.DB.Where(
				"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ? AND price >= ? AND price <= ?",
				user.ID, "Trade", t.Symbol, t.DateTime, t.Quantity-1e-8, t.Quantity+1e-8, t.Price-1e-8, t.Price+1e-8,
			).First(&existing).Error
			if err == nil {
				backfillConid(r.DB, &existing, t)
				log.Printf("Duplicate Trade skipped: %s %v qty=%v price=%v", t.Symbol, t.DateTime, t.Quantity, t.Price)
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
			return nil, fmt.Errorf("inserting trade: %w", err)
		}
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
			return nil, fmt.Errorf("inserting cash txn: %w", err)
		}
	}

	// For the response back to caller, we just return what was parsed
	// (this allows the upload endpoint to say "uploaded N items".
	// We could also return the full historical dataset, but currently
	// the handler just counts what was in the file).
	data.UserHash = userHash
	return data, nil
}

// backfillConid updates an existing duplicate transaction's conid (and symbol)
// when the incoming trade carries a conid that the stored row is missing.
// Also backfills ISIN when the stored row has none.
// This lets a re-upload of a FlexQuery retroactively enrich old rows that were
// imported before conid/ISIN support was added.
func backfillConid(db *gorm.DB, existing *models.Transaction, incoming models.Trade) {
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
		r.DB.Where("symbol = ?", oldEffective).Delete(&models.AssetFundamental{})
		r.DB.Where("fund_symbol = ?", oldEffective).Delete(&models.EtfBreakdown{})
	}

	return nil
}
