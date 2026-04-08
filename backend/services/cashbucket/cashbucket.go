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

// BalanceChange records a delta to the total pending-cash balance.
// Date is truncated to midnight UTC so callers can compare against calendar dates.
type BalanceChange struct {
	Date  time.Time
	Delta float64 // positive = cash added to pending; negative = cash removed
}

// Result is the output of the bucket processing pass.
type Result struct {
	// AdjustedCashFlows contains only real inflows/outflows plus expiry events.
	AdjustedCashFlows []models.CashFlow
	// PendingCash is the sum of all active (non-expired, non-consumed) bucket remainders as of asOf.
	PendingCash float64
	// BalanceChanges is a sorted list of pending-cash deltas covering all events (sells,
	// buy-consumptions, and expirations) regardless of asOf. Callers can use this to
	// reconstruct the pending-cash amount for any historical date by summing deltas up to
	// and including that date.
	BalanceChanges []BalanceChange
}

// ConvertFn converts an amount from fromCurrency to the display currency on the given date.
// For Original accounting mode it should return the amount unchanged.
type ConvertFn func(amount float64, from string, date time.Time) (float64, error)

// truncDate returns t rounded down to midnight UTC so that BalanceChange dates
// align with the calendar-day granularity used by GetDailyValues.
func truncDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

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
	var balanceChanges []BalanceChange

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

		// Non-standard trade types (RSU vests, ESPP grants, etc.) are pure external
		// inflows — they bring shares into the portfolio without spending cash.
		// They must not consume bucket proceeds; pass them through as real flows.
		if !isSell(t) && t.BuySell != "BUY" && t.BuySell != "" {
			if converted != 0 {
				adjustedFlows = append(adjustedFlows, models.CashFlow{Date: t.DateTime, Amount: converted})
			}
			continue
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
				// Pending cash increases when the sell bucket is created.
				balanceChanges = append(balanceChanges, BalanceChange{
					Date:  truncDate(t.DateTime),
					Delta: +converted,
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
				// Pending cash decreases as the bucket is consumed by a buy.
				balanceChanges = append(balanceChanges, BalanceChange{
					Date:  truncDate(t.DateTime),
					Delta: -consume,
				})
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
	// The expiry BalanceChange is always recorded (regardless of asOf) so callers can
	// reconstruct per-day pending cash for any date range.
	var pendingCash float64
	for _, b := range buckets {
		if b.Remaining <= 0 {
			continue
		}
		outflowDate := b.ExpiryDate
		if expiryDays == 0 {
			// Zero expiry: outflow on the same day as the sale.
			outflowDate = b.Date
		}

		// Record the expiry balance change unconditionally so the full pending-cash
		// timeline is always available in BalanceChanges.
		balanceChanges = append(balanceChanges, BalanceChange{
			Date:  truncDate(outflowDate),
			Delta: -b.Remaining,
		})

		if expiryDays == 0 || b.ExpiryDate.Before(asOf) {
			// Bucket has expired — its remaining amount becomes a real outflow on the expiry date.
			adjustedFlows = append(adjustedFlows, models.CashFlow{Date: outflowDate, Amount: b.Remaining})
		} else {
			// Still active — counts as pending cash.
			pendingCash += b.Remaining
		}
	}

	// Sort final flows and balance changes chronologically.
	sort.Slice(adjustedFlows, func(i, j int) bool {
		return adjustedFlows[i].Date.Before(adjustedFlows[j].Date)
	})
	sort.Slice(balanceChanges, func(i, j int) bool {
		return balanceChanges[i].Date.Before(balanceChanges[j].Date)
	})

	return &Result{
		AdjustedCashFlows: adjustedFlows,
		PendingCash:       pendingCash,
		BalanceChanges:    balanceChanges,
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
