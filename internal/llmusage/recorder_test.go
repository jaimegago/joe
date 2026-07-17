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

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/rbac"
)

// fakeInnerAdapter is a hand-rolled llm.LLMAdapter that returns a
// scripted ChatResponse on every Chat call. Used by the recorder unit
// tests to drive the wrapper without spinning up agentloop.
type fakeInnerAdapter struct {
	mu        sync.Mutex
	resp      *llm.ChatResponse
	chatErr   error
	chatCalls int
}

func (f *fakeInnerAdapter) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatCalls++
	if f.chatErr != nil {
		return nil, f.chatErr
	}
	return f.resp, nil
}

// fakeRepo is an in-memory llmusage.Repository for tests. The
// optional insertErr forces Insert to return that error on every
// call, which the fail-open test uses to prove the call still
// succeeds despite the write failure. sumErr does the same for the
// cost-window gate's aggregation query, exercising the gate-read
// fail-open path.
type fakeRepo struct {
	mu           sync.Mutex
	rows         []llmusage.Row
	insertErr    error
	sumErr       error
	sumCalls     int
	foreignCount int64
	foreignErr   error
	foreignCalls int
}

func (r *fakeRepo) Insert(_ context.Context, row llmusage.Row) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.insertErr != nil {
		return r.insertErr
	}
	// Stamp the row with a timestamp when the caller left it zero so
	// the test fake mirrors the SQL repository's behaviour — the gate's
	// aggregation logic in turn sees rows that fall in a window.
	if row.Timestamp.IsZero() {
		row.Timestamp = time.Now().UTC()
	}
	r.rows = append(r.rows, row)
	return nil
}

// SumCostNano sums estimated_cost_nano over rows whose Timestamp falls
// in the half-open range [lower, upper) AND whose Currency matches.
// Mirrors the SQL repository contract without going through SQL so the
// recorder tests can drive the gate against in-memory rows. sumErr
// forces the read to fail, which the fail-open test uses.
func (r *fakeRepo) SumCostNano(_ context.Context, lower, upper time.Time, currency string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sumCalls++
	if r.sumErr != nil {
		return 0, r.sumErr
	}
	var sum int64
	for _, row := range r.rows {
		if row.Currency != currency {
			continue
		}
		t := row.Timestamp
		if !t.Before(lower) && t.Before(upper) {
			sum += row.EstimatedCostNano
		}
	}
	return sum, nil
}

// SessionUsage / AggregateUsage / PerModelUsage / PerPrincipalUsage —
// Stream G phase G5 view-path methods. fakeRepo isn't a primary test
// subject for these (the SQL repository tests in repository_test.go
// cover the GROUP BY semantics directly), so the in-memory
// implementation here is enough to satisfy the interface for the
// recorder/gate tests that already exist. They sum across the same
// in-memory rows the gate test uses; behaviour mirrors the SQL
// repository's columnar shape one row per group.
func (r *fakeRepo) SessionUsage(_ context.Context, sessionID string) ([]llmusage.UsageBreakdown, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byCur := map[string]*llmusage.UsageBreakdown{}
	for _, row := range r.rows {
		if row.SessionID != sessionID {
			continue
		}
		b, ok := byCur[row.Currency]
		if !ok {
			b = &llmusage.UsageBreakdown{Currency: row.Currency, SessionID: sessionID}
			byCur[row.Currency] = b
		}
		b.Calls++
		b.InputTokens += int64(row.InputTokens)
		b.OutputTokens += int64(row.OutputTokens)
		b.EstimatedCostNano += row.EstimatedCostNano
	}
	out := make([]llmusage.UsageBreakdown, 0, len(byCur))
	for _, b := range byCur {
		out = append(out, *b)
	}
	return out, nil
}

func (r *fakeRepo) AggregateUsage(_ context.Context, lower, upper time.Time) ([]llmusage.UsageBreakdown, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byCur := map[string]*llmusage.UsageBreakdown{}
	for _, row := range r.rows {
		t := row.Timestamp
		if t.Before(lower) || !t.Before(upper) {
			continue
		}
		b, ok := byCur[row.Currency]
		if !ok {
			b = &llmusage.UsageBreakdown{Currency: row.Currency}
			byCur[row.Currency] = b
		}
		b.Calls++
		b.InputTokens += int64(row.InputTokens)
		b.OutputTokens += int64(row.OutputTokens)
		b.EstimatedCostNano += row.EstimatedCostNano
	}
	out := make([]llmusage.UsageBreakdown, 0, len(byCur))
	for _, b := range byCur {
		out = append(out, *b)
	}
	return out, nil
}

