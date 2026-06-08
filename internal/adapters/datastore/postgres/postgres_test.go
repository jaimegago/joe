package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// mockQuerier implements the querier interface for testing.
type mockQuerier struct {
	pingErr   error
	scanErr   error
	closed    bool
	callCount int
	responses [][]map[string]any
}

func (m *mockQuerier) ping(_ context.Context) error { return m.pingErr }
func (m *mockQuerier) close()                       { m.closed = true }
func (m *mockQuerier) scan(_ context.Context, _ string) ([]map[string]any, error) {
	if m.scanErr != nil {
		return nil, m.scanErr
	}
	if m.callCount < len(m.responses) {
		result := m.responses[m.callCount]
		m.callCount++
		return result, nil
	}
	return nil, nil
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		wantPort int
		wantErr  bool
	}{
		{
			name: "valid full config",
			input: map[string]any{
				"host": "localhost", "port": 5432, "user": "pguser",
				"password": "secret", "database": "mydb", "ssl_mode": "disable",
			},
			wantPort: 5432,
		},
		{
			name: "default port",
			input: map[string]any{
				"host": "localhost", "user": "pguser", "database": "mydb",
			},
			wantPort: 5432,
		},
		{
			name:    "missing host",
			input:   map[string]any{"user": "pguser", "database": "mydb"},
			wantErr: true,
		},
		{
			name:    "missing user",
			input:   map[string]any{"host": "localhost", "database": "mydb"},
			wantErr: true,
		},
		{
			name:    "missing database",
			input:   map[string]any{"host": "localhost", "user": "pguser"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.Port != tt.wantPort {
				t.Errorf("Port = %d, want %d", cfg.Port, tt.wantPort)
			}
		})
	}
}

func TestConnect_InvalidConfig(t *testing.T) {
	a := New()
	src := store.Component{
		Config: mustMarshal(t, map[string]any{"user": "u", "database": "db"}), // missing host
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for missing host, got nil")
	}
}

func TestConnect_BadJSON(t *testing.T) {
	a := New()
	src := store.Component{Config: []byte(`{invalid json`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for bad JSON, got nil")
	}
}

func TestStatus_NotConnected(t *testing.T) {
	a := New()
	s := a.Status()
	if s.Connected {
		t.Error("Status().Connected = true, want false")
	}
}

func TestStatus_Connected(t *testing.T) {
	q := &mockQuerier{}
	a := NewWithQuerier(q)
	s := a.Status()
	if !s.Connected {
		t.Error("Status().Connected = false, want true")
	}
}

func TestDisconnect(t *testing.T) {
	q := &mockQuerier{}
	a := NewWithQuerier(q)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("Status().Connected = true after disconnect, want false")
	}
}

func TestStat_NotConnected(t *testing.T) {
	a := New()
	_, err := a.Stat(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Stat() error = %v, want ErrNotConnected", err)
	}
}

func TestStat_Success(t *testing.T) {
	activityRows := []map[string]any{
		{
			"pid":         int64(123),
			"state":       "active",
			"wait_event":  "",
			"duration_ms": "100",
			"query":       "SELECT 1",
		},
	}
	tableRows := []map[string]any{
		{
			"schemaname": "public",
			"tablename":  "users",
			"seq_scan":   int64(10),
			"idx_scan":   int64(5),
			"n_live_tup": int64(1000),
			"n_dead_tup": int64(50),
		},
	}
	replRows := []map[string]any{
		{
			"slot_name": "replica1",
			"active":    true,
			"lag_bytes": int64(0),
		},
	}

	q := &mockQuerier{
		responses: [][]map[string]any{activityRows, tableRows, replRows},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if len(stat.Activity) != 1 {
		t.Errorf("len(Activity) = %d, want 1", len(stat.Activity))
	}
	if stat.Activity[0].Query != "SELECT 1" {
		t.Errorf("Activity[0].Query = %q, want %q", stat.Activity[0].Query, "SELECT 1")
	}

	if len(stat.Tables) != 1 {
		t.Errorf("len(Tables) = %d, want 1", len(stat.Tables))
	}
	if stat.Tables[0].Table != "users" {
		t.Errorf("Tables[0].Table = %q, want %q", stat.Tables[0].Table, "users")
	}
	if stat.Tables[0].LiveTup != 1000 {
		t.Errorf("Tables[0].LiveTup = %d, want 1000", stat.Tables[0].LiveTup)
	}

	if len(stat.Replication) != 1 {
		t.Errorf("len(Replication) = %d, want 1", len(stat.Replication))
	}
	if !stat.Replication[0].Active {
		t.Error("Replication[0].Active = false, want true")
	}
}

func TestStat_ScanError(t *testing.T) {
	q := &mockQuerier{scanErr: errors.New("connection lost")}
	a := NewWithQuerier(q)

	_, err := a.Stat(context.Background())
	if err == nil {
		t.Error("Stat() expected error when scan fails, got nil")
	}
}

func TestQuery_NotConnected(t *testing.T) {
	a := New()
	_, err := a.Query(context.Background(), "SELECT 1")
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Query() error = %v, want ErrNotConnected", err)
	}
}

