package flexquery

import (
	"encoding/xml"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"portfolio-analysis/models"
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
	SubCategory     string `xml:"subCategory,attr"`
	Currency        string `xml:"currency,attr"`
	ListingExchange string `xml:"listingExchange,attr"`
	DateTime        string `xml:"dateTime,attr"`
	TradeDate       string `xml:"tradeDate,attr"`
	Quantity        string `xml:"quantity,attr"`
	TradePrice      string `xml:"tradePrice,attr"`
	Proceeds        string `xml:"proceeds,attr"`
	IBCommission    string `xml:"ibCommission,attr"`
	BuySell         string `xml:"buySell,attr"`
	TradeID         string `xml:"tradeID,attr"`  // IB unique identifier
	Conid           string `xml:"conid,attr"`    // IB permanent contract ID
}

type xmlTransfers struct {
	Items []xmlTransfer `xml:"Transfer"`
}

type xmlTransfer struct {
	Symbol          string `xml:"symbol,attr"`
	AssetCategory   string `xml:"assetCategory,attr"`
	SubCategory     string `xml:"subCategory,attr"`
	Currency        string `xml:"currency,attr"`
	ListingExchange string `xml:"listingExchange,attr"`
	DateTime        string `xml:"dateTime,attr"`
	Date            string `xml:"date,attr"`
	Quantity        string `xml:"quantity,attr"`
	PositionAmount  string `xml:"positionAmount,attr"`
	Direction       string `xml:"direction,attr"`
	Conid           string `xml:"conid,attr"` // IB permanent contract ID
}

type xmlOpenPositions struct {
	Items []xmlOpenPosition `xml:"OpenPosition"`
}

type xmlOpenPosition struct {
	Symbol            string `xml:"symbol,attr"`
	AssetCategory     string `xml:"assetCategory,attr"`
	SubCategory       string `xml:"subCategory,attr"`
	Currency          string `xml:"currency,attr"`
	Quantity          string `xml:"quantity,attr"`
	MarkPrice         string `xml:"markPrice,attr"`
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

// Parser handles parsing IB FlexQuery XML files into FlexQueryData.
type Parser struct{}

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
			Conid:           t.Conid,
			Symbol:          t.Symbol,
			AssetCategory:   resolveAssetCategory(t.AssetCategory, t.SubCategory),
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
			Conid:           tr.Conid,
			Symbol:          tr.Symbol,
			AssetCategory:   resolveAssetCategory(tr.AssetCategory, tr.SubCategory),
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
			Symbol:            op.Symbol,
			AssetCategory:     resolveAssetCategory(op.AssetCategory, op.SubCategory),
			Currency:          op.Currency,
			Quantity:          parseFloat(op.Quantity),
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

// resolveAssetCategory returns a normalised asset category.
// IB reports assetCategory="STK" for both stocks and ETFs; subCategory="ETF" distinguishes them.
func resolveAssetCategory(assetCat, subCat string) string {
	if assetCat == "STK" && subCat == "ETF" {
		return "ETF"
	}
	return assetCat
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
