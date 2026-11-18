package audit_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// postgresDSNEnv names a PostgreSQL database these tests may create and drop
// tables in. It is unset in CI — no job provides a PostgreSQL — so the tests
// skip there, and a pass in CI says nothing about PostgreSQL. They are run by
// hand against a disposable database:
//
//	JOE_TEST_POSTGRES_DSN=postgres://… go test ./internal/audit/ -run Postgres -v
//
// They exist because "audit_log is append-only on PostgreSQL" was, before
// D-0171, a sentence nobody had run. PostgreSQL is latent in this binary
// (D-0085): the embedded migration set is SQLite-dialect and does not apply.
// What is established here is what that means for the audit log.
const postgresDSNEnv = "JOE_TEST_POSTGRES_DSN"

func postgresDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; PostgreSQL is not exercised", postgresDSNEnv)
	}
	db, err := sql.Open(store.DriverPostgres, dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return db
}

// TestPostgres_AStoreJoeCanMigrateIsAppendOnly states the invariant for the
// one unsupported store the configuration surface still names: on PostgreSQL,
// either Joe's migrations do not apply — so Joe does not start, no audit_log
// exists, and there is no entry to alter — or they apply and the boot probe
// passes. The third state, a migrated PostgreSQL whose audit_log can be
// rewritten, is the one that must never exist.
//
// Today it takes the first branch (D-0085). The day the migration set is
// ported it takes the second, and fails if the port left the append-only
// enforcement out.
func TestPostgres_AStoreJoeCanMigrateIsAppendOnly(t *testing.T) {
	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; PostgreSQL is not exercised", postgresDSNEnv)
	}
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverPostgres, DSN: dsn}, nil)
	if err != nil {
		t.Fatalf("store.New(pgx): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	migrateErr := s.Migrate()
	if migrateErr != nil {
		t.Logf("migrations do not apply on PostgreSQL, so Joe does not start on it: %v", migrateErr)
		// No audit_log, so the probe cannot run — and says so rather than
		// passing.
		if err := audit.VerifyAppendOnly(context.Background(), s.DB(), store.DriverPostgres); !errors.Is(err, audit.ErrNotAppendOnly) {
			t.Errorf("VerifyAppendOnly on an unmigrated PostgreSQL = %v; want a refusal", err)
		}
		return
	}
	if err := audit.VerifyAppendOnly(context.Background(), s.DB(), store.DriverPostgres); err != nil {
		t.Fatalf("the migration set applies on PostgreSQL and its audit_log is not append-only: %v", err)
	}
}

// TestPostgres_ProbeTellsAnAppendOnlyTableFromOneThatIsNot runs the boot probe
// against PostgreSQL itself, on an audit_log built by hand in PostgreSQL's
// dialect: with the trigger function that refuses UPDATE and DELETE the probe
// passes, and with either trigger dropped it refuses and names the statement.
// It is what shows the probe's savepoints and placeholders work on the second
// dialect, where a refused statement aborts the whole transaction.
func TestPostgres_ProbeTellsAnAppendOnlyTableFromOneThatIsNot(t *testing.T) {
	db := postgresDB(t)
	ctx := context.Background()

	exec := func(stmt string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	exec(`DROP TABLE IF EXISTS audit_log`)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS audit_log`)
		_, _ = db.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS audit_log_append_only()`)
	})
	// The columns and CHECKs of the migrated table, in PostgreSQL's spelling.
	exec(`CREATE TABLE audit_log (
		id           BIGSERIAL PRIMARY KEY,
		created_at   TEXT NOT NULL,
		principal    TEXT,
		action       TEXT NOT NULL,
		zone         TEXT,
		component_id TEXT,
		decision     TEXT NOT NULL,
		reason       TEXT NOT NULL DEFAULT '',
		kind         TEXT NOT NULL,
		context      TEXT NOT NULL DEFAULT '{}',
		session_id   TEXT,
		CHECK (decision IN ('allow', 'deny'))
	)`)
	exec(`CREATE OR REPLACE FUNCTION audit_log_append_only() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'audit_log is append-only: % is not permitted', TG_OP;
		END;
	$$ LANGUAGE plpgsql`)
	exec(`CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
		FOR EACH ROW EXECUTE FUNCTION audit_log_append_only()`)
	exec(`CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
		FOR EACH ROW EXECUTE FUNCTION audit_log_append_only()`)

	rows := func() int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	if err := audit.VerifyAppendOnly(ctx, db, store.DriverPostgres); err != nil {
		t.Fatalf("probe on an append-only PostgreSQL audit_log: %v", err)
	}
	if n := rows(); n != 0 {
		t.Errorf("the probe left %d row(s) behind on PostgreSQL", n)
	}

	for _, tc := range []struct{ trigger, accepts string }{
		{"audit_log_no_update", "UPDATE"},
		{"audit_log_no_delete", "DELETE"},
	} {
		exec(`DROP TRIGGER ` + tc.trigger + ` ON audit_log`)
		err := audit.VerifyAppendOnly(ctx, db, store.DriverPostgres)
		if !errors.Is(err, audit.ErrNotAppendOnly) {
			t.Errorf("without %s the probe returned %v; want ErrNotAppendOnly", tc.trigger, err)
		} else {
			t.Logf("without %s: %v", tc.trigger, err)
		}
		if n := rows(); n != 0 {
			t.Errorf("the failed probe left %d row(s) behind on PostgreSQL", n)
		}
		// Restore it so the next case removes exactly one enforcement.
		exec(`CREATE TRIGGER ` + tc.trigger + ` BEFORE ` + tc.accepts + ` ON audit_log
			FOR EACH ROW EXECUTE FUNCTION audit_log_append_only()`)
	}
}
