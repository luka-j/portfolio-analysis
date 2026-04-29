package stats

import (
	"math"
	"math/big"
)

// BenchmarkMetrics holds comparison metrics between a portfolio and a benchmark.
type BenchmarkMetrics struct {
	Alpha            float64
	Beta             float64
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

	// Difference series (portfolio - benchmark) for tracking error / IR.
	diff := make([]float64, n)
	for i := range n {
		diff[i] = portfolioReturns[i] - benchmarkReturns[i]
	}

	// Beta = cov(Rp, Rb) / var(Rb).
	// Guard against near-zero variance: a benchmark with daily stddev below 1e-6
	// (0.0001%) is effectively flat. Dividing by near-zero variance yields an
	// astronomically large, meaningless beta that causes alpha to overflow to ±Inf.
	covPB := covariance(portfolioReturns, benchmarkReturns)
	varB := variance(benchmarkReturns)
	beta := 0.0
	if math.Sqrt(varB) > 1e-6 {
		beta = covPB / varB
	}

	// Alpha (annualised Jensen's alpha via compounding, not linear scaling).
	// alpha_daily = mean(Rp) - [Rf_daily + beta*(mean(Rb) - Rf_daily)]
	alphaDailyVal := pMean - (dailyRf + beta*(bMean-dailyRf))
	alpha := math.Pow(1+alphaDailyVal, 252) - 1

	// pStd is needed for the Correlation calculation below.
	pStd := stddev(portfolioReturns)

	// Treynor ratio (annualised).
	// Same threshold as beta: near-zero beta means no systematic exposure.
	// Uses linear annualisation of excess return (common industry practice) rather than compounding.
	treynor := 0.0
	if math.Abs(beta) > 1e-6 {
		treynor = (pMean - dailyRf) * 252 / beta
	}

	// Tracking error (annualised).
	diffStd := stddev(diff)
	te := diffStd * math.Sqrt(252)

	// Information ratio.
	// Guard: diffStd below 1e-6 means the portfolio tracks the benchmark almost
	// perfectly; dividing by near-zero TE would produce a meaningless ratio.
	ir := 0.0
	diffMean := mean(diff)
	if diffStd > 1e-6 {
		ir = diffMean * 252 / te
	}

	// Correlation. Reuse pStd; derive bStd from the already-computed varB.
	corr := 0.0
	bStd := math.Sqrt(varB)
	if pStd > 1e-6 && bStd > 1e-6 {
		corr = covPB / (pStd * bStd)
	}

	return BenchmarkMetrics{
		Alpha:            alpha,
		Beta:             beta,
		TreynorRatio:     treynor,
		TrackingError:    te,
		InformationRatio: ir,
		Correlation:      corr,
	}
}

// ---------- Math helpers (precise arithmetic via math/big) ----------
//
// All internal accumulation uses big.Float at bigPrec bits of precision
// (~38 significant decimal digits, vs float64's ~15). This eliminates the
// summation rounding errors that make the sample variance of a constant
// series appear non-zero in native float64 arithmetic.

// bigPrec is the precision in bits for internal big.Float calculations.
const bigPrec = uint(128)

// newF allocates a zero-valued big.Float at bigPrec precision.
func newF() *big.Float { return new(big.Float).SetPrec(bigPrec) }

// bigMean returns the arithmetic mean of xs as a big.Float.
// The caller owns the returned value.
func bigMean(xs []float64) *big.Float {
	n := len(xs)
	if n == 0 {
		return newF()
	}
	sum := newF()
	tmp := newF()
	for _, x := range xs {
		sum.Add(sum, tmp.SetFloat64(x))
	}
	return sum.Quo(sum, newF().SetInt64(int64(n)))
}

// mean returns the arithmetic mean of xs.
func mean(xs []float64) float64 {
	v, _ := bigMean(xs).Float64()
	return v
}

// variance returns the sample variance of xs (Bessel-corrected, n−1 denominator).
func variance(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	m := bigMean(xs)
	sum := newF()
	xi := newF()
	d := newF()
	sq := newF()
	for _, x := range xs {
		xi.SetFloat64(x)
		d.Sub(xi, m)
		sq.Mul(d, d)
		sum.Add(sum, sq)
	}
	sum.Quo(sum, newF().SetInt64(int64(n-1)))
	v, _ := sum.Float64()
	return v
}

// stddev returns the sample standard deviation of xs.
func stddev(xs []float64) float64 {
	return math.Sqrt(variance(xs))
}

// covariance returns the sample covariance between xs and ys.
func covariance(xs, ys []float64) float64 {
	n := len(xs)
	if n < 2 || n != len(ys) {
		return 0
	}
	mx := bigMean(xs)
	my := bigMean(ys)
	sum := newF()
	xi := newF()
	yi := newF()
	dx := newF()
	dy := newF()
	prod := newF()
	for i := 0; i < n; i++ {
		xi.SetFloat64(xs[i])
		yi.SetFloat64(ys[i])
		dx.Sub(xi, mx)
		dy.Sub(yi, my)
		prod.Mul(dx, dy)
		sum.Add(sum, prod)
	}
	sum.Quo(sum, newF().SetInt64(int64(n-1)))
	v, _ := sum.Float64()
	return v
}
