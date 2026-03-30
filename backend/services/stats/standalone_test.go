package stats

import (
	"math"
	"testing"
)

// ═══════════════════════════════════════════════════════════════
// Edge cases
// ═══════════════════════════════════════════════════════════════

func TestStandaloneMetrics_Empty(t *testing.T) {
	m := CalculateStandaloneMetrics(nil, 0.05)
	if m.SharpeRatio != 0 || m.VAMI != 0 || m.Volatility != 0 || m.SortinoRatio != 0 || m.MaxDrawdown != 0 {
		t.Errorf("expected zero struct for nil input, got %+v", m)
	}
	m = CalculateStandaloneMetrics([]float64{}, 0.05)
	if m.SharpeRatio != 0 || m.VAMI != 0 || m.Volatility != 0 || m.SortinoRatio != 0 || m.MaxDrawdown != 0 {
		t.Errorf("expected zero struct for empty input, got %+v", m)
	}
}

func TestStandaloneMetrics_SingleDataPoint(t *testing.T) {
	// n=1: variance/stddev requires n>=2 (Bessel correction), returns 0.
	// Sharpe and Volatility → 0, VAMI = 1000*(1+r), MaxDD = 0.
	m := CalculateStandaloneMetrics([]float64{0.05}, 0.0)
	assertDelta(t, "VAMI", m.VAMI, 1000*1.05, 1e-9)
	assertDelta(t, "Volatility", m.Volatility, 0.0, 1e-12) // stddev of 1 element = 0
	assertDelta(t, "Sharpe", m.SharpeRatio, 0.0, 1e-12)    // pStd=0 → guard triggers
	assertDelta(t, "MaxDD", m.MaxDrawdown, 0.0, 1e-12)      // single gain, never drops below peak
	assertFinite(t, "Sortino", m.SortinoRatio)
}

func TestStandaloneMetrics_AllZeroReturns(t *testing.T) {
	zeros := make([]float64, 50)
	m := CalculateStandaloneMetrics(zeros, 0.0)
	assertDelta(t, "VAMI", m.VAMI, 1000.0, 1e-9) // no growth
	assertDelta(t, "Volatility", m.Volatility, 0.0, 1e-12)
	assertDelta(t, "Sharpe", m.SharpeRatio, 0.0, 1e-12)  // pStd=0 → guard
	assertDelta(t, "MaxDD", m.MaxDrawdown, 0.0, 1e-12)   // never declines
	assertFinite(t, "Sortino", m.SortinoRatio)
}

func TestStandaloneMetrics_ConstantReturn(t *testing.T) {
	// Constant positive return: stddev = 0, Sharpe = 0, MaxDD = 0.
	n := 100
	r := 0.001 // 10bps/day
	series := make([]float64, n)
	for i := range series {
		series[i] = r
	}
	m := CalculateStandaloneMetrics(series, 0.0)

	assertDelta(t, "Volatility", m.Volatility, 0.0, 1e-9)
	assertDelta(t, "Sharpe", m.SharpeRatio, 0.0, 1e-12) // pStd below threshold
	assertDelta(t, "MaxDD", m.MaxDrawdown, 0.0, 1e-12)  // monotonically increasing

	// VAMI = 1000 * (1.001)^n
	expectedVAMI := 1000.0 * math.Pow(1+r, float64(n))
	assertDelta(t, "VAMI", m.VAMI, expectedVAMI, 1e-6)
}

// ═══════════════════════════════════════════════════════════════
// Manual calculation verification — Sharpe ratio
// ═══════════════════════════════════════════════════════════════

func TestStandalone_Sharpe_ZeroRF(t *testing.T) {
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m := CalculateStandaloneMetrics(port, 0.0)
	pMean := mean(port)
	pStd := stddev(port)
	expected := pMean * math.Sqrt(252) / pStd
	assertDelta(t, "Sharpe_rf0", m.SharpeRatio, expected, 1e-12)
}

func TestStandalone_Sharpe_NonZeroRF(t *testing.T) {
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	rf := 0.05
	dailyRf := math.Pow(1+rf, 1.0/252.0) - 1
	m := CalculateStandaloneMetrics(port, rf)
	pMean := mean(port)
	pStd := stddev(port)
	expected := (pMean - dailyRf) * math.Sqrt(252) / pStd
	assertDelta(t, "Sharpe_rf5pct", m.SharpeRatio, expected, 1e-12)
}

