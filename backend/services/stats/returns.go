package stats

import (
	"fmt"
	"math"
	"time"

	"portfolio-analysis/models"
)

// ---------- MWR (Money-Weighted Return / IRR) ----------

// CalculateMWR computes the money-weighted return (internal rate of return)
// given external cash flows and the ending portfolio value.
// Cash flows should have negative amounts for deposits and positive for withdrawals.
// The function uses Newton's method to solve for the annualised IRR.
func CalculateMWR(cashFlows []models.CashFlow, endValue float64, endDate time.Time) (float64, error) {
	if len(cashFlows) == 0 {
		return 0, fmt.Errorf("no cash flows for MWR calculation")
	}

	startDate := cashFlows[0].Date

	// Basic validation: we need at least one deposit (negative amount) or some starting value.
	hasDeposit := false
	for _, cf := range cashFlows {
		if cf.Amount < 0 {
			hasDeposit = true
			break
		}
	}
	if !hasDeposit && endValue <= 0 {
		return 0, fmt.Errorf("no deposits or ending value for MWR calculation")
	}

	// Fast-path edge case: total loss with no withdrawals
	totalWithdrawals := 0.0
	for _, cf := range cashFlows {
		if cf.Amount > 0 {
			totalWithdrawals += cf.Amount
		}
	}
	if totalWithdrawals == 0 && endValue <= 0 {
		return -1.0, nil
	}

	// NPV(r) = sum_i CF_i / (1+r)^t_i + endValue / (1+r)^T = 0
	// where t_i is in years from the first cash flow.
	yearFrac := func(d time.Time) float64 {
		return d.Sub(startDate).Hours() / (365.25 * 24)
	}

	T := yearFrac(endDate)
	if T <= 0 {
		return 0, fmt.Errorf("end date must be after first cash flow")
	}

	npv := func(r float64) float64 {
		sum := 0.0
		for _, cf := range cashFlows {
			t := yearFrac(cf.Date)
			sum += cf.Amount / math.Pow(1+r, t)
		}
		sum += endValue / math.Pow(1+r, T)
		return sum
	}

	dnpv := func(r float64) float64 {
		sum := 0.0
		for _, cf := range cashFlows {
			t := yearFrac(cf.Date)
			sum -= t * cf.Amount / math.Pow(1+r, t+1)
		}
		sum -= T * endValue / math.Pow(1+r, T+1)
		return sum
	}

	// Newton's method.
	r := 0.1 // initial guess
	converged := false
	for i := 0; i < 1000; i++ {
		f := npv(r)
		fp := dnpv(r)

		// If derivative is too small or we hit NaN/Inf, stop.
		if math.Abs(fp) < 1e-15 || math.IsNaN(f) || math.IsInf(f, 0) {
			break
		}

		rNew := r - f/fp

		// Safeguard: r must be > -1 (cannot lose more than 100% annualised)
		if rNew <= -0.999 {
			rNew = -0.999
		}

		if math.Abs(rNew-r) < 1e-10 {
			r = rNew
			converged = true
			break
		}
		r = rNew
	}

	if !converged {
		// If it broke early due to vanishing derivative, or hit max iterations, check if we're actually close to a solution.
		// Normalize NPV by portfolio scale (ending value + absolute sum of cash flows) to make the threshold relative.
		scale := math.Abs(endValue)
		for _, cf := range cashFlows {
			scale += math.Abs(cf.Amount)
		}
		if scale == 0 {
			scale = 1.0
		}

		if math.Abs(npv(r))/scale > 1e-5 && (math.IsNaN(r) || math.IsInf(r, 0) || !converged) {
			return 0, fmt.Errorf("MWR did not converge")
		}
	}

	// Convert annualized IRR 'r' into the unannualized period return matching TWR.
	periodReturn := math.Pow(1+r, T) - 1.0

	return periodReturn, nil
}

// ---------- TWR (Time-Weighted Return) ----------

// CalculateTWR computes the time-weighted return by chaining sub-period returns
// between external cash-flow events.
// dailyValues should be a time-ordered slice of portfolio values.
// cashFlows should be sorted by date.
// Returns the total TWR (not annualised).
func CalculateTWR(dailyValues []models.DailyValue, cashFlows []models.CashFlow) (float64, error) {
	if len(dailyValues) < 2 {
		return 0, fmt.Errorf("need at least 2 daily values for TWR")
	}

	cfIdx := 0
	// Skip any cash flows that occur on or before the first daily value date.
	for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= dailyValues[0].Date {
		cfIdx++
	}

	product := 1.0

	for i := 1; i < len(dailyValues); i++ {
		prevValue := dailyValues[i-1].Value
		curValue := dailyValues[i].Value
		dateStr := dailyValues[i].Date

		// Accumulate any cash flows that occurred strictly after the previous period's date
		// and on or before the current period's date.
		cfAmount := 0.0
		for cfIdx < len(cashFlows) && cashFlows[cfIdx].Date.Format("2006-01-02") <= dateStr {
			cfAmount += cashFlows[cfIdx].Amount
			cfIdx++
		}

		adjustedPrevValue := prevValue - cfAmount

		// If the portfolio was effectively empty yesterday but money was deposited today,
		// adjustedPrevValue will become positive, representing the true cost basis for the day.
		if adjustedPrevValue > 0 {
			subReturn := curValue / adjustedPrevValue
			product *= subReturn
		}
	}

	twr := product - 1.0
	return twr, nil
}
