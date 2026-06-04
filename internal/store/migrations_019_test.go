package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// TestMigration019_UpDownUp_RoundTrip exercises the LLM context-budget
// migration: the singleton table is created and seeded on up, dropped on
// down, and recreated on re-up.
func TestMigration019_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	if !tableExists(t, s, "llm_context_budget") {
		t.Fatal("llm_context_budget missing after first up")
	}
	// Seed row present with the unset (zero) default.
	var frac float64
	if err := s.db.QueryRow(`SELECT budget_fraction FROM llm_context_budget WHERE id = 1`).Scan(&frac); err != nil {
		t.Fatalf("read seeded row: %v", err)
	}
	if frac != 0 {
		t.Errorf("seeded budget_fraction = %v, want 0 (unset sentinel)", frac)
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

	// Step two migrations down: reverts 020 (the admin-audit kind widening,
	// which now sits above 019) then 019. Stepping -2 lands the schema just
	// below 019, isolating 019's down migration as this test intends; the
	// table must be gone.
	if err := m.Steps(-2); err != nil {
		t.Fatalf("Steps(-2): %v", err)
	}
	if tableExists(t, s, "llm_context_budget") {
		t.Error("llm_context_budget still exists after down")
	}

	// Up again re-creates it.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("Up #2: %v", err)
	}
	if !tableExists(t, s, "llm_context_budget") {
		t.Error("llm_context_budget missing after re-up")
	}
}
