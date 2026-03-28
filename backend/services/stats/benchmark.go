package stats

import (
	"math"
)

// BenchmarkMetrics holds comparison metrics between a portfolio and a benchmark.
type BenchmarkMetrics struct {
	Alpha            float64
	Beta             float64
	SharpeRatio      float64
	TreynorRatio     float64
	TrackingError    float64
	InformationRatio float64
	Correlation      float64
}

// CalculateBenchmarkMetrics computes risk-adjusted comparison metrics.
//
// portfolioReturns and benchmarkReturns must be aligned daily return series
// of the same length, already converted to the same target currency.
//
// riskFreeRate is the annualised risk-free rate (e.g. 0.05 for 5%).
func CalculateBenchmarkMetrics(portfolioReturns, benchmarkReturns []float64, riskFreeRate float64) BenchmarkMetrics {
	n := len(portfolioReturns)
	if n == 0 || n != len(benchmarkReturns) {
		return BenchmarkMetrics{}
	}

	dailyRf := math.Pow(1+riskFreeRate, 1.0/252.0) - 1

	// Means.
	pMean := mean(portfolioReturns)
	bMean := mean(benchmarkReturns)

	// Excess returns over risk-free rate.
	excessP := make([]float64, n)
	excessB := make([]float64, n)
	diff := make([]float64, n) // portfolio - benchmark
	for i := 0; i < n; i++ {
		excessP[i] = portfolioReturns[i] - dailyRf
		excessB[i] = benchmarkReturns[i] - dailyRf
		diff[i] = portfolioReturns[i] - benchmarkReturns[i]
	}

	// Beta = cov(Rp, Rb) / var(Rb).
	// Guard against floating-point near-zero variance: a benchmark with daily stddev
	// below 1e-6 (0.0001%) is effectively flat (e.g. all-zero returns with fp rounding
	// errors). Dividing by such a tiny variance yields an astronomically large, meaningless
	// beta, which then causes alpha to overflow to ±Inf. Treat it as zero instead.
	covPB := covariance(portfolioReturns, benchmarkReturns)
	varB := variance(benchmarkReturns)
	beta := 0.0
	if math.Sqrt(varB) > 1e-6 {
		beta = covPB / varB
	}

	// Alpha (annualised Jensen's alpha via compounding, not linear scaling)
	// alpha_daily = mean(Rp) - [Rf_daily + beta*(mean(Rb) - Rf_daily)]
	alphaDailyVal := pMean - (dailyRf + beta*(bMean-dailyRf))
	alpha := math.Pow(1+alphaDailyVal, 252) - 1

	// Sharpe ratio (annualised)
	pStd := stddev(portfolioReturns)
	sharpe := 0.0
	if pStd > 0 {
		sharpe = (pMean - dailyRf) * math.Sqrt(252) / pStd
	}

	// Treynor ratio (annualised)
	treynor := 0.0
	if beta != 0 {
		treynor = (pMean - dailyRf) * 252 / beta
	}

	// Tracking error (annualised)
	te := stddev(diff) * math.Sqrt(252)

	// Information ratio
	ir := 0.0
	diffMean := mean(diff)
	if te > 0 {
		ir = diffMean * 252 / te
	}

	// Correlation
	corr := 0.0
	pStdCalc := stddev(portfolioReturns)
	bStdCalc := stddev(benchmarkReturns)
	if pStdCalc > 0 && bStdCalc > 0 {
		corr = covPB / (pStdCalc * bStdCalc)
	}

	return BenchmarkMetrics{
		Alpha:            alpha,
		Beta:             beta,
		SharpeRatio:      sharpe,
		TreynorRatio:     treynor,
		TrackingError:    te,
		InformationRatio: ir,
		Correlation:      corr,
	}
}

// ---------- Math helpers ----------

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func variance(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	sum := 0.0
	for _, x := range xs {
		d := x - m
		sum += d * d
	}
	return sum / float64(len(xs)-1)
}

func stddev(xs []float64) float64 {
	return math.Sqrt(variance(xs))
}

func covariance(xs, ys []float64) float64 {
	n := len(xs)
	if n < 2 || n != len(ys) {
		return 0
	}
	mx := mean(xs)
	my := mean(ys)
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += (xs[i] - mx) * (ys[i] - my)
	}
	return sum / float64(n-1)
}
