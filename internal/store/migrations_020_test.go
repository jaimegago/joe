package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// TestMigration020_UpDownUp_RoundTrip runs the full chain up (including 020),
// steps one migration down (reverting 020), and re-applies. Migration 020
// (D-0013) widens the audit_log.kind CHECK to admit 'admin_access'; its down
// narrows the CHECK back to the 018 enum. This asserts:
//
//   - After up: an 'admin_access' row is admitted by the CHECK.
//   - After Steps(-1): the CHECK rejects 'admin_access' again, while the
//     018-era kind 'auth_login' is still admitted (the down only removes
//     admin_access, nothing else).
//   - After re-up: 'admin_access' is admitted again, and both append-only
//     triggers are present on the rebuilt table.
//
// Insertability probes (kindAdmitted, migrations_018_test.go) run inside a
// rolled-back transaction so they never persist a row — an uncommitted
// admin_access row would otherwise block the down migration's row-copy (the
// documented narrowing caveat).
func TestMigration020_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 1) Up to latest (includes 020, the head).
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	if !kindAdmitted(t, s, "admin_access") {
		t.Fatal("after up: audit_log CHECK must admit 'admin_access'")
	}

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

	// 2) Step the migrations above 020 down, then 020: reverts 024 (read
	// promotions, the head), 023 (source→component), 022 (chat sessions), 021
	// (the principals table), then 020. The schema lands just below 020,
	// isolating 020's down migration as this test intends; none of 021..024
	// touches audit_log so reverting them first is inert to these probes.
	if err := m.Steps(-10); err != nil {
		t.Fatalf("Steps(-10): %v", err)
	}
	if kindAdmitted(t, s, "admin_access") {
		t.Error("after down: audit_log CHECK must reject 'admin_access' again")
	}
	if !kindAdmitted(t, s, "auth_login") {
		t.Error("after down: the 018-era kind must still be admitted (down removes only admin_access)")
	}

	// 3) Up again: re-applies 020.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("Up #2: %v", err)
	}
	if !kindAdmitted(t, s, "admin_access") {
		t.Fatal("after re-up: audit_log CHECK must admit 'admin_access' again")
	}

	// The append-only triggers must be present on the rebuilt table.
	for _, trig := range []string{"audit_log_no_update", "audit_log_no_delete"} {
		if !triggerExists(t, s, trig) {
			t.Errorf("trigger %s missing after 020 re-up", trig)
		}
	}

	// Persist one admin_access row so the per-row BEFORE UPDATE/DELETE
	// triggers have something to fire on, then assert both still ABORT.
	if _, err := s.db.Exec(
		`INSERT INTO audit_log (created_at, principal, action, decision, reason, kind, context)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"2026-06-04T00:00:00Z", "user:alice", "zone.create", "allow", "admin_mutation", "admin_access", "{}",
	); err != nil {
		t.Fatalf("insert admin_access row after re-up: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE audit_log SET reason = 'x'`); err == nil {
		t.Error("UPDATE on audit_log after re-up returned nil; append-only trigger missing")
	}
	if _, err := s.db.Exec(`DELETE FROM audit_log`); err == nil {
		t.Error("DELETE on audit_log after re-up returned nil; append-only trigger missing")
	}
}
