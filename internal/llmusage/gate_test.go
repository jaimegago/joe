package llmusage_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
)

// tightCostLimits returns the configured per-window thresholds. A
// zero or negative value disables that window's check, matching the
// behaviour the CostLimits interface contract documents.
type tightCostLimits struct {
	hourly  int64
	daily   int64
	monthly int64
}

func (t tightCostLimits) HourlyLimitNano() int64  { return t.hourly }
func (t tightCostLimits) DailyLimitNano() int64   { return t.daily }
func (t tightCostLimits) MonthlyLimitNano() int64 { return t.monthly }

// spyAudit records every Insert call for inspection. Thread-safe so
// concurrent gate paths cannot race with the test assertions.
type spyAudit struct {
	mu     sync.Mutex
	events []audit.Event
}

func (s *spyAudit) Insert(_ context.Context, e audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *spyAudit) snapshot() []audit.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Event, len(s.events))
	copy(out, s.events)
	return out
}

// newGateRecorder is the wiring helper for the gate tests. It threads
// the cost-limits provider and audit sink into NewRecorderAdapter and
// returns the recorder plus the captured log buffer.
func newGateRecorder(t *testing.T, repo llmusage.Repository, inner llm.LLMAdapter, limits llmusage.CostLimits, audit audit.Repository) (*llmusage.RecorderAdapter, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := llmusage.NewRecorderAdapter(llmusage.Config{
		Inner:    inner,
		Repo:     repo,
		Provider: "claude",
		Model:    "claude-sonnet-4-20250514",
		Currency: "USD",
		FXRate:   1.0,
		Limits:   limits,
		Audit:    audit,
		Logger:   logger,
	})
	return rec, buf
}

// TestGate_HourlyLimit_RefusesBeforeInnerCall is the headline G3b
// regression: with a low hourly limit and pre-seeded usage rows in
// the current hour summing at or above it, a Chat is refused with
// llmusage.ErrCostLimitExceeded via errors.Is BEFORE the inner
// adapter is called — assert the inner adapter was not invoked, no
// new usage row was written, and one audit row of the
// limit-triggered kind names the hourly window.
func TestGate_HourlyLimit_RefusesBeforeInnerCall(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Content: "would-be-response",
			Usage:   llm.TokenUsage{InputTokens: 1, OutputTokens: 1},
		},
	}
	repo := &fakeRepo{}
	// Pre-seed a row in the current hour at the limit.
	now := time.Now().UTC()
	repo.rows = []llmusage.Row{{
		Timestamp:         now.Add(-5 * time.Minute),
		Currency:          "USD",
		EstimatedCostNano: 1_000,
	}}
	limits := tightCostLimits{hourly: 1_000, daily: -1, monthly: -1}
	auditSpy := &spyAudit{}
	rec, _ := newGateRecorder(t, repo, inner, limits, auditSpy)

	resp, err := rec.Chat(context.Background(), llm.ChatRequest{})
	if err == nil {
		t.Fatal("Chat returned nil error; gate must refuse when hourly window is over limit")
	}
	if !errors.Is(err, llmusage.ErrCostLimitExceeded) {
		t.Fatalf("errors.Is(err, ErrCostLimitExceeded) = false; err = %v", err)
	}
	if resp != nil {
		t.Errorf("Chat returned resp = %+v; want nil on gated refusal", resp)
	}
	if inner.chatCalls != 0 {
		t.Errorf("inner adapter called %d times; want 0 — gate must fire BEFORE the inner call", inner.chatCalls)
	}
	// Pre-seeded row should still be the only row. The gate must NOT
	// write a usage row for the refused call.
	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Errorf("repo has %d rows; want 1 (pre-seed only) — refused call must not record a usage row", len(rows))
	}
	events := auditSpy.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit rows = %d; want 1 (refusal)", len(events))
	}
	ev := events[0]
	if ev.Kind != audit.KindLLMLimitTriggered {
		t.Errorf("audit kind = %q; want %q", ev.Kind, audit.KindLLMLimitTriggered)
	}
	if ev.Action != audit.ActionLLMCostLimitRefused {
		t.Errorf("audit action = %q; want %q", ev.Action, audit.ActionLLMCostLimitRefused)
	}
	if ev.Decision != audit.DecisionDeny {
		t.Errorf("audit decision = %q; want %q", ev.Decision, audit.DecisionDeny)
	}
	if !strings.Contains(ev.Context, `"hourly"`) {
		t.Errorf("audit context %q missing hourly window name", ev.Context)
	}
	// Message must name window + observed + limit so log readers see
	// the cause without parsing chained errors.
	msg := err.Error()
	if !strings.Contains(msg, "hourly") {
		t.Errorf("error message %q missing 'hourly'", msg)
	}
	if !strings.Contains(msg, "1000") {
		t.Errorf("error message %q missing observed/limit value 1000", msg)
	}
}

