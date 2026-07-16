package agentloop_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/tools"
)

// synthesizingLLM drives the loop to its iteration cap and then answers on the
// tool-less forced-synthesis call. It recognises the synthesis call by its
// appended final user message (prompts.MaxIterationsSynthesis): on that call it
// returns non-empty content; on every in-loop call it returns a tool call
// targeting a nonexistent tool (the executor errors, the loop folds the error
// into history and continues), so the loop runs to exhaustion.
type synthesizingLLM struct{ synthCalls int }

func (m *synthesizingLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Content == prompts.MaxIterationsSynthesis {
		m.synthCalls++
		return &llm.ChatResponse{
			Content: "synthesized answer from evidence gathered so far",
			Usage:   llm.TokenUsage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28},
		}, nil
	}
	return &llm.ChatResponse{
		ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "nonexistent_tool", Args: map[string]any{}}},
		Usage:     llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func maxIterAuditRows(rows []audit.Event) []audit.Event {
	var out []audit.Event
	for _, e := range rows {
		if e.Action == audit.ActionLLMMaxIterationsReached {
			out = append(out, e)
		}
	}
	return out
}

// TestMaxIterations_ForcedSynthesisSucceeds proves the loop-budget-exhaustion
// happy path: the loop exhausts its iteration cap, the forced-synthesis call
// produces an answer, and Run returns that answer with NO error (a completed
// run), stamps the session stop reason, keeps the synthesis call OBSERVER-SILENT
// (so the observed step count equals the cap, not cap+1), and writes exactly one
// llm_max_iterations_reached audit row whose blob records synthesized=true.
func TestMaxIterations_ForcedSynthesisSucceeds(t *testing.T) {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, observability.EnsureMetrics(nil))
	auditRepo := &spyAuditRepo{}
	observer := &agentloop.SliceObserver{}

	agent := agentloop.NewAgent(
		&synthesizingLLM{},
		executor,
		registry,
		"sys",
		agentloop.WithAuditRepo(auditRepo),
		agentloop.WithObserver(observer),
	)
	const iterCap = 3
	agent.SetMaxIterations(iterCap)

	session := agentloop.NewSession(nil)
	defer session.Close()

	answer, err := agent.Run(context.Background(), session, "investigate")
	if err != nil {
		t.Fatalf("Run returned error on successful synthesis: %v", err)
	}
	if answer == "" {
		t.Fatal("Run returned empty answer; want the synthesized text")
	}
	if got := session.StopReason(); got != agentloop.StopReasonMaxIterations {
		t.Errorf("session.StopReason() = %q, want %q", got, agentloop.StopReasonMaxIterations)
	}
	// Observer-silence: the synthesis call must not emit a step, so the observed
	// count is exactly the iteration cap.
	if len(observer.Steps) != iterCap {
		t.Errorf("observed steps = %d, want %d (synthesis call must be observer-silent)", len(observer.Steps), iterCap)
	}

	rows := maxIterAuditRows(auditRepo.snapshot())
	if len(rows) != 1 {
		t.Fatalf("llm_max_iterations_reached audit rows = %d, want 1", len(rows))
	}
	if rows[0].Reason != "max_iterations_exhausted" {
		t.Errorf("audit reason = %q, want max_iterations_exhausted", rows[0].Reason)
	}
	if rows[0].Kind != audit.KindLLMLimitTriggered {
		t.Errorf("audit kind = %q, want %q", rows[0].Kind, audit.KindLLMLimitTriggered)
	}
	var blob map[string]any
	if err := json.Unmarshal([]byte(rows[0].Context), &blob); err != nil {
		t.Fatalf("audit blob unmarshal: %v", err)
	}
	if blob["synthesized"] != true {
		t.Errorf("audit blob synthesized = %v, want true", blob["synthesized"])
	}
}

// TestMaxIterations_SynthesisFailureFallsThrough proves the fallback path: when
// the forced-synthesis call yields empty content (fixedUsageLLM returns a tool
// call, whose content is empty, even on the tool-less synthesis call), Run falls
// through to the ErrMaxIterations sentinel unchanged, leaves the session stop
// reason empty, and STILL writes exactly one audit row (blob synthesized=false).
func TestMaxIterations_SynthesisFailureFallsThrough(t *testing.T) {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, observability.EnsureMetrics(nil))
	auditRepo := &spyAuditRepo{}

	agent := agentloop.NewAgent(
		&fixedUsageLLM{inputPerCall: 10, outputPerCall: 5},
		executor,
		registry,
		"sys",
		agentloop.WithAuditRepo(auditRepo),
	)
	agent.SetMaxIterations(3)

	session := agentloop.NewSession(nil)
	defer session.Close()

	answer, err := agent.Run(context.Background(), session, "loop forever")
	if !errors.Is(err, agentloop.ErrMaxIterations) {
		t.Fatalf("errors.Is(err, ErrMaxIterations) = false; err = %v", err)
	}
	if answer != "" {
		t.Errorf("answer = %q, want empty on synthesis failure", answer)
	}
	if got := session.StopReason(); got != "" {
		t.Errorf("session.StopReason() = %q, want empty on synthesis failure", got)
	}

	rows := maxIterAuditRows(auditRepo.snapshot())
	if len(rows) != 1 {
		t.Fatalf("llm_max_iterations_reached audit rows = %d, want 1 (written whether or not synthesis succeeds)", len(rows))
	}
	var blob map[string]any
	if err := json.Unmarshal([]byte(rows[0].Context), &blob); err != nil {
		t.Fatalf("audit blob unmarshal: %v", err)
	}
	if blob["synthesized"] != false {
		t.Errorf("audit blob synthesized = %v, want false", blob["synthesized"])
	}
}
