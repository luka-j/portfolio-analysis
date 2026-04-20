package stats

import "math"

// CorrelationMatrixResult holds the symbol list and the N×N correlation matrix.
type CorrelationMatrixResult struct {
	Symbols []string
	Matrix  [][]float64
}

// CalculateCorrelationMatrix computes pairwise Pearson correlations for the given
// per-symbol daily return series. Symbols with fewer than minObs non-zero observations
// are excluded. The diagonal is always 1. The matrix is symmetric.
func CalculateCorrelationMatrix(perSymbolReturns map[string][]float64, minObs int) CorrelationMatrixResult {
	if minObs <= 0 {
		minObs = 10
	}

	// Filter and collect symbols that have enough data.
	type symEntry struct {
		key     string
		returns []float64
	}
	var entries []symEntry
	for sym, rets := range perSymbolReturns {
		count := 0
		for _, r := range rets {
			if r != 0 {
				count++
			}
		}
		if count >= minObs {
			entries = append(entries, symEntry{key: sym, returns: rets})
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
			c := pearson(entries[i].returns, entries[j].returns)
			matrix[i][j] = c
			matrix[j][i] = c
		}
	}

	return CorrelationMatrixResult{Symbols: symbols, Matrix: matrix}
}

// dailyReturnsFromValues converts a slice of daily values to daily returns.
// Returns an empty slice when there are fewer than 2 values.
func dailyReturnsFromValues(vals []float64) []float64 {
	if len(vals) < 2 {
		return nil
	}
	out := make([]float64, len(vals)-1)
	for i := 1; i < len(vals); i++ {
		prev := vals[i-1]
		cur := vals[i]
		if prev > 1e-8 {
			out[i-1] = (cur - prev) / prev
		}
	}
	return out
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
