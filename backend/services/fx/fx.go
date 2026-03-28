package fx

import (
	"fmt"
	"time"

	"gofolio-analysis/models"
	"gofolio-analysis/services/market"
)

// Service provides currency conversion using Yahoo Finance FX pairs.
type Service struct {
	MarketProvider market.Provider
	CNBProvider    *market.CNBProvider
}

// NewService creates a new FX service.
func NewService(mp market.Provider, cnb *market.CNBProvider) *Service {
	return &Service{MarketProvider: mp, CNBProvider: cnb}
}

// GetRate returns the exchange rate from one currency to another on a given date.
// It uses CNB for CZK pairs and Yahoo Finance FX pairs (e.g. EURUSD=X) for all others.
// If from == to, returns 1.0.
func (s *Service) GetRate(from, to string, date time.Time) (float64, error) {
	if from == to {
		return 1.0, nil
	}

	if s.CNBProvider != nil {
		if to == "CZK" {
			return s.CNBProvider.GetRate(from, date)
		}
		if from == "CZK" {
			rate, err := s.CNBProvider.GetRate(to, date)
			if err != nil {
				return 0, err
			}
			if rate == 0 {
				return 0, fmt.Errorf("zero rate from CNB for CZK→%s", to)
			}
			return 1.0 / rate, nil
		}
	}

	// Yahoo Finance FX symbol convention: FROMTO=X
	symbol := fmt.Sprintf("%s%s=X", from, to)
	start := date.AddDate(0, 0, -5) // look back a few days for weekends
	points, err := s.MarketProvider.GetHistory(symbol, start, date)
	if err != nil {
		return 0, fmt.Errorf("fetching FX rate %s→%s: %w", from, to, err)
	}

	if len(points) == 0 {
		return 0, fmt.Errorf("no FX data for %s→%s on %s", from, to, date.Format("2006-01-02"))
	}

	// Return the closest rate at or before the requested date.
	var best models.PricePoint
	for _, p := range points {
		if !p.Date.After(date) && p.Close != 0 {
			best = p
		}
	}
	if best.Close == 0 {
		// Fallback to the most recent non-zero point
		for i := len(points) - 1; i >= 0; i-- {
			if points[i].Close != 0 {
				best = points[i]
				break
			}
		}
	}
	if best.Close == 0 {
		return 0, fmt.Errorf("all FX rates zero for %s→%s on %s", from, to, date.Format("2006-01-02"))
	}
	return best.Close, nil
}

// GetSpotRate returns the latest available exchange rate for today.
func (s *Service) GetSpotRate(from, to string) (float64, error) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return s.GetRate(from, to, today)
}

// Convert converts an amount from one currency to another on a given date.
func (s *Service) Convert(amount float64, from, to string, date time.Time) (float64, error) {
	rate, err := s.GetRate(from, to, date)
	if err != nil {
		return 0, err
	}
	return amount * rate, nil
}

// ConvertSpot converts an amount using the latest available rate.
func (s *Service) ConvertSpot(amount float64, from, to string) (float64, error) {
	rate, err := s.GetSpotRate(from, to)
	if err != nil {
		return 0, err
	}
	return amount * rate, nil
}
