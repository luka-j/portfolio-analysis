package market

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"portfolio-analysis/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CNBProvider fetches exchange rates from the Czech National Bank.
type CNBProvider struct {
	DB         *gorm.DB
	HTTPClient *http.Client
}

// NewCNBProvider creates a new CNB API provider.
func NewCNBProvider(db *gorm.DB) *CNBProvider {
	return &CNBProvider{
		DB:         db,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type cnbRate struct {
	ValidFor     string  `json:"validFor"`
	Amount       int     `json:"amount"`
	CurrencyCode string  `json:"currencyCode"`
	Rate         float64 `json:"rate"`
}

type cnbResponse struct {
	Rates []cnbRate `json:"rates"`
}

// GetRate retrieves the CZK rate for the given currency on the specific date.
// Returns (CZK per 1 unit of currency).
// Retries once on transient HTTP errors.
func (p *CNBProvider) GetRate(currency string, date time.Time) (float64, error) {
	if currency == "CZK" {
		return 1.0, nil
	}

	targetDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	symbol := currency + "CZK=X"

	var md models.MarketData
	err := p.DB.Where("symbol = ? AND date = ?", symbol, targetDate).First(&md).Error
	if err == nil {
		if md.Close != 0 && md.Provider == "CNB" {
			return md.Close, nil
		}
	}

	dateStr := targetDate.Format("2006-01-02")
	apiURL := fmt.Sprintf("https://api.cnb.cz/cnbapi/exrates/daily?date=%s", dateStr)

	var body []byte
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return 0, fmt.Errorf("cnb request: %w", err)
		}
		resp, err := p.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("cnb request: %w", err)
			continue
		}
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("reading cnb response: %w", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("cnb returned status %d", resp.StatusCode)
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return 0, lastErr
	}

	var data cnbResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, fmt.Errorf("parsing cnb: %w", err)
	}

	foundMatch := false
	var resultRate float64
	var validDate time.Time

	var batch []models.MarketData
	for _, r := range data.Rates {
		if r.Amount == 0 {
			continue
		}
		val := r.Rate / float64(r.Amount)
		sym := r.CurrencyCode + "CZK=X"
		vDate, err := time.Parse("2006-01-02", r.ValidFor)
		if err != nil {
			continue
		}

		if r.CurrencyCode == currency {
			foundMatch = true
			resultRate = val
			validDate = vDate
		}

		batch = append(batch, models.MarketData{
			Symbol:   sym,
			Date:     vDate,
			Open:     val,
			High:     val,
			Low:      val,
			Close:    val,
			AdjClose: val,
			Provider: "CNB",
		})
	}

	if len(batch) > 0 {
		p.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "symbol"}, {Name: "date"}},
			DoUpdates: clause.AssignmentColumns([]string{"open", "high", "low", "close", "adj_close", "provider"}),
		}).Create(&batch)
	}

	if !foundMatch {
		// Fallback: look for the most recent cached rate within 7 days (weekends/holidays).
		lookback := targetDate.AddDate(0, 0, -7)
		var fallback models.MarketData
		if err := p.DB.Where("symbol = ? AND date >= ? AND date <= ? AND close > 0 AND provider = 'CNB'",
			symbol, lookback, targetDate).Order("date DESC").First(&fallback).Error; err == nil {
			return fallback.Close, nil
		}
		return 0, fmt.Errorf("currency %s not found in CNB rates for %s", currency, dateStr)
	}

	if !validDate.Equal(targetDate) {
		p.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "symbol"}, {Name: "date"}},
			DoUpdates: clause.AssignmentColumns([]string{"open", "high", "low", "close", "adj_close", "provider"}),
		}).Create(&models.MarketData{
			Symbol:   symbol,
			Date:     targetDate,
			Open:     resultRate,
			High:     resultRate,
			Low:      resultRate,
			Close:    resultRate,
			AdjClose: resultRate,
			Provider: "CNB",
		})
	}

	return resultRate, nil
}
