package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration021_UpDownUp_RoundTrip runs the full chain up (including 021),
// steps down past the principals table, and re-applies. Migration 021 creates
// the authoritative identity registry; migrations 022 (chat sessions), 023
// (source→component), and 024 (read promotions) now sit above it, so -4 reverts
// 024, 023, 022, then 021 to isolate it. This asserts:
//
//   - After up: the principals table accepts a row and its status CHECK rejects
//     an unknown status value.
//   - After Steps(-7): the principals table is gone.
//   - After re-up: the table exists again and is empty (the migration creates
//     but does not seed it).
func TestMigration021_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 1) Up to latest (includes 021).
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	if !tableExists(t, s, "principals") {
		t.Fatal("after up: principals table must exist")
	}
	if !principalStatusAdmitted(t, s, "active") || !principalStatusAdmitted(t, s, "disabled") {
		t.Error("after up: principals CHECK must admit 'active' and 'disabled'")
	}
	if principalStatusAdmitted(t, s, "bogus") {
		t.Error("after up: principals CHECK must reject an unknown status")
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

	// 2) Step the migrations above 021 down, then 021: reverts 024 (read
	// promotions, the head), 023 (source→component), 022 (chat sessions), then
	// 021, dropping principals. None of 022..024 touches principals, so
	// reverting them first is inert to this probe.
	if err := m.Steps(-7); err != nil {
		t.Fatalf("Steps(-7): %v", err)
	}
	if tableExists(t, s, "principals") {
		t.Error("after down: principals table must be dropped")
	}

	// 3) Up again: re-applies 021.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("Up #2: %v", err)
	}
	if !tableExists(t, s, "principals") {
		t.Fatal("after re-up: principals table must exist again")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM principals`).Scan(&count); err != nil {
		t.Fatalf("count principals after re-up: %v", err)
	}
	if count != 0 {
		t.Errorf("principals table has %d rows after re-up; migration must not seed it", count)
	}
}

// principalStatusAdmitted probes whether the principals.status CHECK admits the
// given value, without persisting a row: it inserts inside a transaction and
// rolls back. A CHECK rejection surfaces as an INSERT error.
func principalStatusAdmitted(t *testing.T, s *Store, status string) bool {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT INTO principals (principal, created_at, status) VALUES (?, ?, ?)`,
		"user:probe@example.com", "2026-06-05T00:00:00Z", status,
	)
	return err == nil
}
