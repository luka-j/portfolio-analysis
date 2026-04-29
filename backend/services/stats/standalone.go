package stats

import "math"

// StandaloneMetrics holds risk metrics for a single return series,
// computed independently of any benchmark.
type StandaloneMetrics struct {
	SharpeRatio  float64
	VAMI         float64 // Value Added Monthly Index, indexed to 1000
	Volatility   float64 // Annualised standard deviation
	SortinoRatio float64
	MaxDrawdown  float64 // Worst peak-to-trough as a positive magnitude (e.g. 0.20 = 20%)
}

// CalculateStandaloneMetrics computes standalone risk metrics from a daily return series.
//
// returns is a slice of daily portfolio or security returns.
// riskFreeRate is the annualised risk-free rate (e.g. 0.05 for 5%).
//
// Returns zero-valued StandaloneMetrics if returns is empty.
func CalculateStandaloneMetrics(returns []float64, riskFreeRate float64) StandaloneMetrics {
	n := len(returns)
	if n == 0 {
		return StandaloneMetrics{}
	}

	dailyRf := math.Pow(1+riskFreeRate, 1.0/252.0) - 1

	pMean := mean(returns)
	pStd := stddev(returns)

	// Sharpe ratio (annualised).
	sharpe := 0.0
	if pStd > 1e-6 {
		sharpe = (pMean - dailyRf) * math.Sqrt(252) / pStd
	}

	// VAMI: compound all daily returns starting from an index of 1000.
	vami := 1000.0
	for _, r := range returns {
		vami *= (1 + r)
	}

	// Volatility: annualised standard deviation.
	volatility := pStd * math.Sqrt(252)

	// Sortino ratio (annualised).
	// Downside deviation uses only returns below the daily risk-free rate.
	var downsideSum float64
	for _, r := range returns {
		excess := r - dailyRf
		if excess < 0 {
			downsideSum += excess * excess
		}
	}
	downsideDev := math.Sqrt(downsideSum/float64(n)) * math.Sqrt(252)

	sortino := 0.0
	if downsideDev > 1e-6 {
		// Linear annualisation (consistent with Sharpe and common industry practice).
		excessAnnual := (pMean - dailyRf) * 252
		sortino = excessAnnual / downsideDev
	}

	// Maximum drawdown: largest peak-to-trough decline as a positive magnitude.
	wealth := 1.0
	peak := 1.0
	maxDD := 0.0
	for _, r := range returns {
		wealth *= (1 + r)
		if wealth > peak {
			peak = wealth
		}
		if peak > 0 {
			dd := (peak - wealth) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}

	return StandaloneMetrics{
		SharpeRatio:  sharpe,
		VAMI:         vami,
		Volatility:   volatility,
		SortinoRatio: sortino,
		MaxDrawdown:  maxDD,
	}
}
