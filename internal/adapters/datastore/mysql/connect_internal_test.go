package mysql

// This file tests the sqlQuerier methods (ping/close/scan) by registering a
// minimal fake database/sql driver. No real MySQL instance required.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

// ---- fake driver ----

type fakeMySQLDriver struct{}

func (f *fakeMySQLDriver) Open(name string) (driver.Conn, error) {
	if name == "fail" {
		return nil, errors.New("fake connection error")
	}
	return &fakeMySQLConn{dsn: name}, nil
}

type fakeMySQLConn struct {
	dsn string
}

func (c *fakeMySQLConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeMySQLStmt{query: query, dsn: c.dsn}, nil
}
func (c *fakeMySQLConn) Close() error              { return nil }
func (c *fakeMySQLConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }

type fakeMySQLStmt struct {
	query string
	dsn   string
}

func (s *fakeMySQLStmt) Close() error                                    { return nil }
func (s *fakeMySQLStmt) NumInput() int                                   { return 0 }
func (s *fakeMySQLStmt) Exec(args []driver.Value) (driver.Result, error) { return nil, nil }
func (s *fakeMySQLStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &fakeMySQLRows{
		cols: []string{"Id", "User"},
		rows: [][]driver.Value{
			{int64(1), "root"},
		},
	}, nil
}

type fakeMySQLRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func (r *fakeMySQLRows) Columns() []string { return r.cols }
func (r *fakeMySQLRows) Close() error      { return nil }
func (r *fakeMySQLRows) Next(dest []driver.Value) error {
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
	sql.Register("fakemysql", &fakeMySQLDriver{})
}

// ---- tests ----

func TestSQLQuerier_Scan_Success(t *testing.T) {
	db, err := sql.Open("fakemysql", "ok")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	q := &sqlQuerier{db: db}

	rows, err := q.scan(context.Background(), "SELECT Id, User")
	if err != nil {
		t.Fatalf("scan() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["Id"] != int64(1) {
		t.Errorf("Id = %v, want 1", rows[0]["Id"])
	}
}

func TestSQLQuerier_Close(t *testing.T) {
	db, err := sql.Open("fakemysql", "ok")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	q := &sqlQuerier{db: db}
	q.close()
}

func TestSQLQuerier_Ping_Success(t *testing.T) {
	db, err := sql.Open("fakemysql", "ok")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	q := &sqlQuerier{db: db}
	if err := q.ping(context.Background()); err != nil {
		t.Fatalf("ping() unexpected error = %v", err)
	}
}
