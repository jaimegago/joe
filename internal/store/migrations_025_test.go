package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration025_SessionSchemaRewrite asserts the §12.4 clean-schema target
// that migration 025 establishes on agent_sessions:
//
//   - the type domain is exactly {default, incident} (the as-built 'other' and
//     'investigation' are gone);
//   - the visibility column is dropped;
//   - the lifecycle columns (trashed_at/trashed_by/purge_after/archived_at/
//     archived_by/archive_ref) are present;
//   - the incident_state ⇔ type=incident CHECK is preserved;
//   - linked_incident_id is ON DELETE SET NULL (deleting an incident severs the
//     link rather than cascade-deleting the linked session);
//   - retention_class survives; title survives.
//
// It also confirms the legacy migration-001 sessions/session_messages tables are
// left untouched, and runs a down/up round-trip.
func TestMigration025_SessionSchemaRewrite(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up: %v", err)
	}

	// Column shape: lifecycle present, visibility gone, title + retention kept.
	for _, col := range []string{
		"trashed_at", "trashed_by", "purge_after",
		"archived_at", "archived_by", "archive_ref",
		"retention_class", "title", "linked_incident_id",
	} {
		if !columnExists(t, s, "agent_sessions", col) {
			t.Errorf("agent_sessions.%s must exist after 025", col)
		}
	}
	if columnExists(t, s, "agent_sessions", "visibility") {
		t.Error("agent_sessions.visibility must be dropped by 025")
	}

	// Legacy tables MUST remain untouched (learn-from-sessions data source).
	if !tableExists(t, s, "sessions") {
		t.Error("legacy `sessions` table must remain present")
	}
	if !tableExists(t, s, "session_messages") {
		t.Error("legacy `session_messages` table must remain present")
	}

	// Type domain: 'default' and 'incident' admitted; 'other'/'investigation'
	// rejected by the CHECK.
	if !insertTypeAdmitted(t, s, "default", nil) {
		t.Error("type='default' must be admitted")
	}
	if !insertTypeAdmitted(t, s, "incident", strptr("declared")) {
		t.Error("type='incident' with incident_state must be admitted")
	}
	if insertTypeAdmitted(t, s, "other", nil) {
		t.Error("type='other' must be rejected by the 025 CHECK")
	}
	if insertTypeAdmitted(t, s, "investigation", nil) {
		t.Error("type='investigation' must be rejected by the 025 CHECK")
	}

	// incident_state ⇔ type=incident CHECK preserved both directions.
	if insertTypeAdmitted(t, s, "default", strptr("declared")) {
		t.Error("a 'default' session with incident_state must be rejected")
	}
	if insertTypeAdmitted(t, s, "incident", nil) {
		t.Error("an 'incident' session without incident_state must be rejected")
	}

	// linked_incident_id ON DELETE SET NULL: deleting an incident severs the
	// link; the linked 'default' session survives with a NULL link.
	if _, err := s.db.Exec(`
		INSERT INTO agent_sessions (id, type, incident_state, created_at, last_activity_at, creator_principal)
		VALUES ('inc', 'incident', 'declared', '2026-06-21T00:00:00Z', '2026-06-21T00:00:00Z', 'user:alice')`); err != nil {
		t.Fatalf("insert incident: %v", err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO agent_sessions (id, type, created_at, last_activity_at, creator_principal, linked_incident_id)
		VALUES ('chat', 'default', '2026-06-21T00:00:00Z', '2026-06-21T00:00:00Z', 'user:bob', 'inc')`); err != nil {
		t.Fatalf("insert linked chat: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM agent_sessions WHERE id = 'inc'`); err != nil {
		t.Fatalf("delete incident: %v", err)
	}
	var cnt int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM agent_sessions WHERE id = 'chat'`).Scan(&cnt); err != nil {
		t.Fatalf("count chat: %v", err)
	}
	if cnt != 1 {
		t.Errorf("linked session count after incident delete = %d, want 1 (SET NULL, not cascade)", cnt)
	}
	var link any
	if err := s.db.QueryRow(`SELECT linked_incident_id FROM agent_sessions WHERE id = 'chat'`).Scan(&link); err != nil {
		t.Fatalf("read link: %v", err)
	}
	if link != nil {
		t.Errorf("linked_incident_id after incident delete = %v, want NULL", link)
	}

	// Down/up round-trip: revert 025 (restores the 022-era shape), then re-up.
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
	if err := m.Steps(-5); err != nil {
		t.Fatalf("Steps(-5) revert 025: %v", err)
	}
	if !columnExists(t, s, "agent_sessions", "visibility") {
		t.Error("after reverting 025: visibility column must be restored")
	}
	if columnExists(t, s, "agent_sessions", "trashed_at") {
		t.Error("after reverting 025: trashed_at must be gone")
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("re-up: %v", err)
	}
	if columnExists(t, s, "agent_sessions", "visibility") {
		t.Error("after re-up: visibility must be dropped again")
	}
}

// insertTypeAdmitted probes whether the agent_sessions CHECK admits a row with
// the given type and optional incident_state, without persisting it (insert in a
// transaction, then roll back). A CHECK rejection surfaces as an INSERT error.
func insertTypeAdmitted(t *testing.T, s *Store, typ string, incidentState *string) bool {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var stateArg any
	if incidentState != nil {
		stateArg = *incidentState
	}
	_, err = tx.Exec(`
		INSERT INTO agent_sessions (id, type, incident_state, created_at, last_activity_at, creator_principal)
		VALUES (?, ?, ?, '2026-06-21T00:00:00Z', '2026-06-21T00:00:00Z', 'user:probe')`,
		"probe-"+typ, typ, stateArg)
	return err == nil
}

func strptr(s string) *string { return &s }
