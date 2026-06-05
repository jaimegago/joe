package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// TestNew_SQLitePragmasOnEveryPooledConnection guards the fix for the
// per-connection PRAGMA bug: busy_timeout and foreign_keys are per-connection
// settings, so applying them with a one-off db.Exec left all but one pooled
// connection at busy_timeout=0 (instant SQLITE_BUSY under write contention) and
// foreign_keys=OFF. Encoding them in the DSN makes modernc run them on every
// connection. This test forces the pool to materialise several distinct
// connections at once and asserts each carries the pragmas — a regression to
// the old db.Exec form fails it on every connection but the first.
func TestNew_SQLitePragmasOnEveryPooledConnection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "joe.db")

	st, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: dbPath}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	db := st.DB()

	// Hold N connections open simultaneously so the pool cannot satisfy them
	// from a single reused connection — each Conn reserves a distinct one.
	const n = 8
	conns := make([]*sql.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := range n {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn(%d) error = %v", i, err)
		}
		conns = append(conns, c)
	}

	for i, c := range conns {
		var busy int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatalf("conn %d busy_timeout query: %v", i, err)
		}
		if busy != 5000 {
			t.Errorf("conn %d busy_timeout = %d, want 5000", i, busy)
		}

		var fk int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d foreign_keys query: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("conn %d foreign_keys = %d, want 1 (enabled)", i, fk)
		}

		var journal string
		if err := c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
			t.Fatalf("conn %d journal_mode query: %v", i, err)
		}
		if journal != "wal" {
			t.Errorf("conn %d journal_mode = %q, want wal", i, journal)
		}
	}
}
