package portfolio

import (
	"testing"
	"time"

	"portfolio-analysis/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCurrentHoldings(t *testing.T) {
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)

	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{Symbol: "AAPL", ListingExchange: "NASDAQ", Currency: "USD", BuySell: "BUY", Quantity: 10, Price: 150, Proceeds: -1500, DateTime: day1},
			{Symbol: "AAPL", ListingExchange: "NASDAQ", Currency: "USD", BuySell: "SELL", Quantity: -5, Price: 160, Proceeds: 800, DateTime: day2},
			{Symbol: "MSFT", ListingExchange: "NASDAQ", Currency: "USD", BuySell: "BUY", Quantity: 20, Price: 300, Proceeds: -6000, DateTime: day1},
			{Symbol: "MSFT", ListingExchange: "NASDAQ", Currency: "USD", BuySell: "SELL", Quantity: -20, Price: 310, Proceeds: 6200, DateTime: day2},
		},
	}

	svc := NewService(nil, nil, 0)
	
	holdings := svc.GetCurrentHoldings(data)
	require.Len(t, holdings, 2, "both positions should be returned")
	
	var aapl, msft models.Holding
	for _, h := range holdings {
		if h.Symbol == "AAPL" {
			aapl = h
		} else {
			msft = h
		}
	}
	assert.Equal(t, 5.0, aapl.Quantity)
	assert.Equal(t, 0.0, msft.Quantity)
}
