package tax

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gofolio-analysis/models"
	"gofolio-analysis/services/fx"
)

// mockMarketProvider from main_test logic for fx.Service fallback testing.
type mockMarket struct {
	rates map[string]float64
}

func (m *mockMarket) GetHistory(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	rate, ok := m.rates[symbol]
	if !ok {
		return nil, nil // Not found
	}
	return []models.PricePoint{
		{Date: from, Close: rate},
		{Date: to, Close: rate},
	}, nil
}

func setupTaxService() *Service {
	market := &mockMarket{
		rates: map[string]float64{
			"USDCZK=X": 25.0,
			"EURCZK=X": 25.5,
		},
	}
	// Use nil for CNBProvider so fx.Service falls back to Yahoo mock for CZK
	fxSvc := fx.NewService(market, nil)
	return NewService(fxSvc)
}

func TestEmploymentIncome(t *testing.T) {
	svc := setupTaxService()

	costBasis := 40.0
	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{
				Symbol:       "AAPL",
				BuySell:      "ESPP_VEST",
				Currency:     "USD",
				DateTime:     time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC),
				Quantity:     10,
				Price:        100.0,
				TaxCostBasis: &costBasis,
			},
			{
				Symbol:       "AAPL",
				BuySell:      "RSU_VEST",
				Currency:     "USD",
				DateTime:     time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
				Quantity:     5,
				Price:        120.0,
				TaxCostBasis: nil, // RSU usually costs nothing
			},
			{
				Symbol:   "AAPL",
				BuySell:  "ESPP_VEST",
				Currency: "USD",
				DateTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), // Outside year
				Quantity: 20,
				Price:    50.0,
			},
		},
	}

	report, err := svc.GetReport(data, 2025)
	require.NoError(t, err)
	assert.Equal(t, 2025, report.Year)

	emp := report.EmploymentIncome
	assert.Len(t, emp.Transactions, 2, "Should only pick up in-year ESPP/RSU vests")

	// ESPP cost = 0 (we only record the profit as Benefit)
	// RSU cost = 0
	assert.InDelta(t, 0.0, emp.TotalCostCZK, 0.01)

	// ESPP benefit = (100 - 40) * 10 * 25.0 = 15,000 CZK
	// RSU benefit = 5 * 120 * 25.0 = 15,000 CZK
	// Total = 30,000 CZK
	assert.InDelta(t, 30000.0, emp.TotalBenefitCZK, 0.01)

	assert.Equal(t, "ESPP_VEST", emp.Transactions[0].Type)
	assert.Equal(t, 100.0, emp.Transactions[0].NativePrice)
	assert.Equal(t, 0.0, emp.Transactions[0].CostCZK)
	assert.Equal(t, 15000.0, emp.Transactions[0].BenefitCZK)
}

func TestInvestmentIncomeFIFO(t *testing.T) {
	svc := setupTaxService()

	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{
				Symbol:   "MSFT",
				BuySell:  "BUY",
				Currency: "USD",
				DateTime: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				Quantity: 10,
				Price:    200.0, // Historical buy
			},
			{
				Symbol:   "MSFT",
				BuySell:  "BUY",
				Currency: "USD",
				DateTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				Quantity: 5,
				Price:    250.0,
			},
			{
				Symbol:   "MSFT",
				BuySell:  "SELL",
				Currency: "USD",
				DateTime: time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC),
				Quantity: -12, // Sells 10 from 2020, and 2 from 2024
				Price:    300.0,
			},
			{
				// Buy in same year
				Symbol:   "MSFT",
				BuySell:  "BUY",
				Currency: "USD",
				DateTime: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
				Quantity: 3,
				Price:    320.0,
			},
			{
				Symbol:   "MSFT",
				BuySell:  "SELL",
				Currency: "USD",
				DateTime: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
				Quantity: -3, // Sells 3 from 2024
				Price:    350.0,
			},
		},
	}

	report, err := svc.GetReport(data, 2025)
	require.NoError(t, err)

	inv := report.InvestmentIncome
	assert.Len(t, inv.Transactions, 3, "Sell 1 is split into 2 lots, Sell 2 is 1 lot")

	// Tx 1: matches 10 from 2020
	assert.Equal(t, 10.0, inv.Transactions[0].Quantity)
	assert.Equal(t, "2020-01-01", inv.Transactions[0].BuyDate)
	assert.InDelta(t, 10*200*25.0, inv.Transactions[0].CostCZK, 0.01) // 50,000 CZK
	assert.InDelta(t, 10*300*25.0, inv.Transactions[0].BenefitCZK, 0.01) // 75,000 CZK

	// Tx 2: matches 2 from 2024
	assert.Equal(t, 2.0, inv.Transactions[1].Quantity)
	assert.Equal(t, "2024-01-01", inv.Transactions[1].BuyDate)
	assert.InDelta(t, 2*250*25.0, inv.Transactions[1].CostCZK, 0.01) // 12,500 CZK
	assert.InDelta(t, 2*300*25.0, inv.Transactions[1].BenefitCZK, 0.01) // 15,000 CZK

	// Tx 3: matches 3 from 2024
	assert.Equal(t, 3.0, inv.Transactions[2].Quantity)
	assert.Equal(t, "2024-01-01", inv.Transactions[2].BuyDate)
	assert.InDelta(t, 3*250*25.0, inv.Transactions[2].CostCZK, 0.01) // 18,750 CZK
	assert.InDelta(t, 3*350*25.0, inv.Transactions[2].BenefitCZK, 0.01) // 26,250 CZK

	totalCost := inv.TotalCostCZK
	totalBenefit := inv.TotalBenefitCZK
	assert.InDelta(t, 81250.0, totalCost, 0.01)
	assert.InDelta(t, 116250.0, totalBenefit, 0.01)
}

func TestIgnoresIrrelevantTrades(t *testing.T) {
	svc := setupTaxService()
	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{AssetCategory: "CASH", Currency: "USD", Quantity: -100, Price: 1, DateTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}, // Ignored
			{Symbol: "EUR.USD", Currency: "USD", Quantity: -10, Price: 1.05, DateTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},   // FX Ignored
		},
	}

	report, err := svc.GetReport(data, 2025)
	require.NoError(t, err)
	assert.Empty(t, report.EmploymentIncome.Transactions)
	assert.Empty(t, report.InvestmentIncome.Transactions)
}
