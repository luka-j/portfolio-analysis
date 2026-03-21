package flexquery

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"gofolio-analysis/models"
)

// ---------- XML schema for IB FlexQuery reports ----------

type flexQueryResponse struct {
	XMLName        xml.Name       `xml:"FlexQueryResponse"`
	FlexStatements flexStatements `xml:"FlexStatements"`
}

type flexStatements struct {
	FlexStatement flexStatement `xml:"FlexStatement"`
}

type flexStatement struct {
	AccountID        string           `xml:"accountId,attr"`
	Trades           xmlTrades        `xml:"Trades"`
	Transfers        xmlTransfers     `xml:"Transfers"`
	OpenPositions    xmlOpenPositions `xml:"OpenPositions"`
	CashTransactions xmlCashTxns      `xml:"CashTransactions"`
}

type xmlTrades struct {
	Items []xmlTrade `xml:"Trade"`
}

type xmlTrade struct {
	Symbol          string `xml:"symbol,attr"`
	AssetCategory   string `xml:"assetCategory,attr"`
	Currency        string `xml:"currency,attr"`
	ListingExchange string `xml:"listingExchange,attr"`
	DateTime        string `xml:"dateTime,attr"`
	TradeDate       string `xml:"tradeDate,attr"`
	Quantity        string `xml:"quantity,attr"`
	TradePrice      string `xml:"tradePrice,attr"`
	Proceeds        string `xml:"proceeds,attr"`
	IBCommission    string `xml:"ibCommission,attr"`
	BuySell         string `xml:"buySell,attr"`
	TradeID         string `xml:"tradeID,attr"` // IB unique identifier
}

type xmlTransfers struct {
	Items []xmlTransfer `xml:"Transfer"`
}

type xmlTransfer struct {
	Symbol          string `xml:"symbol,attr"`
	AssetCategory   string `xml:"assetCategory,attr"`
	Currency        string `xml:"currency,attr"`
	ListingExchange string `xml:"listingExchange,attr"`
	DateTime        string `xml:"dateTime,attr"`
	Date            string `xml:"date,attr"`
	Quantity        string `xml:"quantity,attr"`
	PositionAmount  string `xml:"positionAmount,attr"`
	Direction       string `xml:"direction,attr"`
}

type xmlOpenPositions struct {
	Items []xmlOpenPosition `xml:"OpenPosition"`
}

type xmlOpenPosition struct {
	Symbol        string `xml:"symbol,attr"`
	AssetCategory string `xml:"assetCategory,attr"`
	Currency      string `xml:"currency,attr"`
	Quantity      string `xml:"quantity,attr"`
	MarkPrice     string `xml:"markPrice,attr"`
	PositionValue     string `xml:"positionValue,attr"`
	CostBasisPerShare string `xml:"costBasisPrice,attr"`
}

type xmlCashTxns struct {
	Items []xmlCashTxn `xml:"CashTransaction"`
}

type xmlCashTxn struct {
	Type          string `xml:"type,attr"`
	Currency      string `xml:"currency,attr"`
	Amount        string `xml:"amount,attr"`
	DateTime      string `xml:"dateTime,attr"`
	ReportDate    string `xml:"reportDate,attr"`
	Description   string `xml:"description,attr"`
	Symbol        string `xml:"symbol,attr"`
	TransactionID string `xml:"transactionID,attr"` // IB unique identifier
}

// Parser handles parsing and persisting FlexQuery XML files into the database.
type Parser struct {
	DB *gorm.DB
}

// NewParser creates a new FlexQuery parser backed by a GORM database.
func NewParser(db *gorm.DB) *Parser {
	return &Parser{DB: db}
}

