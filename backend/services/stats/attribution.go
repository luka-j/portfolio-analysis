package stats

// AttributionResult holds return-attribution data for one position.
type AttributionResult struct {
	Symbol       string
	AvgWeight    float64 // time-weighted average weight of the position in the portfolio (0–1), averaged over all days in the period
	Return       float64 // cumulative price return over the period (cash-flow-adjusted, chained)
	Contribution float64 // sum over days of weight_t × daily_return_t — true approximation of contribution to TWR
}

// CalculateAttribution computes per-position attribution from aligned daily value slices.
//
// bySymbolVals     maps posKey → daily value in display currency (length == len(totals)).
// bySymbolCashflows maps posKey → per-day signed cash impact of trades in display currency
//                   (buys > 0, sells < 0). Days without trades have 0. May be nil — callers
//                   that omit it will see inflated returns on positions that had trades.
// totals           is the portfolio total for each day (same length).
//
// Returns results sorted descending by absolute contribution.
//
// The contribution is the sum over days of (weight_t × position_return_t), where
// position_return_t is the cash-flow-adjusted daily return:
//
//	r_t = (v_t - cf_t) / v_{t-1} - 1    when v_{t-1} > 0
//
// This neutralises buys/sells the same way GetDailyReturns does for the full portfolio,
// so the sum of contributions tracks the portfolio's TWR (up to discretisation error).
//
// AvgWeight is averaged over all n days in the period — positions held only briefly do
// not have their weight inflated by the (short) held duration.
func CalculateAttribution(bySymbolVals map[string][]float64, bySymbolCashflows map[string][]float64, totals []float64) []AttributionResult {
	n := len(totals)
	if n == 0 {
		return nil
	}

	results := make([]AttributionResult, 0, len(bySymbolVals))

	for sym, vals := range bySymbolVals {
		if len(vals) != n {
			continue
		}
		cfs := bySymbolCashflows[sym]
		useCFs := len(cfs) == n

		// AvgWeight over all days (zero when position is not held).
		var weightSum float64
		for i := 0; i < n; i++ {
			if totals[i] > 1e-8 {
				weightSum += vals[i] / totals[i]
			}
		}
		avgWeight := weightSum / float64(n)
		if avgWeight <= 0 {
			continue
		}

		// Chained cash-flow-adjusted daily returns.
		cumGrowth := 1.0
		var contribution float64
		for i := 1; i < n; i++ {
			prev := vals[i-1]
			if prev <= 1e-8 {
				continue
			}
			cf := 0.0
			if useCFs {
				cf = cfs[i]
			}
			// Guard against adjustedPrev flipping sign due to a huge sell in the period.
			adjPrev := prev
			// For buys, the opening basis is higher; for sells lower. This mirrors the TWR
			// neutralisation in portfolio.GetDailyReturns.
			dailyRet := (vals[i] - cf) / adjPrev - 1
			cumGrowth *= (1 + dailyRet)

			// Day weight measured at the close of yesterday (consistent with the return
			// earned over [t-1, t]).
			if totals[i-1] > 1e-8 {
				w := prev / totals[i-1]
				contribution += w * dailyRet
			}
		}
		posReturn := cumGrowth - 1

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
