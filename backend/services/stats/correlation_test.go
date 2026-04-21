package stats

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// buildMatrix is a test helper that calls CalculateCorrelationMatrix and returns the
// correlation value for symbol pair (a, b) from the result.
func buildMatrix(t *testing.T, returns map[string][]float64, masks map[string][]bool, minObs int) map[string]map[string]float64 {
	t.Helper()
	res := CalculateCorrelationMatrix(returns, masks, minObs)
	out := make(map[string]map[string]float64, len(res.Symbols))
	for i, si := range res.Symbols {
		out[si] = make(map[string]float64, len(res.Symbols))
		for j, sj := range res.Symbols {
			out[si][sj] = res.Matrix[i][j]
		}
	}
	return out
}

func TestCalculateCorrelationMatrix_OverlappingWindow(t *testing.T) {
	// A is held for 5 days; B is held only during days 0-2.
	// During the 3 overlapping days both series are identical → r should be 1.0.
	// Without the mask fix, the 2 zero days in B would dilute / distort the result.
	retsA := []float64{0.01, 0.02, -0.01, 0.03, -0.02}
	retsB := []float64{0.01, 0.02, -0.01, 0.0, 0.0} // zeros for non-active days
	maskA := []bool{true, true, true, true, true}
	maskB := []bool{true, true, true, false, false}

	m := buildMatrix(t,
		map[string][]float64{"A": retsA, "B": retsB},
		map[string][]bool{"A": maskA, "B": maskB},
		3, // minObs = 3
	)

	assert.InDelta(t, 1.0, m["A"]["B"], 1e-9,
		"correlation over identical overlapping sub-series must be 1.0")
	assert.InDelta(t, 1.0, m["B"]["A"], 1e-9, "matrix must be symmetric")
	assert.InDelta(t, 1.0, m["A"]["A"], 1e-9, "diagonal must be 1.0")
}

func TestCalculateCorrelationMatrix_NoOverlap(t *testing.T) {
	// A and B are never held at the same time → zero overlapping obs → pearson returns 0.
	retsA := []float64{0.01, 0.02, 0.0, 0.0}
	retsB := []float64{0.0, 0.0, 0.01, 0.02}
	maskA := []bool{true, true, false, false}
	maskB := []bool{false, false, true, true}

	m := buildMatrix(t,
		map[string][]float64{"A": retsA, "B": retsB},
		map[string][]bool{"A": maskA, "B": maskB},
		2,
	)

	assert.Equal(t, 0.0, m["A"]["B"],
		"no overlapping observations → correlation must be 0")
}

func TestCalculateCorrelationMatrix_MinObsFilter(t *testing.T) {
	// Symbol C has only 2 active days, below the minObs threshold of 3.
	// It should be excluded from the result entirely.
	retsA := []float64{0.01, 0.02, -0.01}
	retsC := []float64{0.01, 0.02, 0.0}
	maskA := []bool{true, true, true}
	maskC := []bool{true, true, false}

	res := CalculateCorrelationMatrix(
		map[string][]float64{"A": retsA, "C": retsC},
		map[string][]bool{"A": maskA, "C": maskC},
		3,
	)

	assert.Equal(t, []string{"A"}, res.Symbols,
		"symbol with fewer than minObs active days must be excluded")
	assert.Equal(t, 1, len(res.Matrix))
}

func TestCalculateCorrelationMatrix_PerfectNegative(t *testing.T) {
	// A and B move in exactly opposite directions during their overlap.
	retsA := []float64{0.01, -0.02, 0.03, 0.0, 0.0}
	retsB := []float64{-0.01, 0.02, -0.03, 0.0, 0.0}
	maskA := []bool{true, true, true, false, false}
	maskB := []bool{true, true, true, false, false}

	m := buildMatrix(t,
		map[string][]float64{"A": retsA, "B": retsB},
		map[string][]bool{"A": maskA, "B": maskB},
		3,
	)

	assert.InDelta(t, -1.0, m["A"]["B"], 1e-9,
		"perfectly inverse overlapping series must yield correlation -1.0")
}

