package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration026_027_RetentionAndAuditKind asserts the B007a migrations:
//
//   - 026 creates the single-row session_retention_policy table seeded with the
//     §12.5 defaults (inactivity OFF / NULL, trash-grace 30, terminal action
//     trash_then_purge);
//   - 027 widens the audit_log.kind CHECK to admit 'session_lifecycle';
//   - the legacy migration-001 sessions / session_messages tables are untouched;
//   - a down/up round-trip of both reverts cleanly and re-applies.
func TestMigration026_027_RetentionAndAuditKind(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up: %v", err)
	}

	// 026: the policy table exists and is seeded with the §12.5 defaults.
	if !tableExists(t, s, "session_retention_policy") {
		t.Fatal("session_retention_policy must exist after 026")
	}
	var (
		inactivity any
		trashGrace int
		terminal   string
	)
	if err := s.db.QueryRow(`
		SELECT inactivity_days, trash_grace_days, terminal_action
		FROM session_retention_policy WHERE id = 1`).Scan(&inactivity, &trashGrace, &terminal); err != nil {
		t.Fatalf("read seeded policy: %v", err)
	}
	if inactivity != nil {
		t.Errorf("seeded inactivity_days = %v, want NULL (OFF)", inactivity)
	}
	if trashGrace != 30 {
		t.Errorf("seeded trash_grace_days = %d, want 30", trashGrace)
	}
	if terminal != "trash_then_purge" {
		t.Errorf("seeded terminal_action = %q, want trash_then_purge", terminal)
	}
	// The single-row CHECK forbids a second row.
	if _, err := s.db.Exec(`INSERT INTO session_retention_policy (id) VALUES (2)`); err == nil {
		t.Error("session_retention_policy must reject id != 1 (single-row CHECK)")
	}

	// 027: the audit_log.kind CHECK admits 'session_lifecycle'.
	if !auditKindAdmitted(t, s, "session_lifecycle") {
		t.Error("audit_log.kind CHECK must admit 'session_lifecycle' after 027")
	}
	if auditKindAdmitted(t, s, "not_a_real_kind") {
		t.Error("audit_log.kind CHECK must still reject an unknown kind")
	}

	// Legacy tables MUST remain untouched (learn-from-sessions data source).
	if !tableExists(t, s, "sessions") {
		t.Error("legacy `sessions` table must remain present")
	}
	if !tableExists(t, s, "session_messages") {
		t.Error("legacy `session_messages` table must remain present")
	}

	// Down/up round-trip: revert 027 then 026, assert both gone, re-up, assert back.
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
	if err := m.Steps(-4); err != nil {
		t.Fatalf("Steps(-4) revert 027+026: %v", err)
	}
	if tableExists(t, s, "session_retention_policy") {
		t.Error("after reverting 026: session_retention_policy must be gone")
	}
	if auditKindAdmitted(t, s, "session_lifecycle") {
		t.Error("after reverting 027: audit_log.kind must reject 'session_lifecycle' again")
	}
	// Legacy tables still present at the lower boundary.
	if !tableExists(t, s, "sessions") || !tableExists(t, s, "session_messages") {
		t.Error("legacy tables must remain present across the 026/027 down-revert")
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("re-up: %v", err)
	}
	if !tableExists(t, s, "session_retention_policy") {
		t.Error("after re-up: session_retention_policy must exist again")
	}
	if !auditKindAdmitted(t, s, "session_lifecycle") {
		t.Error("after re-up: audit_log.kind must admit 'session_lifecycle' again")
	}
}

// auditKindAdmitted probes whether the audit_log.kind CHECK admits a kind,
// without persisting the row (insert in a transaction, then roll back).
func auditKindAdmitted(t *testing.T, s *Store, kind string) bool {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`
		INSERT INTO audit_log (created_at, action, decision, kind)
		VALUES ('2026-06-21T00:00:00Z', 'probe', 'allow', ?)`, kind)
	return err == nil
}
