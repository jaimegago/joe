//go:build e2e
// +build e2e

package e2e

import (
	"testing"
)

// TestE2E_TaskCompletesThroughScriptedResponder drives one agentic turn over
// the public API with the scripted responder standing in for the LLM: joe asks
// the responder, executes the tool call it gets back, asks again, takes the
// text as its answer, probes once, and returns a completed turn.
func TestE2E_TaskCompletesThroughScriptedResponder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	resp, err := harness.RunTask("Resolve localhost and tell me what you found.")
	if err != nil {
		t.Fatalf("POST /api/v1/tasks: %v", err)
	}

	if resp.Status != "completed" {
		t.Fatalf("status = %q (error %q), want completed", resp.Status, resp.Error)
	}
	if resp.FinalAnswer != DefaultAnswer {
		t.Errorf("final_answer = %q, want the responder's text %q", resp.FinalAnswer, DefaultAnswer)
	}

	// The tool call the responder asked for was executed, without error.
	var executed bool
	for _, step := range resp.Steps {
		for _, tr := range step.ToolResults {
			if tr.Name != DefaultToolName {
				continue
			}
			if tr.Error != "" {
				t.Errorf("%s returned an error: %s", DefaultToolName, tr.Error)
			}
			executed = true
		}
	}
	if !executed {
		t.Errorf("no %s tool result in the turn's steps; tools_used = %v", DefaultToolName, resp.ToolsUsed)
	}

	// Token usage reported by the responder reaches the turn's totals.
	if resp.TotalTokens.InputTokens == 0 || resp.TotalTokens.OutputTokens == 0 {
		t.Errorf("total_tokens = %+v, want the responder's usage counted", resp.TotalTokens)
	}

	// The responder saw the turn the default script describes: a tool call,
	// then the answer, then the no-tool-call probe.
	calls := harness.Responder.Calls()
	if len(calls) < 3 {
		t.Fatalf("responder received %d calls, want at least 3 (tool call, answer, probe)", len(calls))
	}
	if len(calls[0].Reply.ToolCalls) != 1 {
		t.Errorf("first call was answered with %d tool calls, want 1", len(calls[0].Reply.ToolCalls))
	}
	if !calls[1].Request.HasToolResult() {
		t.Error("second call carried no tool result; the tool call was not fed back to the model")
	}
	if !calls[len(calls)-1].Request.IsToolIntentProbe() {
		t.Error("last call was not the no-tool-call probe")
	}

	t.Logf("✓ task %s completed in %d iterations through %d responder calls", resp.TaskID, resp.Iterations, len(calls))
}
