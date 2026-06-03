package agentloop

import (
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

// TestEstimateMessageTokens_CharsOverFourRoundedUp asserts the heuristic:
// (content + tool-call name + JSON args) chars / 4, rounded up.
func TestEstimateMessageTokens_CharsOverFourRoundedUp(t *testing.T) {
	// 5 content chars -> ceil(5/4) = 2.
	if got := EstimateMessageTokens(llm.Message{Content: "hello"}); got != 2 {
		t.Errorf("5-char content estimate = %d, want 2", got)
	}
	// Empty -> 0.
	if got := EstimateMessageTokens(llm.Message{}); got != 0 {
		t.Errorf("empty message estimate = %d, want 0", got)
	}
	// Tool call contributes name + JSON args. name "t" (1) + args {"a":"b"}
	// marshals to 9 chars -> 10 chars total -> ceil(10/4) = 3.
	msg := llm.Message{ToolCalls: []llm.ToolCall{{Name: "t", Args: map[string]any{"a": "b"}}}}
	if got := EstimateMessageTokens(msg); got != 3 {
		t.Errorf("tool-call estimate = %d, want 3", got)
	}
}

// TestComputeInputTokenBudget_MatchesFormula asserts the budget equals
// floor(window*fraction) - maxOutput - overhead exactly.
func TestComputeInputTokenBudget_MatchesFormula(t *testing.T) {
	cases := []struct {
		window, maxOut, overhead int
		fraction                 float64
		want                     int
	}{
		// floor(200000*0.7)=140000 - 4096 - 1000 = 134904.
		{200000, 4096, 1000, 0.7, 134904},
		// floor(1048576*0.5)=524288 - 4096 - 0 = 520192.
		{1048576, 4096, 0, 0.5, 520192},
		// Floor truncates: floor(100*0.333)=33 (33.3) - 0 - 0 = 33.
		{100, 0, 0, 0.333, 33},
		// Pathologically small window can yield a non-positive result; the
		// formula is returned verbatim (callers clamp).
		{1000, 4096, 0, 0.7, 700 - 4096},
	}
	for _, tc := range cases {
		if got := ComputeInputTokenBudget(tc.window, tc.maxOut, tc.overhead, tc.fraction); got != tc.want {
			t.Errorf("ComputeInputTokenBudget(%d,%d,%d,%v) = %d, want %d",
				tc.window, tc.maxOut, tc.overhead, tc.fraction, got, tc.want)
		}
	}
}

// TestEstimateOverheadTokens_CountsPromptAndTools asserts the overhead
// estimate grows with the system prompt and the tool definitions.
func TestEstimateOverheadTokens_CountsPromptAndTools(t *testing.T) {
	base := EstimateOverheadTokens("you are joe", nil)
	if base == 0 {
		t.Fatal("non-empty system prompt estimated as 0 overhead")
	}
	withTool := EstimateOverheadTokens("you are joe", []llm.ToolDefinition{
		{Name: "graph_query", Description: "query the graph"},
	})
	if withTool <= base {
		t.Errorf("overhead with a tool (%d) should exceed prompt-only (%d)", withTool, base)
	}
}

// TestStaticContextBudget_DefaultFraction pins the backstop fraction.
func TestStaticContextBudget_DefaultFraction(t *testing.T) {
	if f := NewStaticContextBudget().BudgetFraction(); f != DefaultContextBudgetFraction {
		t.Errorf("static fraction = %v, want %v", f, DefaultContextBudgetFraction)
	}
	if DefaultContextBudgetFraction != 0.7 {
		t.Errorf("DefaultContextBudgetFraction = %v, want 0.7", DefaultContextBudgetFraction)
	}
}
