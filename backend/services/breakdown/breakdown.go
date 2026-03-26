package breakdown

import (
	"sort"
	"strings"

	"gorm.io/gorm"

	"gofolio-analysis/models"
)

const (
	// othersThreshold is the minimum percentage below which entries are grouped as "Others".
	othersThreshold = 2.0
	// minTopEntries is the minimum number of entries shown before applying "Others" grouping.
	minTopEntries = 5
	// assetTopEntries is how many individual assets to show before grouping (more granular).
	assetTopEntries = 10
)

// Service calculates portfolio breakdown from cached DB fundamentals and ETF breakdown data.
// It never makes external API calls.
type Service struct {
	DB *gorm.DB
}

// NewService creates a new breakdown Service.
func NewService(db *gorm.DB) *Service {
	return &Service{DB: db}
}

// positionWithValue combines a position and its total value in the display currency.
type positionWithValue struct {
	symbol   string
	value    float64
}

// Calculate returns a complete BreakdownResponse for the given positions.
// Positions must already carry `.Values[currency]` populated by the portfolio service.
func (s *Service) Calculate(positions []models.PositionValue, currency string) (*models.BreakdownResponse, error) {
	var posValues []positionWithValue
	totalPortfolioValue := 0.0
	for _, pos := range positions {
		v := pos.Values[currency]
		if v <= 0 {
			continue
		}
		eff := pos.YahooSymbol
		if eff == "" {
			eff = pos.Symbol
		}
		posValues = append(posValues, positionWithValue{symbol: eff, value: v})
		totalPortfolioValue += v
	}

	if totalPortfolioValue == 0 {
		return &models.BreakdownResponse{Currency: currency, Sections: nil}, nil
	}

	byType       := make(map[string]float64)
	byAsset      := make(map[string]float64)
	byCountry    := make(map[string]float64)
	bySector     := make(map[string]float64)
	byBondRating := make(map[string]float64)

	for _, pos := range posValues {
		fund, err := s.loadFundamentals(pos.symbol)
		if err != nil {
			return nil, err
		}

		assetType := "Unknown"
		if fund != nil && fund.AssetType != "" && fund.AssetType != "Unknown" {
			assetType = fund.AssetType
		}

		label := pos.symbol
		if fund != nil && fund.Name != "" {
			label = fund.Name
		}

		if assetType == "ETF" || assetType == "Bond ETF" {
			breakdowns, err := s.loadBreakdowns(pos.symbol)
			if err != nil {
				return nil, err
			}

			byAsset[label] += pos.value

			if assetType == "Bond ETF" {
				// Bond ETFs: contribute to bond rating section only; excluded from country/sector.
				ratingRows := filterDimension(breakdowns, "bond_rating")
				totalRW := sumWeights(ratingRows)
				if totalRW > 0 {
					for _, bd := range ratingRows {
						byBondRating[bd.Label] += pos.value * (bd.Weight / totalRW)
					}
				} else {
					byBondRating["Unknown"] += pos.value
				}
			} else {
				// Equity ETF: sector + country.
				sectorRows := filterDimension(breakdowns, "sector")
				countryRows := filterDimension(breakdowns, "country")

				totalSW := sumWeights(sectorRows)
				if totalSW > 0 {
					for _, bd := range sectorRows {
						bySector[bd.Label] += pos.value * (bd.Weight / totalSW)
					}
				} else {
					bySector["Unknown"] += pos.value
				}

				totalCW := sumWeights(countryRows)
				if totalCW > 0 {
					for _, bd := range countryRows {
						byCountry[bd.Label] += pos.value * (bd.Weight / totalCW)
					}
				} else {
					byCountry["Unknown"] += pos.value
				}
			}
		} else {
			// Direct holding (stock, commodity, etc.).
			byAsset[label] += pos.value

			country := "Unknown"
			sector  := "Unknown"
			if fund != nil {
				country = fund.Country
				sector  = fund.Sector
			}
			// Commodities are excluded from geographic/sector breakdowns.
			if assetType == "Commodity" {
				byCountry["Commodity (excluded)"] += pos.value
				bySector["Commodity (excluded)"] += pos.value
			} else {
				byCountry[country] += pos.value
				bySector[sector] += pos.value
			}
		}

		byType[assetType] += pos.value
	}

	sections := []models.BreakdownSection{
		buildSection("By Asset Type", byType, totalPortfolioValue, minTopEntries, ""),
		buildSection("By Asset", byAsset, totalPortfolioValue, assetTopEntries, ""),
		buildSection("By Country", byCountry, totalPortfolioValue, minTopEntries,
			"ETF country weights are sourced from Yahoo Finance. Bond ETFs and commodities are excluded."),
		buildSection("By Sector", bySector, totalPortfolioValue, minTopEntries,
			"ETF sector weights are sourced from Yahoo Finance. Bond ETFs and commodities are excluded."),
	}
	if len(byBondRating) > 0 {
		sections = append(sections, buildSection("By Bond Rating", byBondRating, totalPortfolioValue, minTopEntries,
			"Bond ETF credit rating weights sourced from Yahoo Finance."))
	}

	return &models.BreakdownResponse{
		Currency: currency,
		Sections: sections,
	}, nil
}

// buildSection converts a value map into a sorted BreakdownSection with an "Others" bucket.
func buildSection(title string, values map[string]float64, total float64, minTop int, note string) models.BreakdownSection {
	type entry struct {
		label string
		value float64
	}
	var entries []entry
	for k, v := range values {
		entries = append(entries, entry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].value > entries[j].value })

	var result []models.BreakdownEntry
	var othersValue float64

	for i, e := range entries {
		pct := safePct(e.value, total)
		if i >= minTop && pct < othersThreshold {
			othersValue += e.value
			continue
		}
		result = append(result, models.BreakdownEntry{
			Label:      e.label,
			Value:      e.value,
			Percentage: pct,
		})
	}

	if othersValue > 0 {
		result = append(result, models.BreakdownEntry{
			Label:      "Others",
			Value:      othersValue,
			Percentage: safePct(othersValue, total),
		})
	}

	return models.BreakdownSection{
		Title:   title,
		Note:    strings.TrimSpace(note),
		Entries: result,
	}
}

func safePct(v, total float64) float64 {
	if total == 0 {
		return 0
	}
	return (v / total) * 100
}

// loadFundamentals returns cached AssetFundamental for symbol, or nil if absent.
func (s *Service) loadFundamentals(symbol string) (*models.AssetFundamental, error) {
	var f models.AssetFundamental
	if err := s.DB.Where("symbol = ?", symbol).First(&f).Error; err != nil {
		return nil, nil
	}
	return &f, nil
}

// loadBreakdowns returns cached EtfBreakdown rows for a fund symbol.
func (s *Service) loadBreakdowns(fundSymbol string) ([]models.EtfBreakdown, error) {
	var rows []models.EtfBreakdown
	if err := s.DB.Where("fund_symbol = ?", fundSymbol).Find(&rows).Error; err != nil {
		return nil, nil
	}
	return rows, nil
}

func filterDimension(rows []models.EtfBreakdown, dimension string) []models.EtfBreakdown {
	var out []models.EtfBreakdown
	for _, r := range rows {
		if r.Dimension == dimension {
			out = append(out, r)
		}
	}
	return out
}

func sumWeights(rows []models.EtfBreakdown) float64 {
	total := 0.0
	for _, r := range rows {
		total += r.Weight
	}
	return total
}
