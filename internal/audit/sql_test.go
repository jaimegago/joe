package audit_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// freshStore opens an in-memory SQLite with the full migration chain
// applied. Used by both the SQL Insert test and the migration trigger test.
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

// TestSQLRepository_Insert_RoundTrip writes one row and reads it back via
// raw SELECT (audit.Repository deliberately exposes no list method).
func TestSQLRepository_Insert_RoundTrip(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal: "user:alice",
		Action:    "read",
		Zone:      "prod-readonly",
		Source:    "k8s-prod",
		Decision:  audit.DecisionAllow,
		Reason:    "policy_allow",
		Kind:      audit.KindInfraAccess,
		Context:   `{"note":"k8s list"}`,
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var (
		principal, action, zone, source, decision, reason, kind, ctxJSON sql.NullString
	)
	err := s.DB().QueryRowContext(context.Background(),
		`SELECT principal, action, zone, source, decision, reason, kind, context FROM audit_log LIMIT 1`).
		Scan(&principal, &action, &zone, &source, &decision, &reason, &kind, &ctxJSON)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if principal.String != "user:alice" {
		t.Errorf("principal = %q, want %q", principal.String, "user:alice")
	}
	if action.String != "read" || zone.String != "prod-readonly" || source.String != "k8s-prod" {
		t.Errorf("action/zone/source = %q/%q/%q", action.String, zone.String, source.String)
	}
	if decision.String != "allow" || reason.String != "policy_allow" || kind.String != "infra_access" {
		t.Errorf("decision/reason/kind = %q/%q/%q", decision.String, reason.String, kind.String)
	}
	if !strings.Contains(ctxJSON.String, "k8s list") {
		t.Errorf("context = %q, want substring %q", ctxJSON.String, "k8s list")
	}
}

// TestSQLRepository_Insert_NullableColumns verifies that empty
// principal/zone/source values are stored as SQL NULL (sourceless rows
// such as captain transitions take this path).
func TestSQLRepository_Insert_NullableColumns(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal: "user:alice",
		Action:    audit.ActionCaptainAttach,
		Decision:  audit.DecisionAllow,
		Reason:    "transition_recorded",
		Kind:      audit.KindCaptainTransition,
		// Zone and Source intentionally empty.
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var zone, source sql.NullString
	err := s.DB().QueryRowContext(context.Background(),
		`SELECT zone, source FROM audit_log WHERE action = ? LIMIT 1`, audit.ActionCaptainAttach).
		Scan(&zone, &source)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if zone.Valid {
		t.Errorf("zone = %q, want NULL for transition row", zone.String)
	}
	if source.Valid {
		t.Errorf("source = %q, want NULL for transition row", source.String)
	}
}

// TestMigration015_TriggerBlocksUpdate is the database-level half of the
// append-only enforcement (Phase F req 2b, design §2.6). The trigger
// must RAISE/ABORT on any UPDATE against audit_log.
func TestMigration015_TriggerBlocksUpdate(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal: "user:alice",
		Action:    "read",
		Decision:  audit.DecisionAllow,
		Reason:    "policy_allow",
		Kind:      audit.KindInfraAccess,
		Source:    "k8s-prod",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	_, err := s.DB().ExecContext(context.Background(),
		`UPDATE audit_log SET reason = 'tampered' WHERE id = 1`)
	if err == nil {
		t.Fatal("UPDATE on audit_log returned nil; the trigger must abort UPDATEs")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "append-only") &&
		!strings.Contains(strings.ToLower(err.Error()), "update is not permitted") {
		t.Errorf("UPDATE error = %v; expected an append-only abort message", err)
	}
}

// TestMigration015_TriggerBlocksDelete — DELETE counterpart of the
// trigger test.
func TestMigration015_TriggerBlocksDelete(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), audit.Event{
		Principal: "user:alice",
		Action:    "read",
		Decision:  audit.DecisionAllow,
		Reason:    "policy_allow",
		Kind:      audit.KindInfraAccess,
		Source:    "k8s-prod",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	_, err := s.DB().ExecContext(context.Background(),
		`DELETE FROM audit_log WHERE id = 1`)
	if err == nil {
		t.Fatal("DELETE on audit_log returned nil; the trigger must abort DELETEs")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "append-only") &&
		!strings.Contains(strings.ToLower(err.Error()), "delete is not permitted") {
		t.Errorf("DELETE error = %v; expected an append-only abort message", err)
	}

	// And confirm the row is still there.
	var count int
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit_log count after blocked DELETE = %d, want 1", count)
	}
}
