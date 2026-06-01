package llmsettings_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/store"
)

// freshStore opens an in-memory SQLite with the full migration chain
// applied. The settings tables are seeded by migration 017 so every
// mutation under test is an UPDATE.
func freshStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustReadActiveModel(t *testing.T, s *store.Store) string {
	t.Helper()
	var v string
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT active_model FROM llm_settings WHERE id = 1`).Scan(&v); err != nil {
		t.Fatalf("read active_model: %v", err)
	}
	return v
}

func mustReadCostLimit(t *testing.T, s *store.Store, window string) int64 {
	t.Helper()
	var v int64
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT threshold FROM llm_cost_limits WHERE window_name = ?`, window).Scan(&v); err != nil {
		t.Fatalf("read cost limit %q: %v", window, err)
	}
	return v
}

func mustReadRunawayCeiling(t *testing.T, s *store.Store) int {
	t.Helper()
	var v int
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT session_token_ceiling FROM llm_runaway_limits WHERE id = 1`).Scan(&v); err != nil {
		t.Fatalf("read runaway ceiling: %v", err)
	}
	return v
}

func mustReadAuditRows(t *testing.T, s *store.Store) []auditRow {
	t.Helper()
	rows, err := s.DB().QueryContext(context.Background(),
		`SELECT action, decision, kind, context FROM audit_log ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("select audit_log: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.Action, &r.Decision, &r.Kind, &r.Context); err != nil {
			t.Fatalf("scan audit_log: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return out
}

type auditRow struct {
	Action, Decision, Kind, Context string
}

// TestService_SetActiveModel_AtomicWithAudit covers the happy path:
// SetActiveModel updates llm_settings AND writes one
// llm_settings_mutation audit row whose context carries target,
// before, and after. The audit context-key vocabulary is the contract
// later admin readers depend on — assert exact key names.
func TestService_SetActiveModel_AtomicWithAudit(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, auditRepo)

	if got := mustReadActiveModel(t, s); got != "" {
		t.Fatalf("seed active_model = %q, want \"\"", got)
	}

	if err := svc.SetActiveModel(context.Background(), "claude-opus-4-7"); err != nil {
		t.Fatalf("SetActiveModel: %v", err)
	}
	if got := mustReadActiveModel(t, s); got != "claude-opus-4-7" {
		t.Fatalf("active_model after set = %q, want claude-opus-4-7", got)
	}

	rows := mustReadAuditRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("audit row count = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Action != audit.ActionLLMSetActiveModel {
		t.Errorf("audit action = %q, want %q", r.Action, audit.ActionLLMSetActiveModel)
	}
	if r.Kind != string(audit.KindLLMSettingsMutation) {
		t.Errorf("audit kind = %q, want %q", r.Kind, audit.KindLLMSettingsMutation)
	}
	if r.Decision != string(audit.DecisionAllow) {
		t.Errorf("audit decision = %q, want %q", r.Decision, audit.DecisionAllow)
	}

	var blob map[string]any
	if err := json.Unmarshal([]byte(r.Context), &blob); err != nil {
		t.Fatalf("decode audit context: %v", err)
	}
	if got := blob[llmsettings.AuditCtxTarget]; got != llmsettings.AuditCtxTargetActiveModel {
		t.Errorf("audit target = %v, want %q", got, llmsettings.AuditCtxTargetActiveModel)
	}
	if got := blob[llmsettings.AuditCtxBefore]; got != "" {
		t.Errorf("audit before = %v, want \"\"", got)
	}
	if got := blob[llmsettings.AuditCtxAfter]; got != "claude-opus-4-7" {
		t.Errorf("audit after = %v, want claude-opus-4-7", got)
	}
}

// TestService_SetCostLimit_OnlyTargetedWindowChanges proves the write
// targets ONE window: writing the hourly threshold leaves daily and
// monthly at their seeded zeros, and the audit row's target encodes
// the window name in the canonical "cost_limit:<window>" form.
func TestService_SetCostLimit_OnlyTargetedWindowChanges(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, auditRepo)

	const newHourly = int64(50_000_000_000)
	if err := svc.SetCostLimit(context.Background(), llmsettings.WindowHourly, newHourly); err != nil {
		t.Fatalf("SetCostLimit: %v", err)
	}

	if got := mustReadCostLimit(t, s, llmsettings.WindowHourly); got != newHourly {
		t.Errorf("hourly threshold after set = %d, want %d", got, newHourly)
	}
	if got := mustReadCostLimit(t, s, llmsettings.WindowDaily); got != 0 {
		t.Errorf("daily threshold = %d, want 0 (untouched)", got)
	}
	if got := mustReadCostLimit(t, s, llmsettings.WindowMonthly); got != 0 {
		t.Errorf("monthly threshold = %d, want 0 (untouched)", got)
	}

	rows := mustReadAuditRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("audit row count = %d, want 1", len(rows))
	}
	if rows[0].Action != audit.ActionLLMSetCostLimit {
		t.Errorf("audit action = %q, want %q", rows[0].Action, audit.ActionLLMSetCostLimit)
	}

	var blob map[string]any
	if err := json.Unmarshal([]byte(rows[0].Context), &blob); err != nil {
		t.Fatalf("decode audit context: %v", err)
	}
	if got := blob[llmsettings.AuditCtxTarget]; got != llmsettings.AuditCtxTargetCostLimit(llmsettings.WindowHourly) {
		t.Errorf("audit target = %v, want %q", got, llmsettings.AuditCtxTargetCostLimit(llmsettings.WindowHourly))
	}
}