// ParseAndSave reads an IB FlexQuery XML from r, parses it,
// and saves new transactions into the database, skipping duplicates.
func (p *Parser) ParseAndSave(r io.Reader, userHash string) (*models.FlexQueryData, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading upload: %w", err)
	}

	data, err := p.ParseBytes(raw)
	if err != nil {
		return nil, err
	}

	// 1. Get or create the User
	var user models.User
	if err := p.DB.Where(&models.User{TokenHash: userHash}).FirstOrCreate(&user).Error; err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}

	// 2. Insert Trades (with deduplication via IB's TransactionID)
	for _, t := range data.Trades {
		// Filter out FX conversions (AssetCategory == "CASH" or XXX.YYY symbols)
		isFXTrade := t.AssetCategory == "CASH" || (len(t.Symbol) == 7 && t.Symbol[3] == '.')
		if isFXTrade {
			continue
		}

		// If IB provided a stable ID, use it for O(1) dedup.
		// Fallback to the old float-matching query only for legacy rows without an ID.
		if t.TransactionID != "" {
			var count int64
			p.DB.Model(&models.Transaction{}).Where(
				"user_id = ? AND transaction_id = ?",
				user.ID, t.TransactionID,
			).Count(&count)
			if count > 0 {
				log.Printf("Duplicate Trade skipped (id=%s): %s", t.TransactionID, t.Symbol)
				continue
			}
		} else {
			var count int64
			p.DB.Model(&models.Transaction{}).Where(
				"user_id = ? AND type = ? AND symbol = ? AND date_time = ? AND quantity >= ? AND quantity <= ? AND price >= ? AND price <= ?",
				user.ID, "Trade", t.Symbol, t.DateTime, t.Quantity-1e-8, t.Quantity+1e-8, t.Price-1e-8, t.Price+1e-8,
			).Count(&count)
			if count > 0 {
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
			ListingExchange:  t.ListingExchange,
			DateTime:        t.DateTime,
			Quantity:        t.Quantity,
			Price:           t.Price,
			Proceeds:        t.Proceeds,
			Commission:      t.Commission,
			BuySell:         t.BuySell,
		}
		if err := p.DB.Create(&txn).Error; err != nil {
			return nil, fmt.Errorf("inserting trade: %w", err)
		}
	}

	// 3. Insert Cash Transactions (with deduplication via IB's TransactionID)
	for _, ct := range data.CashTransactions {
		if ct.TransactionID != "" {
			var count int64
			p.DB.Model(&models.Transaction{}).Where(
				"user_id = ? AND transaction_id = ?",
				user.ID, ct.TransactionID,
			).Count(&count)
			if count > 0 {
				log.Printf("Duplicate CashTxn skipped (id=%s)", ct.TransactionID)
				continue
			}
		} else {
			var count int64
			p.DB.Model(&models.Transaction{}).Where(
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
		if err := p.DB.Create(&txn).Error; err != nil {
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
func (p *Parser) LoadSaved(userHash string) (*models.FlexQueryData, error) {
	var user models.User
	if err := p.DB.Where(models.User{TokenHash: userHash}).FirstOrCreate(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to get or create user: %w", err)
	}

	var dbTxns []models.Transaction
	if err := p.DB.Where("user_id = ?", user.ID).Find(&dbTxns).Error; err != nil {
		return nil, fmt.Errorf("fetching transactions: %w", err)
	}

	data := &models.FlexQueryData{}

	for _, txn := range dbTxns {
		if txn.Type == "Trade" {
			data.Trades = append(data.Trades, models.Trade{
				TransactionID:   txn.TransactionID,
				Symbol:          txn.Symbol,
				AssetCategory:   txn.AssetCategory,
				Currency:        txn.Currency,
				ListingExchange: txn.ListingExchange,
				DateTime:        txn.DateTime,
				Quantity:        txn.Quantity,
				Price:           txn.Price,
				Proceeds:        txn.Proceeds,
				Commission:      txn.Commission,
				BuySell:         txn.BuySell,
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
func (p *Parser) UpdateSymbolMapping(userHash, symbol, exchange, yahooSymbol string) error {
	var user models.User
	if err := p.DB.Where("token_hash = ?", userHash).First(&user).Error; err != nil {
		return fmt.Errorf("user not found")
	}

	return p.DB.Model(&models.Transaction{}).
		Where("user_id = ? AND symbol = ? AND listing_exchange = ? AND type = ?", user.ID, symbol, exchange, "Trade").
		Update("yahoo_symbol", yahooSymbol).Error
}

// ParseBytes parses raw XML bytes into FlexQueryData.
func (p *Parser) ParseBytes(raw []byte) (*models.FlexQueryData, error) {
	var resp flexQueryResponse
	if err := xml.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing XML: %w", err)
	}

	stmt := resp.FlexStatements.FlexStatement
	data := &models.FlexQueryData{
		AccountID: stmt.AccountID,
	}

	// Parse trades.
	for _, t := range stmt.Trades.Items {
		dt, err := parseIBDateTime(t.DateTime, t.TradeDate)
		if err != nil {
			return nil, fmt.Errorf("parsing trade date for %s: %w", t.Symbol, err)
		}
		data.Trades = append(data.Trades, models.Trade{
			TransactionID:   t.TradeID,
			Symbol:          t.Symbol,
			AssetCategory:   t.AssetCategory,
			Currency:        t.Currency,
			ListingExchange: t.ListingExchange,
			DateTime:        dt,
			Quantity:        parseFloat(t.Quantity),
			Price:           parseFloat(t.TradePrice),
			Proceeds:        parseFloat(t.Proceeds),
			Commission:      parseFloat(t.IBCommission),
			BuySell:         t.BuySell,
		})
	}

	// Parse transfers.
	for _, tr := range stmt.Transfers.Items {
		dt, err := parseIBDateTime(tr.DateTime, tr.Date)
		if err != nil {
			return nil, fmt.Errorf("parsing transfer date for %s: %w", tr.Symbol, err)
		}
		
		qty := parseFloat(tr.Quantity)
		posAmt := parseFloat(tr.PositionAmount)
		
		proceeds := -posAmt
		buySell := "TRANSFER_IN"
		if tr.Direction == "OUT" {
			qty = -qty
			proceeds = posAmt
			buySell = "TRANSFER_OUT"
		}
		
		price := 0.0
		if qty != 0 {
			if qty > 0 {
				price = posAmt / qty
			} else {
				price = posAmt / -qty
			}
		}

		data.Trades = append(data.Trades, models.Trade{
			Symbol:          tr.Symbol,
			AssetCategory:   tr.AssetCategory,
			Currency:        tr.Currency,
			ListingExchange: tr.ListingExchange,
			DateTime:        dt,
			Quantity:        qty,
			Price:           price,
			Proceeds:        proceeds,
			Commission:      0,
			BuySell:         buySell,
		})
	}

	// Parse open positions.
	for _, op := range stmt.OpenPositions.Items {
		data.OpenPositions = append(data.OpenPositions, models.OpenPosition{
			Symbol:        op.Symbol,
			AssetCategory: op.AssetCategory,
			Currency:      op.Currency,
			Quantity:      parseFloat(op.Quantity),
			MarkPrice:         parseFloat(op.MarkPrice),
			PositionValue:     parseFloat(op.PositionValue),
			CostBasisPerShare: parseFloat(op.CostBasisPerShare),
		})
	}

	// Parse cash transactions.
	for _, ct := range stmt.CashTransactions.Items {
		dt, err := parseIBDateTime(ct.DateTime, ct.ReportDate)
		if err != nil {
			return nil, fmt.Errorf("parsing cash txn date: %w", err)
		}
		data.CashTransactions = append(data.CashTransactions, models.CashTransaction{
			TransactionID: ct.TransactionID,
			Type:          ct.Type,
			Currency:      ct.Currency,
			Amount:        parseFloat(ct.Amount),
			DateTime:      dt,
			Description:   ct.Description,
			Symbol:        ct.Symbol,
		})
	}

	return data, nil
}

func parseIBDateTime(dateTime, fallback string) (time.Time, error) {
	s := dateTime
	if s == "" {
		s = fallback
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}

	// IB uses several date formats.
	formats := []string{
		"2006-01-02;15:04:05",
		"2006-01-02 15:04:05",
		"20060102;150405",
		"20060102",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised IB date format: %q", s)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Printf("Warning: failed to parse float %q: %v", s, err)
	}
	return v
}
