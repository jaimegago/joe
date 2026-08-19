package agentloop_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/tools"
)

// TestSessionTokenCeiling_TerminatesAtExpectedIteration drives the real
// loop against a fake adapter whose token usage per call (10k input + 10k
// output = 20k total) is set so the running total crosses a deliberately
// tight ceiling (45k) after the third call. The loop MUST:
//
//   - terminate with ErrSessionTokenCeiling via errors.Is (not a string
//     match);
//   - terminate at the third LLM call (the first call that pushes the
//     total to/above the ceiling);
//   - write exactly one KindLLMLimitTriggered audit row naming the
//     ceiling and the total at termination.
func TestSessionTokenCeiling_TerminatesAtExpectedIteration(t *testing.T) {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, observability.EnsureMetrics(nil))

	stub := &fixedUsageLLM{inputPerCall: 10_000, outputPerCall: 10_000}
	auditRepo := &spyAuditRepo{}
	tightLimits := tightSessionLimits{ceiling: 45_000}

	agent := agentloop.NewAgent(
		stub,
		executor,
		registry,
		llm.StaticSystem("sys"),
		agentloop.WithSessionLimits(tightLimits),
		agentloop.WithAuditRepo(auditRepo),
	)
	// Generous iteration cap — the ceiling, not the iteration cap, must
	// be what terminates this run. Without the ceiling the loop would
	// run to 50 iterations and exit via ErrMaxIterations.
	agent.SetMaxIterations(50)

	session := agentloop.NewSession(nil)
	defer session.Close()

	_, err := agent.Run(context.Background(), session, "go")
	if err == nil {
		t.Fatal("Run returned nil error; want ErrSessionTokenCeiling")
	}
	if !errors.Is(err, agentloop.ErrSessionTokenCeiling) {
		t.Fatalf("errors.Is(err, ErrSessionTokenCeiling) = false; err = %v", err)
	}
	// 20k per call: total reaches 20k, 40k, 60k after calls 1, 2, 3.
	// Call 3 is the first where total >= 45k, so termination is at call 3.
	if stub.callCount != 3 {
		t.Errorf("LLM called %d times; want 3 (terminate when total crosses ceiling)", stub.callCount)
	}
	if session.TotalTokens < 45_000 {
		t.Errorf("session.TotalTokens = %d; want >= 45000 at termination", session.TotalTokens)
	}
	// Message must name both the ceiling and the actual total so log
	// readers see a human-readable cause without parsing chained errors.
	msg := err.Error()
	if !strings.Contains(msg, "45000") {
		t.Errorf("error message %q missing the ceiling (45000)", msg)
	}
	// session.TotalTokens at termination should appear in the message.
	if !strings.Contains(msg, "60000") {
		t.Errorf("error message %q missing the actual session total (60000)", msg)
	}

	// Exactly one audit row, of the limit-triggered kind, naming the
	// ceiling and total in its context blob.
	rows := auditRepo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d; want 1", len(rows))
	}
	row := rows[0]
	if row.Kind != audit.KindLLMLimitTriggered {
		t.Errorf("audit kind = %q; want %q", row.Kind, audit.KindLLMLimitTriggered)
	}
	if row.Action != audit.ActionLLMRunawayTerminated {
		t.Errorf("audit action = %q; want %q", row.Action, audit.ActionLLMRunawayTerminated)
	}
	if row.Decision != audit.DecisionDeny {
		t.Errorf("audit decision = %q; want %q", row.Decision, audit.DecisionDeny)
	}
	if !strings.Contains(row.Context, `"session_token_ceiling":45000`) {
		t.Errorf("audit context %q missing session_token_ceiling=45000", row.Context)
	}
	if !strings.Contains(row.Context, `"session_token_total":60000`) {
		t.Errorf("audit context %q missing session_token_total=60000", row.Context)
	}
}

// TestSessionTokenCeiling_HappyPathUnchanged exercises a normal session
// under a generous ceiling: the LLM returns a final assistant message on
// the first call. Termination must NOT involve the ceiling sentinel —
// proving the ceiling is a backstop and does not fire in legitimate
// operation. This is the regression guard against accidentally tightening
// or always-firing the check.
func TestSessionTokenCeiling_HappyPathUnchanged(t *testing.T) {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, observability.EnsureMetrics(nil))

	stub := &finalAnswerLLM{
		response: "final answer",
		usage:    llm.TokenUsage{InputTokens: 1_000, OutputTokens: 500, TotalTokens: 1_500},
	}
	auditRepo := &spyAuditRepo{}

	agent := agentloop.NewAgent(
		stub,
		executor,
		registry,
		llm.StaticSystem("sys"),
		// Generous ceiling: 100M, well above the single call's 1500 total.
		agentloop.WithSessionLimits(tightSessionLimits{ceiling: 100_000_000}),
		agentloop.WithAuditRepo(auditRepo),
	)

	session := agentloop.NewSession(nil)
	defer session.Close()

	answer, err := agent.Run(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("Run returned err = %v; want nil (happy path)", err)
	}
	if errors.Is(err, agentloop.ErrSessionTokenCeiling) {
		t.Fatal("happy path matched ErrSessionTokenCeiling under errors.Is; backstop fired incorrectly")
	}
	if answer != "final answer" {
		t.Errorf("answer = %q; want %q", answer, "final answer")
	}
	if rows := auditRepo.snapshot(); len(rows) != 0 {
		t.Errorf("audit rows = %d; want 0 — no limit-triggered row on happy path", len(rows))
	}
}