// TestGate_GenerousLimits_HappyPathUnchanged: under generous limits a
// Chat proceeds normally, records its row, and the sentinel is NOT
// returned. This is the regression guard against an always-firing or
// silently-disabled gate — it MUST not fire under normal load.
func TestGate_GenerousLimits_HappyPathUnchanged(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Content: "real answer",
			Usage:   llm.TokenUsage{InputTokens: 12, OutputTokens: 7},
		},
	}
	repo := &fakeRepo{}
	// Generous: 1 USD per window — 1e9 nano-units — far above the
	// per-call estimate (141_000 nano-USD for 12 in / 7 out at the
	// Claude default price).
	limits := tightCostLimits{hourly: 1_000_000_000, daily: 1_000_000_000, monthly: 1_000_000_000}
	auditSpy := &spyAudit{}
	rec, _ := newGateRecorder(t, repo, inner, limits, auditSpy)

	resp, err := rec.Chat(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat returned err = %v on generous limits; gate fired incorrectly", err)
	}
	if errors.Is(err, llmusage.ErrCostLimitExceeded) {
		t.Fatal("happy path matched ErrCostLimitExceeded under errors.Is")
	}
	if resp == nil || resp.Content != "real answer" {
		t.Errorf("Chat lost or mutated the inner response: %+v", resp)
	}
	if inner.chatCalls != 1 {
		t.Errorf("inner adapter called %d times; want 1", inner.chatCalls)
	}
	if rows := repo.snapshot(); len(rows) != 1 {
		t.Errorf("rows = %d; want 1 (happy path records a row)", len(rows))
	}
	if events := auditSpy.snapshot(); len(events) != 0 {
		t.Errorf("audit rows = %d; want 0 — gate must not write on happy path", len(events))
	}
}

// TestGate_AggregationFailure_FailsOpen: when the aggregation query
// errors, the gate ALLOWS the call (fail-open) and emits the
// gate-read-failure warning + audit row. This is the explicit
// opposite of the recording fail-open posture from G2 — it must be
// documented and asserted lest a future change harden the gate to
// fail-closed and turn a database blip into a system-wide outage.
func TestGate_AggregationFailure_FailsOpen(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Content: "served despite gate read failure",
			Usage:   llm.TokenUsage{InputTokens: 3, OutputTokens: 4},
		},
	}
	repo := &fakeRepo{sumErr: errors.New("database is locked")}
	limits := tightCostLimits{hourly: 1, daily: 1, monthly: 1} // tight; would refuse if read worked
	auditSpy := &spyAudit{}
	rec, buf := newGateRecorder(t, repo, inner, limits, auditSpy)

	resp, err := rec.Chat(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat returned err = %v; gate read failure must fail OPEN (allow the call)", err)
	}
	if errors.Is(err, llmusage.ErrCostLimitExceeded) {
		t.Fatal("gate-read failure must NOT manifest as ErrCostLimitExceeded — it must allow the call silently")
	}
	if resp == nil || resp.Content != "served despite gate read failure" {
		t.Errorf("inner response lost: %+v", resp)
	}
	if inner.chatCalls != 1 {
		t.Errorf("inner adapter called %d times; want 1 (fail-open allows the call through)", inner.chatCalls)
	}
	logged := buf.String()
	if !strings.Contains(logged, "fail-open") {
		t.Errorf("warn log missing 'fail-open' phrase; got:\n%s", logged)
	}
	if !strings.Contains(logged, "cost-window gate") {
		t.Errorf("warn log missing 'cost-window gate' phrase; got:\n%s", logged)
	}
	events := auditSpy.snapshot()
	// Expect exactly one gate-read-failure audit row. The mixed-
	// currency detector also runs once but its query (CountForeignCurrency)
	// is independent and returned no rows — so we expect exactly 1.
	gateReadRows := 0
	for _, ev := range events {
		if ev.Reason == "cost_window_gate_read_failed" {
			gateReadRows++
			if ev.Decision != audit.DecisionAllow {
				t.Errorf("gate-read-failure decision = %q; want %q (call allowed)", ev.Decision, audit.DecisionAllow)
			}
			if ev.Kind != audit.KindLLMLimitTriggered {
				t.Errorf("gate-read-failure kind = %q; want %q", ev.Kind, audit.KindLLMLimitTriggered)
			}
		}
	}
	if gateReadRows != 1 {
		t.Errorf("gate-read-failure audit rows = %d; want 1", gateReadRows)
	}
}

