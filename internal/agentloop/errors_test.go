package agentloop_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/tools"
)

// TestErrMaxIterations_IsCheckable runs the loop with a max-iterations
// cap of 2 against an LLM stub that always returns a tool call, so the
// agent exhausts the cap. The returned error MUST be errors.Is against
// agentloop.ErrMaxIterations — the typed sentinel, not a string match.
func TestErrMaxIterations_IsCheckable(t *testing.T) {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, observability.EnsureMetrics(nil))
	agent := agentloop.NewAgent(&alwaysToolCallLLM{}, executor, registry, "sys")
	agent.SetMaxIterations(2)

	session := agentloop.NewSession(nil)
	defer session.Close()

	_, err := agent.Run(context.Background(), session, "hi")
	if err == nil {
		t.Fatal("Run returned nil error; want a max-iterations error")
	}
	if !errors.Is(err, agentloop.ErrMaxIterations) {
		t.Fatalf("errors.Is(err, ErrMaxIterations) = false; err = %v", err)
	}
}

// TestSentinels_Distinct asserts the three terminal-error sentinels
// (one in agentloop with a return site today, one in agentloop reserved
// for the G3 runaway gate, one in llmusage reserved for the G3 cost
// gate) are pairwise distinct under errors.Is — so the downstream
// classifier can branch unambiguously when the two reserved sentinels
// gain return sites.
func TestSentinels_Distinct(t *testing.T) {
	if errors.Is(agentloop.ErrMaxIterations, agentloop.ErrSessionTokenCeiling) {
		t.Error("ErrMaxIterations matches ErrSessionTokenCeiling under errors.Is")
	}
	if errors.Is(agentloop.ErrSessionTokenCeiling, agentloop.ErrMaxIterations) {
		t.Error("ErrSessionTokenCeiling matches ErrMaxIterations under errors.Is")
	}
	if errors.Is(agentloop.ErrMaxIterations, llmusage.ErrCostLimitExceeded) {
		t.Error("ErrMaxIterations matches ErrCostLimitExceeded under errors.Is")
	}
	if errors.Is(llmusage.ErrCostLimitExceeded, agentloop.ErrMaxIterations) {
		t.Error("ErrCostLimitExceeded matches ErrMaxIterations under errors.Is")
	}
	if errors.Is(agentloop.ErrSessionTokenCeiling, llmusage.ErrCostLimitExceeded) {
		t.Error("ErrSessionTokenCeiling matches ErrCostLimitExceeded under errors.Is")
	}
	if errors.Is(llmusage.ErrCostLimitExceeded, agentloop.ErrSessionTokenCeiling) {
		t.Error("ErrCostLimitExceeded matches ErrSessionTokenCeiling under errors.Is")
	}
}

// TestErrMaxIterations_RewordResilience confirms the classifier no
// longer depends on the exact message text. Wrapping the sentinel with
// an arbitrary descriptive prefix — what a future refactor of the
// loop's return statement might do — must still match under errors.Is.
// A pre-fix substring matcher would mis-classify any wrap whose first
// 15 characters differ from "max iterations ".
func TestErrMaxIterations_RewordResilience(t *testing.T) {
	cases := []error{
		fmt.Errorf("max iterations (10) reached without final response: %w", agentloop.ErrMaxIterations),
		fmt.Errorf("the agentic loop ran for too long: %w", agentloop.ErrMaxIterations),
		fmt.Errorf("iteration ceiling hit at step 7: %w", agentloop.ErrMaxIterations),
		fmt.Errorf("LOOP DONE: %w", agentloop.ErrMaxIterations),
	}
	for _, err := range cases {
		if !errors.Is(err, agentloop.ErrMaxIterations) {
			t.Errorf("errors.Is failed for wrapped error %q", err.Error())
		}
	}
}

// alwaysToolCallLLM is a minimal llm.LLMAdapter that always emits a
// tool call referencing a tool that does not exist in the registry, so
// each iteration drives the agentic loop forward (the executor records
// a tool error which is appended to history but does not terminate the
// loop). After MaxIterations iterations the loop exhausts its cap.
type alwaysToolCallLLM struct{}

func (alwaysToolCallLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Name: "nonexistent_tool",
			Args: map[string]any{},
		}},
	}, nil
}

func (alwaysToolCallLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("not implemented")
}
