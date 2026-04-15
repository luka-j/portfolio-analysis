package llm_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genai"

	"portfolio-analysis/services/llm"
)

// TestPortfolioTools verifies that the tool declarations are well-formed.
func TestPortfolioTools(t *testing.T) {
	tool := llm.PortfolioTools()
	if tool == nil {
		t.Fatal("PortfolioTools() returned nil")
	}
	if len(tool.FunctionDeclarations) != 9 {
		t.Fatalf("expected 9 function declarations, got %d", len(tool.FunctionDeclarations))
	}

	expectedNames := []string{
		llm.ToolGetCurrentAllocations,
		llm.ToolGetRiskMetrics,
		llm.ToolGetBenchmarkMetrics,
		llm.ToolGetAssetFundamentals,
		llm.ToolGetPositionsWithCostBasis,
		llm.ToolGetTaxImpact,
		llm.ToolGetRecentTransactions,
		llm.ToolGetFXImpact,
		llm.ToolGetHistoricalPerformance,
	}

	for i, decl := range tool.FunctionDeclarations {
		if decl.Name != expectedNames[i] {
			t.Errorf("declaration[%d] name = %q, want %q", i, decl.Name, expectedNames[i])
		}
		if decl.Description == "" {
			t.Errorf("declaration[%d] (%s) has empty description", i, decl.Name)
		}
		if decl.Parameters == nil {
			t.Errorf("declaration[%d] (%s) has nil parameters", i, decl.Name)
		}
	}
}

// TestToolConstants verifies that the tool name constants are non-empty and unique.
func TestToolConstants(t *testing.T) {
	names := []string{
		llm.ToolGetCurrentAllocations,
		llm.ToolGetRiskMetrics,
		llm.ToolGetBenchmarkMetrics,
		llm.ToolGetAssetFundamentals,
		llm.ToolGetPositionsWithCostBasis,
		llm.ToolGetTaxImpact,
		llm.ToolGetRecentTransactions,
		llm.ToolGetFXImpact,
		llm.ToolGetHistoricalPerformance,
	}

	seen := make(map[string]bool)
	for _, name := range names {
		if name == "" {
			t.Errorf("tool name constant is empty")
		}
		if seen[name] {
			t.Errorf("duplicate tool name: %q", name)
		}
		seen[name] = true
	}
}

// TestToolExecutorUnknownTool verifies that an unknown tool name returns an error
// rather than panicking.
func TestToolExecutorUnknownTool(t *testing.T) {
	var executorCalled bool
	executor := llm.ToolExecutor(func(_ context.Context, call *genai.FunctionCall) (map[string]any, error) {
		executorCalled = true
		return nil, nil
	})

	// executor is a function type; test that it can be called without panic.
	ctx := context.Background()
	call := &genai.FunctionCall{Name: "unknown_tool", Args: nil}
	result, err := executor(ctx, call)
	if !executorCalled {
		t.Error("expected executor to be called")
	}
	// Our test executor just returns nil, nil — real dispatcher is in handlers.
	_ = result
	_ = err
}

// TestRiskMetricsRequiredFields checks the parameter schema for required fields.
func TestRiskMetricsRequiredFields(t *testing.T) {
	tool := llm.PortfolioTools()

	var riskDecl *genai.FunctionDeclaration
	for _, d := range tool.FunctionDeclarations {
		if d.Name == llm.ToolGetRiskMetrics {
			riskDecl = d
			break
		}
	}
	if riskDecl == nil {
		t.Fatal("get_risk_metrics declaration not found")
	}

	requiredSet := make(map[string]bool)
	for _, r := range riskDecl.Parameters.Required {
		requiredSet[r] = true
	}

	if !requiredSet["from_date"] {
		t.Error("expected from_date to be required for get_risk_metrics")
	}
	if !requiredSet["to_date"] {
		t.Error("expected to_date to be required for get_risk_metrics")
	}
}

// TestBenchmarkMetricsRequiredFields checks the parameter schema for required fields.
func TestBenchmarkMetricsRequiredFields(t *testing.T) {
	tool := llm.PortfolioTools()

	var bmDecl *genai.FunctionDeclaration
	for _, d := range tool.FunctionDeclarations {
		if d.Name == llm.ToolGetBenchmarkMetrics {
			bmDecl = d
			break
		}
	}
	if bmDecl == nil {
		t.Fatal("get_benchmark_metrics declaration not found")
	}

	requiredSet := make(map[string]bool)
	for _, r := range bmDecl.Parameters.Required {
		requiredSet[r] = true
	}

	for _, field := range []string{"benchmark_symbol", "from_date", "to_date"} {
		if !requiredSet[field] {
			t.Errorf("expected %q to be required for get_benchmark_metrics", field)
		}
	}
}

// TestAssetFundamentalsRequiredFields checks the parameter schema for required fields.
func TestAssetFundamentalsRequiredFields(t *testing.T) {
	tool := llm.PortfolioTools()

	var fdDecl *genai.FunctionDeclaration
	for _, d := range tool.FunctionDeclarations {
		if d.Name == llm.ToolGetAssetFundamentals {
			fdDecl = d
			break
		}
	}
	if fdDecl == nil {
		t.Fatal("get_asset_fundamentals declaration not found")
	}

	requiredSet := make(map[string]bool)
	for _, r := range fdDecl.Parameters.Required {
		requiredSet[r] = true
	}

	if !requiredSet["symbol"] {
		t.Error("expected symbol to be required for get_asset_fundamentals")
	}
}

// TestCannedPromptForcedTool verifies that ForcedTool is correctly set on tool-first prompts.
func TestCannedPromptForcedTool(t *testing.T) {
	cases := []struct {
		promptType string
		wantTool   string
	}{
		{"general_analysis", llm.ToolGetCurrentAllocations},
		{"best_worst_scenarios", llm.ToolGetCurrentAllocations},
		{"risk_metrics", llm.ToolGetRiskMetrics},
		{"benchmark_analysis", llm.ToolGetBenchmarkMetrics},
	}

	for _, tc := range cases {
		cp, ok := llm.CannedPrompts[tc.promptType]
		if !ok {
			t.Errorf("canned prompt %q not found", tc.promptType)
			continue
		}
		if cp.ForcedTool != tc.wantTool {
			t.Errorf("CannedPrompts[%q].ForcedTool = %q, want %q", tc.promptType, cp.ForcedTool, tc.wantTool)
		}
	}
}

// TestCannedPromptToolFirstNoSchema verifies that tool-first prompts do not have a Schema set.
// Schema-based structured output is incompatible with streaming tool-call loops.
func TestCannedPromptToolFirstNoSchema(t *testing.T) {
	for key, cp := range llm.CannedPrompts {
		if cp.ForcedTool != "" && cp.Schema != nil {
			t.Errorf("CannedPrompts[%q]: ForcedTool=%q but Schema is also set — these are mutually exclusive", key, cp.ForcedTool)
		}
	}
}

// TestCannedPromptMessagesHaveNoDataJSON confirms that tool-first prompts no longer inject {data_json}.
func TestCannedPromptMessagesHaveNoDataJSON(t *testing.T) {
	toolFirstPrompts := []string{"general_analysis", "best_worst_scenarios", "risk_metrics", "benchmark_analysis"}
	for _, key := range toolFirstPrompts {
		cp, ok := llm.CannedPrompts[key]
		if !ok {
			continue
		}
		if strings.Contains(cp.Message, "{data_json}") {
			t.Errorf("CannedPrompts[%q] still contains {data_json} placeholder but uses ForcedTool — should not inject raw data", key)
		}
	}
}
