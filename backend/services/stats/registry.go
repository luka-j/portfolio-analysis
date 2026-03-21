// Package stats provides an extensible registry of portfolio statistics
// calculators. Each calculator implements the StatCalculator interface and
// registers itself so that the stats handler can compute them all dynamically.
//
// ## Accounting Models
//
// All calculations that involve monetary values accept a currency and
// accounting model parameter to control how multi-currency positions are
// converted before calculation:
//
//   - historical: convert each value using the FX rate on that specific date.
//     This gives the most accurate representation of how the portfolio
//     actually performed in the target currency over time.
//
//   - spot: convert all values using today's FX rate. Simpler but can distort
//     historical performance if exchange rates moved significantly.
//
//   - original: no conversion. Values remain in each security's native
//     currency. Only meaningful when all positions share one currency.
package stats

// StatCalculator is the interface for an extensible portfolio statistic.
type StatCalculator interface {
	// Name returns a unique identifier for this statistic (e.g. "mwr", "twr").
	Name() string

	// Description returns a human-readable description of the statistic.
	Description() string

	// Calculate computes the statistic. The params map can carry arbitrary
	// configuration (e.g. date range, currency). The returned interface{}
	// is serialised to JSON in the response.
	Calculate(params map[string]interface{}) (interface{}, error)
}

// Registry holds all registered stat calculators.
var Registry []StatCalculator

// Register adds a calculator to the global registry.
func Register(c StatCalculator) {
	Registry = append(Registry, c)
}

// CalculateAll runs every registered calculator and returns a map of
// name → result.
func CalculateAll(params map[string]interface{}) map[string]interface{} {
	results := make(map[string]interface{})
	for _, c := range Registry {
		val, err := c.Calculate(params)
		if err != nil {
			results[c.Name()] = map[string]string{"error": err.Error()}
		} else {
			results[c.Name()] = val
		}
	}
	return results
}
