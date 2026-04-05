package stats

import (
	"math"
	"testing"
	"time"

	"portfolio-analysis/models"

	"github.com/stretchr/testify/assert"
)

func TestCalculateMWR_EdgeCases(t *testing.T) {
	// Base date
	d1 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	d2 := d1.AddDate(1, 0, 0) // Exactly 1 year later

	t.Run("Zero cashflows", func(t *testing.T) {
		mwr, err := CalculateMWR([]models.CashFlow{}, 1000, d2)
		assert.Error(t, err)
		assert.Equal(t, 0.0, mwr)
	})

	t.Run("Exact 0% return", func(t *testing.T) {
		// Deposit 1000, end with 1000 after 1 year.
		cf := []models.CashFlow{
			{Date: d1, Amount: -1000},
		}
		mwr, err := CalculateMWR(cf, 1000, d2)
		assert.NoError(t, err)
		assert.InDelta(t, 0.0, mwr, 1e-9)
	})

	t.Run("Simple 10% return", func(t *testing.T) {
		// Deposit 1000, end with 1100 after 1 year.
		cf := []models.CashFlow{
			{Date: d1, Amount: -1000},
		}
		mwr, err := CalculateMWR(cf, 1100, d2)
		assert.NoError(t, err)
		assert.InDelta(t, 0.1, mwr, 1e-9)
	})

	t.Run("Heavily negative cashflows (withdrawals exceeding deposits)", func(t *testing.T) {
		// Deposit 1000, Withdraw 2000 (maybe from huge gains), end with 500.
		// Result should be positive and high.
		cf := []models.CashFlow{
			{Date: d1, Amount: -1000},
			{Date: d1.AddDate(0, 6, 0), Amount: 2000}, // Withdraw 2000 halfway
		}
		mwr, err := CalculateMWR(cf, 500, d2)
		assert.NoError(t, err)
		assert.Greater(t, mwr, 1.0) // Significant gain
	})

	t.Run("Extreme loss (near -90%)", func(t *testing.T) {
		cf := []models.CashFlow{
			{Date: d1, Amount: -1000},
		}
		mwr, err := CalculateMWR(cf, 100, d2) // 90% loss
		assert.NoError(t, err)
		assert.InDelta(t, -0.9, mwr, 1e-4)
	})

	t.Run("Extremely short time horizon", func(t *testing.T) {
		shortEnd := d1.Add(time.Hour) // 1 hour later
		cf := []models.CashFlow{
			{Date: d1, Amount: -1000},
		}
		mwr, err := CalculateMWR(cf, 1001, shortEnd)
		assert.NoError(t, err)
		// 0.1% return in 1 hour annualises to a massive IRR, but our implementation
		// returns the period return math.Pow(1+r, T) - 1.0
		// which should be approx 0.001
		assert.InDelta(t, 0.001, mwr, 1e-9)
	})
	
	t.Run("Empty portfolio with withdrawal", func(t *testing.T) {
		// This might cause Newton's method to struggle or converge to odd values.
		cf := []models.CashFlow{
			{Date: d1, Amount: 1000}, // Withdrawal without deposit
		}
		_, err := CalculateMWR(cf, 0, d2)
		// Should likely error or handle gracefully
		assert.Error(t, err)
	})
}