func TestCalculateCorrelationMatrix_SymmetryAndDiagonal(t *testing.T) {
	// Smoke-test that the matrix is symmetric and the diagonal is 1.0 for 3 symbols.
	n := 20
	makeRets := func(seed float64) ([]float64, []bool) {
		rets := make([]float64, n)
		mask := make([]bool, n)
		for i := range rets {
			rets[i] = seed * float64(i%5-2) * 0.005
			mask[i] = true
		}
		return rets, mask
	}
	rA, mA := makeRets(1)
	rB, mB := makeRets(2)
	rC, mC := makeRets(-1)

	m := buildMatrix(t,
		map[string][]float64{"A": rA, "B": rB, "C": rC},
		map[string][]bool{"A": mA, "B": mB, "C": mC},
		5,
	)

	for _, s := range []string{"A", "B", "C"} {
		assert.InDelta(t, 1.0, m[s][s], 1e-9, "diagonal[%s] must be 1.0", s)
	}
	pairs := [][2]string{{"A", "B"}, {"A", "C"}, {"B", "C"}}
	for _, p := range pairs {
		assert.InDelta(t, m[p[0]][p[1]], m[p[1]][p[0]], 1e-12,
			"matrix must be symmetric for (%s, %s)", p[0], p[1])
	}
}

func TestCalculateCorrelationMatrix_SameSeriesFullOverlap(t *testing.T) {
	// When both symbols have the same returns and are always active → r = 1.
	rets := []float64{0.01, -0.02, 0.03, 0.005, -0.01, 0.02, 0.0, 0.015, -0.005, 0.01}
	mask := make([]bool, len(rets))
	for i := range mask {
		mask[i] = true
	}

	m := buildMatrix(t,
		map[string][]float64{"X": rets, "Y": rets},
		map[string][]bool{"X": mask, "Y": mask},
		5,
	)

	assert.InDelta(t, 1.0, m["X"]["Y"], 1e-9)
}

func TestCalculateCorrelationMatrix_PartialOverlapReducesToCorrectSubset(t *testing.T) {
	// A = held days 0-4; B = held days 2-6.
	// Overlap is days 2-4 (3 observations).  During overlap B = 2*A.
	// Scaling a series by a positive constant does not change Pearson → still 1.0.
	rA := []float64{0.01, 0.02, 0.03, -0.01, 0.02, 0.0, 0.0}
	rB := []float64{0.0, 0.0, 0.06, -0.02, 0.04, 0.01, -0.01}
	mA := []bool{true, true, true, true, true, false, false}
	mB := []bool{false, false, true, true, true, true, true}

	m := buildMatrix(t,
		map[string][]float64{"A": rA, "B": rB},
		map[string][]bool{"A": mA, "B": mB},
		3,
	)

	// Overlapping sub-series: A=[0.03,-0.01,0.02], B=[0.06,-0.02,0.04] (B=2A)
	assert.InDelta(t, 1.0, m["A"]["B"], 1e-9,
		"B=2*A during overlap → correlation must be 1.0")
	assert.False(t, math.IsNaN(m["A"]["B"]), "result must not be NaN")
}

// ---------------------------------------------------------------------------
// Cash-flow adjustment tests
//
// These validate the per-position return formula used in the correlations handler:
//   rets[i-1] = (vals[i] - cfs[i] - vals[i-1]) / vals[i-1]
//
// handlerReturns mirrors that conversion so we can assert on the resulting
// return series without spinning up a full Gin context.
// ---------------------------------------------------------------------------

// handlerReturns applies the same cash-flow-adjusted return formula used by
// the GetCorrelations handler to convert a daily-values + cash-flow array into
// a (returns, mask) pair.
func handlerReturns(vals, cfs []float64) (rets []float64, mask []bool) {
	n := len(vals)
	if n < 2 {
		return
	}
	rets = make([]float64, n-1)
	mask = make([]bool, n-1)
	for i := 1; i < n; i++ {
		prev := vals[i-1]
		if prev > 1e-8 {
			cfAmount := 0.0
			if i < len(cfs) {
				cfAmount = cfs[i]
			}
			rets[i-1] = (vals[i] - cfAmount - prev) / prev
			mask[i-1] = true
		}
	}
	return
}

