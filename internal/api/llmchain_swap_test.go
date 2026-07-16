package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
)

// These tests pin the model-swap recording defect fix: a model hot-swapped
// through either swap endpoint must install the SAME recording / cost-gating
// chain the boot path installs (core.Services.BuildLLMChain), NOT the raw
// provider client. Before the fix both handlers installed the raw client, so
// the first admin model swap silently disabled usage recording and the
// cost-window gate until process restart.
//
// They run in package `api` so they can drive the handlers via the
// newModelAdapter factory seam (no real provider credentials) and reuse the
// llmadminFixture, which wires the same dependencies boot does: a real
// SQLite-backed llm_usage repo, the storage-backed CostLimitsProvider, the
// audit repo, a USD currency config, and a live SwappableAdapter.

// recordingTokenAdapter is the raw "provider client" the swap factory seam
// returns: a Chat call reports a fixed, identifiable token usage so the test
// can assert the wrapped recorder wrote a row carrying it. It is the RAW
// inner the handler is expected to wrap via BuildLLMChain.
type recordingTokenAdapter struct {
	in  int
	out int
}

func (a recordingTokenAdapter) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Usage: llm.TokenUsage{InputTokens: a.in, OutputTokens: a.out}}, nil
}

// stubSwapFactory points the newModelAdapter seam at a recordingTokenAdapter
// reporting the given token counts, so every swap installs a wrappable raw
// client without real credentials.
func stubSwapFactory(t *testing.T, in, out int) {
	t.Helper()
	orig := newModelAdapter
	newModelAdapter = func(context.Context, config.ModelConfig) (llm.LLMAdapter, error) {
		return recordingTokenAdapter{in: in, out: out}, nil
	}
	t.Cleanup(func() { newModelAdapter = orig })
}

// disableCostGate sets every cost window to the explicit-disable sentinel so
// the pre-call gate skips entirely — isolating the recording assertions in
// the recording tests from any accumulated-spend bookkeeping.
func disableCostGate(t *testing.T, f *llmadminFixture) {
	t.Helper()
	for _, win := range []string{"hourly", "daily", "monthly"} {
		if err := f.settings.SetCostLimit(context.Background(), win, -1); err != nil {
			t.Fatalf("disable %s cost limit: %v", win, err)
		}
	}
}

// lastUsageRow returns the most recently inserted llm_usage row's identity
// and token counts.
func lastUsageRow(t *testing.T, f *llmadminFixture) (model string, in, out int) {
	t.Helper()
	err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT model, input_tokens, output_tokens FROM llm_usage ORDER BY id DESC LIMIT 1`).
		Scan(&model, &in, &out)
	if err != nil {
		t.Fatalf("read last llm_usage row: %v", err)
	}
	return model, in, out
}

func countUsageRows(t *testing.T, f *llmadminFixture) int {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM llm_usage`).Scan(&n); err != nil {
		t.Fatalf("count llm_usage rows: %v", err)
	}
	return n
}

func swappable(t *testing.T, f *llmadminFixture) *llm.SwappableAdapter {
	t.Helper()
	sw, ok := f.services.LLM.(*llm.SwappableAdapter)
	if !ok {
		t.Fatalf("services.LLM is %T, want *llm.SwappableAdapter", f.services.LLM)
	}
	return sw
}

