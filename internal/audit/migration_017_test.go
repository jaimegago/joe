package audit_test

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// TestMigration017_AuditLogRebuild_PreservesAppendOnly is the load-bearing
// guard for the audit_log table rebuild that migration 017 performs to
// widen the kind CHECK constraint. SQLite cannot alter a CHECK in place, so
// 017 creates a new audit_log_new, copies all rows, drops the old table,
// renames, and recreates the three indexes and two append-only triggers. A
// typo anywhere in that sequence could silently strip a trigger or an
// index. This test asserts all four invariants:
//
//  1. Both append-only triggers still ABORT UPDATE and DELETE on the
//     rebuilt table.
//  2. All three pre-existing indexes are present by name after rebuild.
//  3. The pre-existing kinds (infra_access, captain_transition) still
//     insert successfully.
//  4. (Implicitly via Insert success) the row shape — columns, defaults,
//     and the decision CHECK — is preserved.
func TestMigration017_AuditLogRebuild_PreservesAppendOnly(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// Insert one row of each pre-existing kind. These round-trip exercises
	// confirm the rebuilt audit_log still accepts the kinds defined by
	// migration 015.
	if err := repo.Insert(ctx, audit.Event{
		Principal: "user:alice",
		Action:    "read",
		Zone:      "prod-readonly",
		Source:    "k8s-prod",
		Decision:  audit.DecisionAllow,
		Reason:    "policy_allow",
		Kind:      audit.KindInfraAccess,
	}); err != nil {
		t.Fatalf("Insert infra_access: %v", err)
	}
	if err := repo.Insert(ctx, audit.Event{
		Principal: "user:alice",
		Action:    audit.ActionCaptainAttach,
		Decision:  audit.DecisionAllow,
		Reason:    "transition_recorded",
		Kind:      audit.KindCaptainTransition,
	}); err != nil {
		t.Fatalf("Insert captain_transition: %v", err)
	}

	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("audit_log count = %d, want 2", count)
	}

	// (1) Triggers still ABORT UPDATE.
	if _, err := s.DB().ExecContext(ctx, `UPDATE audit_log SET reason = 'tampered'`); err == nil {
		t.Errorf("UPDATE on rebuilt audit_log returned nil; trigger missing after rebuild")
	} else if !strings.Contains(strings.ToLower(err.Error()), "append-only") {
		t.Errorf("UPDATE error = %v; expected an append-only abort message", err)
	}

	// (1) Triggers still ABORT DELETE.
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM audit_log`); err == nil {
		t.Errorf("DELETE on rebuilt audit_log returned nil; trigger missing after rebuild")
	} else if !strings.Contains(strings.ToLower(err.Error()), "append-only") {
		t.Errorf("DELETE error = %v; expected an append-only abort message", err)
	}

	// (2) All three named indexes are present.
	wantIndexes := []string{
		"idx_audit_log_created_at",
		"idx_audit_log_kind",
		"idx_audit_log_principal",
	}
	got := indexesOnTable(t, s.DB(), "audit_log")
	if !sliceContainsAll(got, wantIndexes) {
		t.Errorf("indexes on audit_log = %v, want all of %v", got, wantIndexes)
	}

	// The pre-existing rows are unaffected by the failed UPDATE/DELETE.
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&count); err != nil {
		t.Fatalf("count after failed mutations: %v", err)
	}
	if count != 2 {
		t.Fatalf("audit_log count after failed UPDATE/DELETE = %d, want 2 (append-only must hold)", count)
	}
}

// TestMigration017_AuditLog_NewKindInsert exercises the widened CHECK
// directly: an audit row with kind 'llm_settings_mutation' must persist.
// Pre-017, the same row would fail against the closed CHECK from
// migration 015.
func TestMigration017_AuditLog_NewKindInsert(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	if err := repo.Insert(ctx, audit.Event{
		Principal: "user:operator",
		Action:    audit.ActionLLMSetActiveModel,
		Decision:  audit.DecisionAllow,
		Reason:    "settings_change",
		Kind:      audit.KindLLMSettingsMutation,
		Context:   `{"new_model":"claude-opus-4-7"}`,
	}); err != nil {
		t.Fatalf("Insert llm_settings_mutation: %v (pre-G1 CHECK still in place?)", err)
	}

	var kind sql.NullString
	if err := s.DB().QueryRowContext(ctx,
		`SELECT kind FROM audit_log WHERE action = ?`, audit.ActionLLMSetActiveModel,
	).Scan(&kind); err != nil {
		t.Fatalf("select: %v", err)
	}
	if kind.String != string(audit.KindLLMSettingsMutation) {
		t.Errorf("kind = %q, want %q", kind.String, audit.KindLLMSettingsMutation)
	}
}

// TestMigration017_LLMUsage_IndexesByName asserts that the three llm_usage
// indexes created by migration 017 exist by exact name after migrating up.
// Same style as the audit_log rebuild test above: pull all indexes attached
// to the table and assert containment of the three expected names. Catches
// a typo or accidental drop in the up migration that a count-only check
// would miss.
func TestMigration017_LLMUsage_IndexesByName(t *testing.T) {
	s := freshStore(t)

	wantIndexes := []string{
		"idx_llm_usage_created_at",
		"idx_llm_usage_model",
		"idx_llm_usage_principal",
	}
	got := indexesOnTable(t, s.DB(), "llm_usage")
	if !sliceContainsAll(got, wantIndexes) {
		t.Errorf("indexes on llm_usage = %v, want all of %v", got, wantIndexes)
	}
}

// TestMigration017_AuditLog_AllKindConstantsInsertable round-trips every
// Kind constant declared in the audit package through an Insert and
// asserts each persists. This guards the constant-to-CHECK contract: no
// Kind constant can name a value the CHECK rejects, and no CHECK value
// can lack a matching Kind constant (the test fails open on the latter —
// it only enumerates the declared constants).
func TestMigration017_AuditLog_AllKindConstantsInsertable(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	kinds := []audit.Kind{
		audit.KindInfraAccess,
		audit.KindRegimeTransition,
		audit.KindCaptainTransition,
		audit.KindLLMSettingsMutation,
		audit.KindLLMLimitTriggered,
		audit.KindAuthLogin,
	}
	for _, k := range kinds {
		if err := repo.Insert(ctx, audit.Event{
			Principal: "user:alice",
			Action:    "test_action",
			Decision:  audit.DecisionAllow,
			Reason:    "constant_check",
			Kind:      k,
		}); err != nil {
			t.Errorf("Insert kind=%q: %v (constant declared but CHECK rejects it — drift between audit.Kind and migration 017's CHECK enum)", k, err)
		}
	}

	// Confirm one row exists per kind.
	for _, k := range kinds {
		var n int
		if err := s.DB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM audit_log WHERE kind = ?`, string(k),
		).Scan(&n); err != nil {
			t.Fatalf("count for kind %q: %v", k, err)
		}
		if n != 1 {
			t.Errorf("kind %q row count = %d, want 1", k, n)
		}
	}
}

// indexesOnTable returns the names of all indexes attached to the given
// SQLite table, lowercased and sorted, omitting any auto-generated
// "sqlite_autoindex_*" ones.
func indexesOnTable(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name=? AND name NOT LIKE 'sqlite_autoindex_%'`,
		table,
	)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	sort.Strings(out)
	return out
}

func sliceContainsAll(haystack, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}
