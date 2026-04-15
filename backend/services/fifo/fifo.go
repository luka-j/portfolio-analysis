package fifo

import (
	"sort"
	"time"

	"portfolio-analysis/models"
)

// Lot represents an open buy lot in FIFO matching.
type Lot struct {
	Qty   float64
	Price float64
	Date  time.Time
	Curr  string
}

// MatchedSell represents a sell matched against one or more buy lots via FIFO.
type MatchedSell struct {
	Qty       float64
	SellPrice float64
	CostPrice float64
	SellDate  time.Time
	CostDate  time.Time
	Curr      string
	Comm      float64 // commission allocated to this chunk (only the first chunk carries it)
}

// Match runs FIFO lot matching on the given trades.
// Trades are sorted chronologically internally. The caller should pre-filter
// any trades that should be excluded (e.g. FX trades).
// Returns remaining open lots and all matched sells.
func Match(trades []models.Trade) ([]Lot, []MatchedSell) {
	sorted := make([]models.Trade, len(trades))
	copy(sorted, trades)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].DateTime.Before(sorted[j].DateTime)
	})

	var openLots []Lot
	var matchedSells []MatchedSell

	for _, t := range sorted {
		if t.BuySell == "TRANSFER_IN" {
			continue
		}
		// STOCK_DIVIDEND shares are received at zero cost. They are added as zero-price lots so that
		// holdings counts and FIFO lot ordering stay consistent. When sold, their cost is 0 (100%
		// of proceeds is gain). A future enhancement could distribute cost proportionally across
		// existing lots for jurisdictions that require a pro-rated cost basis.
		if t.BuySell == "STOCK_DIVIDEND" {
			openLots = append(openLots, Lot{
				Qty: t.Quantity, Price: 0, Date: t.DateTime, Curr: t.Currency,
			})
			continue
		}
		if t.Quantity > 0 {
			openLots = append(openLots, Lot{
				Qty: t.Quantity, Price: t.Price, Date: t.DateTime, Curr: t.Currency,
			})
		} else if t.Quantity < 0 {
			sellQty := -t.Quantity
			comm := t.Commission

			for sellQty > 1e-9 && len(openLots) > 0 {
				matchQty := openLots[0].Qty
				if matchQty > sellQty {
					matchQty = sellQty
				}

				matchedSells = append(matchedSells, MatchedSell{
					Qty:       matchQty,
					SellPrice: t.Price,
					CostPrice: openLots[0].Price,
					SellDate:  t.DateTime,
					CostDate:  openLots[0].Date,
					Curr:      t.Currency,
					Comm:      comm,
				})
				// Allocate commission fully to the first matched chunk to avoid double counting.
				comm = 0

				openLots[0].Qty -= matchQty
				sellQty -= matchQty
				if openLots[0].Qty <= 1e-9 {
					openLots = openLots[1:]
				}
			}
		}
	}
	return openLots, matchedSells
}