func TestCalculateTWR_EdgeCases(t *testing.T) {
	t.Run("Require at least 2 daily values", func(t *testing.T) {
		twr, err := CalculateTWR([]models.DailyValue{{Date: "2023-01-01", Value: 100}}, nil)
		assert.Error(t, err)
		assert.Equal(t, 0.0, twr)
	})

	t.Run("Exact 0% return", func(t *testing.T) {
		dv := []models.DailyValue{
			{Date: "2023-01-01", Value: 1000},
			{Date: "2023-01-02", Value: 1000},
		}
		twr, err := CalculateTWR(dv, nil)
		assert.NoError(t, err)
		assert.InDelta(t, 0.0, twr, 1e-9)
	})

	t.Run("Simple growth with no cashflows", func(t *testing.T) {
		dv := []models.DailyValue{
			{Date: "2023-01-01", Value: 1000},
			{Date: "2023-01-02", Value: 1100},
		}
		twr, err := CalculateTWR(dv, nil)
		assert.NoError(t, err)
		assert.InDelta(t, 0.1, twr, 1e-9)
	})

	t.Run("TWR with mid-period cashflow", func(t *testing.T) {
		// Day 1: 1000
		// Day 2 morning: Deposit 500 (Total 1500 base)
		// Day 2 evening: 1650
		// Return for Day 1->2 should be 10% (1650 / 1500 - 1)
		dv := []models.DailyValue{
			{Date: "2023-01-01", Value: 1000},
			{Date: "2023-01-02", Value: 1650},
		}
		cf := []models.CashFlow{
			{Date: time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC), Amount: -500}, // Deposit is negative in models.CashFlow for MWR... 
			// Wait, check returning.go line 110: cfAmount := cfDates[dateStr]
			// line 112: adjustedPrevValue := prevValue - cfAmount
			// If deposit is negative, - (-500) = +500. Correct.
		}
		twr, err := CalculateTWR(dv, cf)
		assert.NoError(t, err)
		assert.InDelta(t, 0.1, twr, 1e-9)
	})

	t.Run("Initial deposit handling", func(t *testing.T) {
		// Day 1: 0 value, but we deposit 1000.
		// Day 2: 1100.
		// Return should be 10%.
		dv := []models.DailyValue{
			{Date: "2023-01-01", Value: 0},
			{Date: "2023-01-02", Value: 1100},
		}
		cf := []models.CashFlow{
			{Date: time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC), Amount: -1000},
		}
		twr, err := CalculateTWR(dv, cf)
		assert.NoError(t, err)
		assert.InDelta(t, 0.1, twr, 1e-9)
	})

	t.Run("Weekend cash flow skipped from daily dates but accumulated correctly", func(t *testing.T) {
		// Simulate a situation where market is closed on the weekend.
		// Day 1 (Friday): Portfolio ends at 1000.
		// Day 2 (Saturday): Deposit 500 (ESPP/RSU vest lands on a Saturday). No pricing available.
		// Day 3 (Monday): Portfolio opens at 1650.
		// Return for Friday -> Monday is evaluated correctly incorporating Saturday's cash flow.
		// (1650 / (1000 + 500)) - 1 = 10%
		dv := []models.DailyValue{
			{Date: "2023-01-06", Value: 1000}, // Friday
			{Date: "2023-01-09", Value: 1650}, // Monday
		}
		cf := []models.CashFlow{
			{Date: time.Date(2023, 1, 7, 0, 0, 0, 0, time.UTC), Amount: -500}, // Saturday
		}
		twr, err := CalculateTWR(dv, cf)
		assert.NoError(t, err)
		assert.InDelta(t, 0.1, twr, 1e-9)
	})

	t.Run("Multiple missing days and multiple cash flows", func(t *testing.T) {
		// Day 1: 1000
		// Gap containing two deposits and one withdrawal
		// Day 5: 2200
		dv := []models.DailyValue{
			{Date: "2023-01-01", Value: 1000},
			{Date: "2023-01-05", Value: 2200},
		}
		cf := []models.CashFlow{
			{Date: time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC), Amount: -500}, // Deposit
			{Date: time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC), Amount: -700}, // Deposit
			{Date: time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC), Amount: 200},  // Withdrawal
		}
		// Net cashflow: -1000 (net deposit of 1000)
		// Base becomes 1000 + 1000 = 2000
		// End is 2200. Return is 2200 / 2000 - 1 = 10%
		twr, err := CalculateTWR(dv, cf)
		assert.NoError(t, err)
		assert.InDelta(t, 0.1, twr, 1e-9)
	})
}