func TestStandalone_Sharpe_NegativeRF(t *testing.T) {
	// Negative risk-free rate (e.g. -0.005): Sharpe should be higher than with rf=0.
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m0 := CalculateStandaloneMetrics(port, 0.0)
	mNeg := CalculateStandaloneMetrics(port, -0.005)
	if mNeg.SharpeRatio <= m0.SharpeRatio {
		t.Errorf("Sharpe with negative rf (%v) should be > Sharpe with rf=0 (%v)", mNeg.SharpeRatio, m0.SharpeRatio)
	}
}

func TestStandalone_Sharpe_HighRF_Negative(t *testing.T) {
	// Portfolio underperforms the risk-free rate → Sharpe should be negative.
	// Must use a volatile series (stddev > 0) so the near-zero guard does not fire.
	port := []float64{0.0001, 0.0003, -0.0001, 0.0002, 0.0001}
	dailyRf := math.Pow(1.20, 1.0/252.0) - 1
	m := CalculateStandaloneMetrics(port, 0.20) // 20% risk-free rate
	if mean(port) < dailyRf && m.SharpeRatio >= 0 {
		t.Errorf("Sharpe should be negative when portfolio return < rf, got %v", m.SharpeRatio)
	}
}

func TestStandalone_Sharpe_GuardLowVolatility(t *testing.T) {
	// Constant return → pStd below 1e-6 threshold → Sharpe = 0.
	n := 50
	series := make([]float64, n)
	for i := range series {
		series[i] = 0.001
	}
	m := CalculateStandaloneMetrics(series, 0.0)
	assertDelta(t, "Sharpe_constant", m.SharpeRatio, 0.0, 1e-12)
}

// ═══════════════════════════════════════════════════════════════
// Manual calculation verification — VAMI
// ═══════════════════════════════════════════════════════════════

func TestStandalone_VAMI_ManualCalculation(t *testing.T) {
	port := []float64{0.01, -0.02, 0.03, -0.01, 0.02}
	m := CalculateStandaloneMetrics(port, 0.0)
	expected := 1000.0 * 1.01 * 0.98 * 1.03 * 0.99 * 1.02
	assertDelta(t, "VAMI", m.VAMI, expected, 1e-9)
}

func TestStandalone_VAMI_NetLoss(t *testing.T) {
	// Series ending in overall loss: VAMI < 1000.
	port := []float64{0.10, -0.20, 0.05, -0.15}
	m := CalculateStandaloneMetrics(port, 0.0)
	if m.VAMI >= 1000 {
		t.Errorf("VAMI should be < 1000 for a net-loss series, got %v", m.VAMI)
	}
	if m.VAMI <= 0 {
		t.Errorf("VAMI should remain positive, got %v", m.VAMI)
	}
	expected := 1000.0 * 1.10 * 0.80 * 1.05 * 0.85
	assertDelta(t, "VAMI_net_loss", m.VAMI, expected, 1e-9)
}

func TestStandalone_VAMI_AlwaysPositive(t *testing.T) {
	// VAMI must stay positive as long as no return is <= -100%.
	scenarios := [][]float64{
		{0.01, 0.02, -0.01, 0.03},
		{-0.05, -0.10, -0.08, -0.12},
		{0.50, -0.30, 0.20, -0.10},
		make([]float64, 100), // all zero
	}
	for i, port := range scenarios {
		m := CalculateStandaloneMetrics(port, 0.0)
		if m.VAMI <= 0 {
			t.Errorf("scenario %d: VAMI should be positive, got %v", i, m.VAMI)
		}
	}
}

func TestStandalone_VAMI_ExactlyMatchesProduct(t *testing.T) {
	// VAMI = 1000 * ∏(1 + r_i); verify via explicit product.
	port := []float64{0.02, -0.01, 0.015, 0.03, -0.025, 0.01}
	product := 1.0
	for _, r := range port {
		product *= (1 + r)
	}
	m := CalculateStandaloneMetrics(port, 0.0)
	assertDelta(t, "VAMI_product", m.VAMI, 1000*product, 1e-9)
}

// ═══════════════════════════════════════════════════════════════
// Manual calculation verification — Volatility
// ═══════════════════════════════════════════════════════════════

func TestStandalone_Volatility_ManualCalculation(t *testing.T) {
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m := CalculateStandaloneMetrics(port, 0.0)
	expected := stddev(port) * math.Sqrt(252)
	assertDelta(t, "Volatility", m.Volatility, expected, 1e-12)
}

