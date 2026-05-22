package llm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"portfolio-analysis/services/llm"
)

func TestCannedPrompt_Render(t *testing.T) {
	prompt := llm.CannedPrompt{
		Message: "Hello {name}, your target is {target}.",
	}

	// Test Render with empty variables
	assert.Equal(t, "Hello {name}, your target is {target}.", prompt.Render(nil))
	assert.Equal(t, "Hello {name}, your target is {target}.", prompt.Render(map[string]string{}))

	// Test Render with variables
	vars := map[string]string{
		"name":   "Alice",
		"target": "portfolio analysis",
	}
	assert.Equal(t, "Hello Alice, your target is portfolio analysis.", prompt.Render(vars))
}

func TestIsValidCannedType(t *testing.T) {
	// Test a valid chat accessible canned prompt type
	assert.True(t, llm.IsValidCannedType("general_analysis"))

	// Test a valid canned prompt type that is not chat accessible
	assert.False(t, llm.IsValidCannedType("market_summary"))

	// Test an invalid prompt type
	assert.False(t, llm.IsValidCannedType("non_existent_prompt"))
}