func (r *fakeRepo) PerModelUsage(_ context.Context, lower, upper time.Time) ([]llmusage.UsageBreakdown, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	type key struct{ model, currency string }
	groups := map[key]*llmusage.UsageBreakdown{}
	for _, row := range r.rows {
		t := row.Timestamp
		if t.Before(lower) || !t.Before(upper) {
			continue
		}
		k := key{row.Model, row.Currency}
		b, ok := groups[k]
		if !ok {
			b = &llmusage.UsageBreakdown{Model: row.Model, Currency: row.Currency}
			groups[k] = b
		}
		b.Calls++
		b.InputTokens += int64(row.InputTokens)
		b.OutputTokens += int64(row.OutputTokens)
		b.EstimatedCostNano += row.EstimatedCostNano
	}
	out := make([]llmusage.UsageBreakdown, 0, len(groups))
	for _, b := range groups {
		out = append(out, *b)
	}
	return out, nil
}

func (r *fakeRepo) PerPrincipalUsage(_ context.Context, lower, upper time.Time) ([]llmusage.UsageBreakdown, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	type key struct{ principal, currency string }
	groups := map[key]*llmusage.UsageBreakdown{}
	for _, row := range r.rows {
		t := row.Timestamp
		if t.Before(lower) || !t.Before(upper) {
			continue
		}
		k := key{row.Principal, row.Currency}
		b, ok := groups[k]
		if !ok {
			b = &llmusage.UsageBreakdown{Principal: row.Principal, Currency: row.Currency}
			groups[k] = b
		}
		b.Calls++
		b.InputTokens += int64(row.InputTokens)
		b.OutputTokens += int64(row.OutputTokens)
		b.EstimatedCostNano += row.EstimatedCostNano
	}
	out := make([]llmusage.UsageBreakdown, 0, len(groups))
	for _, b := range groups {
		out = append(out, *b)
	}
	return out, nil
}

// CountForeignCurrency counts rows whose Currency differs from the
// supplied currency. Mirrors the SQL repository contract.
func (r *fakeRepo) CountForeignCurrency(_ context.Context, currency string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.foreignCalls++
	if r.foreignErr != nil {
		return 0, r.foreignErr
	}
	if r.foreignCount != 0 {
		return r.foreignCount, nil
	}
	var n int64
	for _, row := range r.rows {
		if row.Currency != currency {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) snapshot() []llmusage.Row {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]llmusage.Row, len(r.rows))
	copy(out, r.rows)
	return out
}

func (r *fakeRepo) sumCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sumCalls
}

func (r *fakeRepo) foreignCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.foreignCalls
}

// newTestRecorder constructs a RecorderAdapter wired to in-memory
// fakes. The returned logger captures warnings so tests can assert on
// the fail-open warning text. priceForRecorder is the model the recorder
// stamps on each row; passing one of the built-in priced models picks
// up that built-in price, passing an unknown one exercises the
// zero-cost branch.
func newTestRecorder(provider, model, currency string, fxRate float64, repo *fakeRepo, inner *fakeInnerAdapter) (*llmusage.RecorderAdapter, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rec := llmusage.NewRecorderAdapter(llmusage.Config{
		Inner:    inner,
		Repo:     repo,
		Provider: provider,
		Model:    model,
		Currency: currency,
		FXRate:   fxRate,
		Logger:   logger,
	})
	return rec, buf
}

