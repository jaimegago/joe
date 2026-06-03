package llmsettings_test

import (
	"context"
	"encoding/json"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llmsettings"
)

func mustReadContextBudget(t *testing.T, repo llmsettings.Repository) float64 {
	t.Helper()
	v, err := repo.ReadContextBudget(context.Background())
	if err != nil {
		t.Fatalf("ReadContextBudget: %v", err)
	}
	return v
}

// TestService_SetContextBudget_AtomicWithAudit asserts the write persists
// AND writes one llm_settings_mutation audit row carrying the canonical
// target/before/after keys — the same atomic-with-audit contract the
// cost-limit and runaway-ceiling mutations honour.
func TestService_SetContextBudget_AtomicWithAudit(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), s.Driver())
	svc := llmsettings.NewMutationService(repo, audit.NewRepository(s.DB(), s.Driver()))

	if err := svc.SetContextBudget(context.Background(), 0.55); err != nil {
		t.Fatalf("SetContextBudget: %v", err)
	}
	if got := mustReadContextBudget(t, repo); got != 0.55 {
		t.Errorf("stored fraction = %v, want 0.55", got)
	}

	rows := mustReadAuditRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Action != audit.ActionLLMSetContextBudget {
		t.Errorf("audit action = %q, want %q", r.Action, audit.ActionLLMSetContextBudget)
	}
	if r.Kind != string(audit.KindLLMSettingsMutation) {
		t.Errorf("audit kind = %q, want %q", r.Kind, audit.KindLLMSettingsMutation)
	}
	var ctxBlob map[string]any
	if err := json.Unmarshal([]byte(r.Context), &ctxBlob); err != nil {
		t.Fatalf("unmarshal audit context: %v", err)
	}
	if ctxBlob[llmsettings.AuditCtxTarget] != llmsettings.AuditCtxTargetContextBudget {
		t.Errorf("audit target = %v, want %q", ctxBlob[llmsettings.AuditCtxTarget], llmsettings.AuditCtxTargetContextBudget)
	}
	// before was the migration-seeded zero; after is the written value.
	if before, _ := ctxBlob[llmsettings.AuditCtxBefore].(float64); before != 0 {
		t.Errorf("audit before = %v, want 0", ctxBlob[llmsettings.AuditCtxBefore])
	}
	if after, _ := ctxBlob[llmsettings.AuditCtxAfter].(float64); after != 0.55 {
		t.Errorf("audit after = %v, want 0.55", ctxBlob[llmsettings.AuditCtxAfter])
	}
}

// TestContextBudgetProvider_BackstopAndStored covers the provider's
// backstop convention: unset (zero) and out-of-range values fall back to the
// hardcoded default fraction; a valid stored value passes through.
func TestContextBudgetProvider_BackstopAndStored(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), s.Driver())
	svc := llmsettings.NewMutationService(repo, audit.NewRepository(s.DB(), s.Driver()))
	prov := llmsettings.NewContextBudgetProvider(repo, agentloop.NewStaticContextBudget(), nil)

	// Migration-seeded zero -> backstop default.
	if got := prov.BudgetFraction(); got != agentloop.DefaultContextBudgetFraction {
		t.Errorf("unset fraction = %v, want backstop %v", got, agentloop.DefaultContextBudgetFraction)
	}

	// A valid stored value passes through.
	if err := svc.SetContextBudget(context.Background(), 0.42); err != nil {
		t.Fatalf("SetContextBudget: %v", err)
	}
	if got := prov.BudgetFraction(); got != 0.42 {
		t.Errorf("stored fraction = %v, want 0.42", got)
	}

	// An out-of-range stored value (defence-in-depth against a corrupt row)
	// falls back to the backstop rather than being used verbatim. The
	// endpoint rejects >1, so simulate a corrupt row by writing 2.0 directly.
	if _, err := s.DB().ExecContext(context.Background(),
		`UPDATE llm_context_budget SET budget_fraction = 2.0 WHERE id = 1`); err != nil {
		t.Fatalf("write corrupt fraction: %v", err)
	}
	if got := prov.BudgetFraction(); got != agentloop.DefaultContextBudgetFraction {
		t.Errorf("out-of-range stored fraction used verbatim = %v, want backstop %v", got, agentloop.DefaultContextBudgetFraction)
	}
}

// TestContextBudgetProvider_LiveAdjustable asserts a value written between
// two reads is reflected on the second read without reconstructing the
// provider (the provider re-reads the store per call).
func TestContextBudgetProvider_LiveAdjustable(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), s.Driver())
	svc := llmsettings.NewMutationService(repo, audit.NewRepository(s.DB(), s.Driver()))
	prov := llmsettings.NewContextBudgetProvider(repo, agentloop.NewStaticContextBudget(), nil)

	if err := svc.SetContextBudget(context.Background(), 0.6); err != nil {
		t.Fatalf("set 0.6: %v", err)
	}
	first := prov.BudgetFraction()
	if err := svc.SetContextBudget(context.Background(), 0.3); err != nil {
		t.Fatalf("set 0.3: %v", err)
	}
	second := prov.BudgetFraction()
	if first != 0.6 || second != 0.3 {
		t.Errorf("live adjust: first=%v second=%v, want 0.6 then 0.3", first, second)
	}
}