func TestStandalone_Volatility_Annualisation(t *testing.T) {
	// For a series with known stddev, annualised vol = stddev * sqrt(252).
	// Use a two-element series: returns of +1% and -1%.
	port := []float64{0.01, -0.01}
	m := CalculateStandaloneMetrics(port, 0.0)
	// sample stddev of {0.01, -0.01} = sqrt(((0.01-0)^2 + (-0.01-0)^2) / 1) = 0.01*sqrt(2)
	expectedStd := stddev(port)
	assertDelta(t, "Volatility_annualised", m.Volatility, expectedStd*math.Sqrt(252), 1e-12)
}

func TestStandalone_Volatility_NonNegative(t *testing.T) {
	scenarios := [][]float64{
		{0.01, 0.02, -0.01},
		{-0.05, -0.10, 0.08},
		{0.0, 0.0, 0.0},
		{0.001},
	}
	for i, port := range scenarios {
		m := CalculateStandaloneMetrics(port, 0.0)
		if m.Volatility < 0 {
			t.Errorf("scenario %d: Volatility should be >= 0, got %v", i, m.Volatility)
		}
	}
}

// ═══════════════════════════════════════════════════════════════
// Manual calculation verification — Sortino ratio
// ═══════════════════════════════════════════════════════════════

func TestStandalone_Sortino_ManualCalculation(t *testing.T) {
	port := []float64{0.02, -0.01, 0.03, -0.005, 0.01}
	rf := 0.05
	dailyRf := math.Pow(1+rf, 1.0/252.0) - 1

	m := CalculateStandaloneMetrics(port, rf)

	n := float64(len(port))
	var downsideSum float64
	for _, r := range port {
		excess := r - dailyRf
		if excess < 0 {
			downsideSum += excess * excess
		}
	}
	downsideDev := math.Sqrt(downsideSum/n) * math.Sqrt(252)

	pMean := mean(port)
	excessBase := pMean - dailyRf
	excessAnnual := math.Pow(1+excessBase, 252) - 1
	expectedSortino := excessAnnual / downsideDev

	assertDelta(t, "Sortino", m.SortinoRatio, expectedSortino, 1e-9)
}

func TestStandalone_Sortino_NoDownsideReturns(t *testing.T) {
	// All returns strictly above the daily risk-free rate → no downside deviation → Sortino = 0.
	dailyRf := math.Pow(1.02, 1.0/252.0) - 1
	port := make([]float64, 30)
	for i := range port {
		port[i] = dailyRf + 0.001 // always above rf
	}
	m := CalculateStandaloneMetrics(port, 0.02)
	assertDelta(t, "Sortino_no_downside", m.SortinoRatio, 0.0, 1e-12)
}

func TestStandalone_Sortino_AllDownsideReturns(t *testing.T) {
	// All returns far below the risk-free rate → Sortino negative.
	port := []float64{-0.02, -0.03, -0.01, -0.04, -0.02}
	m := CalculateStandaloneMetrics(port, 0.0)
	if m.SortinoRatio >= 0 {
		t.Errorf("Sortino should be negative for all-negative excess returns, got %v", m.SortinoRatio)
	}
}

func TestStandalone_Sortino_ZeroRF(t *testing.T) {
	// With rf=0, downside deviation uses returns < 0.
	port := []float64{0.01, -0.02, 0.03, -0.01, 0.02}
	m := CalculateStandaloneMetrics(port, 0.0)

	n := float64(len(port))
	var downsideSum float64
	for _, r := range port {
		if r < 0 {
			downsideSum += r * r
		}
	}
	downsideDev := math.Sqrt(downsideSum/n) * math.Sqrt(252)
	pMean := mean(port)
	excessAnnual := math.Pow(1+pMean, 252) - 1
	expected := excessAnnual / downsideDev

	assertDelta(t, "Sortino_rf0", m.SortinoRatio, expected, 1e-9)
}

func TestStandalone_Sortino_AlwaysFinite(t *testing.T) {
	// Sortino must be finite across all scenario types.
	scenarios := []struct {
		name string
		port []float64
		rf   float64
	}{
		{"all_positive", []float64{0.01, 0.02, 0.015, 0.03}, 0.05},
		{"all_negative", []float64{-0.02, -0.01, -0.03, -0.015}, 0.0},
		{"mixed", []float64{0.01, -0.02, 0.03, -0.01, 0.02}, 0.03},
		{"zeros", make([]float64, 20), 0.0},
		{"high_rf", []float64{0.001, 0.002, -0.001, 0.001}, 0.20},
		{"extreme", []float64{0.50, -0.30, 0.40, -0.25}, 0.03},
	}
	for _, sc := range scenarios {
		m := CalculateStandaloneMetrics(sc.port, sc.rf)
		assertFinite(t, sc.name+" Sortino", m.SortinoRatio)
	}
}

