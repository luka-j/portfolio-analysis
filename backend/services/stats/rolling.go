package stats

// RollingPoint is a single observation in a rolling-metric time series.
type RollingPoint struct {
	Date  string
	Value float64
}

// CalculateRollingStandalone computes a windowed standalone metric (sharpe or volatility)
// over the provided daily return series. Each window produces one point placed at the
// last date in that window. Windows shorter than the requested window size are skipped.
// metric must be "sharpe" or "volatility".
func CalculateRollingStandalone(returns []float64, dates []string, window int, metric string, riskFreeRate float64) []RollingPoint {
	n := len(returns)
	if n == 0 || window <= 0 || window > n || len(dates) != n {
		return nil
	}

	out := make([]RollingPoint, 0, n-window+1)
	for i := window - 1; i < n; i++ {
		slice := returns[i-window+1 : i+1]
		m := CalculateStandaloneMetrics(slice, riskFreeRate)
		var val float64
		switch metric {
		case "sharpe":
			val = m.SharpeRatio
		case "volatility":
			val = m.Volatility
		case "sortino":
			val = m.SortinoRatio
		default:
			val = m.SharpeRatio
		}
		out = append(out, RollingPoint{Date: dates[i], Value: val})
	}
	return out
}

// CalculateRollingBeta computes a windowed beta of portfolio returns relative to benchmark
// returns. Both slices must be the same length (pre-aligned by the caller). windows shorter
// than the requested size are skipped.
func CalculateRollingBeta(portfolioReturns, benchmarkReturns []float64, dates []string, window int, riskFreeRate float64) []RollingPoint {
	n := len(portfolioReturns)
	if n == 0 || window <= 0 || window > n || len(dates) != n || len(benchmarkReturns) != n {
		return nil
	}

	out := make([]RollingPoint, 0, n-window+1)
	for i := window - 1; i < n; i++ {
		pSlice := portfolioReturns[i-window+1 : i+1]
		bSlice := benchmarkReturns[i-window+1 : i+1]
		m := CalculateBenchmarkMetrics(pSlice, bSlice, riskFreeRate)
		out = append(out, RollingPoint{Date: dates[i], Value: m.Beta})
	}
	return out
}