func TestCashFlowAdjustment_BuyMore(t *testing.T) {
	// Day 0: qty=10, price=10 → val=100
	// Day 1: price→11, qty still 10 → val=110  (+10%)
	// Day 2: buy 5 more at 11 → qty=15, val=165, cfs[2]=+55
	//         raw return: (165-110)/110 ≈ +50% — WRONG (includes capital)
	//         adjusted:   (165-55-110)/110 = 0%  — correct (price flat)
	// Day 3: price→12, val=180, cfs[3]=0  → return = 12/11-1 ≈ 9.09%
	vals := []float64{100, 110, 165, 180}
	cfs := []float64{0, 0, 55, 0}

	rets, mask := handlerReturns(vals, cfs)

	assert.True(t, mask[0])
	assert.InDelta(t, 0.10, rets[0], 1e-9, "day 0→1: +10%%")
	assert.True(t, mask[1])
	assert.InDelta(t, 0.0, rets[1], 1e-9, "day 1→2: price flat; buy stripped")
	assert.True(t, mask[2])
	assert.InDelta(t, 12.0/11.0-1, rets[2], 1e-9, "day 2→3: +9.09%%")
}

func TestCashFlowAdjustment_PartialSell(t *testing.T) {
	// Day 0: val=100  Day 1: val=110 (+10%)
	// Day 2: sell 3 at 11, val=77, cfs[2]=-33
	//         raw: (77-110)/110 ≈ -30% — WRONG; adjusted: (77+33-110)/110=0%
	// Day 3: price→12, val=84 → return = 12/11-1 ≈ 9.09%
	vals := []float64{100, 110, 77, 84}
	cfs := []float64{0, 0, -33, 0}

	rets, mask := handlerReturns(vals, cfs)

	assert.InDelta(t, 0.10, rets[0], 1e-9)
	assert.True(t, mask[1])
	assert.InDelta(t, 0.0, rets[1], 1e-9, "sell day: price flat; proceeds stripped")
	assert.InDelta(t, 12.0/11.0-1, rets[2], 1e-9)
}

func TestCashFlowAdjustment_FullSellLastDay(t *testing.T) {
	// Full sell on day 2 at the same price → adjusted return must be 0%.
	vals := []float64{100, 110, 0}
	cfs := []float64{0, 0, -110}

	rets, mask := handlerReturns(vals, cfs)

	assert.InDelta(t, 0.10, rets[0], 1e-9)
	assert.True(t, mask[1], "sell day must be active (prev > 0)")
	assert.InDelta(t, 0.0, rets[1], 1e-9, "full sell at same price → 0%% return")
}

func TestCashFlowAdjustment_NoCashFlows(t *testing.T) {
	// No trades → cfs all zero → formula degenerates to plain price return.
	vals := []float64{100, 110, 105, 115}
	cfs := []float64{0, 0, 0, 0}

	rets, _ := handlerReturns(vals, cfs)

	assert.InDelta(t, 0.10, rets[0], 1e-9)
	assert.InDelta(t, 105.0/110.0-1, rets[1], 1e-9)
	assert.InDelta(t, 115.0/105.0-1, rets[2], 1e-9)
}

func TestCashFlowAdjustment_CorrelationUnaffectedByBuys(t *testing.T) {
	// Two stocks have identical price moves (10→11→11→12).
	// Stock B additionally buys 5 extra shares on day 2 at price 11.
	// After adjustment correlations must still be 1.0.
	valsA := []float64{100, 110, 110, 120}
	cfsA := []float64{0, 0, 0, 0}

	valsB := []float64{100, 110, 165, 180}
	cfsB := []float64{0, 0, 55, 0} // 5 extra @ 11

	retsA, maskA := handlerReturns(valsA, cfsA)
	retsB, maskB := handlerReturns(valsB, cfsB)

	m := buildMatrix(t,
		map[string][]float64{"A": retsA, "B": retsB},
		map[string][]bool{"A": maskA, "B": maskB},
		3,
	)

	assert.InDelta(t, 1.0, m["A"]["B"], 1e-9,
		"identical price moves must yield r=1.0 even when one stock has add-to-position trades")
}

