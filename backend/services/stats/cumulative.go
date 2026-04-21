package stats

// CumulativePoint is a single observation in a cumulative-return series.
//
// Value is the cumulative return as a percentage (e.g. 12.5 means +12.5%),
// starting from 0 on the first date.
type CumulativePoint struct {
	Date  string
	Value float64
}

// CalculateCumulativeReturnSeries chain-compounds a daily return series into a cumulative
// percentage series. Output length equals len(returns)+1 only when firstDate is non-empty;
// otherwise it equals len(returns) and the first point is the return after the first period.
//
// When a firstDate is provided, the series is prepended with a 0% point on that date —
// useful for charting where the starting datum is the baseline. When firstDate is empty,
// the output aligns with dates[i] for i in [0, len(returns)).
//
// Returns nil when inputs are inconsistent.
func CalculateCumulativeReturnSeries(returns []float64, dates []string, firstDate string) []CumulativePoint {
	if len(returns) != len(dates) {
		return nil
	}
	var out []CumulativePoint
	if firstDate != "" {
		out = make([]CumulativePoint, 0, len(returns)+1)
		out = append(out, CumulativePoint{Date: firstDate, Value: 0})
	} else {
		out = make([]CumulativePoint, 0, len(returns))
	}
	growth := 1.0
	for i, r := range returns {
		growth *= (1 + r)
		out = append(out, CumulativePoint{Date: dates[i], Value: (growth - 1) * 100})
	}
	return out
}