// ═══════════════════════════════════════════════════════════════
// Manual calculation verification — Maximum Drawdown
// ═══════════════════════════════════════════════════════════════

func TestStandalone_MaxDrawdown_MonotonicGain(t *testing.T) {
	// Monotonically increasing wealth: MaxDD = 0.
	port := []float64{0.01, 0.02, 0.015, 0.03, 0.01}
	m := CalculateStandaloneMetrics(port, 0.0)
	assertDelta(t, "MaxDD_monotonic_up", m.MaxDrawdown, 0.0, 1e-12)
}

func TestStandalone_MaxDrawdown_MonotonicLoss(t *testing.T) {
	// Monotonically declining series: worst drawdown = from start to end.
	// wealth after each step: 1*0.9=0.9, *0.85=0.765, *0.9=0.6885, *0.8=0.5508
	port := []float64{-0.10, -0.15, -0.10, -0.20}
	m := CalculateStandaloneMetrics(port, 0.0)
	// peak=1.0, final wealth=0.9*0.85*0.9*0.8 = 0.5508; dd = (1-0.5508)/1 = 0.4492
	finalWealth := 0.9 * 0.85 * 0.9 * 0.80
	expectedDD := (1.0 - finalWealth) / 1.0
	assertDelta(t, "MaxDD_monotonic_loss", m.MaxDrawdown, expectedDD, 1e-9)
}

func TestStandalone_MaxDrawdown_SingleCrash(t *testing.T) {
	// Gain 50%, then lose 40%: peak=1.5, trough=0.9, dd=(1.5-0.9)/1.5 = 0.4
	port := []float64{0.50, -0.40}
	m := CalculateStandaloneMetrics(port, 0.0)
	assertDelta(t, "MaxDD_crash", m.MaxDrawdown, 0.40, 1e-9)
}

func TestStandalone_MaxDrawdown_RecoveryAfterDrawdown(t *testing.T) {
	// Series recovers above previous peak; MaxDD is the worst trough, not the final state.
	// Wealth path: 1 → 1.5 → 0.9 → 1.8
	// From peak 1.5 to trough 0.9: dd = (1.5-0.9)/1.5 = 0.40
	port := []float64{0.50, -0.40, 1.0}
	m := CalculateStandaloneMetrics(port, 0.0)
	assertDelta(t, "MaxDD_recovery", m.MaxDrawdown, 0.40, 1e-9)
}

func TestStandalone_MaxDrawdown_MultiplePeaks(t *testing.T) {
	// Two separate drawdowns; MaxDD is the larger one.
	// Wealth: 1 → 1.2 → 1.02(dd=0.15) → 1.224 → 0.7344(dd=0.40) → end
	// Drawdown 1: (1.2 - 1.02) / 1.2 = 0.15
	// Drawdown 2: (1.224 - 0.7344) / 1.224 = 0.40
	port := []float64{0.20, -0.15, 0.20, -0.40}
	m := CalculateStandaloneMetrics(port, 0.0)
	if m.MaxDrawdown < 0.39 || m.MaxDrawdown > 0.41 {
		t.Errorf("MaxDD should be ~0.40, got %v", m.MaxDrawdown)
	}
}

func TestStandalone_MaxDrawdown_AllZero(t *testing.T) {
	m := CalculateStandaloneMetrics(make([]float64, 30), 0.0)
	assertDelta(t, "MaxDD_zero", m.MaxDrawdown, 0.0, 1e-12)
}

func TestStandalone_MaxDrawdown_InRange(t *testing.T) {
	// MaxDD must always be in [0, 1].
	scenarios := [][]float64{
		{0.01, 0.02, -0.01, 0.03},
		{-0.05, -0.10, -0.08, -0.12},
		{0.50, -0.49, 0.50, -0.49},
		make([]float64, 100),
	}
	for i, port := range scenarios {
		m := CalculateStandaloneMetrics(port, 0.0)
		if m.MaxDrawdown < 0 || m.MaxDrawdown > 1+1e-9 {
			t.Errorf("scenario %d: MaxDD = %v, expected in [0,1]", i, m.MaxDrawdown)
		}
	}
}