// TestRecorder_Chat_RecordsOneRow is the happy-path baseline: a single
// Chat call records exactly one row, capturing the principal, model,
// separated token counts, configured currency, and the session/task
// ids threaded through context.
func TestRecorder_Chat_RecordsOneRow(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Content: "hi",
			Usage:   llm.TokenUsage{InputTokens: 12, OutputTokens: 7, TotalTokens: 19},
		},
	}
	repo := &fakeRepo{}
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	ctx := rbac.WithPrincipal(context.Background(), rbac.Principal("user:alice"))
	ctx = agentctx.WithSessionID(ctx, "sess-1")
	ctx = agentctx.WithTaskID(ctx, "task-7")

	resp, err := rec.Chat(ctx, llm.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if resp == nil || resp.Content != "hi" {
		t.Fatalf("Chat returned wrong response: %+v", resp)
	}

	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 usage row, got %d", len(rows))
	}
	got := rows[0]
	if got.Principal != "user:alice" {
		t.Errorf("principal = %q, want user:alice", got.Principal)
	}
	if got.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want claude-sonnet-4-20250514", got.Model)
	}
	if got.InputTokens != 12 || got.OutputTokens != 7 {
		t.Errorf("tokens (in/out) = %d/%d, want 12/7", got.InputTokens, got.OutputTokens)
	}
	if got.Currency != "USD" {
		t.Errorf("currency = %q, want USD", got.Currency)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want sess-1", got.SessionID)
	}
	if got.TaskID != "task-7" {
		t.Errorf("task_id = %q, want task-7", got.TaskID)
	}
	// Cost: 12 input * (3/1e6) + 7 output * (15/1e6) = 36e-6 + 105e-6 = 141e-6 USD
	// nano-units: 141e-6 * 1e9 = 141_000
	if got.EstimatedCostNano != 141_000 {
		t.Errorf("estimated_cost_nano = %d, want 141_000", got.EstimatedCostNano)
	}
}

// TestRecorder_Chat_EmptyPrincipalMapsToNull — the audit convention:
// both an empty principal and the rbac.Unknown sentinel resolve to the
// repository-empty-string-as-NULL convention. We assert the empty
// case here; the next test covers Unknown.
func TestRecorder_Chat_EmptyPrincipalMapsToNull(t *testing.T) {
	inner := &fakeInnerAdapter{resp: &llm.ChatResponse{Usage: llm.TokenUsage{}}}
	repo := &fakeRepo{}
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	// No principal set on context — PrincipalFromContext returns Unknown.
	if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Principal != "" {
		t.Errorf("principal = %q, want empty (mapped to NULL)", rows[0].Principal)
	}
}

// TestRecorder_Chat_UnknownPrincipalSentinelMapsToNull — explicitly
// setting the Unknown sentinel via WithPrincipal must NOT round-trip
// to the row; it must map to empty (NULL) just like the missing case.
func TestRecorder_Chat_UnknownPrincipalSentinelMapsToNull(t *testing.T) {
	inner := &fakeInnerAdapter{resp: &llm.ChatResponse{Usage: llm.TokenUsage{}}}
	repo := &fakeRepo{}
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	ctx := rbac.WithPrincipal(context.Background(), rbac.Unknown)
	if _, err := rec.Chat(ctx, llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Principal != "" {
		t.Errorf("principal = %q, want empty (Unknown maps to NULL)", rows[0].Principal)
	}
}

// TestRecorder_Chat_InnerErrorPropagatedNoRow — when the inner adapter
// returns an error, the recorder must return that error unchanged and
// MUST NOT record a row (we have no real token counts to record).
func TestRecorder_Chat_InnerErrorPropagatedNoRow(t *testing.T) {
	sentinel := errors.New("upstream provider failure")
	inner := &fakeInnerAdapter{chatErr: sentinel}
	repo := &fakeRepo{}
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	resp, err := rec.Chat(context.Background(), llm.ChatRequest{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Chat err = %v, want sentinel %v", err, sentinel)
	}
	if resp != nil {
		t.Errorf("Chat resp = %+v, want nil on error", resp)
	}
	if rows := repo.snapshot(); len(rows) != 0 {
		t.Errorf("expected 0 rows on inner error, got %d", len(rows))
	}
}

// TestRecorder_Chat_RepositoryFailureIsFailOpen is the mandatory
// fail-open guarantee: a forced repository write error must NOT
// propagate out of Chat. The recorder logs the failure at warn level
// and returns the inner adapter's real response with nil error. This
// is the OPPOSITE posture from the audit writer — see the recorder's
// package doc.
func TestRecorder_Chat_RepositoryFailureIsFailOpen(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Content: "real response from upstream",
			Usage:   llm.TokenUsage{InputTokens: 1, OutputTokens: 2},
		},
	}
	repo := &fakeRepo{insertErr: errors.New("database is on fire")}
	rec, buf := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	resp, err := rec.Chat(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat returned error %v; recording-failure path must be fail-open", err)
	}
	if resp == nil || resp.Content != "real response from upstream" {
		t.Errorf("Chat lost or mutated the real response: %+v", resp)
	}
	logged := buf.String()
	if !strings.Contains(logged, "recording failed") {
		t.Errorf("warn log missing 'recording failed' phrase; got:\n%s", logged)
	}
	if !strings.Contains(logged, "fail-open") {
		t.Errorf("warn log should mention fail-open posture; got:\n%s", logged)
	}
}