func TestQuery_RejectsNonSelect(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{"DROP TABLE", "DROP TABLE users"},
		{"INSERT", "INSERT INTO users VALUES (1)"},
		{"UPDATE", "UPDATE users SET name='x'"},
		{"DELETE", "DELETE FROM users"},
		{"with spaces", "  DELETE FROM users"},
	}

	q := &mockQuerier{}
	a := NewWithQuerier(q)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := a.Query(context.Background(), tt.sql)
			if err == nil {
				t.Errorf("Query(%q) expected error, got nil", tt.sql)
			}
		})
	}
}

func TestQuery_AcceptsSelect(t *testing.T) {
	rows := []map[string]any{{"id": int64(1), "name": "alice"}}
	q := &mockQuerier{responses: [][]map[string]any{rows}}
	a := NewWithQuerier(q)

	result, err := a.Query(context.Background(), "SELECT id, name FROM users")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len(result) = %d, want 1", len(result))
	}
	if result[0]["name"] != "alice" {
		t.Errorf("result[0][name] = %v, want alice", result[0]["name"])
	}
}

func TestQuery_ScanError(t *testing.T) {
	q := &mockQuerier{scanErr: errors.New("timeout")}
	a := NewWithQuerier(q)

	_, err := a.Query(context.Background(), "SELECT 1")
	if err == nil {
		t.Error("Query() expected error when scan fails, got nil")
	}
}