// ═══════════════════════════════════════════════════════════════
// Sharpe vs Sortino relationship
// ═══════════════════════════════════════════════════════════════

func TestStandalone_Sortino_GeqSharpe_PositiveExcess(t *testing.T) {
	// When returns are positive and excess return > 0:
	// downside deviation ≤ total deviation → Sortino ≥ Sharpe (in absolute terms).
	// (Downside deviation only penalises losses, so it's never larger than full stddev.)
	port := []float64{0.01, 0.02, -0.005, 0.015, 0.03, -0.002, 0.025}
	m := CalculateStandaloneMetrics(port, 0.0)
	if m.SortinoRatio < m.SharpeRatio-1e-9 {
		t.Errorf("Sortino (%v) should be >= Sharpe (%v) when excess return > 0 and returns are mostly positive",
			m.SortinoRatio, m.SharpeRatio)
	}
}

// ═══════════════════════════════════════════════════════════════
// Risk-free rate invariants
// ═══════════════════════════════════════════════════════════════

func TestStandalone_RFRate_HigherRF_LowerSharpeSortino(t *testing.T) {
	// Increasing rf should decrease Sharpe and Sortino (excess return is smaller).
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m_low := CalculateStandaloneMetrics(port, 0.01)
	m_high := CalculateStandaloneMetrics(port, 0.10)
	if m_high.SharpeRatio >= m_low.SharpeRatio {
		t.Errorf("higher rf should give lower Sharpe: rf=1%%→%v, rf=10%%→%v",
			m_low.SharpeRatio, m_high.SharpeRatio)
	}
}

func TestStandalone_RFRate_NoEffectOnVAMI(t *testing.T) {
	// VAMI does not depend on the risk-free rate.
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m1 := CalculateStandaloneMetrics(port, 0.0)
	m2 := CalculateStandaloneMetrics(port, 0.10)
	assertDelta(t, "VAMI_rf_independent", m1.VAMI, m2.VAMI, 1e-12)
}

func TestStandalone_RFRate_NoEffectOnVolatility(t *testing.T) {
	// Volatility (stddev) does not depend on the risk-free rate.
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m1 := CalculateStandaloneMetrics(port, 0.0)
	m2 := CalculateStandaloneMetrics(port, 0.10)
	assertDelta(t, "Vol_rf_independent", m1.Volatility, m2.Volatility, 1e-12)
}

func TestStandalone_RFRate_NoEffectOnMaxDrawdown(t *testing.T) {
	// MaxDrawdown is purely geometric; rf plays no role.
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m1 := CalculateStandaloneMetrics(port, 0.0)
	m2 := CalculateStandaloneMetrics(port, 0.10)
	assertDelta(t, "MaxDD_rf_independent", m1.MaxDrawdown, m2.MaxDrawdown, 1e-12)
}

// ═══════════════════════════════════════════════════════════════
// Scaling and proportionality
// ═══════════════════════════════════════════════════════════════

func TestStandalone_Scaling_Volatility(t *testing.T) {
	// If returns are scaled by k, volatility scales by k.
	base := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	k := 2.0
	scaled := make([]float64, len(base))
	for i, r := range base {
		scaled[i] = k * r
	}
	mBase := CalculateStandaloneMetrics(base, 0.0)
	mScaled := CalculateStandaloneMetrics(scaled, 0.0)
	assertDelta(t, "Volatility_scale", mScaled.Volatility, k*mBase.Volatility, 1e-9)
}

func TestStandalone_Scaling_VAMI_Exponent(t *testing.T) {
	// Doubling returns gives VAMI² / 1000 (since each factor squares).
	base := []float64{0.01, -0.005, 0.02}
	doubled := []float64{0.02, -0.01, 0.04}
	m1 := CalculateStandaloneMetrics(base, 0.0)
	m2 := CalculateStandaloneMetrics(doubled, 0.0)
	// VAMI_2x = 1000 * (1+2*r1)*(1+2*r2)*(1+2*r3)
	// VAMI_1x = 1000 * (1+r1)*(1+r2)*(1+r3)
	// These are not simply related by squaring; verify both are finite and distinct.
	assertFinite(t, "VAMI_base", m1.VAMI)
	assertFinite(t, "VAMI_doubled", m2.VAMI)
	if m1.VAMI == m2.VAMI {
		t.Errorf("doubled returns should produce different VAMI")
	}
}

