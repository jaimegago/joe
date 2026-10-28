package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration034_AutoRuns_UpDownUp_RoundTrip isolates migration 034 (the Auto
// Joe run record and schedules). Asserts:
//
//   - At HEAD: auto_runs and auto_schedules exist with their indexes, and the
//     one-live-run-per-scope partial unique index refuses a second 'running'
//     row on one scope while admitting one on another scope and a completed
//     row on the same scope.
//   - After reverting 034: both tables and every index are gone.
//   - After re-up to HEAD: both tables are back.
func TestMigration034_AutoRuns_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	for _, table := range []string{"auto_runs", "auto_schedules"} {
		if !tableExists(t, s, table) {
			t.Fatalf("at HEAD: %s must exist", table)
		}
	}
	indexes := []string{
		"idx_auto_runs_scope_key", "idx_auto_runs_started_at",
		"idx_auto_runs_one_live_per_scope", "idx_auto_schedules_next_run_at",
	}
	for _, idx := range indexes {
		if !indexExists(t, s, idx) {
			t.Errorf("at HEAD: index %s must exist", idx)
		}
	}

	// One live run per scope is structural: the second 'running' row on scope
	// "a" fails the partial unique index; a running row on scope "b" and a
	// completed row on scope "a" both insert.
	insert := func(id, scope, state string) error {
		_, err := s.db.Exec(`INSERT INTO auto_runs
			(id, activator, principal, selection, scope_key, resolved_components,
			 token_ceiling, wall_clock_seconds, report_reserve_tokens, state, started_at)
			VALUES (?, 'user:op', 'svc:agent:core', '{}', ?, '[]', 1, 1, 1, ?, '2026-10-28T00:00:00Z')`,
			id, scope, state)
		return err
	}
	if err := insert("r1", "a", "running"); err != nil {
		t.Fatalf("first running row on scope a: %v", err)
	}
	if err := insert("r2", "a", "running"); err == nil {
		t.Fatal("second running row on scope a must fail the one-live-per-scope index")
	}
	if err := insert("r3", "b", "running"); err != nil {
		t.Fatalf("running row on scope b: %v", err)
	}
	if err := insert("r4", "a", "completed"); err != nil {
		t.Fatalf("completed row on scope a: %v", err)
	}
	if err := insert("r5", "a", "paused"); err == nil {
		t.Fatal("state outside the CHECK domain must be refused")
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

	if err := m.Steps(stepsDownTo(t, 33)); err != nil {
		t.Fatalf("step down to 033 boundary: %v", err)
	}
	for _, table := range []string{"auto_runs", "auto_schedules"} {
		if tableExists(t, s, table) {
			t.Errorf("after down: %s must be dropped", table)
		}
	}
	for _, idx := range indexes {
		if indexExists(t, s, idx) {
			t.Errorf("after down: index %s must be dropped", idx)
		}
	}

	if err := m.Up(); err != nil {
		t.Fatalf("re-up to HEAD: %v", err)
	}
	for _, table := range []string{"auto_runs", "auto_schedules"} {
		if !tableExists(t, s, table) {
			t.Errorf("after re-up: %s must exist", table)
		}
	}
}
