package portfolio

import (
	"testing"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/fx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockProvider is a mock implementation of market.Provider used here for FX mock
type MockFXProvider struct {
	mock.Mock
}

func (m *MockFXProvider) GetHistory(symbol string, from, to time.Time) ([]models.PricePoint, error) {
	args := m.Called(symbol, from, to)
	var pts []models.PricePoint
	if args.Get(0) != nil {
		pts = args.Get(0).([]models.PricePoint)
	}
	return pts, args.Error(1)
}

func (m *MockFXProvider) TradingDates(from, to time.Time) ([]time.Time, error) {
	args := m.Called(from, to)
	var dates []time.Time
	if args.Get(0) != nil {
		dates = args.Get(0).([]time.Time)
	}
	return dates, args.Error(1)
}

func (m *MockFXProvider) GetLatestPrice(symbol string) (float64, error) {
	args := m.Called(symbol)
	return args.Get(0).(float64), args.Error(1)
}


func TestGetCurrentValueMulti(t *testing.T) {
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)

	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{Symbol: "AAPL", ListingExchange: "NASDAQ", Currency: "USD", BuySell: "BUY", Quantity: 10, Price: 150, Proceeds: -1500, DateTime: day1},
			{Symbol: "EUR_STOCK", ListingExchange: "XETRA", Currency: "EUR", BuySell: "BUY", Quantity: 20, Price: 50, Proceeds: -1000, DateTime: day2},
		},
	}

	mp := &mockMarketProvider{
		current: 160,
	}

	mockFx := new(MockFXProvider)
	fxSvc := fx.NewService(mockFx, nil)

	today := time.Now().UTC()
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	startSpot := todayDate.AddDate(0, 0, -5)
	
	mockFx.On("GetHistory", "EURUSD=X", startSpot, todayDate).Return([]models.PricePoint{
		{Date: todayDate, Close: 1.10}, 
	}, nil)

	svc := NewService(mp, fxSvc, 0)
	
	t.Run("Original Accounting Model", func(t *testing.T) {
		results, err := svc.GetCurrentValueMulti(data, []string{"USD"}, models.AccountingModelOriginal)
		require.NoError(t, err)

		result := results["USD"]
		require.Len(t, result.Positions, 2)
		
		var aapl models.PositionValue
		var eurStock models.PositionValue
		for _, p := range result.Positions {
			if p.Symbol == "AAPL" {
				aapl = p
			} else {
				eurStock = p
			}
		}

		assert.Equal(t, 1600.0, aapl.Value)
		assert.Equal(t, 150.0, aapl.CostBasis)
		
		assert.Equal(t, 3200.0, eurStock.Value)
		assert.Equal(t, 50.0, eurStock.CostBasis)
	})

	t.Run("Spot Accounting Model", func(t *testing.T) {
		results, err := svc.GetCurrentValueMulti(data, []string{"USD"}, models.AccountingModelSpot)
		require.NoError(t, err)

		result := results["USD"]
		require.Len(t, result.Positions, 2)
		
		var eurStock models.PositionValue
		for _, p := range result.Positions {
			if p.Symbol == "EUR_STOCK" {
				eurStock = p
			}
		}

		assert.InDelta(t, 3520.0, eurStock.Value, 0.001)
		assert.InDelta(t, 55.0, eurStock.CostBasis, 0.001)
	})
}