func TestCalculateBenchmarkMetrics_EdgeCases(t *testing.T) {
	t.Run("Empty returns", func(t *testing.T) {
		metrics := CalculateBenchmarkMetrics([]float64{}, []float64{}, 0.05)
		assert.Equal(t, 0.0, metrics.Beta)
		assert.Equal(t, 0.0, metrics.Alpha)
	})

	t.Run("Mismatched lengths", func(t *testing.T) {
		metrics := CalculateBenchmarkMetrics([]float64{0.01}, []float64{0.01, 0.02}, 0.05)
		assert.Equal(t, 0.0, metrics.Beta)
	})

	t.Run("Perfect correlation", func(t *testing.T) {
		p := []float64{0.01, 0.02, -0.01, 0.03}
		b := []float64{0.01, 0.02, -0.01, 0.03}
		metrics := CalculateBenchmarkMetrics(p, b, 0.0)
		assert.InDelta(t, 1.0, metrics.Beta, 1e-9)
		assert.InDelta(t, 0.0, metrics.Alpha, 1e-9)
		assert.InDelta(t, 1.0, metrics.Correlation, 1e-9)
	})

	t.Run("Negative beta", func(t *testing.T) {
		p := []float64{0.01, -0.02, 0.01}
		b := []float64{-0.01, 0.02, -0.01}
		metrics := CalculateBenchmarkMetrics(p, b, 0.0)
		assert.Less(t, metrics.Beta, 0.0)
		assert.InDelta(t, -1.0, metrics.Correlation, 1e-9)
	})

	t.Run("Zero volatility benchmark", func(t *testing.T) {
		p := []float64{0.01, 0.02, 0.03}
		b := []float64{0.0, 0.0, 0.0}
		metrics := CalculateBenchmarkMetrics(p, b, 0.0)
		assert.Equal(t, 0.0, metrics.Beta)
		assert.Equal(t, 0.0, metrics.Correlation)
	})
}

func TestAlphaDoesNotOverflow(t *testing.T) {
	t.Run("Flat benchmark all-zero returns", func(t *testing.T) {
		// EUR_BENCH scenario: FX-corrected benchmark has exactly 0% returns every day.
		// varB should be treated as zero to avoid division by near-zero float.
		n := 140
		bRet := make([]float64, n)
		pRet := make([]float64, n)
		for i := range pRet {
			pRet[i] = 0.0003
		}
		metrics := CalculateBenchmarkMetrics(pRet, bRet, 0.025)
		assert.False(t, math.IsInf(metrics.Alpha, 0), "alpha must not be Inf for flat benchmark")
		assert.False(t, math.IsNaN(metrics.Alpha), "alpha must not be NaN for flat benchmark")
		assert.Equal(t, 0.0, metrics.Beta, "beta must be 0 for flat benchmark")
	})

	t.Run("Near-zero variance from floating-point rounding", func(t *testing.T) {
		// Benchmark returns are near-zero due to fp rounding (e.g. (100/x)*x - 1 ≈ 1e-16).
		// varB is positive but tiny; without the stddev threshold beta would explode.
		n := 213
		bRet := make([]float64, n)
		pRet := make([]float64, n)
		for i := range pRet {
			pRet[i] = 0.0003
			bRet[i] = 1e-16 * float64(i%3-1) // fp-scale noise: -1e-16, 0, +1e-16 cycling
		}
		metrics := CalculateBenchmarkMetrics(pRet, bRet, 0.025)
		assert.False(t, math.IsInf(metrics.Alpha, 0), "alpha must not be Inf for fp-noise benchmark")
		assert.False(t, math.IsNaN(metrics.Alpha), "alpha must not be NaN for fp-noise benchmark")
		// Beta should be zero (stddev(bRet) << 1e-6 threshold)
		assert.Equal(t, 0.0, metrics.Beta)
	})
}
