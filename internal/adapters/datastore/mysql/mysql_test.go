package mysql

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
	callCount int
	responses [][]map[string]any
	closed    bool
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
				"host": "localhost", "port": 3306, "user": "root",
				"password": "secret", "database": "mydb",
			},
			wantPort: 3306,
		},
		{
			name: "default port",
			input: map[string]any{
				"host": "localhost", "user": "root", "database": "mydb",
			},
			wantPort: 3306,
		},
		{
			name:    "missing host",
			input:   map[string]any{"user": "root", "database": "mydb"},
			wantErr: true,
		},
		{
			name:    "missing user",
			input:   map[string]any{"host": "localhost", "database": "mydb"},
			wantErr: true,
		},
		{
			name:    "missing database",
			input:   map[string]any{"host": "localhost", "user": "root"},
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
	src := store.Source{
		Config: mustMarshal(t, map[string]any{"user": "root", "database": "db"}), // missing host
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for missing host, got nil")
	}
}

func TestConnect_BadJSON(t *testing.T) {
	a := New()
	src := store.Source{Config: []byte(`{invalid json`)}
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

func TestStat_Success_WithReplica(t *testing.T) {
	procRows := []map[string]any{
		{
			"Id":      int64(1),
			"User":    "app",
			"Host":    "10.0.0.1",
			"db":      "mydb",
			"Command": "Query",
			"Time":    int64(5),
			"State":   "executing",
			"Info":    "SELECT 1",
		},
	}
	replicaRows := []map[string]any{
		{
			"Replica_Running":       "Yes",
			"Seconds_Behind_Master": int64(0),
			"Last_Error":            "",
			"Master_Log_File":       "mysql-bin.000001",
			"Read_Master_Log_Pos":   int64(12345),
		},
	}

	q := &mockQuerier{
		responses: [][]map[string]any{procRows, replicaRows},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if len(stat.Processes) != 1 {
		t.Errorf("len(Processes) = %d, want 1", len(stat.Processes))
	}
	if stat.Processes[0].User != "app" {
		t.Errorf("Processes[0].User = %q, want %q", stat.Processes[0].User, "app")
	}
	if stat.Replica == nil {
		t.Fatal("Replica = nil, want non-nil")
	}
	if !stat.Replica.Running {
		t.Error("Replica.Running = false, want true")
	}
	if stat.Replica.BinlogFile != "mysql-bin.000001" {
		t.Errorf("Replica.BinlogFile = %q, want %q", stat.Replica.BinlogFile, "mysql-bin.000001")
	}
}

func TestStat_Success_NoReplica(t *testing.T) {
	procRows := []map[string]any{
		{
			"Id": int64(1), "User": "root", "Host": "localhost",
			"db": "sys", "Command": "Sleep", "Time": int64(0),
			"State": "", "Info": "",
		},
	}

	// Both SHOW REPLICA STATUS and SHOW SLAVE STATUS return no rows (not a replica)
	q := &mockQuerier{
		responses: [][]map[string]any{procRows, {}, {}},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Replica != nil {
		t.Error("Replica should be nil for non-replica instance")
	}
}

func TestStat_ProcesslistError(t *testing.T) {
	q := &mockQuerier{scanErr: errors.New("query failed")}
	a := NewWithQuerier(q)

	_, err := a.Stat(context.Background())
	if err == nil {
		t.Error("Stat() expected error when processlist fails, got nil")
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
		{"DROP", "DROP TABLE users"},
		{"INSERT", "INSERT INTO users VALUES (1)"},
		{"UPDATE", "UPDATE users SET name='x'"},
		{"DELETE", "DELETE FROM users"},
		{"with leading spaces", "   DELETE FROM users"},
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
	rows := []map[string]any{{"id": int64(1), "name": "bob"}}
	q := &mockQuerier{responses: [][]map[string]any{rows}}
	a := NewWithQuerier(q)

	result, err := a.Query(context.Background(), "SELECT id, name FROM users")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len(result) = %d, want 1", len(result))
	}
	if result[0]["name"] != "bob" {
		t.Errorf("result[0][name] = %v, want bob", result[0]["name"])
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
	// Test type conversions via Stat with different native types.
	procRows := []map[string]any{
		{
			"Id":      int32(5),          // int32 → int64
			"User":    []byte("replica"), // []byte → string
			"Host":    "db-host:3306",
			"db":      nil, // nil → ""
			"Command": "Binlog Dump",
			"Time":    uint64(3600), // uint64 → int64
			"State":   "Master has sent all binlog",
			"Info":    []byte(""), // []byte → string
		},
	}
	replicaRows := []map[string]any{
		{
			"Slave_Running":         "Yes",       // Slave_Running fallback field
			"Seconds_Behind_Master": []byte("5"), // []byte numeric → int64
			"Last_Error":            "",
			"Master_Log_File":       "bin.000001",
			"Read_Master_Log_Pos":   float64(99999),
		},
	}

	q := &mockQuerier{
		responses: [][]map[string]any{procRows, replicaRows},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if len(stat.Processes) != 1 {
		t.Errorf("len(Processes) = %d, want 1", len(stat.Processes))
	}
	if stat.Processes[0].User != "replica" {
		t.Errorf("User = %q, want replica", stat.Processes[0].User)
	}
	if stat.Processes[0].DB != "" {
		t.Errorf("DB from nil = %q, want empty", stat.Processes[0].DB)
	}
	if stat.Replica == nil {
		t.Fatal("Replica should not be nil")
	}
	if !stat.Replica.Running {
		t.Error("Replica.Running from Slave_Running=Yes should be true")
	}
	if stat.Replica.BinlogPos != 99999 {
		t.Errorf("BinlogPos = %d, want 99999", stat.Replica.BinlogPos)
	}
}

func TestDisconnect_NoClient(t *testing.T) {
	a := New()
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestStat_NumericFallbacks(t *testing.T) {
	// Test fallback conversions for edge case types in MySQL row data.
	procRows := []map[string]any{
		{
			"Id":      int(42),   // int → int64
			"User":    int32(99), // non-string, non-[]byte → fmt.Sprintf
			"Host":    "h",
			"db":      "db",
			"Command": "Query",
			"Time":    int32(10), // int32 → int64
			"State":   "",
			"Info":    "",
		},
	}

	q := &mockQuerier{
		responses: [][]map[string]any{procRows, {}},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Processes[0].ID != 42 {
		t.Errorf("ID = %d, want 42", stat.Processes[0].ID)
	}
	// User from int32(99) → toString → fmt.Sprintf → "99"
	if stat.Processes[0].User != "99" {
		t.Errorf("User = %q, want 99", stat.Processes[0].User)
	}
}

func TestToInt64_Float64(t *testing.T) {
	// float64 → int64 branch is not exercised via Stat; test directly.
	procRows := []map[string]any{
		{
			"Id":      float64(7),
			"User":    "test",
			"Host":    "h",
			"db":      "db",
			"Command": "Query",
			"Time":    float64(1.9),
			"State":   "",
			"Info":    "",
		},
	}

	q := &mockQuerier{
		responses: [][]map[string]any{procRows, {}},
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Processes[0].ID != 7 {
		t.Errorf("ID from float64 = %d, want 7", stat.Processes[0].ID)
	}
	if stat.Processes[0].Time != 1 {
		t.Errorf("Time from float64 = %d, want 1", stat.Processes[0].Time)
	}
}

func TestStat_ReplicaStatusFallback_BothFail(t *testing.T) {
	// Both SHOW REPLICA STATUS and SHOW SLAVE STATUS fail; Stat should still
	// succeed and leave Replica nil.
	callCount := 0
	q := &errOnCallQuerier{
		firstOK: []map[string]any{
			{"Id": int64(1), "User": "root", "Host": "h", "db": "d",
				"Command": "Query", "Time": int64(0), "State": "", "Info": ""},
		},
		callThreshold: 1, // fail from second call onwards
		err:           errors.New("replica status not supported"),
	}
	a := NewWithQuerier(q)

	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Replica != nil {
		t.Error("Replica should be nil when both replica status calls fail")
	}
	_ = callCount
}

// errOnCallQuerier returns firstOK on the first call then errors thereafter.
type errOnCallQuerier struct {
	firstOK       []map[string]any
	callThreshold int
	err           error
	calls         int
}

func (e *errOnCallQuerier) ping(_ context.Context) error { return nil }
func (e *errOnCallQuerier) close()                       {}
func (e *errOnCallQuerier) scan(_ context.Context, _ string) ([]map[string]any, error) {
	e.calls++
	if e.calls <= e.callThreshold {
		return e.firstOK, nil
	}
	return nil, e.err
}

func TestStat_ReplicaRunning_SlaveRunningField(t *testing.T) {
	// Stat with Slave_Running=Yes (old-style field) and replica nil for Running.
	procRows := []map[string]any{
		{"Id": int64(1), "User": "r", "Host": "h", "db": "d",
			"Command": "Sleep", "Time": int64(0), "State": "", "Info": ""},
	}
	replicaRows := []map[string]any{
		{
			"Replica_Running":       "No",
			"Slave_Running":         "Yes",
			"Seconds_Behind_Master": int64(3),
			"Last_Error":            "",
			"Master_Log_File":       "bin.001",
			"Read_Master_Log_Pos":   int64(0),
		},
	}
	q := &mockQuerier{responses: [][]map[string]any{procRows, replicaRows}}
	a := NewWithQuerier(q)
	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Replica == nil {
		t.Fatal("expected Replica non-nil")
	}
	if !stat.Replica.Running {
		t.Error("expected Running=true when Slave_Running=Yes")
	}
}

func TestConnect_EmptyConfig(t *testing.T) {
	a := New()
	src := store.Source{Config: nil}
	// Empty config is missing host — ParseConfig should error.
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for empty config, got nil")
	}
}

func TestConnect_PingFails(t *testing.T) {
	a := New()
	src := store.Source{
		Config: mustMarshal(t, map[string]any{
			"host": "127.0.0.1", "port": float64(19998),
			"user": "nobody", "database": "nodb",
		}),
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected ping error for unreachable host, got nil")
	}
}

func TestToInt64_InvalidBytes(t *testing.T) {
	// []byte with non-numeric value should return 0.
	procRows := []map[string]any{
		{
			"Id":      []byte("notanumber"),
			"User":    "u",
			"Host":    "h",
			"db":      "d",
			"Command": "Q",
			"Time":    []byte("abc"),
			"State":   "",
			"Info":    "",
		},
	}
	q := &mockQuerier{responses: [][]map[string]any{procRows, {}}}
	a := NewWithQuerier(q)
	stat, err := a.Stat(context.Background())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Processes[0].ID != 0 {
		t.Errorf("ID from invalid bytes = %d, want 0", stat.Processes[0].ID)
	}
}