// TestGate_ZeroLimit_NotEnforced: a window with a zero or negative
// limit is not enforced. Even with a pre-seeded row that would trip
// it under a positive limit, the call must proceed unrefused.
func TestGate_ZeroLimit_NotEnforced(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{Usage: llm.TokenUsage{}},
	}
	repo := &fakeRepo{}
	now := time.Now().UTC()
	repo.rows = []llmusage.Row{{
		Timestamp:         now.Add(-1 * time.Minute),
		Currency:          "USD",
		EstimatedCostNano: 999_999_999_999,
	}}
	// All three windows disabled.
	limits := tightCostLimits{hourly: 0, daily: -1, monthly: 0}
	auditSpy := &spyAudit{}
	rec, _ := newGateRecorder(t, repo, inner, limits, auditSpy)

	if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat returned err = %v; zero/negative limits must disable enforcement", err)
	}
	if inner.chatCalls != 1 {
		t.Errorf("inner adapter called %d times; want 1", inner.chatCalls)
	}
	// The gate must not even READ the window when its limit is
	// disabled. SumCostNano was, however, NOT skipped for the rows we
	// counted via the foreign-currency detector — that's a separate
	// query. We only assert SumCostNano was never called.
	if got := repo.sumCallCount(); got != 0 {
		t.Errorf("SumCostNano called %d times; want 0 (all windows disabled, no read needed)", got)
	}
	if events := auditSpy.snapshot(); len(events) != 0 {
		t.Errorf("audit rows = %d; want 0 — disabled windows must not write refusal rows", len(events))
	}
}

// TestGate_MultipleWindowsOver_AllNamedInAuditContext: when more than
// one window is over its limit, the refusal audit row's structured
// context must name EVERY tripped window — not only the first — so
// the operator can see whether one or several tripped.
func TestGate_MultipleWindowsOver_AllNamedInAuditContext(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{Usage: llm.TokenUsage{}},
	}
	repo := &fakeRepo{}
	now := time.Now().UTC()
	// Seed enough rows in the current hour, day, and month to trip
	// all three windows simultaneously. A single row in the current
	// hour is also in the current day and current month, so one row
	// at 3 nano covers all three windows when limits are tight.
	repo.rows = []llmusage.Row{{
		Timestamp:         now.Add(-10 * time.Minute),
		Currency:          "USD",
		EstimatedCostNano: 3,
	}}
	limits := tightCostLimits{hourly: 1, daily: 1, monthly: 1}
	auditSpy := &spyAudit{}
	rec, _ := newGateRecorder(t, repo, inner, limits, auditSpy)

	_, err := rec.Chat(context.Background(), llm.ChatRequest{})
	if !errors.Is(err, llmusage.ErrCostLimitExceeded) {
		t.Fatalf("errors.Is(err, ErrCostLimitExceeded) = false; err = %v", err)
	}
	events := auditSpy.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit rows = %d; want 1 refusal row", len(events))
	}
	ctx := events[0].Context
	for _, name := range []string{`"hourly"`, `"daily"`, `"monthly"`} {
		if !strings.Contains(ctx, name) {
			t.Errorf("audit context %q missing window name %s — all tripped windows must be named", ctx, name)
		}
	}
}

// TestGate_DefaultProvider_GateActiveOnNilLimits: a Config with no
// Limits field (nil) must still install the StaticCostLimits provider
// so the gate is enforced. Same posture as agentloop.NewAgent's
// default session-limits installation.
func TestGate_DefaultProvider_GateActiveOnNilLimits(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{Usage: llm.TokenUsage{}},
	}
	repo := &fakeRepo{}
	// No Limits, no Audit — the recorder should still install the
	// hardcoded static backstop and run the gate. With an empty table
	// and the static thresholds (~100/500/5,000 units), nothing fires
	// and the call passes through.
	rec := llmusage.NewRecorderAdapter(llmusage.Config{
		Inner:    inner,
		Repo:     repo,
		Provider: "claude",
		Model:    "claude-sonnet-4-20250514",
		Currency: "USD",
		FXRate:   1.0,
	})
	if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat err = %v on empty-table default-provider path; gate should not fire", err)
	}
	if inner.chatCalls != 1 {
		t.Errorf("inner adapter called %d times; want 1", inner.chatCalls)
	}
	// The gate must have READ the three windows on the call — proving
	// it ran rather than being silently disabled.
	if got := repo.sumCallCount(); got != 3 {
		t.Errorf("SumCostNano called %d times; want 3 (one per window, static provider returns positive limits)", got)
	}
}

