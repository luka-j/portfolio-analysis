package stats

import (
	"encoding/csv"
	"math"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════
// Test data helpers
// ═══════════════════════════════════════════════════════════════

type pricePoint struct {
	date     time.Time
	adjClose float64
}

type datedReturn struct {
	date time.Time
	ret  float64
}

const testdataDir = "../../testdata/"

// loadPriceCSV reads a market_data CSV export and returns sorted, valid price points.
func loadPriceCSV(t *testing.T, filename string) []pricePoint {
	t.Helper()
	f, err := os.Open(testdataDir + filename)
	if err != nil {
		t.Fatalf("open %s: %v", filename, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv %s: %v", filename, err)
	}

	var pts []pricePoint
	for i, rec := range records {
		if i == 0 {
			continue
		}
		if len(rec) < 8 {
			continue
		}
		dateStr := rec[2]
		if len(dateStr) < 10 {
			continue
		}
		d, err := time.Parse("2006-01-02", dateStr[:10])
		if err != nil {
			continue
		}
		adj, err := strconv.ParseFloat(rec[7], 64)
		if err != nil || adj <= 0 {
			continue
		}
		pts = append(pts, pricePoint{date: d, adjClose: adj})
	}

	sort.Slice(pts, func(i, j int) bool {
		return pts[i].date.Before(pts[j].date)
	})

	// Deduplicate by date (keep last seen, i.e. the one with higher adj_close ID).
	deduped := pts[:0]
	for i, p := range pts {
		if i > 0 && p.date.Equal(pts[i-1].date) {
			deduped[len(deduped)-1] = p // overwrite with later entry
			continue
		}
		deduped = append(deduped, p)
	}
	return deduped
}

// toDatedReturns computes simple daily returns from sorted price points.
func toDatedReturns(pts []pricePoint) []datedReturn {
	rets := make([]datedReturn, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		r := pts[i].adjClose/pts[i-1].adjClose - 1
		rets = append(rets, datedReturn{date: pts[i].date, ret: r})
	}
	return rets
}

// alignByDate returns paired return slices for dates present in both series.
func alignByDate(a, b []datedReturn) (aOut, bOut []float64) {
	bMap := make(map[string]float64, len(b))
	for _, dr := range b {
		bMap[dr.date.Format("2006-01-02")] = dr.ret
	}
	for _, dr := range a {
		if bRet, ok := bMap[dr.date.Format("2006-01-02")]; ok {
			aOut = append(aOut, dr.ret)
			bOut = append(bOut, bRet)
		}
	}
	return
}

// rawReturns extracts the float64 slice from datedReturns.
func rawReturns(drs []datedReturn) []float64 {
	out := make([]float64, len(drs))
	for i, dr := range drs {
		out[i] = dr.ret
	}
	return out
}

// assertDelta is a compact test helper.
func assertDelta(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) {
		t.Errorf("%s: got NaN, want %v", name, want)
		return
	}
	if math.IsInf(got, 0) {
		t.Errorf("%s: got Inf, want %v", name, want)
		return
	}
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %v, want %v (tol %v)", name, got, want, tol)
	}
}

// assertFinite checks that the value is not NaN or Inf.
func assertFinite(t *testing.T, name string, v float64) {
	t.Helper()
	if math.IsNaN(v) || math.IsInf(v, 0) {
		t.Errorf("%s: got %v, expected finite", name, v)
	}
}

// ═══════════════════════════════════════════════════════════════
// Math helper unit tests
// ═══════════════════════════════════════════════════════════════

