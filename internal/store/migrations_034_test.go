package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration034_RunFindings_UpDownUp_RoundTrip isolates migration 034 (the
// run findings record). Asserts:
//
//   - At HEAD: run_finding_sets and run_findings exist with their indexes.
//   - After reverting 034: both tables and all six indexes are gone, and the
//     human findings table (011) is untouched.
//   - After re-up to HEAD: both tables are back.
func TestMigration034_RunFindings_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	indexes := []string{
		"idx_run_finding_sets_scope", "idx_run_finding_sets_session",
		"idx_run_findings_run", "idx_run_findings_key", "idx_run_findings_session", "idx_run_findings_subject",
	}
	for _, tbl := range []string{"run_finding_sets", "run_findings"} {
		if !tableExists(t, s, tbl) {
			t.Errorf("at HEAD: %s must exist", tbl)
		}
	}
	for _, idx := range indexes {
		if !indexExists(t, s, idx) {
			t.Errorf("at HEAD: index %s must exist", idx)
		}
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
	for _, tbl := range []string{"run_finding_sets", "run_findings"} {
		if tableExists(t, s, tbl) {
			t.Errorf("after down: %s must be dropped", tbl)
		}
	}
	for _, idx := range indexes {
		if indexExists(t, s, idx) {
			t.Errorf("after down: index %s must be dropped", idx)
		}
	}
	if !tableExists(t, s, "findings") {
		t.Error("after down: the human findings table (011) must be untouched")
	}

	if err := m.Up(); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	for _, tbl := range []string{"run_finding_sets", "run_findings"} {
		if !tableExists(t, s, tbl) {
			t.Errorf("after re-up: %s must exist", tbl)
		}
	}
}
