package llmusage_test

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/tools"
)

// TestAgenticLoop_RecordsOnePerChatCall drives the real agentloop.Agent
// with the recorder wrapping a scripted inner adapter. The invariant under
// test is one usage row per Chat call — the loop's call COUNT is not the
// subject and is asserted against the adapter's own counter, because a
// no-tool-call response now costs a second Chat call: the loop probes it for
// an unfulfilled tool intent before accepting it as final (see
// agentloop.probeUnfulfilledToolIntent). That probe is real spend and must be
// billed like any other call, which is exactly what this test pins.
//
// We assert the row contains the principal, model, separated token
// counts, configured currency, and the session AND task ids threaded
// through context — matching how the production /tasks handler wires
// things up at tasks.go:165.
func TestAgenticLoop_RecordsOnePerChatCall(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Content: "done",
			Usage:   llm.TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		},
	}
	repo := &fakeRepo{}
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	// Drive the real agentloop.Agent with the recorder as its
	// LLMAdapter. An empty registry/executor is sufficient because the
	// scripted response carries no tool calls — the loop terminates
	// after the first iteration.
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, nil)
	agent := agentloop.NewAgent(rec, executor, registry, llm.StaticSystem("system"))

	// Same context shape the /tasks handler installs: principal +
	// prepared session id + freshly-minted task id.
	ctx := rbac.WithPrincipal(context.Background(), rbac.Principal("user:alice"))
	ctx = agentctx.WithSessionID(ctx, "sess-abc")
	ctx = agentctx.WithTaskID(ctx, "task-xyz")

	session := agentloop.NewSession(nil)
	defer session.Close()
	if _, err := agent.Run(ctx, session, "hello"); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	rows := repo.snapshot()
	if len(rows) != inner.chatCalls {
		t.Fatalf("expected exactly 1 usage row per Chat call: %d Chat calls, %d rows", inner.chatCalls, len(rows))
	}
	if len(rows) == 0 {
		t.Fatal("expected at least one usage row")
	}
	got := rows[0]
	if got.Principal != "user:alice" {
		t.Errorf("principal = %q, want user:alice", got.Principal)
	}
	if got.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want claude-sonnet-4-20250514", got.Model)
	}
	if got.InputTokens != 100 || got.OutputTokens != 50 {
		t.Errorf("tokens (in/out) = %d/%d, want 100/50", got.InputTokens, got.OutputTokens)
	}
	if got.Currency != "USD" {
		t.Errorf("currency = %q, want USD", got.Currency)
	}
	if got.SessionID != "sess-abc" {
		t.Errorf("session_id = %q, want sess-abc", got.SessionID)
	}
	if got.TaskID != "task-xyz" {
		t.Errorf("task_id = %q, want task-xyz", got.TaskID)
	}
}

// TestNonAgenticChat_RecordsSessionWithoutTaskID reproduces the non-
// agentic Web UI chat path (internal/api/webui.go's handleChat): the
// handler threads ONLY the session id into context — no task id, since
// there is no task wrapping the call. The recorded row must capture
// the principal and session id; task_id must be empty (which the
// repository persists as SQL NULL).
func TestNonAgenticChat_RecordsSessionWithoutTaskID(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Content: "ok",
			Usage:   llm.TokenUsage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
		},
	}
	repo := &fakeRepo{}
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	// Mirror the webui.go wiring: principal (from edge auth) + session
	// id (from the request body) + no task id.
	ctx := rbac.WithPrincipal(context.Background(), rbac.Principal("user:bob"))
	ctx = agentctx.WithSessionID(ctx, "sess-non-agentic")

	if _, err := rec.Chat(ctx, llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.Principal != "user:bob" {
		t.Errorf("principal = %q, want user:bob", got.Principal)
	}
	if got.SessionID != "sess-non-agentic" {
		t.Errorf("session_id = %q, want sess-non-agentic", got.SessionID)
	}
	if got.TaskID != "" {
		t.Errorf("task_id = %q, want empty (non-agentic path has no task)", got.TaskID)
	}
}
