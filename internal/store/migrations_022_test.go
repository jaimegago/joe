package store

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/jaimegago/joe/internal/observability"
)

// TestMigration022_UpDownUp_RoundTrip isolates migration 022 (the chat_messages
// table plus the agent_sessions title/visibility columns). Migration 025 later
// rewrites agent_sessions — it removes the visibility column and collapses the
// type domain — so the 022-era shape (visibility column, 'other' type) only
// exists at the 022 boundary, not at full HEAD. The test therefore steps DOWN to
// 022 to assert its artifacts, steps one more to assert 022's down, then steps
// back up to HEAD. Asserts:
//
//   - At the 022 boundary: chat_messages exists; agent_sessions has title +
//     visibility; visibility defaults to 'private'.
//   - After reverting 022: chat_messages and both columns are gone.
//   - After re-up to HEAD: chat_messages exists again and is empty (025 keeps
//     the permanent transcript table; it never reseeds it).
func TestMigration022_UpDownUp_RoundTrip(t *testing.T) {
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 1) Up to latest (includes 025, which removes visibility from agent_sessions).
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate up #1: %v", err)
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

	// 2) Step down to the 022 boundary: reverts 025 (session rewrite — restores
	// the visibility column + 'other' type), 024 (read promotions), 023
	// (source→component). 022 is now the head.
	if err := m.Steps(-3); err != nil {
		t.Fatalf("Steps(-3) to 022 boundary: %v", err)
	}
	if !tableExists(t, s, "chat_messages") {
		t.Fatal("at 022: chat_messages table must exist")
	}
	if !columnExists(t, s, "agent_sessions", "title") {
		t.Error("at 022: agent_sessions.title must exist")
	}
	if !columnExists(t, s, "agent_sessions", "visibility") {
		t.Error("at 022: agent_sessions.visibility must exist")
	}
	if got := insertSessionAndReadVisibility(t, s, "vis-default"); got != "private" {
		t.Errorf("at 022: visibility default = %q, want 'private'", got)
	}

	// 3) Step down one more: reverts 022 itself.
	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) to revert 022: %v", err)
	}
	if tableExists(t, s, "chat_messages") {
		t.Error("after down: chat_messages must be dropped")
	}
	if columnExists(t, s, "agent_sessions", "visibility") {
		t.Error("after down: agent_sessions.visibility must be dropped")
	}
	if columnExists(t, s, "agent_sessions", "title") {
		t.Error("after down: agent_sessions.title must be dropped")
	}

	// 4) Up again to HEAD: re-applies 022 (and 023/024/025 above it).
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("Up #2: %v", err)
	}
	if !tableExists(t, s, "chat_messages") {
		t.Fatal("after re-up: chat_messages must exist again")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_messages`).Scan(&count); err != nil {
		t.Fatalf("count chat_messages after re-up: %v", err)
	}
	if count != 0 {
		t.Errorf("chat_messages has %d rows after re-up; migration must not seed it", count)
	}
}

// columnExists reports whether table has a column of the given name.
func columnExists(t *testing.T, s *Store, table, column string) bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

// insertSessionAndReadVisibility inserts a minimal 'other' session WITHOUT
// specifying visibility and returns the stored value, exercising the column
// default.
func insertSessionAndReadVisibility(t *testing.T, s *Store, id string) string {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO agent_sessions (id, type, created_at, last_activity_at, creator_principal)
		VALUES (?, 'other', '2026-06-06T00:00:00Z', '2026-06-06T00:00:00Z', 'user:alice@example.com')`,
		id); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	var vis string
	if err := s.db.QueryRow(`SELECT visibility FROM agent_sessions WHERE id = ?`, id).Scan(&vis); err != nil {
		t.Fatalf("read visibility: %v", err)
	}
	return vis
}