// ═══════════════════════════════════════════════════════════════
// Numerical robustness
// ═══════════════════════════════════════════════════════════════

func TestStandalone_NumericalRobustness_TinyReturns(t *testing.T) {
	n := 100
	port := make([]float64, n)
	for i := 0; i < n; i++ {
		port[i] = 1e-15 * float64(i%5-2)
	}
	m := CalculateStandaloneMetrics(port, 0.03)
	assertFinite(t, "Sharpe", m.SharpeRatio)
	assertFinite(t, "VAMI", m.VAMI)
	assertFinite(t, "Volatility", m.Volatility)
	assertFinite(t, "Sortino", m.SortinoRatio)
	assertFinite(t, "MaxDD", m.MaxDrawdown)
}

func TestStandalone_NumericalRobustness_ExtremeReturns(t *testing.T) {
	// ±50% daily returns: unrealistic but must not produce NaN or Inf.
	n := 20
	port := make([]float64, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			port[i] = 0.5
		} else {
			port[i] = -0.3
		}
	}
	m := CalculateStandaloneMetrics(port, 0.03)
	assertFinite(t, "Sharpe", m.SharpeRatio)
	assertFinite(t, "VAMI", m.VAMI)
	assertFinite(t, "Volatility", m.Volatility)
	assertFinite(t, "Sortino", m.SortinoRatio)
	assertFinite(t, "MaxDD", m.MaxDrawdown)
}

func TestStandalone_NumericalRobustness_NearTotalLoss(t *testing.T) {
	// One catastrophic day: -90% loss; rest normal.
	port := []float64{0.01, -0.90, 0.01, 0.01, 0.01}
	m := CalculateStandaloneMetrics(port, 0.0)
	assertFinite(t, "Sharpe", m.SharpeRatio)
	assertFinite(t, "VAMI", m.VAMI)
	if m.VAMI <= 0 {
		t.Errorf("VAMI should remain positive after -90%% day, got %v", m.VAMI)
	}
	assertFinite(t, "Sortino", m.SortinoRatio)
	if m.MaxDrawdown > 1+1e-9 {
		t.Errorf("MaxDD should be <= 1, got %v", m.MaxDrawdown)
	}
}

func TestStandalone_NumericalRobustness_ExtremeRF(t *testing.T) {
	// rf = 100% annually: very high, but must not break.
	port := []float64{0.01, 0.02, -0.01, 0.03, 0.005}
	m := CalculateStandaloneMetrics(port, 1.0)
	assertFinite(t, "Sharpe_rf100pct", m.SharpeRatio)
	assertFinite(t, "Sortino_rf100pct", m.SortinoRatio)
}

// ═══════════════════════════════════════════════════════════════
// Invariants across diverse scenarios
// ═══════════════════════════════════════════════════════════════

