package postgres

// This file tests the sqlQuerier methods (ping/close/scan) by registering a
// minimal fake database/sql driver. No real PostgreSQL instance required.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

// ---- fake driver ----

// fakePostgresDriver is a minimal sql.Driver that succeeds or fails based on DSN.
type fakePostgresDriver struct{}

func (f *fakePostgresDriver) Open(name string) (driver.Conn, error) {
	if name == "fail" {
		return nil, errors.New("fake connection error")
	}
	return &fakeConn{dsn: name}, nil
}

type fakeConn struct {
	dsn    string
	closed bool
}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{query: query, dsn: c.dsn}, nil
}
func (c *fakeConn) Close() error { c.closed = true; return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not supported")
}

// fakeStmt returns rows based on query content.
type fakeStmt struct {
	query string
	dsn   string
}

func (s *fakeStmt) Close() error                                    { return nil }
func (s *fakeStmt) NumInput() int                                   { return 0 }
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) { return nil, nil }
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	if s.dsn == "scanerr" {
		return &fakeRows{err: errors.New("scan error")}, nil
	}
	return &fakeRows{
		cols: []string{"col1", "col2"},
		rows: [][]driver.Value{
			{"hello", int64(42)},
		},
	}, nil
}

type fakeRows struct {
	cols  []string
	rows  [][]driver.Value
	idx   int
	err   error
	nexts int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.err != nil && r.nexts > 0 {
		// Fail on second Next call (after first row) to test scan error path.
		return r.err
	}
	r.nexts++
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	for i, v := range row {
		if i < len(dest) {
			dest[i] = v
		}
	}
	return nil
}

func init() {
	sql.Register("fakepgx", &fakePostgresDriver{})
}

// ---- tests ----

func TestSQLQuerier_Scan_Success(t *testing.T) {
	db, err := sql.Open("fakepgx", "ok")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	q := &sqlQuerier{db: db}

	rows, err := q.scan(context.Background(), "SELECT col1, col2")
	if err != nil {
		t.Fatalf("scan() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["col1"] != "hello" {
		t.Errorf("col1 = %v, want hello", rows[0]["col1"])
	}
}

func TestSQLQuerier_Close(t *testing.T) {
	db, err := sql.Open("fakepgx", "ok")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	q := &sqlQuerier{db: db}
	q.close() // should not panic
}

func TestSQLQuerier_Ping_Success(t *testing.T) {
	db, err := sql.Open("fakepgx", "ok")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	q := &sqlQuerier{db: db}
	// PingContext with a fake driver will call Open + Conn.
	// Our fake driver succeeds for any non-"fail" DSN.
	if err := q.ping(context.Background()); err != nil {
		t.Fatalf("ping() unexpected error = %v", err)
	}
}