func TestStat_WithVariousTypes(t *testing.T) {
	// Test type conversion helpers via Stat with different Go native types.
	activityRows := []map[string]any{
		{
			"pid":         int32(99), // int32 → int
			"state":       "idle",
			"wait_event":  "Lock",
			"duration_ms": float64(250.5), // float64 → string via fmt.Sprintf
			"query":       "SELECT 2",
		},
	}
	tableRows := []map[string]any{
		{
			"schemaname": "myschema",
			"tablename":  "orders",
			"seq_scan":   float64(100), // float64 → int64
			"idx_scan":   int32(50),    // int32 → int64
			"n_live_tup": int(9999),    // int → int64
			"n_dead_tup": int64(10),
		},
	}
	replRows := []map[string]any{
		{
			"slot_name": "slot1",
			"active":    int64(1),  // int64 → bool
			"lag_bytes": int(1024), // int → int64
		},
		{
			"slot_name": "slot2",
			"active":    "true", // string → bool
			"lag_bytes": float64(512),
		},
	}

	q := &mockQuerier{
		responses: [][]map[string]any{activityRows, tableRows, replRows},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if len(stat.Activity) != 1 {
		t.Errorf("len(Activity) = %d, want 1", len(stat.Activity))
	}
	if stat.Activity[0].PID != 99 {
		t.Errorf("PID = %d, want 99", stat.Activity[0].PID)
	}
	if stat.Activity[0].Wait != "Lock" {
		t.Errorf("Wait = %q, want Lock", stat.Activity[0].Wait)
	}

	if len(stat.Tables) != 1 {
		t.Errorf("len(Tables) = %d, want 1", len(stat.Tables))
	}
	if stat.Tables[0].SeqScan != 100 {
		t.Errorf("SeqScan = %d, want 100", stat.Tables[0].SeqScan)
	}

	if len(stat.Replication) != 2 {
		t.Errorf("len(Replication) = %d, want 2", len(stat.Replication))
	}
	if !stat.Replication[0].Active {
		t.Error("Replication[0].Active from int64(1) = false, want true")
	}
	if !stat.Replication[1].Active {
		t.Error("Replication[1].Active from string 'true' = false, want true")
	}
}

func TestStat_NilValues(t *testing.T) {
	// nil values should gracefully become zero values.
	activityRows := []map[string]any{
		{"pid": nil, "state": nil, "wait_event": nil, "duration_ms": nil, "query": nil},
	}
	tableRows := []map[string]any{
		{"schemaname": nil, "tablename": nil, "seq_scan": nil, "idx_scan": nil, "n_live_tup": nil, "n_dead_tup": nil},
	}
	replRows := []map[string]any{
		{"slot_name": nil, "active": nil, "lag_bytes": nil},
	}

	q := &mockQuerier{
		responses: [][]map[string]any{activityRows, tableRows, replRows},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() with nil values error = %v", err)
	}
	if len(stat.Activity) != 1 {
		t.Errorf("len(Activity) = %d, want 1", len(stat.Activity))
	}
	if stat.Activity[0].PID != 0 {
		t.Errorf("PID from nil = %d, want 0", stat.Activity[0].PID)
	}
}

func TestDisconnect_NoClient(t *testing.T) {
	// Disconnect when db is nil should not panic.
	a := New()
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestToInt_AdditionalBranches(t *testing.T) {
	// Test toInt with int64 via Stat (PID field).
	activityRows := []map[string]any{
		{
			"pid":         int64(200),
			"state":       "active",
			"wait_event":  "",
			"duration_ms": "10",
			"query":       "SELECT 3",
		},
	}
	q := &mockQuerier{
		responses: [][]map[string]any{activityRows, {}, {}},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Activity[0].PID != 200 {
		t.Errorf("PID from int64 = %d, want 200", stat.Activity[0].PID)
	}
}

func TestToInt_IntBranch(t *testing.T) {
	// toInt with plain int — exercise via Stat PID field.
	activityRows := []map[string]any{
		{
			"pid":         int(42),
			"state":       "active",
			"wait_event":  "",
			"duration_ms": "0",
			"query":       "SELECT 4",
		},
	}
	q := &mockQuerier{
		responses: [][]map[string]any{activityRows, {}, {}},
	}
	a := NewWithQuerier(q)
	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Activity[0].PID != 42 {
		t.Errorf("PID from int = %d, want 42", stat.Activity[0].PID)
	}
}

func TestToInt_Float64Branch(t *testing.T) {
	// toInt with float64.
	activityRows := []map[string]any{
		{
			"pid":         float64(77),
			"state":       "active",
			"wait_event":  "",
			"duration_ms": "0",
			"query":       "SELECT 5",
		},
	}
	q := &mockQuerier{
		responses: [][]map[string]any{activityRows, {}, {}},
	}
	a := NewWithQuerier(q)
	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Activity[0].PID != 77 {
		t.Errorf("PID from float64 = %d, want 77", stat.Activity[0].PID)
	}
}

func TestToInt_Int32Branch(t *testing.T) {
	// toInt with int32.
	activityRows := []map[string]any{
		{
			"pid":         int32(55),
			"state":       "active",
			"wait_event":  "",
			"duration_ms": "0",
			"query":       "SELECT 6",
		},
	}
	q := &mockQuerier{
		responses: [][]map[string]any{activityRows, {}, {}},
	}
	a := NewWithQuerier(q)
	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Activity[0].PID != 55 {
		t.Errorf("PID from int32 = %d, want 55", stat.Activity[0].PID)
	}
}

func TestStat_TableScanError(t *testing.T) {
	// Activity succeeds, table scan fails.
	callCount := 0
	q := &mockQuerier{}
	q.responses = [][]map[string]any{
		{{"pid": int64(1), "state": "active", "wait_event": "", "duration_ms": "0", "query": "SELECT 1"}},
	}
	// Override scan to fail on the second call.
	_ = callCount
	qFail := &failOnCallQuerier{after: 1, err: errors.New("table scan failed")}
	a := NewWithQuerier(qFail)
	_, err := a.Stat(context.Background())
	if err == nil {
		t.Error("Stat() expected error when table scan fails, got nil")
	}
}

func TestStat_ReplicationScanError(t *testing.T) {
	// Activity and tables succeed, replication scan fails.
	qFail := &failOnCallQuerier{after: 2, err: errors.New("replication scan failed")}
	a := NewWithQuerier(qFail)
	_, err := a.Stat(context.Background())
	if err == nil {
		t.Error("Stat() expected error when replication scan fails, got nil")
	}
}

func TestConnect_EmptyConfig(t *testing.T) {
	// Connect with empty (nil) config should fail because host is required.
	a := New()
	src := store.Component{Config: nil}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for empty config (missing host), got nil")
	}
}

func TestConnect_PingFails(t *testing.T) {
	// The real Connect path tries to open and ping a real DB; provide an invalid
	// DSN so that Open succeeds but Ping fails (pgx doesn't validate on Open).
	a := New()
	src := store.Component{
		Config: mustMarshal(t, map[string]any{
			"host": "127.0.0.1", "port": float64(19999),
			"user": "nobody", "database": "nodb",
		}),
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected ping error for unreachable host, got nil")
	}
}

// failOnCallQuerier returns one empty row-set per call until `after` calls, then errors.
type failOnCallQuerier struct {
	after int
	err   error
	calls int
}

func (f *failOnCallQuerier) ping(_ context.Context) error { return nil }
func (f *failOnCallQuerier) close()                       {}
func (f *failOnCallQuerier) scan(_ context.Context, _ string) ([]map[string]any, error) {
	f.calls++
	if f.calls > f.after {
		return nil, f.err
	}
	return nil, nil
}