// TestSessionTokenCeiling_DefaultProviderActive constructs an Agent with
// NO WithSessionLimits option — proving the safe-default static provider
// installed by NewAgent enforces the ceiling without explicit wiring.
// We use a fake adapter whose tokens-per-call is sized to overshoot the
// hardcoded DefaultSessionTokenCeiling in one shot, so the ceiling fires
// even though no provider was wired explicitly.
func TestSessionTokenCeiling_DefaultProviderActive(t *testing.T) {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, observability.EnsureMetrics(nil))

	// One call's usage equals the entire hardcoded ceiling, so the
	// first AddTokenUsage hits the threshold and the loop terminates
	// before iterating again.
	stub := &fixedUsageLLM{
		inputPerCall:  agentloop.DefaultSessionTokenCeiling / 2,
		outputPerCall: agentloop.DefaultSessionTokenCeiling / 2,
	}

	// No WithSessionLimits, no WithAuditRepo — the agent is wired
	// exactly as a legacy caller would wire it. The hardcoded static
	// provider must still be active.
	agent := agentloop.NewAgent(stub, executor, registry, llm.StaticSystem("sys"))
	agent.SetMaxIterations(50)

	session := agentloop.NewSession(nil)
	defer session.Close()

	_, err := agent.Run(context.Background(), session, "go")
	if err == nil {
		t.Fatal("Run returned nil; want ErrSessionTokenCeiling under default static provider")
	}
	if !errors.Is(err, agentloop.ErrSessionTokenCeiling) {
		t.Fatalf("errors.Is(err, ErrSessionTokenCeiling) = false; err = %v", err)
	}
	if stub.callCount != 1 {
		t.Errorf("LLM called %d times; want 1 (single call meets the hardcoded ceiling)", stub.callCount)
	}
}

// fixedUsageLLM emits a tool call on every Chat with a fixed-size token
// usage, so the running total grows by inputPerCall + outputPerCall per
// iteration. It targets a tool that does not exist; the executor returns
// a per-call error which the loop folds into the conversation rather
// than terminating, so iteration continues until the ceiling fires.
type fixedUsageLLM struct {
	inputPerCall  int
	outputPerCall int
	callCount     int
}

func (f *fixedUsageLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	f.callCount++
	return &llm.ChatResponse{
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Name: "nonexistent_tool",
			Args: map[string]any{},
		}},
		Usage: llm.TokenUsage{
			InputTokens:  f.inputPerCall,
			OutputTokens: f.outputPerCall,
			TotalTokens:  f.inputPerCall + f.outputPerCall,
		},
	}, nil
}

// finalAnswerLLM returns a single non-tool-call response that ends the
// loop cleanly on iteration 1.
type finalAnswerLLM struct {
	response string
	usage    llm.TokenUsage
}

func (f *finalAnswerLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: f.response, Usage: f.usage}, nil
}

// tightSessionLimits is a SessionLimits stub returning a configurable
// ceiling, used to keep the regression tests fast (the production
// hardcoded ceiling is 10M tokens — exercising it directly would be
// noisy in unit tests).
type tightSessionLimits struct{ ceiling int }

func (t tightSessionLimits) SessionTokenCeiling() int { return t.ceiling }

// spyAuditRepo records every Insert call for later inspection. Thread-
// safe so the loop's audit call can race with test assertions cleanly.
type spyAuditRepo struct {
	mu   sync.Mutex
	rows []audit.Event
}

func (s *spyAuditRepo) Insert(_ context.Context, e audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, e)
	return nil
}

// InsertTx satisfies the Stream G phase G4 addition to
// audit.Repository. The agent loop only calls Insert; the
// transactional path exists for the settings service. Stub records
// through the same slice so an unexpected use is visible.
func (s *spyAuditRepo) InsertTx(_ context.Context, _ *sql.Tx, e audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, e)
	return nil
}

func (s *spyAuditRepo) snapshot() []audit.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Event, len(s.rows))
	copy(out, s.rows)
	return out
}
