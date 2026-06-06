package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
)

// overflowLLM rejects every Chat with a wrapped llm.ErrContextOverflow, exactly
// as a provider adapter does when the assembled prompt exceeds the model's
// context window (see internal/llm/claude.enhanceError / gemini).
type overflowLLM struct{ calls int }

func (o *overflowLLM) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	o.calls++
	return nil, fmt.Errorf("provider rejected: prompt is too long: %w", llm.ErrContextOverflow)
}

func (o *overflowLLM) Embed(context.Context, string) ([]float32, error) { return nil, nil }

// recordingAudit records every Insert/InsertTx for later inspection. Thread-safe
// so an assertion never races the writer.
type recordingAudit struct {
	mu   sync.Mutex
	rows []audit.Event
}

func (r *recordingAudit) Insert(_ context.Context, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, e)
	return nil
}

func (r *recordingAudit) InsertTx(_ context.Context, _ *sql.Tx, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, e)
	return nil
}

func (r *recordingAudit) snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Event, len(r.rows))
	copy(out, r.rows)
	return out
}

// TestContextOverflow_WritesAuditRow drives the real agentic loop against an
// adapter that rejects with a wrapped llm.ErrContextOverflow, then exercises the
// handler's audit gate (errors.Is(runErr, llm.ErrContextOverflow) →
// writeContextOverflowAudit). It MUST:
//
//   - classify the terminal status as "context_overflow" via errors.Is;
//   - write exactly one KindLLMLimitTriggered audit row, action
//     llm_context_overflow, decision deny, with the real caller principal and a
//     typed context blob naming the model, the effective context window, and the
//     session/task ids.
//
// Mirrors TestSessionTokenCeiling_TerminatesAtExpectedIteration's assertion
// shape (internal/agentloop/limits_test.go) for the sibling failure mode.
func TestContextOverflow_WritesAuditRow(t *testing.T) {
	srv, _, _ := setupContextPassServer(t)
	spy := &recordingAudit{}
	srv.services.Audit = spy
	// Swap the capturing LLM for one that overflows; the model key stays
	// "default" → claude-sonnet-4 → a 200000-token window in the capabilities
	// table, which the audit row must record.
	srv.services.LLM = llm.NewSwappableAdapter(&overflowLLM{}, "default")
	h := &taskHandler{server: srv}

	ctx := rbac.WithPrincipal(context.Background(), rbac.Principal("user:alice"))
	ctx = agentctx.WithSessionID(ctx, "sess-overflow")
	ctx = agentctx.WithTaskID(ctx, "task-overflow")

	prepared := h.buildTaskRun(ctx, taskRequest{Message: "hi"}, 1, &agentloop.SliceObserver{})
	defer prepared.session.Close()

	_, runErr := prepared.agent.Run(ctx, prepared.session, "hi")
	if !errors.Is(runErr, llm.ErrContextOverflow) {
		t.Fatalf("Run err = %v; want a wrapped llm.ErrContextOverflow", runErr)
	}
	if status, _ := taskStatus(ctx, runErr); status != "context_overflow" {
		t.Fatalf("taskStatus = %q; want context_overflow", status)
	}

	// The handler's audit gate (handleTask / handleTaskStream).
	if errors.Is(runErr, llm.ErrContextOverflow) {
		h.writeContextOverflowAudit(ctx, prepared)
	}

	rows := spy.snapshot()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d; want exactly 1", len(rows))
	}
	row := rows[0]
	if row.Kind != audit.KindLLMLimitTriggered {
		t.Errorf("audit kind = %q; want %q", row.Kind, audit.KindLLMLimitTriggered)
	}
	if row.Action != audit.ActionLLMContextOverflow {
		t.Errorf("audit action = %q; want %q", row.Action, audit.ActionLLMContextOverflow)
	}
	if row.Decision != audit.DecisionDeny {
		t.Errorf("audit decision = %q; want %q", row.Decision, audit.DecisionDeny)
	}
	if row.Principal != "user:alice" {
		t.Errorf("audit principal = %q; want %q", row.Principal, "user:alice")
	}
	if row.Reason != "context_window_exceeded" {
		t.Errorf("audit reason = %q; want context_window_exceeded", row.Reason)
	}
	for _, want := range []string{
		`"session_id":"sess-overflow"`,
		`"task_id":"task-overflow"`,
		`"model":"default"`,
		`"context_window_tokens":200000`,
		`"estimated_input_tokens":`,
	} {
		if !strings.Contains(row.Context, want) {
			t.Errorf("audit context %q missing %s", row.Context, want)
		}
	}
}

// TestContextOverflow_HappyPathNoAudit proves the overflow audit is NOT written
// on a turn that completes normally — the regression guard against an
// always-firing writer, mirroring TestSessionTokenCeiling_HappyPathUnchanged.
func TestContextOverflow_HappyPathNoAudit(t *testing.T) {
	srv, _, _ := setupContextPassServer(t)
	spy := &recordingAudit{}
	srv.services.Audit = spy
	// The default capturingChatLLM returns a clean final answer; the loop ends
	// without an overflow, so the handler's gate never fires.
	h := &taskHandler{server: srv}

	ctx := rbac.WithPrincipal(context.Background(), rbac.Principal("user:alice"))
	ctx = agentctx.WithSessionID(ctx, "sess-ok")
	ctx = agentctx.WithTaskID(ctx, "task-ok")

	prepared := h.buildTaskRun(ctx, taskRequest{Message: "hi"}, 1, &agentloop.SliceObserver{})
	defer prepared.session.Close()

	_, runErr := prepared.agent.Run(ctx, prepared.session, "hi")
	if runErr != nil {
		t.Fatalf("Run err = %v; want nil (happy path)", runErr)
	}
	if errors.Is(runErr, llm.ErrContextOverflow) {
		h.writeContextOverflowAudit(ctx, prepared)
	}
	if rows := spy.snapshot(); len(rows) != 0 {
		t.Errorf("audit rows = %d; want 0 — no overflow row on a completed turn", len(rows))
	}
}
