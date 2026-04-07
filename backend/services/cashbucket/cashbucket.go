package cashbucket

import (
	"sort"
	"time"

	"portfolio-analysis/models"
)

// Bucket holds the proceeds of a sale, temporarily preventing them from counting as an outflow.
type Bucket struct {
	Date       time.Time
	Remaining  float64 // in display currency after FX conversion
	ExpiryDate time.Time
}

// Result is the output of the bucket processing pass.
type Result struct {
	// AdjustedCashFlows contains only real inflows/outflows plus expiry events.
	AdjustedCashFlows []models.CashFlow
	// PendingCash is the sum of all active (non-expired, non-consumed) bucket remainders as of asOf.
	PendingCash float64
}

// ConvertFn converts an amount from fromCurrency to the display currency on the given date.
// For Original accounting mode it should return the amount unchanged.
type ConvertFn func(amount float64, from string, date time.Time) (float64, error)

// Process applies cash-bucket logic to a list of trades and dividend cash flows.
// Sells create buckets; buys consume from the oldest bucket first (FIFO). Dividends pass through
// unchanged. Buckets that are still non-empty after expiryDays become real outflows on their expiry date.
func Process(
	trades []models.Trade,
	dividendFlows []models.CashFlow,
	expiryDays int,
	asOf time.Time,
	convertFn ConvertFn,
) (*Result, error) {
	// Sort trades chronologically.
	sorted := make([]models.Trade, len(trades))
	copy(sorted, trades)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].DateTime.Before(sorted[j].DateTime)
	})

	var buckets []*Bucket
	var adjustedFlows []models.CashFlow

	for _, t := range sorted {
		if models.IsFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}

		// Proceeds is positive for a SELL (cash received) and negative for a BUY (cash paid out).
		// We use Proceeds + Commission because that is what GetCashFlows uses.
		rawAmount := t.Proceeds + t.Commission

		converted, err := convertFn(rawAmount, t.Currency, t.DateTime)
		if err != nil {
			return nil, err
		}

		if isSell(t) {
			// Positive amount means cash inflow from sale — create a bucket.
			if converted > 0 {
				expiry := t.DateTime.AddDate(0, 0, expiryDays)
				buckets = append(buckets, &Bucket{
					Date:       t.DateTime,
					Remaining:  converted,
					ExpiryDate: expiry,
				})
			} else {
				// Sell with zero/negative proceeds (e.g. short sale recorded oddly) — treat as real flow.
				adjustedFlows = append(adjustedFlows, models.CashFlow{Date: t.DateTime, Amount: converted})
			}
		} else {
			// BUY: converted is negative (cash leaves portfolio). The absolute value is the cost.
			cost := -converted // positive number
			remaining := cost

			for _, b := range buckets {
				if remaining <= 0 {
					break
				}
				if b.Remaining <= 0 {
					continue
				}
				if b.ExpiryDate.Before(t.DateTime) {
					continue
				}
				consume := min64(b.Remaining, remaining)
				b.Remaining -= consume
				remaining -= consume
			}

			if remaining > 0 {
				// Excess buy cost not covered by buckets → real inflow (deposit).
				adjustedFlows = append(adjustedFlows, models.CashFlow{Date: t.DateTime, Amount: -remaining})
			}
		}
	}

	// Dividend / withholding flows pass through unchanged.
	adjustedFlows = append(adjustedFlows, dividendFlows...)

	// Evaluate bucket expiry as of asOf.
	var pendingCash float64
	for _, b := range buckets {
		if b.Remaining <= 0 {
			continue
		}
		if expiryDays == 0 || b.ExpiryDate.Before(asOf) {
			// Bucket has expired — its remaining amount becomes a real outflow on the expiry date.
			outflowDate := b.ExpiryDate
			if expiryDays == 0 {
				// Zero expiry: outflow on the same day as the sale.
				outflowDate = b.Date
			}
			adjustedFlows = append(adjustedFlows, models.CashFlow{Date: outflowDate, Amount: b.Remaining})
		} else {
			// Still active — counts as pending cash.
			pendingCash += b.Remaining
		}
	}

	// Sort final flows chronologically.
	sort.Slice(adjustedFlows, func(i, j int) bool {
		return adjustedFlows[i].Date.Before(adjustedFlows[j].Date)
	})

	return &Result{
		AdjustedCashFlows: adjustedFlows,
		PendingCash:       pendingCash,
	}, nil
}

// isSell returns true when a trade is a sell (or short).
func isSell(t models.Trade) bool {
	if t.BuySell == "SELL" {
		return true
	}
	// Infer from quantity sign when BuySell tag is absent.
	return t.BuySell == "" && t.Quantity < 0
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
