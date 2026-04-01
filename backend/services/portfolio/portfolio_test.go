package portfolio

import (
	"math"
	"testing"
	"time"

	"gofolio-analysis/models"
)

// mockMarketProvider implements market.Provider
type mockMarketProvider struct {
	prices  map[string][]models.PricePoint
	history []models.PricePoint
	current float64
}

func (m *mockMarketProvider) GetHistory(symbol string, from, to time.Time, cachedOnly bool) ([]models.PricePoint, error) {
	if p, ok := m.prices[symbol]; ok {
		return p, nil
	}
	return m.history, nil
}

func (m *mockMarketProvider) GetCurrentPrice(symbol string, cachedOnly bool) (float64, error) {
	return m.current, nil
}

func TestGetDailyReturns_WeekendCashFlow(t *testing.T) {
	// 1. Setup mock data
	// Let's create a portfolio that trades "TEST"
	// Day 0: Friday (trade)
	// Day 1: Saturday (cash flow deposit)
	// Day 2: Sunday (nothing)
	// Day 3: Monday (market pricing resumes)

	friday := time.Date(2024, 10, 11, 0, 0, 0, 0, time.UTC)
	saturday := friday.AddDate(0, 0, 1)
	monday := friday.AddDate(0, 0, 3)

	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{
				Symbol:          "TEST",
				Currency:        "USD",
				ListingExchange: "EX",
				BuySell:         "BUY",
				Quantity:        100,
				Price:           10,
				Proceeds:        -1000,
				DateTime:        friday,
			},
			{
				Symbol:          "TEST",
				Currency:        "USD",
				ListingExchange: "EX",
				BuySell:         "BUY",
				Quantity:        10,
				Price:           10,
				Proceeds:        -100, // Deposit on Saturday
				DateTime:        saturday,
			},
		},
	}

	mockProvider := &mockMarketProvider{
		prices: map[string][]models.PricePoint{
			"TEST": {
				{Date: friday, Close: 10, AdjClose: 10},
				// No price on weekend
				{Date: monday, Close: 11, AdjClose: 11}, // Market went up 10%
			},
		},
	}

	// 2. Initialize Service with mock provider
	// We use Original accounting model to reliably bypass FX lookups.
	svc := NewService(mockProvider, nil, nil)

	returns, startDates, endDates, err := svc.GetDailyReturns(data, friday, monday, "USD", models.AccountingModelOriginal)
	if err != nil {
		t.Fatalf("GetDailyReturns failed: %v", err)
	}

	// 3. Verify exactly one return period was established (Friday -> Monday)
	if len(returns) != 1 {
		t.Fatalf("Expected 1 return period over the weekend gap, got %d", len(returns))
	}

	// 4. Verify the dates are perfectly paired 
	expectedStart := "2024-10-11"
	expectedEnd := "2024-10-14"

	if startDates[0] != expectedStart {
		t.Errorf("Expected start date %s, got %s", expectedStart, startDates[0])
	}
	if endDates[0] != expectedEnd {
		t.Errorf("Expected end date %s, got %s", expectedEnd, endDates[0])
	}

	// 5. Verify the math of the return:
	// Day 0 (Friday): Value = 100 * 10 = 1000. prevValue = 1000.
	// Between Fri and Mon, CF of -100 (Deposit 100) occurred on Saturday.
	// adjustedPrev = 1000 - (-100) = 1100.
	// Day 3 (Monday): Holdings = 110. Price = 11. curValue = 110 * 11 = 1210.
	// return = (1210 / 1100) - 1 = +10% (0.10)
	// Without the bug fix, it would have been: 
	// Saturday skipped. Monday cfMap empty. adjustedPrev = 1000.
	// return = (1210 / 1000) - 1 = +21% !!
	
	expectedReturn := 0.10
	if math.Abs(returns[0]-expectedReturn) > 1e-6 {
		t.Errorf("Expected return %f, got %f", expectedReturn, returns[0])
	}
}

func TestGetDailyReturns_DatesOffsetSkip(t *testing.T) {
	// Verify that if a day experiences `adjustedPrev <= 0` and is skipped,
	// the dates slice is not decoupled or offset.
	
	day1 := time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	day3 := day1.AddDate(0, 0, 2)
	day4 := day1.AddDate(0, 0, 3)

	data := &models.FlexQueryData{
		Trades: []models.Trade{
			{
				Symbol:          "TEST",
				Currency:        "USD",
				ListingExchange: "EX",
				BuySell:         "BUY",
				Quantity:        10,
				Price:           10,
				Proceeds:        -100,
				DateTime:        day1,
			},
			{
				Symbol:          "TEST",
				Currency:        "USD",
				ListingExchange: "EX",
				BuySell:         "SELL",
				Quantity:        -10,
				Price:           12,
				Proceeds:        120, // Value hits 0 explicitly
				DateTime:        day2,
			},
			{
				Symbol:          "TEST",
				Currency:        "USD",
				ListingExchange: "EX",
				BuySell:         "BUY",
				Quantity:        10,
				Price:           10,
				Proceeds:        -100, // Recover
				DateTime:        day4,
			},
		},
	}

	mockProvider := &mockMarketProvider{
		prices: map[string][]models.PricePoint{
			"TEST": {
				{Date: day1, Close: 10, AdjClose: 10},
				{Date: day2, Close: 12, AdjClose: 12}, 
				{Date: day3, Close: 10, AdjClose: 10}, 
				{Date: day4, Close: 10, AdjClose: 10}, 
			},
		},
	}

	svc := NewService(mockProvider, nil, nil)

	returns, startDates, endDates, err := svc.GetDailyReturns(data, day1, day4, "USD", models.AccountingModelOriginal)
	if err != nil {
		t.Fatalf("GetDailyReturns failed: %v", err)
	}

	// Expectation:
	// Day 1 to Day 2: prev=100. cur=0 (sold). cfAmount=120. adjustedPrev = 100 - 120 = -20. SKIPPED.
	// Day 2 to Day 3: prev=0. SKIPPED.
	// Day 3 to Day 4: prev=0. cfAmount=-100. adjustedPrev=100. cur=100. Return = 100/100-1 = 0%.
	
	if len(returns) != 1 {
		t.Fatalf("Expected strictly 1 evaluated return due to skips, got %d", len(returns))
	}
	
	// The core bug was that dates were decoupled. 
	// The start date corresponding to this recovered return MUST be day3, not day1.
	if startDates[0] != day3.Format("2006-01-02") {
		t.Errorf("Expected recovered period start date to be %v, got %s", day3.Format("2006-01-02"), startDates[0])
	}
	if endDates[0] != day4.Format("2006-01-02") {
		t.Errorf("Expected recovered period end date to be %v, got %s", day4.Format("2006-01-02"), endDates[0])
	}
}