// TestGate_ForeignCurrencyRow_ExcludedFromWindowSum is the locked
// Rule 4 enforcement at the gate site: a row in a different currency
// in the same window must NOT count toward the limit. Without the
// same-currency filter on SumCostNano this test fails by refusing the
// call.
func TestGate_ForeignCurrencyRow_ExcludedFromWindowSum(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{Usage: llm.TokenUsage{}},
	}
	repo := &fakeRepo{}
	now := time.Now().UTC()
	// EUR row in the current hour with a cost large enough to trip
	// the limit IF it were counted in the USD sum. The gate must
	// exclude it.
	repo.rows = []llmusage.Row{{
		Timestamp:         now.Add(-10 * time.Minute),
		Currency:          "EUR",
		EstimatedCostNano: 999_999,
	}}
	limits := tightCostLimits{hourly: 1_000, daily: -1, monthly: -1}
	auditSpy := &spyAudit{}
	rec, _ := newGateRecorder(t, repo, inner, limits, auditSpy)

	if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat err = %v; EUR row must NOT count toward USD sum (Rule 4)", err)
	}
	if inner.chatCalls != 1 {
		t.Errorf("inner adapter called %d times; want 1", inner.chatCalls)
	}
}

// TestGate_MixedCurrency_DetectedOnceWithWarningAndAudit pins the
// once-only forensic signal: when the table already contains a row in
// a foreign currency, the recorder emits ONE warning + audit row on
// first use, and the per-call enforcement path does NOT issue a
// foreign-currency count on subsequent calls.
func TestGate_MixedCurrency_DetectedOnceWithWarningAndAudit(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{Usage: llm.TokenUsage{}},
	}
	repo := &fakeRepo{}
	// One foreign-currency row already present; the configured
	// currency is USD.
	repo.rows = []llmusage.Row{{
		Timestamp:         time.Now().UTC().Add(-1 * time.Hour),
		Currency:          "EUR",
		EstimatedCostNano: 42,
	}}
	limits := tightCostLimits{hourly: 1_000_000_000, daily: 1_000_000_000, monthly: 1_000_000_000}
	auditSpy := &spyAudit{}
	rec, buf := newGateRecorder(t, repo, inner, limits, auditSpy)

	// First call triggers the once-only detector.
	if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("first Chat err = %v", err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "mixed-currency") {
		t.Errorf("warn log missing 'mixed-currency' phrase; got:\n%s", logged)
	}
	events := auditSpy.snapshot()
	mixedRows := 0
	for _, ev := range events {
		if ev.Reason == "mixed_currency_history_detected" {
			mixedRows++
			if ev.Decision != audit.DecisionAllow {
				t.Errorf("mixed-currency decision = %q; want %q (forensic, not blocking)", ev.Decision, audit.DecisionAllow)
			}
			if ev.Kind != audit.KindLLMLimitTriggered {
				t.Errorf("mixed-currency kind = %q; want %q", ev.Kind, audit.KindLLMLimitTriggered)
			}
			if !strings.Contains(ev.Context, `"USD"`) {
				t.Errorf("mixed-currency context %q missing configured currency USD", ev.Context)
			}
		}
	}
	if mixedRows != 1 {
		t.Errorf("mixed-currency audit rows after first call = %d; want 1", mixedRows)
	}

	// Subsequent calls must NOT issue another foreign-currency count
	// and must NOT emit another mixed-currency warning/audit row.
	beforeForeign := repo.foreignCallCount()
	if beforeForeign != 1 {
		t.Errorf("foreign-currency count called %d times after first Chat; want 1 (once-only detector)", beforeForeign)
	}
	for i := 0; i < 3; i++ {
		if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
			t.Fatalf("subsequent Chat err = %v", err)
		}
	}
	if afterForeign := repo.foreignCallCount(); afterForeign != 1 {
		t.Errorf("foreign-currency count called %d times total; want 1 (per-call path must NOT issue a foreign-currency count)", afterForeign)
	}
	// Mixed-currency audit row count must not grow either.
	mixedAfter := 0
	for _, ev := range auditSpy.snapshot() {
		if ev.Reason == "mixed_currency_history_detected" {
			mixedAfter++
		}
	}
	if mixedAfter != 1 {
		t.Errorf("mixed-currency audit rows = %d after subsequent calls; want 1 (once-only)", mixedAfter)
	}
}
