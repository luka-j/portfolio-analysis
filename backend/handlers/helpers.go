package handlers

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gofolio-analysis/models"
	"gofolio-analysis/services/market"
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

// parseCachedOnly extracts the cachedOnly query parameter.
func parseCachedOnly(c *gin.Context) bool {
	return c.Query("cachedOnly") == "true"
}

// buildFXRateMap pre-fetches historical exchange rates for nativeCcy→displayCcy and returns
// a date-keyed map of rates, forward-filled over weekends/holidays. Returns nil when no
// conversion is needed (same currency) or when the fetch fails.
func buildFXRateMap(mp market.Provider, nativeCcy, displayCcy string, from, to time.Time) map[string]float64 {
	if nativeCcy == "" || nativeCcy == displayCcy {
		return nil
	}
	fxSymbol := fmt.Sprintf("%s%s=X", nativeCcy, displayCcy)
	points, err := mp.GetHistory(fxSymbol, from.AddDate(0, 0, -5), to, false)
	if err != nil {
		log.Printf("Warning: pre-fetching FX %s: %v", fxSymbol, err)
		return nil
	}
	pc := make(map[string]float64)
	for _, p := range points {
		pc[p.Date.Format("2006-01-02")] = p.Close
	}
	var lastRate float64
	if len(points) > 0 {
		lastRate = points[0].Close
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		if r, ok := pc[ds]; ok {
			lastRate = r
		} else if lastRate != 0 {
			pc[ds] = lastRate
		}
	}
	return pc
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
