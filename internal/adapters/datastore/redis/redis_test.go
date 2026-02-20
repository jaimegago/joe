package redis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// mockRediser implements the rediser interface for testing.
type mockRediser struct {
	pingErr     error
	infoResult  string
	infoErr     error
	slowlogData []SlowLogEntry
	slowlogErr  error
	dbsizeVal   int64
	dbsizeErr   error
	closed      bool
}

func (m *mockRediser) ping(_ context.Context) error { return m.pingErr }
func (m *mockRediser) info(_ context.Context, _ string) (string, error) {
	return m.infoResult, m.infoErr
}
func (m *mockRediser) slowlogGet(_ context.Context, _ int64) ([]SlowLogEntry, error) {
	return m.slowlogData, m.slowlogErr
}
func (m *mockRediser) dbsize(_ context.Context) (int64, error) {
	return m.dbsizeVal, m.dbsizeErr
}
func (m *mockRediser) close() error {
	m.closed = true
	return nil
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
			name:     "valid config",
			input:    map[string]any{"host": "localhost", "port": 6379},
			wantPort: 6379,
		},
		{
			name:     "default port",
			input:    map[string]any{"host": "localhost"},
			wantPort: 6379,
		},
		{
			name:    "missing host",
			input:   map[string]any{"port": 6379},
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
	src := store.Source{Config: mustMarshal(t, map[string]any{})} // missing host
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for missing host, got nil")
	}
}

func TestConnect_BadJSON(t *testing.T) {
	a := New()
	src := store.Source{Config: []byte(`{invalid`)}
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
	m := &mockRediser{}
	a := NewWithClient(m)
	s := a.Status()
	if !s.Connected {
		t.Error("Status().Connected = false, want true")
	}
}

func TestDisconnect(t *testing.T) {
	m := &mockRediser{}
	a := NewWithClient(m)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("Status().Connected = true after disconnect, want false")
	}
	if !m.closed {
		t.Error("close() was not called on the underlying client")
	}
}

func TestInfo_NotConnected(t *testing.T) {
	a := New()
	_, err := a.Info(context.Background(), "server")
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Info() error = %v, want ErrNotConnected", err)
	}
}

func TestInfo_ParsesOutput(t *testing.T) {
	raw := "# Server\r\nredis_version:7.0.0\r\nuptime_in_seconds:12345\r\n\r\n# Clients\r\nconnected_clients:5\r\n"
	m := &mockRediser{infoResult: raw}
	a := NewWithClient(m)

	result, err := a.Info(context.Background(), "server")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}

	if result["redis_version"] != "7.0.0" {
		t.Errorf("redis_version = %q, want %q", result["redis_version"], "7.0.0")
	}
	if result["connected_clients"] != "5" {
		t.Errorf("connected_clients = %q, want %q", result["connected_clients"], "5")
	}
	// Section headers should not appear as keys
	if _, ok := result["# Server"]; ok {
		t.Error("section header should not appear as key")
	}
}

func TestInfo_Error(t *testing.T) {
	m := &mockRediser{infoErr: errors.New("connection reset")}
	a := NewWithClient(m)
	_, err := a.Info(context.Background(), "")
	if err == nil {
		t.Error("Info() expected error, got nil")
	}
}

func TestSlowLog_NotConnected(t *testing.T) {
	a := New()
	_, err := a.SlowLog(context.Background(), 10)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("SlowLog() error = %v, want ErrNotConnected", err)
	}
}

func TestSlowLog_Success(t *testing.T) {
	entries := []SlowLogEntry{
		{ID: 1, ExecutionTimeUS: 5000, Keys: 2, Command: []string{"SET", "key", "value"}},
	}
	m := &mockRediser{slowlogData: entries}
	a := NewWithClient(m)

	result, err := a.SlowLog(context.Background(), 10)
	if err != nil {
		t.Fatalf("SlowLog() error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len(result) = %d, want 1", len(result))
	}
	if result[0].ExecutionTimeUS != 5000 {
		t.Errorf("ExecutionTimeUS = %d, want 5000", result[0].ExecutionTimeUS)
	}
	if result[0].Command[0] != "SET" {
		t.Errorf("Command[0] = %q, want SET", result[0].Command[0])
	}
}

func TestSlowLog_Error(t *testing.T) {
	m := &mockRediser{slowlogErr: errors.New("failed")}
	a := NewWithClient(m)
	_, err := a.SlowLog(context.Background(), 10)
	if err == nil {
		t.Error("SlowLog() expected error, got nil")
	}
}

func TestDBSize_NotConnected(t *testing.T) {
	a := New()
	_, err := a.DBSize(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("DBSize() error = %v, want ErrNotConnected", err)
	}
}

func TestDBSize_Success(t *testing.T) {
	m := &mockRediser{dbsizeVal: 42}
	a := NewWithClient(m)

	n, err := a.DBSize(context.Background())
	if err != nil {
		t.Fatalf("DBSize() error = %v", err)
	}
	if n != 42 {
		t.Errorf("DBSize() = %d, want 42", n)
	}
}

func TestDBSize_Error(t *testing.T) {
	m := &mockRediser{dbsizeErr: errors.New("network error")}
	a := NewWithClient(m)
	_, err := a.DBSize(context.Background())
	if err == nil {
		t.Error("DBSize() expected error, got nil")
	}
}
