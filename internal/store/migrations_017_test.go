package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration017_UpDownUp_RoundTrip runs the full migration chain up to
// 017, steps one migration down (reverting 017), and re-applies the chain.
// All four new tables (llm_usage, llm_settings, llm_cost_limits,
// llm_runaway_limits) must exist after the final up. This guards the
// migration's down step against typos that would either fail to drop the
// tables on the way down (visible as a "already exists" error on the
// re-up) or leave them present (which the assertion would still pass —
// the missing case is the table absent after re-up, which would mean the
// up step wasn't replayed). Both directions are checked.
func TestMigration017_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 1) Up to the latest (includes 017).
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
	}
	// Sanity check: the new tables are present after the first up.
	mustHaveLLMTables(t, s, "after first up")

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

	// 2) Step one migration down: reverts 017 only.
	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1): %v", err)
	}
	// After the down, the four new tables must be gone.
	for _, table := range []string{
		"llm_usage", "llm_settings", "llm_cost_limits", "llm_runaway_limits",
	} {
		if tableExists(t, s, table) {
			t.Errorf("table %s still exists after Steps(-1); the down migration did not drop it", table)
		}
	}

	// 2b) Confirm the audit_log kind CHECK is LEFT WIDENED by the down
	// migration. 017's down deliberately does NOT narrow the CHECK back
	// (the asymmetry is documented in 017_llm_instrumentation.down.sql);
	// this assertion proves that documented behavior holds. Pre-G1 the
	// closed CHECK from migration 015 would reject this row.
	if _, err := s.db.Exec(
		`INSERT INTO audit_log (created_at, principal, action, decision, reason, kind, context)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"2026-05-30T00:00:00Z",
		"user:operator",
		"llm_set_active_model",
		"allow",
		"down_state_check",
		"llm_settings_mutation",
		"{}",
	); err != nil {
		t.Errorf("INSERT llm_settings_mutation after Steps(-1) failed: %v; the down migration must leave the widened CHECK in place", err)
	}

	// 3) Up again: re-applies 017.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("Up #2: %v", err)
	}
	mustHaveLLMTables(t, s, "after re-up")
}

func mustHaveLLMTables(t *testing.T, s *Store, where string) {
	t.Helper()
	for _, table := range []string{
		"llm_usage", "llm_settings", "llm_cost_limits", "llm_runaway_limits",
	} {
		if !tableExists(t, s, table) {
			t.Errorf("table %s missing %s; migration 017 up did not create it", table, where)
		}
	}
}

func tableExists(t *testing.T, s *Store, table string) bool {
	t.Helper()
	var name string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&name)
	if err != nil {
		return false
	}
	return name == table
}