func TestMathHelpers(t *testing.T) {
	const eps = 1e-12

	t.Run("mean/empty", func(t *testing.T) {
		if got := mean(nil); got != 0 {
			t.Errorf("mean(nil) = %v, want 0", got)
		}
	})
	t.Run("mean/single", func(t *testing.T) {
		if got := mean([]float64{42}); got != 42 {
			t.Errorf("got %v, want 42", got)
		}
	})
	t.Run("mean/symmetric", func(t *testing.T) {
		if got := mean([]float64{-2, -1, 0, 1, 2}); math.Abs(got) > eps {
			t.Errorf("got %v, want 0", got)
		}
	})
	t.Run("mean/known", func(t *testing.T) {
		if got := mean([]float64{1, 2, 3, 4, 5}); math.Abs(got-3) > eps {
			t.Errorf("got %v, want 3", got)
		}
	})
	t.Run("mean/negative", func(t *testing.T) {
		if got := mean([]float64{-0.01, -0.02, -0.03}); math.Abs(got-(-0.02)) > eps {
			t.Errorf("got %v, want -0.02", got)
		}
	})

	t.Run("variance/empty", func(t *testing.T) {
		if got := variance(nil); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})
	t.Run("variance/single", func(t *testing.T) {
		if got := variance([]float64{5}); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})
	t.Run("variance/two_elements", func(t *testing.T) {
		// [2,4]: mean=3, Σ(d²)=2, n-1=1 → 2.0
		got := variance([]float64{2, 4})
		if math.Abs(got-2.0) > eps {
			t.Errorf("got %v, want 2.0", got)
		}
	})
	t.Run("variance/known_5", func(t *testing.T) {
		// [1..5]: mean=3, Σ(d²)=10, n-1=4 → 2.5
		got := variance([]float64{1, 2, 3, 4, 5})
		if math.Abs(got-2.5) > eps {
			t.Errorf("got %v, want 2.5", got)
		}
	})
	t.Run("variance/constant", func(t *testing.T) {
		got := variance([]float64{7, 7, 7, 7})
		if math.Abs(got) > eps {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("stddev/known", func(t *testing.T) {
		want := math.Sqrt(2.5)
		got := stddev([]float64{1, 2, 3, 4, 5})
		if math.Abs(got-want) > eps {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("covariance/self_equals_variance", func(t *testing.T) {
		xs := []float64{1, 2, 3, 4, 5}
		got := covariance(xs, xs)
		want := variance(xs)
		if math.Abs(got-want) > eps {
			t.Errorf("cov(x,x) = %v, var(x) = %v, should be equal", got, want)
		}
	})
	t.Run("covariance/perfect_negative", func(t *testing.T) {
		got := covariance([]float64{1, 2, 3}, []float64{3, 2, 1})
		if math.Abs(got+1.0) > eps {
			t.Errorf("got %v, want -1", got)
		}
	})
	t.Run("covariance/with_constant", func(t *testing.T) {
		got := covariance([]float64{1, 2, 3}, []float64{4, 4, 4})
		if math.Abs(got) > eps {
			t.Errorf("got %v, want 0", got)
		}
	})
	t.Run("covariance/scaled", func(t *testing.T) {
		xs := []float64{1, 2, 3, 4}
		ys := make([]float64, len(xs))
		for i, x := range xs {
			ys[i] = 3 * x
		}
		// cov(X, 3X) = 3 * var(X)
		got := covariance(xs, ys)
		want := 3 * variance(xs)
		if math.Abs(got-want) > eps {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("covariance/mismatched_length", func(t *testing.T) {
		if got := covariance([]float64{1, 2}, []float64{1}); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})
}

// ═══════════════════════════════════════════════════════════════
// Mathematical property tests for BenchmarkMetrics
// ═══════════════════════════════════════════════════════════════

// benchSeries is a realistic-looking daily return series used across property tests.
var benchSeries = []float64{
	0.010, -0.005, 0.020, -0.010, 0.015,
	0.003, -0.008, 0.012, -0.002, 0.007,
	-0.003, 0.009, 0.001, -0.006, 0.011,
	0.005, -0.004, 0.008, -0.007, 0.013,
	0.002, -0.001, 0.006, 0.004, -0.009,
	0.014, -0.003, 0.010, -0.005, 0.008,
}

func TestBenchmarkMetrics_IdenticalReturns(t *testing.T) {
	for _, rf := range []float64{0.0, 0.03, 0.05, -0.01} {
		m := CalculateBenchmarkMetrics(benchSeries, benchSeries, rf)
		assertDelta(t, "Beta", m.Beta, 1.0, 1e-9)
		assertDelta(t, "Alpha", m.Alpha, 0.0, 1e-9)
		assertDelta(t, "Correlation", m.Correlation, 1.0, 1e-9)
		assertDelta(t, "TrackingError", m.TrackingError, 0.0, 1e-9)
		assertDelta(t, "InformationRatio", m.InformationRatio, 0.0, 1e-9)

		// Treynor * beta = annualised excess return; same should come from Sharpe * σ * √252
		// Sharpe = (pMean-dailyRf)*√252 / σ, Treynor = (pMean-dailyRf)*252 / β
		// For β=1: Treynor = Sharpe * σ * √252
		sm := CalculateStandaloneMetrics(benchSeries, rf)
		pStd := stddev(benchSeries)
		assertDelta(t, "Treynor=Sharpe*σ*√252", m.TreynorRatio,
			sm.SharpeRatio*pStd*math.Sqrt(252), 1e-9)
	}
}

func TestBenchmarkMetrics_ScaledPortfolio(t *testing.T) {
	tests := []struct {
		name     string
		scale    float64
		wantBeta float64
		wantCorr float64
	}{
		{"2x leveraged", 2.0, 2.0, 1.0},
		{"0.5x half exposure", 0.5, 0.5, 1.0},
		{"3x leveraged", 3.0, 3.0, 1.0},
		{"0.1x minimal exposure", 0.1, 0.1, 1.0},
		{"-1x inverse", -1.0, -1.0, -1.0},
		{"-2x inverse leveraged", -2.0, -2.0, -1.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			port := make([]float64, len(benchSeries))
			for i, r := range benchSeries {
				port[i] = tc.scale * r
			}
			m := CalculateBenchmarkMetrics(port, benchSeries, 0.03)
			assertDelta(t, "Beta", m.Beta, tc.wantBeta, 1e-9)
			assertDelta(t, "Correlation", m.Correlation, tc.wantCorr, 1e-9)
			assertFinite(t, "Alpha", m.Alpha)
			assertFinite(t, "Sharpe", CalculateStandaloneMetrics(port, 0.03).SharpeRatio)
			assertFinite(t, "Treynor", m.TreynorRatio)

			// For any scale k, Jensen's alpha (daily) = pMean - (dailyRf + k*(bMean - dailyRf))
			// = k*bMean - dailyRf - k*bMean + k*dailyRf = (k-1)*dailyRf
			dailyRf := math.Pow(1.03, 1.0/252.0) - 1
			expectedAlphaDaily := (tc.scale - 1) * dailyRf
			expectedAlpha := math.Pow(1+expectedAlphaDaily, 252) - 1
			assertDelta(t, "Alpha", m.Alpha, expectedAlpha, 1e-6)
		})
	}
}

func TestBenchmarkMetrics_ShiftedByConstantAlpha(t *testing.T) {
	shifts := []float64{0.0001, 0.001, 0.005, -0.001, -0.005}

	for _, shift := range shifts {
		t.Run("shift="+strconv.FormatFloat(shift, 'f', 4, 64), func(t *testing.T) {
			port := make([]float64, len(benchSeries))
			for i, r := range benchSeries {
				port[i] = r + shift
			}
			m := CalculateBenchmarkMetrics(port, benchSeries, 0.0)

			assertDelta(t, "Beta", m.Beta, 1.0, 1e-9)
			assertDelta(t, "Correlation", m.Correlation, 1.0, 1e-9)

			// Alpha = compounded daily shift (rf=0 so alphaDailyVal = shift)
			expectedAlpha := math.Pow(1+shift, 252) - 1
			assertDelta(t, "Alpha", m.Alpha, expectedAlpha, 1e-6)

			// Tracking error should be ~0 because diff is a constant.
			// FP noise from (benchReturn + shift) - benchReturn is below the 1e-6
			// threshold, so TE is effectively zero.
			if m.TrackingError > 1e-6 {
				t.Errorf("TrackingError = %v, expected near-zero for constant diff", m.TrackingError)
			}

			// IR should be 0 because the diffStd threshold guards against FP noise.
			assertDelta(t, "InformationRatio", m.InformationRatio, 0.0, 1e-12)
		})
	}
}

func TestBenchmarkMetrics_OrthogonalReturns(t *testing.T) {
	// Construct two series with exactly zero covariance.
	// p varies on even indices, b varies on odd indices; both have mean 0.
	n := 100
	port := make([]float64, n)
	bench := make([]float64, n)
	a := 0.01
	for i := 0; i < n; i++ {
		if i%4 == 0 {
			port[i] = a
		} else if i%4 == 1 {
			bench[i] = a
		} else if i%4 == 2 {
			port[i] = -a
		} else {
			bench[i] = -a
		}
	}
	// Both series have mean 0. Cross products p_i*b_i = 0 for all i.
	// So covariance = 0, beta = 0, correlation = 0.
	m := CalculateBenchmarkMetrics(port, bench, 0.0)
	assertDelta(t, "Beta", m.Beta, 0.0, 1e-9)
	assertDelta(t, "Correlation", m.Correlation, 0.0, 1e-9)
}

// ═══════════════════════════════════════════════════════════════
// Edge cases
// ═══════════════════════════════════════════════════════════════

func TestBenchmarkMetrics_SingleDataPoint(t *testing.T) {
	// n=1: variance/covariance need n>=2 and return 0.
	m := CalculateBenchmarkMetrics([]float64{0.01}, []float64{0.02}, 0.0)
	// With stddev(b)=0, the threshold blocks beta computation.
	assertDelta(t, "Beta", m.Beta, 0.0, 1e-12)
	assertDelta(t, "Correlation", m.Correlation, 0.0, 1e-12)
	assertDelta(t, "TrackingError", m.TrackingError, 0.0, 1e-12)
	assertFinite(t, "Alpha", m.Alpha)
	assertFinite(t, "Sharpe", CalculateStandaloneMetrics([]float64{0.01}, 0.0).SharpeRatio)
}

func TestBenchmarkMetrics_TwoDataPoints(t *testing.T) {
	// Minimum meaningful sample: n=2.
	p := []float64{0.01, 0.02}
	b := []float64{0.005, 0.01}
	// p = 2*b, so beta should be 2.
	m := CalculateBenchmarkMetrics(p, b, 0.0)
	assertDelta(t, "Beta", m.Beta, 2.0, 1e-9)
	assertDelta(t, "Correlation", m.Correlation, 1.0, 1e-9)
	assertFinite(t, "Alpha", m.Alpha)
	assertFinite(t, "Sharpe", CalculateStandaloneMetrics(p, 0.0).SharpeRatio)
	assertFinite(t, "Treynor", m.TreynorRatio)
}

func TestBenchmarkMetrics_AllZeroReturns(t *testing.T) {
	n := len(benchSeries) // match benchSeries length for the volatile sub-test
	zeros := make([]float64, n)

	t.Run("both_zero", func(t *testing.T) {
		m := CalculateBenchmarkMetrics(zeros, zeros, 0.0)
		assertDelta(t, "Beta", m.Beta, 0.0, 1e-12)
		assertDelta(t, "Correlation", m.Correlation, 0.0, 1e-12)
		assertDelta(t, "TrackingError", m.TrackingError, 0.0, 1e-12)
		assertFinite(t, "Alpha", m.Alpha)
	})

	t.Run("both_zero_with_rf", func(t *testing.T) {
		m := CalculateBenchmarkMetrics(zeros, zeros, 0.05)
		assertDelta(t, "Beta", m.Beta, 0.0, 1e-12)
		assertFinite(t, "Alpha", m.Alpha)
		// Alpha daily = 0 - (dailyRf + 0*(0 - dailyRf)) = -dailyRf
		dailyRf := math.Pow(1.05, 1.0/252.0) - 1
		expectedAlpha := math.Pow(1-dailyRf, 252) - 1
		assertDelta(t, "Alpha", m.Alpha, expectedAlpha, 1e-9)
	})

	t.Run("portfolio_zero_benchmark_volatile", func(t *testing.T) {
		bench := benchSeries[:n] // use first n elements of benchSeries
		m := CalculateBenchmarkMetrics(zeros, bench, 0.0)
		assertDelta(t, "Beta", m.Beta, 0.0, 1e-9)
		assertDelta(t, "Correlation", m.Correlation, 0.0, 1e-9)
		assertDelta(t, "Sharpe", CalculateStandaloneMetrics(zeros, 0.0).SharpeRatio, 0.0, 1e-12) // pStd=0
	})
}

func TestBenchmarkMetrics_ConstantPortfolioReturn(t *testing.T) {
	// A portfolio that returns exactly 5bps every day (like an ideal money market).
	// Benchmark is volatile equity.
	n := 30
	constRet := 0.0005
	port := make([]float64, n)
	for i := range port {
		port[i] = constRet
	}
	bench := benchSeries[:n]

	m := CalculateBenchmarkMetrics(port, bench, 0.0)

	// Portfolio has near-zero variance (FP noise from summation in mean()).
	// The 1e-6 stddev threshold now correctly guards Sharpe/Treynor/Correlation.
	assertDelta(t, "Beta", m.Beta, 0.0, 1e-12)
	assertDelta(t, "Correlation", m.Correlation, 0.0, 1e-12)

	// Sharpe: pStd below threshold → 0.
	assertDelta(t, "Sharpe", CalculateStandaloneMetrics(port, 0.0).SharpeRatio, 0.0, 1e-12)

	// Treynor: beta effectively 0 → 0.
	assertDelta(t, "Treynor", m.TreynorRatio, 0.0, 1e-12)

	// Alpha = compounded daily return (since beta=0 and rf=0)
	expectedAlpha := math.Pow(1+constRet, 252) - 1
	assertDelta(t, "Alpha", m.Alpha, expectedAlpha, 1e-6)

	// TE > 0 because the diff (port - bench) varies.
	if m.TrackingError <= 0 {
		t.Error("TE should be positive")
	}
}

func TestBenchmarkMetrics_RiskFreeRateVariations(t *testing.T) {
	port := benchSeries

	t.Run("zero_rf", func(t *testing.T) {
		m := CalculateBenchmarkMetrics(port, port, 0.0)
		assertDelta(t, "Alpha", m.Alpha, 0.0, 1e-9)
		// dailyRf=0, so Sharpe = pMean * √252 / pStd
		pMean := mean(port)
		pStd := stddev(port)
		expectedSharpe := pMean * math.Sqrt(252) / pStd
		assertDelta(t, "Sharpe", CalculateStandaloneMetrics(port, 0.0).SharpeRatio, expectedSharpe, 1e-9)
	})

	t.Run("negative_rf", func(t *testing.T) {
		m := CalculateBenchmarkMetrics(port, port, -0.005)
		assertDelta(t, "Beta", m.Beta, 1.0, 1e-9)
		assertDelta(t, "Alpha", m.Alpha, 0.0, 1e-9)
		assertFinite(t, "Sharpe", CalculateStandaloneMetrics(port, -0.005).SharpeRatio)
		assertFinite(t, "Treynor", m.TreynorRatio)
	})

	t.Run("high_rf_above_portfolio_return", func(t *testing.T) {
		// Rf = 20% annual. Portfolio likely has lower annual return → negative excess.
		m := CalculateBenchmarkMetrics(port, port, 0.20)
		assertDelta(t, "Beta", m.Beta, 1.0, 1e-9)
		assertDelta(t, "Alpha", m.Alpha, 0.0, 1e-9)
		// Sharpe should be negative (portfolio underperforms rf)
		dailyRf := math.Pow(1.20, 1.0/252.0) - 1
		smHigh := CalculateStandaloneMetrics(port, 0.20)
		if mean(port) < dailyRf && smHigh.SharpeRatio >= 0 {
			t.Errorf("Sharpe should be negative when portfolio < rf, got %v", smHigh.SharpeRatio)
		}
	})

	t.Run("extreme_rf_100pct", func(t *testing.T) {
		m := CalculateBenchmarkMetrics(port, port, 1.0)
		assertDelta(t, "Beta", m.Beta, 1.0, 1e-9)
		assertDelta(t, "Alpha", m.Alpha, 0.0, 1e-9)
		assertFinite(t, "Sharpe", CalculateStandaloneMetrics(port, 1.0).SharpeRatio)
		assertFinite(t, "Treynor", m.TreynorRatio)
	})
}

func TestBenchmarkMetrics_ExtremeDailyReturns(t *testing.T) {
	t.Run("circuit_breaker_day", func(t *testing.T) {
		// One day with -20% crash (like a circuit breaker), rest normal.
		port := make([]float64, 30)
		bench := make([]float64, 30)
		copy(port, benchSeries)
		copy(bench, benchSeries)
		port[15] = -0.20  // 20% crash
		bench[15] = -0.07 // benchmark drops 7%
		m := CalculateBenchmarkMetrics(port, bench, 0.03)
		assertFinite(t, "Alpha", m.Alpha)
		assertFinite(t, "Beta", m.Beta)
		assertFinite(t, "Sharpe", CalculateStandaloneMetrics(port, 0.03).SharpeRatio)
		assertFinite(t, "Treynor", m.TreynorRatio)
		assertFinite(t, "TE", m.TrackingError)
		assertFinite(t, "IR", m.InformationRatio)
		assertFinite(t, "Correlation", m.Correlation)
	})

	t.Run("multiple_extreme_days", func(t *testing.T) {
		n := 50
		port := make([]float64, n)
		bench := make([]float64, n)
		for i := 0; i < n; i++ {
			bench[i] = 0.001
			port[i] = 0.001
		}
		// Inject extreme swings
		port[10] = 0.15
		port[20] = -0.15
		bench[10] = 0.10
		bench[20] = -0.10
		m := CalculateBenchmarkMetrics(port, bench, 0.03)
		assertFinite(t, "Alpha", m.Alpha)
		assertFinite(t, "Beta", m.Beta)
		if m.Beta <= 0 {
			t.Errorf("expected positive beta, got %v", m.Beta)
		}
	})

	t.Run("near_total_loss_daily", func(t *testing.T) {
		// Portfolio loses 90% in one day; tests alpha compounding stability.
		port := []float64{0.01, -0.90, 0.01, 0.01, 0.01}
		bench := []float64{0.01, -0.05, 0.01, 0.01, 0.01}
		m := CalculateBenchmarkMetrics(port, bench, 0.0)
		assertFinite(t, "Alpha", m.Alpha)
		assertFinite(t, "Beta", m.Beta)
	})
}

func TestBenchmarkMetrics_MeanRevertingPattern(t *testing.T) {
	// Alternating +1% / -1% pattern. Mean ≈ 0.
	n := 100
	port := make([]float64, n)
	bench := make([]float64, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			port[i] = 0.01
			bench[i] = 0.005
		} else {
			port[i] = -0.01
			bench[i] = -0.005
		}
	}
	m := CalculateBenchmarkMetrics(port, bench, 0.0)
	assertDelta(t, "Beta", m.Beta, 2.0, 1e-9)
	assertDelta(t, "Correlation", m.Correlation, 1.0, 1e-9)
	// Mean of port ≈ 0, mean of bench ≈ 0 for even n.
	// With rf=0: Sharpe = pMean * √252 / pStd ≈ 0
	assertDelta(t, "Sharpe≈0", CalculateStandaloneMetrics(port, 0.0).SharpeRatio, 0.0, 0.01)
}

func TestBenchmarkMetrics_PortfolioWithAddedNoise(t *testing.T) {
	// Portfolio = benchmark + small noise. Beta ≈ 1, correlation < 1.
	n := len(benchSeries)
	port := make([]float64, n)
	noise := []float64{
		0.002, -0.001, 0.003, -0.002, 0.001,
		-0.003, 0.002, -0.001, 0.003, -0.002,
		0.001, -0.003, 0.002, -0.001, 0.003,
		-0.002, 0.001, -0.003, 0.002, -0.001,
		0.003, -0.002, 0.001, -0.003, 0.002,
		-0.001, 0.003, -0.002, 0.001, -0.003,
	}
	for i := range benchSeries {
		port[i] = benchSeries[i] + noise[i]
	}

	m := CalculateBenchmarkMetrics(port, benchSeries, 0.03)

	// Beta should be close to 1 (noise is small relative to benchmark variance).
	if m.Beta < 0.8 || m.Beta > 1.2 {
		t.Errorf("Beta = %v, expected near 1.0", m.Beta)
	}
	// Correlation high but not 1.0.
	if m.Correlation < 0.8 || m.Correlation > 0.999 {
		t.Errorf("Correlation = %v, expected high but <1", m.Correlation)
	}
	// TE > 0.
	if m.TrackingError <= 0 {
		t.Error("TE should be positive")
	}
}

// ═══════════════════════════════════════════════════════════════
// Real-world data tests
// ═══════════════════════════════════════════════════════════════

func TestBenchmarkMetrics_RealData_SPYSelfBenchmark(t *testing.T) {
	pts := loadPriceCSV(t, "spy-price.csv")
	if len(pts) < 100 {
		t.Fatalf("too few SPY data points: %d", len(pts))
	}

	rets := rawReturns(toDatedReturns(pts))

	// Full history.
	m := CalculateBenchmarkMetrics(rets, rets, 0.03)
	assertDelta(t, "Beta", m.Beta, 1.0, 1e-9)
	assertDelta(t, "Alpha", m.Alpha, 0.0, 1e-9)
	assertDelta(t, "Correlation", m.Correlation, 1.0, 1e-9)
	assertDelta(t, "TrackingError", m.TrackingError, 0.0, 1e-9)
	assertDelta(t, "InformationRatio", m.InformationRatio, 0.0, 1e-9)

	// Sharpe should be reasonable (0.1 to 1.0 for long-run SPY with rf=3%).
	smSPY := CalculateStandaloneMetrics(rets, 0.03)
	if smSPY.SharpeRatio < 0.05 || smSPY.SharpeRatio > 1.5 {
		t.Errorf("SPY Sharpe = %v, outside plausible range", smSPY.SharpeRatio)
	}

	t.Logf("SPY (full history, %d days): Sharpe=%.4f, Treynor=%.4f", len(rets), smSPY.SharpeRatio, m.TreynorRatio)
}

func TestBenchmarkMetrics_RealData_2xSPYvsSPY(t *testing.T) {
	pts := loadPriceCSV(t, "spy-price.csv")
	rets := rawReturns(toDatedReturns(pts))
	if len(rets) < 100 {
		t.Fatalf("too few data points")
	}

	leveraged := make([]float64, len(rets))
	for i, r := range rets {
		leveraged[i] = 2 * r
	}

	m := CalculateBenchmarkMetrics(leveraged, rets, 0.03)
	assertDelta(t, "Beta", m.Beta, 2.0, 1e-9)
	assertDelta(t, "Correlation", m.Correlation, 1.0, 1e-9)

	// Alpha = riskFreeRate for 2x leverage.
	dailyRf := math.Pow(1.03, 1.0/252.0) - 1
	expectedAlpha := math.Pow(1+dailyRf, 252) - 1
	assertDelta(t, "Alpha≈Rf", m.Alpha, expectedAlpha, 1e-6)

	// Sharpe_kx = (k*pMean - dailyRf)*√252 / (k*pStd).
	// For k=2: Sharpe_2x = (2*pMean - dailyRf)*√252 / (2*pStd)
	//                    = Sharpe_1x + dailyRf*√252 / (2*pStd)
	// So 2x Sharpe > 1x Sharpe when rf > 0 (leverage gets "free" excess return).
	pStd := stddev(rets)
	expectedDelta := dailyRf * math.Sqrt(252) / (2 * pStd)
	sm2x := CalculateStandaloneMetrics(leveraged, 0.03)
	sm1x := CalculateStandaloneMetrics(rets, 0.03)
	assertDelta(t, "Sharpe_2x - Sharpe_1x", sm2x.SharpeRatio-sm1x.SharpeRatio, expectedDelta, 1e-6)
}

func TestBenchmarkMetrics_RealData_SXR8vsSPY(t *testing.T) {
	spyPts := loadPriceCSV(t, "spy-price.csv")
	sxrPts := loadPriceCSV(t, "sxr8de-price.csv")

	spyRets := toDatedReturns(spyPts)
	sxrRets := toDatedReturns(sxrPts)

	sxrAligned, spyAligned := alignByDate(sxrRets, spyRets)
	n := len(sxrAligned)
	if n < 50 {
		t.Fatalf("too few overlapping days between SXR8 and SPY: %d", n)
	}

	m := CalculateBenchmarkMetrics(sxrAligned, spyAligned, 0.03)

	// Both track S&P 500 but in different currencies (EUR vs USD).
	// Over long periods, the EUR/USD FX effect adds significant noise,
	// reducing correlation more than one might expect (often 0.3-0.7).
	if m.Correlation < 0.2 {
		t.Errorf("Correlation = %v, expected > 0.2 for S&P trackers (even with FX)", m.Correlation)
	}

	// Beta should be positive, roughly around 0.5-1.5.
	if m.Beta < 0.1 || m.Beta > 3.0 {
		t.Errorf("Beta = %v, outside plausible range for S&P cross-currency", m.Beta)
	}

	assertFinite(t, "Alpha", m.Alpha)
	smSXR8 := CalculateStandaloneMetrics(sxrAligned, 0.03)
	assertFinite(t, "Sharpe", smSXR8.SharpeRatio)
	assertFinite(t, "Treynor", m.TreynorRatio)
	if m.TrackingError < 0 {
		t.Errorf("TE should be non-negative, got %v", m.TrackingError)
	}

	t.Logf("SXR8.DE vs SPY (%d days): Beta=%.4f, Corr=%.4f, Alpha=%.4f, Sharpe=%.4f, TE=%.4f",
		n, m.Beta, m.Correlation, m.Alpha, smSXR8.SharpeRatio, m.TrackingError)
}

func TestBenchmarkMetrics_RealData_ARMYvsSPY(t *testing.T) {
	spyPts := loadPriceCSV(t, "spy-price.csv")
	armyPts := loadPriceCSV(t, "armyl-price.csv")

	spyRets := toDatedReturns(spyPts)
	armyRets := toDatedReturns(armyPts)

	armyAligned, spyAligned := alignByDate(armyRets, spyRets)
	n := len(armyAligned)
	if n < 20 {
		t.Fatalf("too few overlapping days between ARMY.L and SPY: %d", n)
	}

	m := CalculateBenchmarkMetrics(armyAligned, spyAligned, 0.03)

	// ARMY.L is a niche European defence ETF (GBP) vs SPY (USD broad US equity).
	// Correlation could be moderate; not necessarily high.
	assertFinite(t, "Alpha", m.Alpha)
	assertFinite(t, "Beta", m.Beta)
	smARMY := CalculateStandaloneMetrics(armyAligned, 0.03)
	assertFinite(t, "Sharpe", smARMY.SharpeRatio)
	assertFinite(t, "Treynor", m.TreynorRatio)
	assertFinite(t, "TE", m.TrackingError)
	assertFinite(t, "IR", m.InformationRatio)
	assertFinite(t, "Correlation", m.Correlation)

	if m.Correlation < -1.0 || m.Correlation > 1.0 {
		t.Errorf("Correlation out of [-1,1]: %v", m.Correlation)
	}
	if m.TrackingError < 0 {
		t.Errorf("TE should be non-negative: %v", m.TrackingError)
	}

	t.Logf("ARMY.L vs SPY (%d days): Beta=%.4f, Corr=%.4f, Alpha=%.4f, Sharpe=%.4f, TE=%.4f, IR=%.4f",
		n, m.Beta, m.Correlation, m.Alpha, smARMY.SharpeRatio, m.TrackingError, m.InformationRatio)
}

func TestBenchmarkMetrics_RealData_ARMYvsSXR8(t *testing.T) {
	sxrPts := loadPriceCSV(t, "sxr8de-price.csv")
	armyPts := loadPriceCSV(t, "armyl-price.csv")

	sxrRets := toDatedReturns(sxrPts)
	armyRets := toDatedReturns(armyPts)

	armyAligned, sxrAligned := alignByDate(armyRets, sxrRets)
	n := len(armyAligned)
	if n < 20 {
		t.Fatalf("too few overlapping days between ARMY.L and SXR8.DE: %d", n)
	}

	m := CalculateBenchmarkMetrics(armyAligned, sxrAligned, 0.025)

	// Both are European-listed, but different sectors and currencies (GBP vs EUR).
	assertFinite(t, "Alpha", m.Alpha)
	assertFinite(t, "Beta", m.Beta)
	assertFinite(t, "Correlation", m.Correlation)
	smARMYvsSXR8 := CalculateStandaloneMetrics(armyAligned, 0.025)
	assertFinite(t, "Sharpe", smARMYvsSXR8.SharpeRatio)
	assertFinite(t, "TE", m.TrackingError)
	if m.TrackingError < 0 {
		t.Errorf("TE should be non-negative: %v", m.TrackingError)
	}

	t.Logf("ARMY.L vs SXR8.DE (%d days): Beta=%.4f, Corr=%.4f, Alpha=%.4f, Sharpe=%.4f, TE=%.4f",
		n, m.Beta, m.Correlation, m.Alpha, smARMYvsSXR8.SharpeRatio, m.TrackingError)
}

func TestBenchmarkMetrics_RealData_SyntheticMixedPortfolio(t *testing.T) {
	// Construct a portfolio that starts as 100% SPY, then becomes 60/40 SPY/ARMY.
	spyPts := loadPriceCSV(t, "spy-price.csv")
	armyPts := loadPriceCSV(t, "armyl-price.csv")

	spyRets := toDatedReturns(spyPts)
	armyRets := toDatedReturns(armyPts)

	// Align dates.
	armyAligned, spyAligned := alignByDate(armyRets, spyRets)
	n := len(armyAligned)
	if n < 40 {
		t.Fatalf("too few overlapping days: %d", n)
	}

	// Mixed portfolio: first half is pure SPY, second half is 60% SPY + 40% ARMY.
	mid := n / 2
	mixedRets := make([]float64, n)
	for i := 0; i < n; i++ {
		if i < mid {
			mixedRets[i] = spyAligned[i]
		} else {
			mixedRets[i] = 0.6*spyAligned[i] + 0.4*armyAligned[i]
		}
	}

	m := CalculateBenchmarkMetrics(mixedRets, spyAligned, 0.03)

	// The mixed portfolio has SPY exposure, so beta should be positive.
	if m.Beta <= 0 {
		t.Errorf("expected positive beta for SPY-heavy portfolio, got %v", m.Beta)
	}
	// Beta should be < 1 if ARMY has lower beta to SPY.
	// But it could be > 1 if ARMY amplifies SPY moves. Just check it's reasonable.
	if m.Beta > 5 || m.Beta < -2 {
		t.Errorf("Beta = %v, implausible for 60/40 mix", m.Beta)
	}

	assertFinite(t, "Alpha", m.Alpha)
	smMixed := CalculateStandaloneMetrics(mixedRets, 0.03)
	assertFinite(t, "Sharpe", smMixed.SharpeRatio)
	assertFinite(t, "Treynor", m.TreynorRatio)
	assertFinite(t, "TE", m.TrackingError)
	assertFinite(t, "IR", m.InformationRatio)
	assertFinite(t, "Correlation", m.Correlation)

	t.Logf("Mixed 60/40 vs SPY (%d days): Beta=%.4f, Corr=%.4f, Alpha=%.4f, Sharpe=%.4f",
		n, m.Beta, m.Correlation, m.Alpha, smMixed.SharpeRatio)
}

func TestBenchmarkMetrics_RealData_LongHistorySanity(t *testing.T) {
	// Use full SPY history (6000+ trading days) to verify numerical stability.
	pts := loadPriceCSV(t, "spy-price.csv")
	rets := rawReturns(toDatedReturns(pts))
	if len(rets) < 5000 {
		t.Fatalf("expected 5000+ SPY returns, got %d", len(rets))
	}

	// Portfolio = SPY + 2bps daily alpha
	port := make([]float64, len(rets))
	for i, r := range rets {
		port[i] = r + 0.0002
	}

	m := CalculateBenchmarkMetrics(port, rets, 0.04)
	assertDelta(t, "Beta", m.Beta, 1.0, 1e-9)
	assertDelta(t, "Correlation", m.Correlation, 1.0, 1e-9)

	// Alpha = compounded (0.0002 + some rf adjustment)
	// With rf=4%, beta=1: alphaDailyVal = 0.0002
	expectedAlpha := math.Pow(1.0002, 252) - 1
	assertDelta(t, "Alpha", m.Alpha, expectedAlpha, 1e-4)

	smLong := CalculateStandaloneMetrics(port, 0.04)
	assertFinite(t, "Sharpe", smLong.SharpeRatio)
	assertFinite(t, "Treynor", m.TreynorRatio)

	t.Logf("SPY+2bps (%d days): Alpha=%.4f (expected %.4f), Sharpe=%.4f",
		len(rets), m.Alpha, expectedAlpha, smLong.SharpeRatio)
}

func TestBenchmarkMetrics_RealData_AdjCloseVsClose(t *testing.T) {
	// Compare metrics using adj_close (includes dividends) vs close (price only).
	// This tests whether dividend-inclusive vs dividend-exclusive returns produce
	// meaningfully different risk metrics.
	f, err := os.Open(testdataDir + "spy-price.csv")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	type dp struct {
		date     time.Time
		close    float64
		adjClose float64
	}
	var data []dp
	for i, rec := range records {
		if i == 0 || len(rec) < 8 {
			continue
		}
		d, err := time.Parse("2006-01-02", rec[2][:10])
		if err != nil {
			continue
		}
		cl, err1 := strconv.ParseFloat(rec[6], 64)
		adj, err2 := strconv.ParseFloat(rec[7], 64)
		if err1 != nil || err2 != nil || cl <= 0 || adj <= 0 {
			continue
		}
		data = append(data, dp{date: d, close: cl, adjClose: adj})
	}
	sort.Slice(data, func(i, j int) bool { return data[i].date.Before(data[j].date) })

	// Use last 1000 days for comparison.
	if len(data) < 1001 {
		t.Fatalf("not enough data: %d", len(data))
	}
	data = data[len(data)-1001:]

	adjRets := make([]float64, 1000)
	closeRets := make([]float64, 1000)
	for i := 1; i < len(data); i++ {
		adjRets[i-1] = data[i].adjClose/data[i-1].adjClose - 1
		closeRets[i-1] = data[i].close/data[i-1].close - 1
	}

	// Portfolio = adj_close returns, benchmark = close returns.
	m := CalculateBenchmarkMetrics(adjRets, closeRets, 0.03)

	// Should be very close to self-benchmark since dividends are small relative to daily moves.
	if m.Correlation < 0.99 {
		t.Errorf("adj vs close correlation = %v, expected >0.99", m.Correlation)
	}
	if m.Beta < 0.95 || m.Beta > 1.05 {
		t.Errorf("adj vs close beta = %v, expected near 1.0", m.Beta)
	}
	// Alpha should be slightly positive (dividends add return).
	assertFinite(t, "Alpha", m.Alpha)

	t.Logf("SPY adj_close vs close (%d days): Beta=%.4f, Corr=%.6f, Alpha=%.4f",
		1000, m.Beta, m.Correlation, m.Alpha)
}

// ═══════════════════════════════════════════════════════════════
// Cross-market trading calendar tests
// ═══════════════════════════════════════════════════════════════

func TestBenchmarkMetrics_RealData_TradingCalendarAlignment(t *testing.T) {
	// Verify that EU and US markets have non-trivial numbers of non-overlapping days.
	spyPts := loadPriceCSV(t, "spy-price.csv")
	sxrPts := loadPriceCSV(t, "sxr8de-price.csv")

	spyRets := toDatedReturns(spyPts)
	sxrRets := toDatedReturns(sxrPts)

	sxrMap := make(map[string]bool)
	for _, dr := range sxrRets {
		sxrMap[dr.date.Format("2006-01-02")] = true
	}
	spyMap := make(map[string]bool)
	for _, dr := range spyRets {
		spyMap[dr.date.Format("2006-01-02")] = true
	}

	// Find the overlapping period.
	var sxrStart, sxrEnd time.Time
	if len(sxrRets) > 0 {
		sxrStart = sxrRets[0].date
		sxrEnd = sxrRets[len(sxrRets)-1].date
	}

	spyOnly, sxrOnly, both := 0, 0, 0
	for _, dr := range spyRets {
		if dr.date.Before(sxrStart) || dr.date.After(sxrEnd) {
			continue
		}
		if sxrMap[dr.date.Format("2006-01-02")] {
			both++
		} else {
			spyOnly++
		}
	}
	for _, dr := range sxrRets {
		if !spyMap[dr.date.Format("2006-01-02")] {
			sxrOnly++
		}
	}

	t.Logf("Overlapping period: SPY-only=%d, SXR8-only=%d, both=%d", spyOnly, sxrOnly, both)

	if spyOnly == 0 && sxrOnly == 0 {
		t.Log("Warning: no calendar differences detected (possibly same exchange holidays)")
	}

	// The key test: metrics computed on aligned dates should still be sensible,
	// even though we drop some trading days.
	sxrAligned, spyAligned := alignByDate(sxrRets, spyRets)
	if len(sxrAligned) == 0 {
		t.Fatal("no aligned dates")
	}
	m := CalculateBenchmarkMetrics(sxrAligned, spyAligned, 0.03)
	assertFinite(t, "Alpha", m.Alpha)
	assertFinite(t, "Beta", m.Beta)
	assertFinite(t, "Correlation", m.Correlation)
}

// ═══════════════════════════════════════════════════════════════
// Numerical robustness
// ═══════════════════════════════════════════════════════════════

func TestBenchmarkMetrics_NumericalRobustness(t *testing.T) {
	t.Run("near_epsilon_returns", func(t *testing.T) {
		n := 100
		port := make([]float64, n)
		bench := make([]float64, n)
		for i := 0; i < n; i++ {
			port[i] = 1e-15 * float64(i%5-2)
			bench[i] = 1e-15 * float64((i+1)%5-2)
		}
		m := CalculateBenchmarkMetrics(port, bench, 0.03)
		assertFinite(t, "Alpha", m.Alpha)
		assertFinite(t, "Beta", m.Beta)
		assertFinite(t, "Sharpe", CalculateStandaloneMetrics(port, 0.03).SharpeRatio)
		assertFinite(t, "Treynor", m.TreynorRatio)
		assertFinite(t, "TE", m.TrackingError)
		assertFinite(t, "IR", m.InformationRatio)
		assertFinite(t, "Correlation", m.Correlation)
	})

	t.Run("very_large_returns", func(t *testing.T) {
		// Daily returns of 50% (unrealistic but should not crash).
		n := 20
		port := make([]float64, n)
		bench := make([]float64, n)
		for i := 0; i < n; i++ {
			if i%2 == 0 {
				port[i] = 0.5
				bench[i] = 0.3
			} else {
				port[i] = -0.3
				bench[i] = -0.2
			}
		}
		m := CalculateBenchmarkMetrics(port, bench, 0.03)
		assertFinite(t, "Beta", m.Beta)
		assertFinite(t, "Correlation", m.Correlation)
		// Alpha may be extreme due to compounding, but should be finite.
		assertFinite(t, "Alpha", m.Alpha)
	})

	t.Run("mixed_magnitudes", func(t *testing.T) {
		// Some days normal, some days extreme.
		port := []float64{0.001, 0.5, -0.001, -0.3, 0.002, 0.1, -0.05, 0.001, 0.001, 0.001}
		bench := []float64{0.001, 0.3, -0.001, -0.2, 0.001, 0.05, -0.03, 0.001, 0.001, 0.001}
		m := CalculateBenchmarkMetrics(port, bench, 0.03)
		assertFinite(t, "Beta", m.Beta)
		assertFinite(t, "Alpha", m.Alpha)
		assertFinite(t, "Correlation", m.Correlation)
		assertFinite(t, "Sharpe", CalculateStandaloneMetrics(port, 0.03).SharpeRatio)
	})

	t.Run("benchmark_near_zero_stddev_above_threshold", func(t *testing.T) {
		// Benchmark with very small but above-threshold stddev (> 1e-6 daily).
		// Simulates a low-vol money market benchmark.
		n := 200
		port := make([]float64, n)
		bench := make([]float64, n)
		for i := 0; i < n; i++ {
			bench[i] = 0.0001 + 2e-6*float64(i%3-1) // mean 0.01%, tiny stddev
			port[i] = 0.001 * float64(i%5-2)        // more volatile
		}
		m := CalculateBenchmarkMetrics(port, bench, 0.0)
		assertFinite(t, "Beta", m.Beta)
		assertFinite(t, "Alpha", m.Alpha)
		assertFinite(t, "Sharpe", CalculateStandaloneMetrics(port, 0.0).SharpeRatio)
		assertFinite(t, "Treynor", m.TreynorRatio)
		assertFinite(t, "TE", m.TrackingError)
		assertFinite(t, "IR", m.InformationRatio)
		assertFinite(t, "Correlation", m.Correlation)
	})
}

// ═══════════════════════════════════════════════════════════════
// Consistency invariants
// ═══════════════════════════════════════════════════════════════

func TestBenchmarkMetrics_Invariants(t *testing.T) {
	// Test universal invariants on a variety of input combinations.
	scenarios := []struct {
		name  string
		port  []float64
		bench []float64
		rf    float64
	}{
		{"identical", benchSeries, benchSeries, 0.03},
		{"2x_leverage", func() []float64 {
			p := make([]float64, len(benchSeries))
			for i, r := range benchSeries {
				p[i] = 2 * r
			}
			return p
		}(), benchSeries, 0.03},
		{"inverse", func() []float64 {
			p := make([]float64, len(benchSeries))
			for i, r := range benchSeries {
				p[i] = -r
			}
			return p
		}(), benchSeries, 0.03},
		{"shifted", func() []float64 {
			p := make([]float64, len(benchSeries))
			for i, r := range benchSeries {
				p[i] = r + 0.001
			}
			return p
		}(), benchSeries, 0.0},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			m := CalculateBenchmarkMetrics(sc.port, sc.bench, sc.rf)

			// 1. Correlation ∈ [-1, 1]
			if m.Correlation < -1.0-1e-9 || m.Correlation > 1.0+1e-9 {
				t.Errorf("Correlation = %v, outside [-1,1]", m.Correlation)
			}

			// 2. Tracking error ≥ 0
			if m.TrackingError < -1e-12 {
				t.Errorf("TrackingError = %v, should be >= 0", m.TrackingError)
			}

			// 3. No NaN or Inf in any field
			for _, pair := range []struct {
				name string
				val  float64
			}{
				{"Alpha", m.Alpha},
				{"Beta", m.Beta},
				{"SharpeRatio", CalculateStandaloneMetrics(sc.port, sc.rf).SharpeRatio},
				{"TreynorRatio", m.TreynorRatio},
				{"TrackingError", m.TrackingError},
				{"InformationRatio", m.InformationRatio},
				{"Correlation", m.Correlation},
			} {
				if math.IsNaN(pair.val) || math.IsInf(pair.val, 0) {
					t.Errorf("%s = %v (NaN or Inf)", pair.name, pair.val)
				}
			}

			// 4. IR sign should match the sign of mean(diff) when TE > 0
			diff := make([]float64, len(sc.port))
			for i := range diff {
				diff[i] = sc.port[i] - sc.bench[i]
			}
			diffMean := mean(diff)
			if m.TrackingError > 1e-9 {
				if diffMean > 1e-12 && m.InformationRatio < -1e-9 {
					t.Errorf("IR sign mismatch: diffMean=%v but IR=%v", diffMean, m.InformationRatio)
				}
				if diffMean < -1e-12 && m.InformationRatio > 1e-9 {
					t.Errorf("IR sign mismatch: diffMean=%v but IR=%v", diffMean, m.InformationRatio)
				}
			}

			// 5. Beta and correlation should have the same sign (when both non-zero).
			if m.Beta != 0 && m.Correlation != 0 {
				if (m.Beta > 0) != (m.Correlation > 0) {
					t.Errorf("Beta=%v and Correlation=%v should have same sign", m.Beta, m.Correlation)
				}
			}
		})
	}
}

func TestBenchmarkMetrics_InvariantsOnRealData(t *testing.T) {
	spyPts := loadPriceCSV(t, "spy-price.csv")
	armyPts := loadPriceCSV(t, "armyl-price.csv")

	spyRets := toDatedReturns(spyPts)
	armyRets := toDatedReturns(armyPts)

	armyAligned, spyAligned := alignByDate(armyRets, spyRets)
	if len(armyAligned) < 20 {
		t.Fatalf("too few aligned days: %d", len(armyAligned))
	}

	for _, rf := range []float64{0.0, 0.03, 0.05, -0.01} {
		m := CalculateBenchmarkMetrics(armyAligned, spyAligned, rf)

		// Correlation in [-1, 1]
		if m.Correlation < -1.01 || m.Correlation > 1.01 {
			t.Errorf("rf=%v: Correlation=%v outside [-1,1]", rf, m.Correlation)
		}
		// TE ≥ 0
		if m.TrackingError < -1e-12 {
			t.Errorf("rf=%v: TE=%v negative", rf, m.TrackingError)
		}
		// All finite
		for _, pair := range []struct {
			n string
			v float64
		}{
			{"Alpha", m.Alpha}, {"Beta", m.Beta}, {"Sharpe", CalculateStandaloneMetrics(armyAligned, rf).SharpeRatio},
			{"Treynor", m.TreynorRatio}, {"TE", m.TrackingError},
			{"IR", m.InformationRatio}, {"Corr", m.Correlation},
		} {
			assertFinite(t, pair.n, pair.v)
		}
	}
}

// ═══════════════════════════════════════════════════════════════
// Sub-period analysis: verify metrics change across market regimes
// ═══════════════════════════════════════════════════════════════

func TestBenchmarkMetrics_RealData_SubPeriods(t *testing.T) {
	pts := loadPriceCSV(t, "spy-price.csv")
	rets := rawReturns(toDatedReturns(pts))

	if len(rets) < 2000 {
		t.Fatalf("need at least 2000 SPY returns, got %d", len(rets))
	}

	// Split into first half and second half.
	mid := len(rets) / 2
	first := rets[:mid]
	second := rets[mid:]

	// Add constant alpha in first half, negative alpha in second half.
	port := make([]float64, len(rets))
	for i, r := range rets {
		if i < mid {
			port[i] = r + 0.0003 // +3 bps/day
		} else {
			port[i] = r - 0.0002 // -2 bps/day
		}
	}

	mFull := CalculateBenchmarkMetrics(port, rets, 0.03)
	mFirst := CalculateBenchmarkMetrics(port[:mid], first, 0.03)
	mSecond := CalculateBenchmarkMetrics(port[mid:], second, 0.03)

	// First half should have positive alpha, second half negative.
	if mFirst.Alpha <= 0 {
		t.Errorf("first half alpha should be positive, got %v", mFirst.Alpha)
	}
	if mSecond.Alpha >= 0 {
		t.Errorf("second half alpha should be negative, got %v", mSecond.Alpha)
	}

	// Full period alpha should be between the two halves.
	if mFull.Alpha >= mFirst.Alpha {
		t.Errorf("full alpha (%v) should be < first half alpha (%v)", mFull.Alpha, mFirst.Alpha)
	}
	if mFull.Alpha <= mSecond.Alpha {
		t.Errorf("full alpha (%v) should be > second half alpha (%v)", mFull.Alpha, mSecond.Alpha)
	}

	// Beta should be 1.0 within each half (constant shift doesn't affect beta).
	assertDelta(t, "Beta_first", mFirst.Beta, 1.0, 1e-9)
	assertDelta(t, "Beta_second", mSecond.Beta, 1.0, 1e-9)
	// Full period beta is slightly off 1.0 because the shift changes mid-series,
	// which introduces a tiny mean-shift effect on covariance.
	assertDelta(t, "Beta_full", mFull.Beta, 1.0, 1e-3)

	t.Logf("Sub-periods: Alpha_first=%.4f, Alpha_second=%.4f, Alpha_full=%.4f",
		mFirst.Alpha, mSecond.Alpha, mFull.Alpha)
}

// ═══════════════════════════════════════════════════════════════
// Sharpe ratio: specific numerical verification
// ═══════════════════════════════════════════════════════════════

func TestSharpeRatio_ManualCalculation(t *testing.T) {
	// Hand-calculate Sharpe for a small series and verify via CalculateStandaloneMetrics.
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}

	rf := 0.0 // zero risk-free for simplicity
	sm := CalculateStandaloneMetrics(port, rf)

	pMean := mean(port)
	pStd := stddev(port)
	expectedSharpe := pMean * math.Sqrt(252) / pStd

	assertDelta(t, "Sharpe", sm.SharpeRatio, expectedSharpe, 1e-12)

	// Now with non-zero rf.
	rf = 0.05
	dailyRf := math.Pow(1.05, 1.0/252.0) - 1
	sm = CalculateStandaloneMetrics(port, rf)
	expectedSharpe = (pMean - dailyRf) * math.Sqrt(252) / pStd
	assertDelta(t, "Sharpe_rf5pct", sm.SharpeRatio, expectedSharpe, 1e-12)
}

func TestTreynorRatio_ManualCalculation(t *testing.T) {
	port := []float64{0.02, 0.04, -0.02, 0.06, 0.01}
	bench := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	// port = 2 * bench, so beta = 2.

	rf := 0.03
	dailyRf := math.Pow(1.03, 1.0/252.0) - 1
	m := CalculateBenchmarkMetrics(port, bench, rf)

	assertDelta(t, "Beta", m.Beta, 2.0, 1e-9)
	expectedTreynor := (mean(port) - dailyRf) * 252 / m.Beta
	assertDelta(t, "Treynor", m.TreynorRatio, expectedTreynor, 1e-9)
}

func TestInformationRatio_ManualCalculation(t *testing.T) {
	port := []float64{0.012, 0.018, -0.008, 0.032, 0.003}
	bench := []float64{0.01, 0.02, -0.01, 0.03, 0.005}

	m := CalculateBenchmarkMetrics(port, bench, 0.0)

	diff := make([]float64, len(port))
	for i := range diff {
		diff[i] = port[i] - bench[i]
	}
	diffMean := mean(diff)
	te := stddev(diff) * math.Sqrt(252)
	expectedIR := diffMean * 252 / te

	assertDelta(t, "IR", m.InformationRatio, expectedIR, 1e-9)
	assertDelta(t, "TE", m.TrackingError, te, 1e-12)
}

func TestAlpha_ManualCalculation(t *testing.T) {
	port := []float64{0.012, 0.018, -0.008, 0.032, 0.003}
	bench := []float64{0.01, 0.02, -0.01, 0.03, 0.005}

	rf := 0.04
	dailyRf := math.Pow(1+rf, 1.0/252.0) - 1
	m := CalculateBenchmarkMetrics(port, bench, rf)

	pMean := mean(port)
	bMean := mean(bench)
	alphaDailyVal := pMean - (dailyRf + m.Beta*(bMean-dailyRf))
	expectedAlpha := math.Pow(1+alphaDailyVal, 252) - 1

	assertDelta(t, "Alpha", m.Alpha, expectedAlpha, 1e-9)
}

// ═══════════════════════════════════════════════════════════════
// Dead code / code quality observations
// ═══════════════════════════════════════════════════════════════

func TestBenchmarkMetrics_ExcessReturnSlicesUnused(t *testing.T) {
	// Verify the Sharpe (via CalculateStandaloneMetrics) matches the expected
	// formula using raw returns and dailyRf — confirming it uses pMean/dailyRf
	// directly and not any excess-return slices.
	port := []float64{0.01, 0.02, -0.01}

	dailyRf := math.Pow(1.03, 1.0/252.0) - 1
	pMean := mean(port)
	pStd := stddev(port)
	expectedSharpe := (pMean - dailyRf) * math.Sqrt(252) / pStd
	sm := CalculateStandaloneMetrics(port, 0.03)
	assertDelta(t, "Sharpe_no_excess_slice", sm.SharpeRatio, expectedSharpe, 1e-12)
}

// ═══════════════════════════════════════════════════════════════
// Treynor ratio with near-zero beta
// ═══════════════════════════════════════════════════════════════

func TestTreynorRatio_NearZeroBeta(t *testing.T) {
	// When beta is very small but non-zero (above the stddev threshold),
	// Treynor can become astronomically large. Verify it's at least finite.
	n := 200
	port := make([]float64, n)
	bench := make([]float64, n)
	for i := 0; i < n; i++ {
		// Benchmark with moderate variance.
		bench[i] = 0.01 * float64(i%5-2) // -0.02, -0.01, 0, 0.01, 0.02
		// Portfolio almost uncorrelated (slight positive correlation).
		port[i] = 0.005 + 0.0001*float64(i%5-2) // mostly constant, tiny beta
	}

	m := CalculateBenchmarkMetrics(port, bench, 0.0)

	// Beta should be small but positive.
	if m.Beta < 0 || m.Beta > 0.1 {
		t.Errorf("Beta = %v, expected small positive", m.Beta)
	}

	// Treynor could be very large when dividing by small beta.
	assertFinite(t, "Treynor", m.TreynorRatio)

	t.Logf("Near-zero beta: Beta=%.6f, Treynor=%.2f", m.Beta, m.TreynorRatio)
}
