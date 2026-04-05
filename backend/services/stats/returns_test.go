package stats_test

import (
	"math"
	"testing"
	"time"

	"portfolio-analysis/models"
	"portfolio-analysis/services/stats"
)

// parseDate makes time.Time creation inline-friendly.
func parseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// approxEqual compares two floats with a tolerance.
func approxEqual(a, b float64) bool {
	// 0.005 % tolerance is suitable for iterative IRR calculations
	return math.Abs(a-b) < 1e-4
}

func TestCalculateMWR(t *testing.T) {
	tests := []struct {
		name        string
		cashFlows   []models.CashFlow
		endValue    float64
		endDate     time.Time
		expectError bool
		expected    float64 // Expected unannualized period return
	}{
		{
			name: "Simple 1 year 10% gain",
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: -100},
			},
			endDate:     parseDate("2021-01-01"), // Roughly 1 year
			endValue:    110,
			expectError: false,
			expected:    0.10,
		},
		{
			name: "Simple half year 10% period gain (not annualized)",
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: -100},
			},
			endDate:     parseDate("2020-07-02"), // About 0.5 years
			endValue:    110,
			expectError: false,
			expected:    0.10, // Unannualized period return is still +10%
		},
		{
			name: "Total loss resulting in -100%",
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: -100},
			},
			endDate:     parseDate("2021-01-01"),
			endValue:    0,
			expectError: false,
			expected:    -1.0,
		},
		{
			name: "No return scenario with intermediate withdrawal",
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: -100}, // Initial Deposit
				{Date: parseDate("2020-07-01"), Amount: 50},   // Withdraw 50 exactly half-way
			},
			endDate:     parseDate("2021-01-01"),
			endValue:    50, // Ends up with 50, so net zero gain
			expectError: false,
			expected:    0.0,
		},
		{
			name: "No return scenario with intermediate deposit",
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: -100}, // Initial Deposit
				{Date: parseDate("2020-07-01"), Amount: -100}, // Deposit another 100 half-way
			},
			endDate:     parseDate("2021-01-01"),
			endValue:    200, // Ends up with 200, so net zero gain
			expectError: false,
			expected:    0.0,
		},
		{
			name: "Gain with heavy mid-year withdrawal vs baseline",
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: -100}, // Initial Deposit
				{Date: parseDate("2020-07-01"), Amount: 110},  // Withdraw 110 (10% gain realized early)
			},
			endDate:     parseDate("2021-01-01"),
			endValue:    0, // Nothing left, IRR is approx 21.1% annualized. Over 1 year -> 21.1%.
			expectError: false,
			expected:    0.211267,
		},
		{
			name:        "Error: no cash flows",
			cashFlows:   []models.CashFlow{},
			endDate:     parseDate("2021-01-01"),
			endValue:    100,
			expectError: true,
		},
		{
			name: "Error: only withdrawal, no deposit or start value",
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: 50}, // Withdrawal
			},
			endDate:     parseDate("2021-01-01"),
			endValue:    0,
			expectError: true, // Cannot compute IRR without capital investment
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := stats.CalculateMWR(tc.cashFlows, tc.endValue, tc.endDate)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !approxEqual(res, tc.expected) {
				t.Errorf("expected return approx %v, got %v", tc.expected, res)
			}
		})
	}
}

func TestCalculateTWR(t *testing.T) {
	tests := []struct {
		name        string
		dailyValues []models.DailyValue
		cashFlows   []models.CashFlow
		expectError bool
		expected    float64
	}{
		{
			name: "Simple 10% daily growth, no cashflows",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-01", Value: 100},
				{Date: "2020-01-02", Value: 105}, // 5% gain
				{Date: "2020-01-03", Value: 110}, // overall ~ 10% vs start (110/100)
			},
			cashFlows:   []models.CashFlow{},
			expectError: false,
			expected:    0.10,
		},
		{
			name: "Intra-period deposit neutralized",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-01", Value: 100},
				{Date: "2020-01-02", Value: 165}, // Grew 10% (100->110) AND deposited 50 (grew 10% -> 55). Total 165.
			},
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-02"), Amount: -50}, // Deposit 50
			},
			expectError: false,
			expected:    0.10, // Adjusted basis = 100 + 50 = 150. Return = 165/150 - 1 = 10% (0.10)
		},
		{
			name: "Intra-period withdrawal neutralized",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-01", Value: 100},
				{Date: "2020-01-02", Value: 66}, // Grew 10% (100->110) AND withdrew 40 (grew -> -44). Output total 66.
				// Wait, withdrawal of 40. Adjusted basis = 100 - 40 = 60.
				// 66 / 60 = 1.10 -> 10% gain.
			},
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-02"), Amount: 40}, // Withdraw 40
			},
			expectError: false,
			expected:    0.10,
		},
		{
			name: "Multiple sub-periods compounding",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-01", Value: 100},
				{Date: "2020-01-02", Value: 110}, // 10% gain
				{Date: "2020-01-03", Value: 132}, // Another 20% gain. Total 10% then 20% -> 1.1 * 1.2 = 1.32 (32% gain)
			},
			cashFlows:   []models.CashFlow{},
			expectError: false,
			expected:    0.32,
		},
		{
			name: "Heavy intra-period cashflow crossing 0 effectively",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-01", Value: 100},
				{Date: "2020-01-02", Value: 50}, // Assume -50% loss. From 100 to 50.
				{Date: "2020-01-03", Value: 151}, // Then we deposit 100 on top of 50 -> Basis = 150. End = 151. Very minor gain.
			},
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-03"), Amount: -100}, // Deposit 100
			},
			expectError: false,
			expected:    -0.49666, // (50/100) * (151/150) = 0.5 * 1.00666 = 0.503333 -> -49.666% return
		},
		{
			name: "Not enough daily values",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-01", Value: 100},
			},
			cashFlows:   []models.CashFlow{},
			expectError: true,
		},
		{
			name: "CashFlow before inception dropped",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-02", Value: 100},
				{Date: "2020-01-03", Value: 110}, // 10% return
			},
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-01"), Amount: -1000}, // Ignored because it's before matching the first daily value
			},
			expectError: false,
			expected:    0.10,
		},
		{
			name: "Multiple CashFlows same day",
			dailyValues: []models.DailyValue{
				{Date: "2020-01-01", Value: 100},
				{Date: "2020-01-02", Value: 165}, // Start 100, Net Deposit: +50 -> Base: 150. End: 165 -> 10%
			},
			cashFlows: []models.CashFlow{
				{Date: parseDate("2020-01-02"), Amount: -100}, // Deposit 100
				{Date: parseDate("2020-01-02"), Amount: 50},   // Withdraw 50 -> net -50 deposit
			},
			expectError: false,
			expected:    0.10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := stats.CalculateTWR(tc.dailyValues, tc.cashFlows)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !approxEqual(res, tc.expected) {
				t.Errorf("expected return approx %v, got %v", tc.expected, res)
			}
		})
	}
}