// TestService_SetRunawayCeiling_AtomicWithAudit covers the runaway
// ceiling setter end-to-end.
func TestService_SetRunawayCeiling_AtomicWithAudit(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, auditRepo)

	if err := svc.SetRunawayCeiling(context.Background(), 5_000_000); err != nil {
		t.Fatalf("SetRunawayCeiling: %v", err)
	}
	if got := mustReadRunawayCeiling(t, s); got != 5_000_000 {
		t.Errorf("runaway ceiling = %d, want 5_000_000", got)
	}
	rows := mustReadAuditRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("audit row count = %d, want 1", len(rows))
	}
	if rows[0].Action != audit.ActionLLMSetRunawayCeiling {
		t.Errorf("audit action = %q, want %q", rows[0].Action, audit.ActionLLMSetRunawayCeiling)
	}
	var blob map[string]any
	if err := json.Unmarshal([]byte(rows[0].Context), &blob); err != nil {
		t.Fatalf("decode audit context: %v", err)
	}
	if got := blob[llmsettings.AuditCtxTarget]; got != llmsettings.AuditCtxTargetRunawayCeiling {
		t.Errorf("audit target = %v, want %q", got, llmsettings.AuditCtxTargetRunawayCeiling)
	}
}

// failingAudit forces audit.Repository.InsertTx to return an error.
// This isolates the rollback-on-audit-failure path: the settings
// UPDATE has already executed against the transaction by the time
// InsertTx fires, so the transaction must be rolled back to leave
// llm_settings unchanged.
type failingAudit struct{}

func (failingAudit) Insert(_ context.Context, _ audit.Event) error { return nil }
func (failingAudit) InsertTx(_ context.Context, _ *sql.Tx, _ audit.Event) error {
	return errors.New("forced audit failure")
}

// TestService_SetActiveModel_RollsBackOnAuditFailure is the
// load-bearing atomicity test. When the audit insert fails, the
// settings UPDATE inside the same transaction must be rolled back.
// llm_settings.active_model stays at its prior value AND audit_log
// stays empty — neither half of the mutation leaked.
func TestService_SetActiveModel_RollsBackOnAuditFailure(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, failingAudit{})

	err := svc.SetActiveModel(context.Background(), "claude-opus-4-7")
	if err == nil {
		t.Fatalf("SetActiveModel returned nil; expected the forced audit failure to surface")
	}
	if !errors.Is(err, llmsettings.ErrSettingsWriteFailed) {
		t.Errorf("returned err is not wrapped in ErrSettingsWriteFailed: %v", err)
	}

	if got := mustReadActiveModel(t, s); got != "" {
		t.Fatalf("active_model after failed mutation = %q, want \"\" (rollback should have left it untouched)", got)
	}
	rows := mustReadAuditRows(t, s)
	if len(rows) != 0 {
		t.Fatalf("audit row count after failed mutation = %d, want 0; the audit row must roll back together with the settings row", len(rows))
	}
}

// TestService_AuditTimestampMatchesClockSeam pins the deterministic
// behaviour of the WithClock seam. last_modified is written with the
// supplied "now"; tests that need a stable value can depend on it.
func TestService_AuditTimestampMatchesClockSeam(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	fixed := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	svc := llmsettings.NewMutationService(repo, auditRepo).WithClock(func() time.Time { return fixed })

	if err := svc.SetActiveModel(context.Background(), "x"); err != nil {
		t.Fatalf("SetActiveModel: %v", err)
	}

	var lastMod string
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT last_modified FROM llm_settings WHERE id = 1`).Scan(&lastMod); err != nil {
		t.Fatalf("read last_modified: %v", err)
	}
	if lastMod != fixed.UTC().Format(time.RFC3339Nano) {
		t.Errorf("last_modified = %q, want %q", lastMod, fixed.UTC().Format(time.RFC3339Nano))
	}
}
