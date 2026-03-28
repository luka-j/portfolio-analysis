package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/models"
)

// parseDateRange extracts and validates from/to query parameters.
func parseDateRange(c *gin.Context) (time.Time, time.Time, error) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr == "" || toStr == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("from and to parameters are required (YYYY-MM-DD)")
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid from date: %w", err)
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid to date: %w", err)
	}

	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("to date must be on or after from date")
	}

	return from, to, nil
}

// splitCurrencies parses a comma-separated currencies string into a non-empty slice.
// Returns ["USD"] if the input is empty.
func splitCurrencies(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []string{"USD"}
	}
	return out
}

// parseAccountingModel extracts the accounting_model query parameter.
func parseAccountingModel(c *gin.Context) models.AccountingModel {
	return models.ParseAccountingModel(c.DefaultQuery("accounting_model", "historical"))
}

// DateRangeFromData determines the logical start and end dates from the portfolio data.
func DateRangeFromData(data *models.FlexQueryData) (time.Time, time.Time) {
	var earliest, latest time.Time

	for _, t := range data.Trades {
		// Skip trades that don't create holdings — they must not drive the
		// inception date, otherwise GetDailyValues returns zero-value rows
		// before any securities are actually held.
		if models.IsFXTrade(t) || t.BuySell == "TRANSFER_IN" {
			continue
		}
		if earliest.IsZero() || t.DateTime.Before(earliest) {
			earliest = t.DateTime
		}
		if t.DateTime.After(latest) {
			latest = t.DateTime
		}
	}
	for _, ct := range data.CashTransactions {
		if earliest.IsZero() || ct.DateTime.Before(earliest) {
			earliest = ct.DateTime
		}
		if ct.DateTime.After(latest) {
			latest = ct.DateTime
		}
	}

	if latest.IsZero() {
		return earliest, time.Now().UTC()
	}

	// Extend to today if latest is in the past.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if today.After(latest) {
		latest = today
	}

	return earliest, latest
}
