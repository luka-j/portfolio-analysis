package stats

// AttributionResult holds return-attribution data for one position.
type AttributionResult struct {
	Symbol       string
	AvgWeight    float64 // time-weighted average portfolio weight 0–1
	Return       float64 // price return over the period
	Contribution float64 // avg_weight × return (approximation of position's contribution to TWR)
}

// CalculateAttribution computes per-position attribution from aligned daily value slices.
//
// bySymbol maps posKey → daily value in display currency (length == len(totals)).
// totals is the portfolio total for each day (same length).
// Returns results sorted descending by absolute contribution.
func CalculateAttribution(bySymbol map[string][]float64, totals []float64) []AttributionResult {
	n := len(totals)
	if n == 0 {
		return nil
	}

	results := make([]AttributionResult, 0, len(bySymbol))

	for sym, vals := range bySymbol {
		if len(vals) != n {
			continue
		}

		// Time-weighted average weight: mean of (position_value / portfolio_total) across days
		// where portfolio total is positive.
		var weightSum float64
		var weightCount int
		for i := 0; i < n; i++ {
			if totals[i] > 1e-8 && vals[i] > 0 {
				weightSum += vals[i] / totals[i]
				weightCount++
			}
		}
		if weightCount == 0 {
			continue
		}
		avgWeight := weightSum / float64(weightCount)

		// Position return: find first and last non-zero values.
		var firstVal, lastVal float64
		for i := 0; i < n; i++ {
			if vals[i] > 1e-8 {
				firstVal = vals[i]
				break
			}
		}
		for i := n - 1; i >= 0; i-- {
			if vals[i] > 1e-8 {
				lastVal = vals[i]
				break
			}
		}

		posReturn := 0.0
		if firstVal > 1e-8 {
			posReturn = (lastVal - firstVal) / firstVal
		}

		contribution := avgWeight * posReturn

		results = append(results, AttributionResult{
			Symbol:       sym,
			AvgWeight:    avgWeight,
			Return:       posReturn,
			Contribution: contribution,
		})
	}

	// Sort by absolute contribution descending.
	for i := 1; i < len(results); i++ {
		for j := i; j > 0; j-- {
			ai := results[j].Contribution
			aj := results[j-1].Contribution
			if ai < 0 {
				ai = -ai
			}
			if aj < 0 {
				aj = -aj
			}
			if ai > aj {
				results[j], results[j-1] = results[j-1], results[j]
			} else {
				break
			}
		}
	}

	return results
}
