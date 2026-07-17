package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration033_DropOnboardingClarifications_UpDownUp_RoundTrip isolates
// migration 033 (drop the clarifications and onboarding_facts tables). Asserts:
//
//   - At HEAD: both tables and their four indexes are gone.
//   - After reverting 033: both tables exist and accept an insert, their indexes
//     are back, and onboarding_facts carries component_id (the post-023 column
//     name, not source_id).
//   - After re-up to HEAD: both tables are dropped again.
func TestMigration033_DropOnboardingClarifications_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 1) Up to HEAD (includes 033, which drops both tables).
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	if tableExists(t, s, "clarifications") {
		t.Error("at HEAD: clarifications must be dropped")
	}
	if tableExists(t, s, "onboarding_facts") {
		t.Error("at HEAD: onboarding_facts must be dropped")
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

	// 2) Step down to the 032 boundary: reverts 033, recreating both tables.
	if err := m.Steps(stepsDownTo(t, 32)); err != nil {
		t.Fatalf("step down to 032 boundary: %v", err)
	}
	if !tableExists(t, s, "clarifications") {
		t.Fatal("after down: clarifications must be restored")
	}
	if !tableExists(t, s, "onboarding_facts") {
		t.Fatal("after down: onboarding_facts must be restored")
	}
	for _, idx := range []string{
		"idx_clarifications_status", "idx_clarifications_type",
		"idx_facts_subject", "idx_facts_type",
	} {
		if !indexExists(t, s, idx) {
			t.Errorf("after down: index %s must be restored", idx)
		}
	}

	// A restored clarifications row is insertable and defaults status to pending.
	if _, err := s.db.Exec(
		`INSERT INTO clarifications (id, type, context, question) VALUES ('c1','edge_confirm','{}','q?')`,
	); err != nil {
		t.Fatalf("insert clarification after down: %v", err)
	}
	var status string
	if err := s.db.QueryRow(`SELECT status FROM clarifications WHERE id = 'c1'`).Scan(&status); err != nil {
		t.Fatalf("read clarification status: %v", err)
	}
	if status != "pending" {
		t.Errorf("clarification status default = %q, want 'pending'", status)
	}

	// A restored onboarding_facts row is insertable and carries component_id (the
	// post-023 column name). Naming source_id would fail here.
	if _, err := s.db.Exec(
		`INSERT INTO onboarding_facts (fact_type, subject, content, source, component_id)
		 VALUES ('t','s','c','src','comp-1')`,
	); err != nil {
		t.Fatalf("insert onboarding_fact after down: %v", err)
	}
	var componentID string
	if err := s.db.QueryRow(
		`SELECT component_id FROM onboarding_facts WHERE subject = 's'`,
	).Scan(&componentID); err != nil {
		t.Fatalf("read onboarding_fact component_id: %v", err)
	}
	if componentID != "comp-1" {
		t.Errorf("onboarding_fact component_id = %q, want 'comp-1'", componentID)
	}

	// 3) Up again to HEAD: re-applies 033, dropping both tables.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("Up #2: %v", err)
	}
	if tableExists(t, s, "clarifications") {
		t.Error("after re-up: clarifications must be dropped again")
	}
	if tableExists(t, s, "onboarding_facts") {
		t.Error("after re-up: onboarding_facts must be dropped again")
	}
}

// indexExists reports whether an index of the given name exists.
func indexExists(t *testing.T, s *Store, name string) bool {
	t.Helper()
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name,
	).Scan(&n); err != nil {
		t.Fatalf("count index %q: %v", name, err)
	}
	return n > 0
}