// TestRecorder_Chat_WithoutCancelPreservesPrincipal covers the
// background-goroutine path: an LLM call dispatched from a goroutine that
// outlives the originating HTTP request, so the request context is already
// cancelled by the time recording runs. The recorder must derive its write
// context with context.WithoutCancel so the principal value still rides the
// context and the row lands.
func TestRecorder_Chat_WithoutCancelPreservesPrincipal(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Usage: llm.TokenUsage{InputTokens: 3, OutputTokens: 4},
		},
	}
	repo := &fakeRepo{}
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "USD", 1.0, repo, inner)

	parent, cancel := context.WithCancel(context.Background())
	parent = rbac.WithPrincipal(parent, rbac.Principal("user:carol"))
	cancel() // already-cancelled before Chat runs.

	resp, err := rec.Chat(parent, llm.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat returned error on cancelled ctx: %v", err)
	}
	if resp == nil {
		t.Fatalf("Chat returned nil response")
	}
	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row even on cancelled ctx, got %d", len(rows))
	}
	if rows[0].Principal != "user:carol" {
		t.Errorf("principal lost: row.Principal=%q, want user:carol", rows[0].Principal)
	}
}

// TestRecorder_Chat_UnknownModelRecordsZeroCostWithWarning — the
// unknown provider/model branch: the row IS recorded with real token
// counts and zero cost, and a warning is emitted. The recorder must
// never silently drop a usage row, even for a model not in the price
// table.
func TestRecorder_Chat_UnknownModelRecordsZeroCostWithWarning(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Usage: llm.TokenUsage{InputTokens: 42, OutputTokens: 13},
		},
	}
	repo := &fakeRepo{}
	rec, buf := newTestRecorder("madeup", "model-9000", "USD", 1.0, repo, inner)

	if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row for unknown model, got %d", len(rows))
	}
	got := rows[0]
	if got.InputTokens != 42 || got.OutputTokens != 13 {
		t.Errorf("tokens (in/out) = %d/%d, want 42/13", got.InputTokens, got.OutputTokens)
	}
	if got.EstimatedCostNano != 0 {
		t.Errorf("estimated_cost_nano = %d, want 0 for unknown model", got.EstimatedCostNano)
	}
	logged := buf.String()
	if !strings.Contains(logged, "unpriced provider/model") {
		t.Errorf("warn log missing 'unpriced' phrase; got:\n%s", logged)
	}
	if !strings.Contains(logged, "madeup") || !strings.Contains(logged, "model-9000") {
		t.Errorf("warn log should name the unpriced model; got:\n%s", logged)
	}
}

// TestRecorder_Chat_ConfiguredCurrencyAppliedAtRecord — when the
// configured currency differs from the source, the recorder applies
// the FX rate and stamps the configured currency on the row. The
// arithmetic mirrors the unit test on EstimateCostNano but driven
// through the wrapper to catch a regression in the wiring.
func TestRecorder_Chat_ConfiguredCurrencyAppliedAtRecord(t *testing.T) {
	inner := &fakeInnerAdapter{
		resp: &llm.ChatResponse{
			Usage: llm.TokenUsage{InputTokens: 10_000, OutputTokens: 4_000},
		},
	}
	repo := &fakeRepo{}
	// claude-sonnet-4-20250514: $3/MTok input, $15/MTok output (USD source).
	// 10_000 * (3/1e6) + 4_000 * (15/1e6) = 0.03 + 0.06 = 0.09 USD.
	// FX 0.9 USD->EUR => 0.081 EUR => 81_000_000 nano-EUR.
	rec, _ := newTestRecorder("claude", "claude-sonnet-4-20250514", "EUR", 0.9, repo, inner)

	if _, err := rec.Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.Currency != "EUR" {
		t.Errorf("row currency = %q, want EUR (stamped at record time)", got.Currency)
	}
	if got.EstimatedCostNano != 81_000_000 {
		t.Errorf("row cost = %d nano-EUR, want 81_000_000", got.EstimatedCostNano)
	}
}
