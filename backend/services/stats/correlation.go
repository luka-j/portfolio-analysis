package stats

import "math"

// CorrelationMatrixResult holds the symbol list and the N×N correlation matrix.
type CorrelationMatrixResult struct {
	Symbols []string
	Matrix  [][]float64
}

// CalculateCorrelationMatrix computes pairwise Pearson correlations for the given
// per-symbol daily return series. For each pair only days where both symbols are
// simultaneously active (mask == true) are used, so positions held for different
// periods are correlated only over their overlapping window instead of being padded
// with zeros. Symbols with fewer than minObs active observations are excluded.
// The diagonal is always 1. The matrix is symmetric.
func CalculateCorrelationMatrix(perSymbolReturns map[string][]float64, perSymbolMask map[string][]bool, minObs int) CorrelationMatrixResult {
	if minObs <= 0 {
		minObs = 10
	}

	// Filter and collect symbols that have enough active observations.
	type symEntry struct {
		key     string
		returns []float64
		mask    []bool
	}
	var entries []symEntry
	for sym, rets := range perSymbolReturns {
		mask := perSymbolMask[sym]
		count := 0
		for _, m := range mask {
			if m {
				count++
			}
		}
		if count >= minObs {
			entries = append(entries, symEntry{key: sym, returns: rets, mask: mask})
		}
	}
	if len(entries) == 0 {
		return CorrelationMatrixResult{}
	}

	// Stable sort by symbol name for reproducible ordering.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].key < entries[j-1].key; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	n := len(entries)
	symbols := make([]string, n)
	for i, e := range entries {
		symbols[i] = e.key
	}

	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
		matrix[i][i] = 1.0
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			c := pearsonMasked(entries[i].returns, entries[j].returns, entries[i].mask, entries[j].mask)
			matrix[i][j] = c
			matrix[j][i] = c
		}
	}

	return CorrelationMatrixResult{Symbols: symbols, Matrix: matrix}
}

// pearsonMasked computes Pearson correlation using only indices where both mx[i]
// and my[i] are true (the overlapping active window). Returns 0 when fewer than
// 2 overlapping observations exist or either sub-series has zero variance.
func pearsonMasked(xs, ys []float64, mx, my []bool) float64 {
	n := len(xs)
	if n != len(ys) || n != len(mx) || n != len(my) || n == 0 {
		return 0
	}
	// Collect the intersection in-place to avoid allocating two full-length slices.
	xsub := xs[:0:0]
	ysub := ys[:0:0]
	for i := 0; i < n; i++ {
		if mx[i] && my[i] {
			xsub = append(xsub, xs[i])
			ysub = append(ysub, ys[i])
		}
	}
	return pearson(xsub, ysub)
}

// pearson computes the Pearson correlation coefficient for two equal-length slices.
// Returns 0 when either series has zero variance.
func pearson(xs, ys []float64) float64 {
	n := len(xs)
	if n != len(ys) || n < 2 {
		return 0
	}
	cov := covariance(xs, ys)
	sx := stddev(xs)
	sy := stddev(ys)
	if sx < 1e-12 || sy < 1e-12 {
		return 0
	}
	r := cov / (sx * sy)
	// Clamp to [-1, 1] to guard against floating-point noise.
	return math.Max(-1, math.Min(1, r))
}
