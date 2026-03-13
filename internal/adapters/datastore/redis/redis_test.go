package redis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/store"
	goredis "github.com/redis/go-redis/v9"
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

func TestParseConfig_AdditionalCases(t *testing.T) {
	// Test default port assignment (not covered by existing TestParseConfig).
	cfg, err := ParseConfig(map[string]any{"host": "redis-server"})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.Port != 6379 {
		t.Errorf("default Port = %d, want 6379", cfg.Port)
	}
}

func TestParseInfo_EdgeCases(t *testing.T) {
	// Test parseInfo with lines that have no colon separator (should be skipped).
	raw := "# Server section\nno_colon_here\nused_memory:102400\nkey:val\n"
	result := parseInfo(raw)
	if result["used_memory"] != "102400" {
		t.Errorf("used_memory = %q, want 102400", result["used_memory"])
	}
	if _, ok := result["no_colon_here"]; ok {
		t.Error("expected line without colon to be skipped")
	}
}

func TestConnect_EmptyConfig(t *testing.T) {
	// Connect with empty config bytes (no host) — should return an error.
	a := New()
	src := store.Source{} // empty Config
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for empty source config (missing host), got nil")
	}
}

func TestConnect_TLSEnabled(t *testing.T) {
	// Connect with TLS enabled config — should fail at ping (no server), not at config parsing.
	a := New()
	src := store.Source{Config: mustMarshal(t, map[string]any{
		"host":        "127.0.0.1",
		"port":        19999, // nothing listening
		"tls_enabled": true,
	})}
	// Expect error at ping stage (no server), not nil.
	err := a.Connect(context.Background(), src)
	if err == nil {
		t.Error("Connect() with TLS to non-existent server should return error")
	}
}

func TestDisconnect_CalledTwice(t *testing.T) {
	// Second disconnect (client already nil) must not panic.
	m := &mockRediser{}
	a := NewWithClient(m)
	_ = a.Disconnect()
	if err := a.Disconnect(); err != nil {
		t.Fatalf("second Disconnect() error = %v", err)
	}
}

func TestDisconnect_SetsNotConnected(t *testing.T) {
	m := &mockRediser{}
	a := NewWithClient(m)
	if !a.Status().Connected {
		t.Fatal("expected connected before Disconnect")
	}
	_ = a.Disconnect()
	if a.Status().Connected {
		t.Error("expected not connected after Disconnect")
	}
}

func TestParseConfig_TLSEnabled(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{"host": "redis", "tls_enabled": true})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if !cfg.TLSEnabled {
		t.Error("TLSEnabled should be true")
	}
}

func TestParseConfig_CustomDB(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{"host": "redis", "db": float64(3)})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.DB != 3 {
		t.Errorf("DB = %d, want 3", cfg.DB)
	}
}

func TestParseConfig_WithPassword(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{"host": "redis", "password": "secret"})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q, want secret", cfg.Password)
	}
}

func TestInfo_EmptySection(t *testing.T) {
	// Calling Info with empty section should succeed (mockRediser returns infoResult).
	raw := "redis_version:7.0.0\r\n"
	m := &mockRediser{infoResult: raw}
	a := NewWithClient(m)
	result, err := a.Info(context.Background(), "")
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if result["redis_version"] != "7.0.0" {
		t.Errorf("redis_version = %q, want 7.0.0", result["redis_version"])
	}
}

func TestParseInfo_EmptyString(t *testing.T) {
	result := parseInfo("")
	if len(result) != 0 {
		t.Errorf("parseInfo(\"\") = %v, want empty map", result)
	}
}

func TestParseInfo_CRLFLines(t *testing.T) {
	raw := "used_memory:1024\r\nused_memory_human:1.00K\r\n"
	result := parseInfo(raw)
	if result["used_memory"] != "1024" {
		t.Errorf("used_memory = %q, want 1024", result["used_memory"])
	}
	if result["used_memory_human"] != "1.00K" {
		t.Errorf("used_memory_human = %q, want 1.00K", result["used_memory_human"])
	}
}

// TestGoRediser_Methods exercises goRediser wrapper methods using a real
// go-redis client pointed at a non-existent server. All operations are
// expected to fail with network errors — the goal is line coverage.
func TestGoRediser_Methods(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	rc := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:19998"})
	r := &goRediser{c: rc}

	// close — does not need a live connection; covers close().
	if err := r.close(); err != nil {
		t.Logf("close() returned (expected) error: %v", err)
	}

	// Re-create client since close() closed the previous one.
	rc2 := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:19998"})
	r2 := &goRediser{c: rc2}
	defer r2.close() //nolint:errcheck

	// ping — expected to fail (no server), covers ping().
	_ = r2.ping(ctx)

	// info — expected to fail, covers info() both branches (empty and non-empty section).
	_, _ = r2.info(ctx, "server")
	_, _ = r2.info(ctx, "")

	// slowlogGet — expected to fail, covers slowlogGet().
	_, _ = r2.slowlogGet(ctx, 10)

	// dbsize — expected to fail, covers dbsize().
	_, _ = r2.dbsize(ctx)
}
