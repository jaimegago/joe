package audit_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// TestMigration018_AuditLogRebuild_PreservesAppendOnly is the load-bearing
// guard for the audit_log table rebuild that migration 018 performs to widen
// the kind CHECK to admit 'auth_login'. Same structure as the 017 rebuild
// guard: a typo in the rebuild sequence could silently strip a trigger or an
// index. This asserts:
//
//  1. Both append-only triggers still ABORT UPDATE and DELETE after rebuild.
//  2. All three pre-existing indexes are present by name after rebuild.
//  3. The new 'auth_login' kind inserts successfully (the widening worked).
func TestMigration018_AuditLogRebuild_PreservesAppendOnly(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// (3) The new kind inserts successfully against the widened CHECK.
	if err := repo.Insert(ctx, audit.Event{
		Principal: "user:alice@example.com",
		Action:    audit.ActionOIDCLogin,
		Source:    "oidc",
		Decision:  audit.DecisionAllow,
		Reason:    "oidc_login",
		Kind:      audit.KindAuthLogin,
		Context:   `{"email":"alice@example.com"}`,
	}); err != nil {
		t.Fatalf("Insert auth_login: %v (migration 018 CHECK widening missing?)", err)
	}

	// (1) Triggers still ABORT UPDATE.
	if _, err := s.DB().ExecContext(ctx, `UPDATE audit_log SET reason = 'tampered'`); err == nil {
		t.Errorf("UPDATE on rebuilt audit_log returned nil; trigger missing after 018 rebuild")
	} else if !strings.Contains(strings.ToLower(err.Error()), "append-only") {
		t.Errorf("UPDATE error = %v; expected an append-only abort message", err)
	}

	// (1) Triggers still ABORT DELETE.
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM audit_log`); err == nil {
		t.Errorf("DELETE on rebuilt audit_log returned nil; trigger missing after 018 rebuild")
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

	// The auth_login row survived the failed UPDATE/DELETE (append-only holds).
	var n int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE kind = ?`, string(audit.KindAuthLogin),
	).Scan(&n); err != nil {
		t.Fatalf("count auth_login: %v", err)
	}
	if n != 1 {
		t.Fatalf("auth_login row count = %d, want 1 (append-only must hold)", n)
	}
}

// TestMigration018_AuditLog_AuthLoginKindRoundTrip exercises the widened CHECK
// directly: an audit row with kind 'auth_login' must persist and read back.
// Pre-018, the same row would fail against the migration-017 CHECK.
func TestMigration018_AuditLog_AuthLoginKindRoundTrip(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	if err := repo.Insert(ctx, audit.Event{
		Principal: "svc:ci",
		Action:    audit.ActionBreakGlassUse,
		Source:    "break-glass",
		Decision:  audit.DecisionAllow,
		Reason:    "break_glass_credential_used",
		Kind:      audit.KindAuthLogin,
		Context:   `{"remote":"10.0.0.1:1234"}`,
	}); err != nil {
		t.Fatalf("Insert break-glass auth_login: %v", err)
	}

	var kind, action sql.NullString
	if err := s.DB().QueryRowContext(ctx,
		`SELECT kind, action FROM audit_log WHERE principal = ?`, "svc:ci",
	).Scan(&kind, &action); err != nil {
		t.Fatalf("select: %v", err)
	}
	if kind.String != string(audit.KindAuthLogin) {
		t.Errorf("kind = %q, want %q", kind.String, audit.KindAuthLogin)
	}
	if action.String != audit.ActionBreakGlassUse {
		t.Errorf("action = %q, want %q", action.String, audit.ActionBreakGlassUse)
	}
}
