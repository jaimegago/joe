package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
)

// The declared conclusion is only worth declaring if it ARRIVES: an evaluator
// keys on what reaches the wire, and a field that stops inside joe is a missing
// pipe rather than a field addition. These tests pin the shape the wire carries
// — joe-pm threads/declared-diagnostic-conclusion.md, order part 2.

func runConclusionTurn(t *testing.T, content string) taskResponse {
	t.Helper()
	reg := tools.NewRegistry()
	exec := tools.NewExecutor(reg, nil)
	// The second response answers the unfulfilled-tool-intent probe, which
	// every tool-call-free turn goes through.
	scripted := &finalScriptLLM{responses: []*llm.ChatResponse{{Content: content}, {Content: "DONE"}}}
	agent := agentloop.NewAgent(scripted, exec, reg, llm.StaticSystem("sys"))
	session := agentloop.NewSession(nil)
	t.Cleanup(session.Close)

	answer, err := agent.Run(context.Background(), session, "user-service keeps restarting")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return finalizeTaskResponse("t1", "s1", "completed", "", answer, nil, session, 200000, "", "", time.Second)
}

// TestConclusionReachesTheWire is order part 2 in one assertion: what the model
// declared is present on the response the evaluation harness reads.
func TestConclusionReachesTheWire(t *testing.T) {
	resp := runConclusionTurn(t,
		"The node CPU is the batch job's; user-service is being OOM killed.\n"+
			"ROOT-CAUSE: a memory leak in user-service exhausting its memory limit\n"+
			"DISCARDED: node-1 CPU at 97% | it is the batch workload's, and user-service shows no CPU throttling\n"+
			"TURN-KIND: answer")

	if !resp.ConclusionDeclared {
		t.Fatal("conclusion_declared = false, want true")
	}
	if resp.RootCause != "a memory leak in user-service exhausting its memory limit" {
		t.Errorf("root_cause = %q", resp.RootCause)
	}
	if len(resp.Discarded) != 1 {
		t.Fatalf("discarded = %+v, want one entry", resp.Discarded)
	}
	if resp.Discarded[0].Signal != "node-1 CPU at 97%" || resp.Discarded[0].Rationale == "" {
		t.Errorf("discarded[0] = %+v", resp.Discarded[0])
	}

	// The markers are plumbing. The operator-facing answer must not carry them.
	for _, marker := range []string{"ROOT-CAUSE", "DISCARDED", "TURN-KIND"} {
		if strings.Contains(resp.FinalAnswer, marker) {
			t.Errorf("final_answer leaked %s: %q", marker, resp.FinalAnswer)
		}
	}
}

// TestDiscardedSerializesAsEmptyArrayNotNull is the reason the field is not
// omitempty. A JSON null reintroduces exactly the absent-field ambiguity the
// tag exists to remove — "ruled nothing out" against "declared nothing" — and
// it would do it silently, on the far side of a pipe nobody re-reads.
func TestDiscardedSerializesAsEmptyArrayNotNull(t *testing.T) {
	t.Run("declared with nothing discarded", func(t *testing.T) {
		resp := runConclusionTurn(t, "ROOT-CAUSE: the init container failed\nTURN-KIND: answer")
		if !resp.ConclusionDeclared {
			t.Fatal("conclusion_declared = false, want true")
		}
		assertDiscardedIsEmptyArray(t, resp)
	})

	t.Run("nothing declared at all", func(t *testing.T) {
		resp := runConclusionTurn(t, "the readiness probe is misconfigured\nTURN-KIND: answer")
		if resp.ConclusionDeclared {
			t.Error("conclusion_declared = true, want false")
		}
		if resp.RootCause != "" {
			t.Errorf("root_cause = %q, want absent — joe must not infer one", resp.RootCause)
		}
		assertDiscardedIsEmptyArray(t, resp)
	})
}

func assertDiscardedIsEmptyArray(t *testing.T, resp taskResponse) {
	t.Helper()
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"discarded":[]`) {
		t.Errorf(`serialized response does not carry "discarded":[]; got %s`, string(encoded))
	}
	if strings.Contains(string(encoded), `"discarded":null`) {
		t.Error(`"discarded" serialized as null; the field must never be absent or null`)
	}
}