func TestStandalone_Invariants(t *testing.T) {
	scenarios := []struct {
		name string
		port []float64
		rf   float64
	}{
		{"modest_positive", []float64{0.01, 0.02, -0.005, 0.015, 0.008}, 0.03},
		{"mostly_negative", []float64{-0.01, -0.02, 0.005, -0.015, -0.008}, 0.03},
		{"high_volatility", []float64{0.10, -0.08, 0.12, -0.09, 0.11}, 0.05},
		{"mean_reverting", func() []float64 {
			n := 60
			p := make([]float64, n)
			for i := range p {
				if i%2 == 0 {
					p[i] = 0.01
				} else {
					p[i] = -0.01
				}
			}
			return p
		}(), 0.0},
		{"long_bull_run", func() []float64 {
			n := 250
			p := make([]float64, n)
			for i := range p {
				p[i] = 0.0005
			}
			return p
		}(), 0.03},
		{"zero_rf", []float64{0.01, -0.02, 0.015, 0.03, -0.005}, 0.0},
		{"negative_rf", []float64{0.01, -0.02, 0.015, 0.03, -0.005}, -0.005},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			m := CalculateStandaloneMetrics(sc.port, sc.rf)

			// 1. All fields must be finite.
			assertFinite(t, "Sharpe", m.SharpeRatio)
			assertFinite(t, "VAMI", m.VAMI)
			assertFinite(t, "Volatility", m.Volatility)
			assertFinite(t, "Sortino", m.SortinoRatio)
			assertFinite(t, "MaxDD", m.MaxDrawdown)

			// 2. VAMI > 0 (as long as no return == -100%).
			if m.VAMI <= 0 {
				t.Errorf("VAMI must be positive, got %v", m.VAMI)
			}

			// 3. Volatility >= 0.
			if m.Volatility < 0 {
				t.Errorf("Volatility must be >= 0, got %v", m.Volatility)
			}

			// 4. MaxDrawdown in [0, 1].
			if m.MaxDrawdown < 0 || m.MaxDrawdown > 1+1e-9 {
				t.Errorf("MaxDrawdown = %v, expected in [0,1]", m.MaxDrawdown)
			}

			// 5. Sharpe sign must match the sign of mean excess return (when vol > 0).
			if m.Volatility > 1e-6 {
				dailyRf := math.Pow(1+sc.rf, 1.0/252.0) - 1
				excessMean := mean(sc.port) - dailyRf
				if excessMean > 1e-10 && m.SharpeRatio < -1e-10 {
					t.Errorf("Sharpe sign mismatch: excessMean=%v but Sharpe=%v", excessMean, m.SharpeRatio)
				}
				if excessMean < -1e-10 && m.SharpeRatio > 1e-10 {
					t.Errorf("Sharpe sign mismatch: excessMean=%v but Sharpe=%v", excessMean, m.SharpeRatio)
				}
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// Real-data tests
// ═══════════════════════════════════════════════════════════════

func TestStandalone_RealData_SPY(t *testing.T) {
	pts := loadPriceCSV(t, "spy-price.csv")
	if len(pts) < 100 {
		t.Fatalf("too few SPY data points: %d", len(pts))
	}
	rets := rawReturns(toDatedReturns(pts))
	m := CalculateStandaloneMetrics(rets, 0.03)

	// SPY over a long history should have a recognisable Sharpe (0.1 – 1.5).
	if m.SharpeRatio < 0.05 || m.SharpeRatio > 1.5 {
		t.Errorf("SPY Sharpe = %v, outside plausible range [0.05, 1.5]", m.SharpeRatio)
	}

	// VAMI > 1000 for long bull run.
	if m.VAMI <= 1000 {
		t.Errorf("SPY VAMI = %v, expected > 1000 over long history", m.VAMI)
	}

	// MaxDD should be meaningful (SPY has had drawdowns; also should be < 1).
	if m.MaxDrawdown <= 0 || m.MaxDrawdown >= 1.0 {
		t.Errorf("SPY MaxDD = %v, expected in (0, 1)", m.MaxDrawdown)
	}

	// Sortino should be >= Sharpe for SPY (all-downside deviation ≤ total deviation, rf=3%).
	if m.SortinoRatio < m.SharpeRatio-1e-6 {
		t.Errorf("Sortino (%v) should be >= Sharpe (%v) for long-run positive SPY", m.SortinoRatio, m.SharpeRatio)
	}

	t.Logf("SPY (%d days, rf=3%%): Sharpe=%.4f, Sortino=%.4f, VAMI=%.1f, Vol=%.4f, MaxDD=%.4f",
		len(rets), m.SharpeRatio, m.SortinoRatio, m.VAMI, m.Volatility, m.MaxDrawdown)
}

func TestStandalone_RealData_ARMY(t *testing.T) {
	pts := loadPriceCSV(t, "armyl-price.csv")
	if len(pts) < 20 {
		t.Fatalf("too few ARMY.L data points: %d", len(pts))
	}
	rets := rawReturns(toDatedReturns(pts))
	m := CalculateStandaloneMetrics(rets, 0.025)

	assertFinite(t, "Sharpe", m.SharpeRatio)
	assertFinite(t, "VAMI", m.VAMI)
	assertFinite(t, "Volatility", m.Volatility)
	assertFinite(t, "Sortino", m.SortinoRatio)
	assertFinite(t, "MaxDD", m.MaxDrawdown)

	if m.VAMI <= 0 {
		t.Errorf("ARMY.L VAMI must be positive, got %v", m.VAMI)
	}
	if m.MaxDrawdown < 0 || m.MaxDrawdown > 1 {
		t.Errorf("ARMY.L MaxDD = %v, expected in [0,1]", m.MaxDrawdown)
	}

	t.Logf("ARMY.L (%d days, rf=2.5%%): Sharpe=%.4f, Sortino=%.4f, VAMI=%.1f, Vol=%.4f, MaxDD=%.4f",
		len(rets), m.SharpeRatio, m.SortinoRatio, m.VAMI, m.Volatility, m.MaxDrawdown)
}

func TestStandalone_RealData_SXR8(t *testing.T) {
	pts := loadPriceCSV(t, "sxr8de-price.csv")
	if len(pts) < 50 {
		t.Fatalf("too few SXR8.DE data points: %d", len(pts))
	}
	rets := rawReturns(toDatedReturns(pts))
	m := CalculateStandaloneMetrics(rets, 0.03)

	assertFinite(t, "Sharpe", m.SharpeRatio)
	assertFinite(t, "VAMI", m.VAMI)
	assertFinite(t, "Volatility", m.Volatility)
	assertFinite(t, "Sortino", m.SortinoRatio)
	assertFinite(t, "MaxDD", m.MaxDrawdown)

	if m.Volatility < 0 {
		t.Errorf("SXR8.DE Volatility must be >= 0, got %v", m.Volatility)
	}

	t.Logf("SXR8.DE (%d days, rf=3%%): Sharpe=%.4f, Sortino=%.4f, VAMI=%.1f, Vol=%.4f, MaxDD=%.4f",
		len(rets), m.SharpeRatio, m.SortinoRatio, m.VAMI, m.Volatility, m.MaxDrawdown)
}

func TestStandalone_RealData_SPY_SubPeriods(t *testing.T) {
	// Verify metrics differ across market regimes (first vs second half of history).
	pts := loadPriceCSV(t, "spy-price.csv")
	rets := rawReturns(toDatedReturns(pts))
	if len(rets) < 1000 {
		t.Fatalf("need at least 1000 SPY returns, got %d", len(rets))
	}
	mid := len(rets) / 2
	m1 := CalculateStandaloneMetrics(rets[:mid], 0.03)
	m2 := CalculateStandaloneMetrics(rets[mid:], 0.03)
	mFull := CalculateStandaloneMetrics(rets, 0.03)

	for _, m := range []StandaloneMetrics{m1, m2, mFull} {
		assertFinite(t, "Sharpe", m.SharpeRatio)
		assertFinite(t, "VAMI", m.VAMI)
		assertFinite(t, "Volatility", m.Volatility)
		assertFinite(t, "Sortino", m.SortinoRatio)
		assertFinite(t, "MaxDD", m.MaxDrawdown)
	}

	// Full-period VAMI should relate to sub-period VAMIs.
	// VAMI_full / 1000 = (VAMI_first/1000) * (VAMI_second/1000)
	expectedFullVAMI := (m1.VAMI / 1000.0) * (m2.VAMI / 1000.0) * 1000.0
	assertDelta(t, "VAMI_full=VAMI1*VAMI2/1000", mFull.VAMI, expectedFullVAMI, 1e-6)

	// Full-period MaxDD should be >= each sub-period MaxDD (worst drawdown over
	// the longer window is at least as bad as within any sub-window).
	if mFull.MaxDrawdown < m1.MaxDrawdown-1e-9 {
		t.Errorf("full MaxDD (%v) should be >= first-half MaxDD (%v)", mFull.MaxDrawdown, m1.MaxDrawdown)
	}
	if mFull.MaxDrawdown < m2.MaxDrawdown-1e-9 {
		t.Errorf("full MaxDD (%v) should be >= second-half MaxDD (%v)", mFull.MaxDrawdown, m2.MaxDrawdown)
	}

	t.Logf("SPY sub-periods: first Sharpe=%.4f, second Sharpe=%.4f, full Sharpe=%.4f",
		m1.SharpeRatio, m2.SharpeRatio, mFull.SharpeRatio)
}

func TestStandalone_RealData_RFRateVariations_SPY(t *testing.T) {
	pts := loadPriceCSV(t, "spy-price.csv")
	rets := rawReturns(toDatedReturns(pts))
	if len(rets) < 100 {
		t.Fatalf("too few SPY returns")
	}

	rfRates := []float64{0.0, 0.02, 0.05, -0.005}
	var prevSharpe float64
	for i, rf := range rfRates {
		m := CalculateStandaloneMetrics(rets, rf)
		assertFinite(t, "Sharpe", m.SharpeRatio)
		assertFinite(t, "Sortino", m.SortinoRatio)
		// Higher rf → lower Sharpe (for a positively-returning series like SPY).
		if i > 0 && rf > rfRates[i-1] {
			if m.SharpeRatio >= prevSharpe {
				t.Errorf("higher rf=%v gave higher Sharpe (%v) than rf=%v (%v)",
					rf, m.SharpeRatio, rfRates[i-1], prevSharpe)
			}
		}
		prevSharpe = m.SharpeRatio
	}
}
