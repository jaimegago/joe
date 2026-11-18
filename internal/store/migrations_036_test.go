package store

import (
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration036_AuditSession_UpDownUp_RoundTrip isolates migration 036 (the
// audit_log session column). Asserts:
//
//   - At HEAD: audit_log.session_id and its index exist.
//   - After reverting 036: both are gone, and the table is otherwise intact.
//   - After re-up to HEAD: both are back.
//   - At every one of the three points the table refuses UPDATE and DELETE:
//     neither direction of this migration rebuilds the table, so the two
//     append-only triggers are never dropped, not even for the span of the
//     migration.
func TestMigration036_AuditSession_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}

	// One row, written before any stepping, to be refused UPDATE and DELETE at
	// each point and to be found intact at the end.
	if _, err := s.db.Exec(`INSERT INTO audit_log (created_at, action, decision, kind, session_id)
		VALUES ('2026-11-18T00:00:00Z', 'read', 'allow', 'infra_access', 's-1')`); err != nil {
		t.Fatalf("insert at HEAD: %v", err)
	}
	refusesRewrite := func(at string) {
		t.Helper()
		for name, stmt := range map[string]string{
			"UPDATE": `UPDATE audit_log SET reason = 'tampered'`,
			"DELETE": `DELETE FROM audit_log`,
		} {
			_, err := s.db.Exec(stmt)
			if err == nil {
				t.Errorf("%s: %s on audit_log was accepted", at, name)
			} else if !strings.Contains(strings.ToLower(err.Error()), "append-only") {
				t.Errorf("%s: %s error = %v; expected the append-only abort", at, name, err)
			}
		}
	}

	if !columnExists(t, s, "audit_log", "session_id") {
		t.Error("at HEAD: audit_log.session_id must exist")
	}
	if !indexExists(t, s, "idx_audit_log_session") {
		t.Error("at HEAD: idx_audit_log_session must exist")
	}
	refusesRewrite("at HEAD")

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs.New: %v", err)
	}
	driver, err := migrateSQLite.WithInstance(s.db, &migrateSQLite.Config{})
	if err != nil {
		t.Fatalf("WithInstance: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, DriverSQLite, driver)
	if err != nil {
		t.Fatalf("NewWithInstance: %v", err)
	}

	if err := m.Steps(stepsDownTo(t, 35)); err != nil {
		t.Fatalf("step down to 035 boundary: %v", err)
	}
	if columnExists(t, s, "audit_log", "session_id") {
		t.Error("after down: audit_log.session_id must be dropped")
	}
	if indexExists(t, s, "idx_audit_log_session") {
		t.Error("after down: idx_audit_log_session must be dropped")
	}
	if !columnExists(t, s, "audit_log", "context") || !indexExists(t, s, "idx_audit_log_kind") {
		t.Error("after down: the rest of audit_log must be untouched")
	}
	refusesRewrite("after down")

	if err := m.Up(); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if !columnExists(t, s, "audit_log", "session_id") || !indexExists(t, s, "idx_audit_log_session") {
		t.Error("after re-up: audit_log.session_id and its index must exist")
	}
	refusesRewrite("after re-up")

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'read' AND reason = ''`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("the row written before stepping: found %d unchanged, want 1", n)
	}
}