// TestModelSwap_RecordsUsageWithNewIdentity_ModelsCurrent: swap via
// POST /api/v1/models/current, then a Chat through the live swappable adapter
// must record a usage row stamped with the NEW model's identity. Before the
// fix the swap installed the raw client and no row was written at all.
func TestModelSwap_RecordsUsageWithNewIdentity_ModelsCurrent(t *testing.T) {
	stubSwapFactory(t, 100, 50)
	f := newLLMAdminFixture(t, false) // auth disabled: no admin principal needed
	disableCostGate(t, f)

	w := f.do(http.MethodPost, "/api/v1/models/current", `{"name":"alt"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("swap status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	if _, err := swappable(t, f).Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat after swap returned error: %v (wrapped chain must not break the call)", err)
	}

	// "alt" maps to gemini-2.5-flash in the fixture catalogue.
	model, in, out := lastUsageRow(t, f)
	if model != "gemini-2.5-flash" {
		t.Errorf("recorded model = %q; want gemini-2.5-flash (recorder identity must reflect the NEW model)", model)
	}
	if in != 100 || out != 50 {
		t.Errorf("recorded tokens = (%d,%d); want (100,50) — Chat went through the recorder", in, out)
	}
}

// TestModelSwap_RecordsUsageWithNewIdentity_ActiveModel: same guarantee via
// the admin surface POST /api/v1/llm/settings/active-model.
func TestModelSwap_RecordsUsageWithNewIdentity_ActiveModel(t *testing.T) {
	stubSwapFactory(t, 11, 22)
	f := newLLMAdminFixture(t, false) // auth disabled permits the admin gate
	disableCostGate(t, f)

	w := f.do(http.MethodPost, "/api/v1/llm/settings/active-model", `{"name":"alt"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("active-model swap status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	if _, err := swappable(t, f).Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat after active-model swap returned error: %v", err)
	}

	model, in, out := lastUsageRow(t, f)
	if model != "gemini-2.5-flash" {
		t.Errorf("recorded model = %q; want gemini-2.5-flash", model)
	}
	if in != 11 || out != 22 {
		t.Errorf("recorded tokens = (%d,%d); want (11,22)", in, out)
	}
}

// TestModelSwap_CostGateFiresAfterSwap: with a cost limit already tripped, a
// Chat made AFTER a swap must be refused by the cost-window gate. Before the
// fix the swapped-in raw client had no gate, so cost enforcement silently
// stopped at the first swap.
func TestModelSwap_CostGateFiresAfterSwap(t *testing.T) {
	stubSwapFactory(t, 1, 1)
	f := newLLMAdminFixture(t, false)
	ctx := context.Background()

	// Tighten the hourly window and pre-seed spend above it for the current
	// hour, in the configured currency the gate sums over.
	if err := f.settings.SetCostLimit(ctx, "hourly", 1_000); err != nil {
		t.Fatalf("set hourly limit: %v", err)
	}
	if err := f.usageRepo.Insert(ctx, llmusage.Row{
		Timestamp:         time.Now().UTC(),
		Model:             "seed-model",
		Currency:          "USD",
		EstimatedCostNano: 1_000_000,
	}); err != nil {
		t.Fatalf("seed usage row: %v", err)
	}

	w := f.do(http.MethodPost, "/api/v1/models/current", `{"name":"alt"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("swap status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	_, err := swappable(t, f).Chat(ctx, llm.ChatRequest{})
	if !errors.Is(err, llmusage.ErrCostLimitExceeded) {
		t.Fatalf("Chat after swap err = %v; want ErrCostLimitExceeded (gate must enforce on the swapped chain)", err)
	}
}

// TestModelSwap_RecordingSurvivesTwoConsecutiveSwaps guards against a
// one-shot fix that only wraps the first swap: after two consecutive swaps a
// Chat must still be recorded, stamped with the SECOND swap's model identity.
func TestModelSwap_RecordingSurvivesTwoConsecutiveSwaps(t *testing.T) {
	stubSwapFactory(t, 7, 3)
	f := newLLMAdminFixture(t, false)
	disableCostGate(t, f)

	if w := f.do(http.MethodPost, "/api/v1/models/current", `{"name":"alt"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("first swap status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	if w := f.do(http.MethodPost, "/api/v1/models/current", `{"name":"default"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("second swap status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	if _, err := swappable(t, f).Chat(context.Background(), llm.ChatRequest{}); err != nil {
		t.Fatalf("Chat after two swaps returned error: %v", err)
	}

	if got := countUsageRows(t, f); got != 1 {
		t.Fatalf("llm_usage row count = %d; want 1 — recording must survive the SECOND swap", got)
	}
	// "default" maps to claude-sonnet-4-20250514.
	model, in, out := lastUsageRow(t, f)
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("recorded model = %q; want claude-sonnet-4-20250514 (identity of the second swap)", model)
	}
	if in != 7 || out != 3 {
		t.Errorf("recorded tokens = (%d,%d); want (7,3)", in, out)
	}
}
