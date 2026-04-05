package flexquery

import (
	"errors"
	"fmt"
	"io"
	"log"

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
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading upload: %w", err)
	}

	data, err := r.parser.ParseBytes(raw)
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
		// Fallback to the old float-matching query only for legacy rows without an ID.
		if t.TransactionID != "" {
			var existing models.Transaction
			err := r.DB.Where("user_id = ? AND transaction_id = ?", user.ID, t.TransactionID).
				First(&existing).Error
			if err == nil {
				// Row already exists. Patch asset_category in case it was stored before
				// subCategory resolution was added (old rows have "STK" instead of "ETF").
				if existing.AssetCategory != t.AssetCategory {
					r.DB.Model(&existing).Update("asset_category", t.AssetCategory)
				}
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
				if existing.AssetCategory != t.AssetCategory {
					r.DB.Model(&existing).Update("asset_category", t.AssetCategory)
				}
				log.Printf("Duplicate Trade skipped: %s %v qty=%v price=%v", t.Symbol, t.DateTime, t.Quantity, t.Price)
				continue
			}
		}

		txn := models.Transaction{
			UserID:          user.ID,
			Type:            "Trade",
			TransactionID:   t.TransactionID,
			Symbol:          t.Symbol,
			AssetCategory:   t.AssetCategory,
			Currency:        t.Currency,
			ListingExchange: t.ListingExchange,
			DateTime:        t.DateTime,
			Quantity:        t.Quantity,
			Price:           t.Price,
			Proceeds:        t.Proceeds,
			Commission:      t.Commission,
			BuySell:         t.BuySell,
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
		}
		if err := r.DB.Create(&txn).Error; err != nil {
			return nil, fmt.Errorf("inserting cash txn: %w", err)
		}
	}

	// For the response back to caller, we just return what was parsed
	// (this allows the upload endpoint to say "uploaded N items".
	// We could also return the full historical dataset, but currently
	// the handler just counts what was in the file).
	return data, nil
}

// LoadSaved loads all historical transactions from the DB for a user.
func (r *Repository) LoadSaved(userHash string) (*models.FlexQueryData, error) {
	var user models.User
	if err := r.DB.Where(models.User{TokenHash: userHash}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// User has never uploaded data — return an empty portfolio instead of an error.
			return &models.FlexQueryData{}, nil
		}
		return nil, fmt.Errorf("fetching user: %w", err)
	}

	var dbTxns []models.Transaction
	if err := r.DB.Where("user_id = ?", user.ID).Find(&dbTxns).Error; err != nil {
		return nil, fmt.Errorf("fetching transactions: %w", err)
	}

	data := &models.FlexQueryData{}

	exchangeMap := make(map[string]string)
	for _, txn := range dbTxns {
		if (txn.Type == "Trade" || txn.Type == "ESPP_VEST" || txn.Type == "RSU_VEST") && txn.ListingExchange != "" {
			if _, exists := exchangeMap[txn.Symbol]; !exists {
				exchangeMap[txn.Symbol] = txn.ListingExchange
			}
		}
	}

	for _, txn := range dbTxns {
		if txn.Type == "Trade" || txn.Type == "ESPP_VEST" || txn.Type == "RSU_VEST" {
			listingExchange := txn.ListingExchange
			if listingExchange == "" {
				if ex, ok := exchangeMap[txn.Symbol]; ok {
					listingExchange = ex
				}
			}

			buySell := txn.BuySell
			if txn.Type == "ESPP_VEST" || txn.Type == "RSU_VEST" {
				buySell = txn.Type
			}

			data.Trades = append(data.Trades, models.Trade{
				TransactionID:   txn.TransactionID,
				Symbol:          txn.Symbol,
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

	return data, nil
}

// UpdateSymbolMapping updates the YahooSymbol for all trades of a given symbol and exchange for a user.
func (r *Repository) UpdateSymbolMapping(userHash, symbol, exchange, yahooSymbol string) error {
	var user models.User
	if err := r.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		return fmt.Errorf("user not found")
	}

	query := r.DB.Model(&models.Transaction{}).
		Where("user_id = ? AND symbol = ? AND type IN ?", user.ID, symbol, []string{"Trade", "ESPP_VEST", "RSU_VEST"})

	if exchange != "" {
		query = query.Where("(listing_exchange = ? OR listing_exchange = '')", exchange)
	} else {
		query = query.Where("listing_exchange = ''")
	}

	return query.Update("yahoo_symbol", yahooSymbol).Error
}
