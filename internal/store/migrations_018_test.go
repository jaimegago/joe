package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration018_UpDownUp_RoundTrip runs the full chain up (including 018),
// steps one migration down (reverting 018), and re-applies. Migration 018
// widens the audit_log.kind CHECK to admit 'auth_login'; its down narrows the
// CHECK back to the 017 enum. This asserts:
//
//   - After up: an 'auth_login' row is admitted by the CHECK.
//   - After Steps(-1): the CHECK rejects 'auth_login' again, while the
//     017-era kind 'llm_settings_mutation' is still admitted (the down only
//     removes auth_login, nothing else).
//   - After re-up: 'auth_login' is admitted again, and both append-only
//     triggers are present on the rebuilt table.
//
// Insertability probes run inside a rolled-back transaction so they never
// persist a row — an uncommitted auth_login row would otherwise block the
// down migration's row-copy (the documented narrowing caveat).
func TestMigration018_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 1) Up to latest (includes 018).
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	if !kindAdmitted(t, s, "auth_login") {
		t.Fatal("after up: audit_log CHECK must admit 'auth_login'")
	}

	// Build a fresh migrator against the same DB so we can step down.
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

	// 2) Step four migrations down: reverts 021 (principals) then 020
	// (admin-audit kind widening) then 019 (the LLM context-budget table) then
	// 018 — 019, 020, and 021 now sit above 018. Stepping -4 lands the schema
	// just below 018, isolating 018's down migration as this test intends.
	if err := m.Steps(-4); err != nil {
		t.Fatalf("Steps(-4): %v", err)
	}
	if kindAdmitted(t, s, "auth_login") {
		t.Error("after down: audit_log CHECK must reject 'auth_login' again")
	}
	if !kindAdmitted(t, s, "llm_settings_mutation") {
		t.Error("after down: the 017-era kind must still be admitted (down removes only auth_login)")
	}

	// 3) Up again: re-applies 018.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("Up #2: %v", err)
	}
	if !kindAdmitted(t, s, "auth_login") {
		t.Fatal("after re-up: audit_log CHECK must admit 'auth_login' again")
	}

	// The append-only triggers must be present on the rebuilt table.
	for _, trig := range []string{"audit_log_no_update", "audit_log_no_delete"} {
		if !triggerExists(t, s, trig) {
			t.Errorf("trigger %s missing after 018 re-up", trig)
		}
	}

	// Persist one row so the per-row BEFORE UPDATE/DELETE triggers have
	// something to fire on (they do not fire on an empty table), then assert
	// both still ABORT.
	if _, err := s.db.Exec(
		`INSERT INTO audit_log (created_at, principal, action, decision, reason, kind, context)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"2026-06-02T00:00:00Z", "svc:ci", "break_glass_use", "allow", "break_glass_credential_used", "auth_login", "{}",
	); err != nil {
		t.Fatalf("insert auth_login row after re-up: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE audit_log SET reason = 'x'`); err == nil {
		t.Error("UPDATE on audit_log after re-up returned nil; append-only trigger missing")
	}
	if _, err := s.db.Exec(`DELETE FROM audit_log`); err == nil {
		t.Error("DELETE on audit_log after re-up returned nil; append-only trigger missing")
	}
}

// kindAdmitted probes whether the audit_log.kind CHECK admits the given kind
// value, without persisting a row: it inserts inside a transaction and rolls
// back. A CHECK rejection surfaces as an INSERT error.
func kindAdmitted(t *testing.T, s *Store, kind string) bool {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT INTO audit_log (created_at, principal, action, decision, reason, kind, context)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"2026-06-02T00:00:00Z", "user:probe", "probe", "allow", "probe", kind, "{}",
	)
	return err == nil
}

// triggerExists reports whether a named trigger exists in sqlite_master.
func triggerExists(t *testing.T, s *Store, name string) bool {
	t.Helper()
	var got string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='trigger' AND name=?`, name,
	).Scan(&got)
	if err != nil {
		return false
	}
	return got == name
}
