package stats

import "math"

// DrawdownPoint is a single day in a drawdown time series.
type DrawdownPoint struct {
	Date        string
	DrawdownPct float64 // negative fraction, e.g. -0.15 = 15% below peak
	Peak        float64
	Wealth      float64
}

// CalculateDrawdownSeries builds a per-day drawdown series from a daily return series.
// dates and returns must be the same length; dates[i] is the date for returns[i].
func CalculateDrawdownSeries(returns []float64, dates []string) []DrawdownPoint {
	n := len(returns)
	if n == 0 || len(dates) != n {
		return nil
	}

	out := make([]DrawdownPoint, n)
	wealth := 1.0
	peak := 1.0

	for i, r := range returns {
		wealth *= (1 + r)
		if wealth > peak {
			peak = wealth
		}
		dd := 0.0
		if peak > 1e-12 {
			dd = (wealth - peak) / peak // negative or zero
		}
		// Round near-zero to zero to avoid floating-point noise.
		if math.Abs(dd) < 1e-10 {
			dd = 0
		}
		out[i] = DrawdownPoint{
			Date:        dates[i],
			DrawdownPct: dd,
			Peak:        peak,
			Wealth:      wealth,
		}
	}
	return out
}
