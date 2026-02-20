package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// mockRunner implements the mongoRunner interface for testing.
type mockRunner struct {
	pingErr       error
	runResults    []map[string]any
	runErr        error
	disconnectErr error
	disconnected  bool
	callCount     int
}

func (m *mockRunner) ping(_ context.Context) error { return m.pingErr }
func (m *mockRunner) disconnect(_ context.Context) error {
	m.disconnected = true
	return m.disconnectErr
}
func (m *mockRunner) runCommand(_ context.Context, _ string, _ any) (map[string]any, error) {
	if m.runErr != nil {
		return nil, m.runErr
	}
	if m.callCount < len(m.runResults) {
		result := m.runResults[m.callCount]
		m.callCount++
		return result, nil
	}
	return map[string]any{}, nil
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
		name    string
		input   map[string]any
		wantDB  string
		wantErr bool
	}{
		{
			name:   "valid config with database",
			input:  map[string]any{"uri": "mongodb://localhost:27017", "database": "mydb"},
			wantDB: "mydb",
		},
		{
			name:   "default database",
			input:  map[string]any{"uri": "mongodb://localhost:27017"},
			wantDB: "admin",
		},
		{
			name:    "missing uri",
			input:   map[string]any{"database": "mydb"},
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
			if !tt.wantErr && cfg.Database != tt.wantDB {
				t.Errorf("Database = %q, want %q", cfg.Database, tt.wantDB)
			}
		})
	}
}

func TestConnect_InvalidConfig(t *testing.T) {
	a := New()
	src := store.Source{Config: mustMarshal(t, map[string]any{})} // missing uri
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for missing uri, got nil")
	}
}

func TestConnect_BadJSON(t *testing.T) {
	a := New()
	src := store.Source{Config: []byte(`{bad json`)}
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
	r := &mockRunner{}
	a := NewWithRunner(r)
	s := a.Status()
	if !s.Connected {
		t.Error("Status().Connected = false, want true")
	}
}

func TestDisconnect(t *testing.T) {
	r := &mockRunner{}
	a := NewWithRunner(r)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("Status().Connected = true after disconnect, want false")
	}
	if !r.disconnected {
		t.Error("disconnect() was not called on the runner")
	}
}

func TestServerStatus_NotConnected(t *testing.T) {
	a := New()
	_, err := a.ServerStatus(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("ServerStatus() error = %v, want ErrNotConnected", err)
	}
}

func TestServerStatus_Success(t *testing.T) {
	result := map[string]any{"host": "mongo1:27017", "version": "6.0.0", "ok": float64(1)}
	r := &mockRunner{runResults: []map[string]any{result}}
	a := NewWithRunner(r)

	got, err := a.ServerStatus(context.Background())
	if err != nil {
		t.Fatalf("ServerStatus() error = %v", err)
	}
	if got["host"] != "mongo1:27017" {
		t.Errorf("host = %v, want mongo1:27017", got["host"])
	}
}

func TestServerStatus_Error(t *testing.T) {
	r := &mockRunner{runErr: errors.New("command failed")}
	a := NewWithRunner(r)
	_, err := a.ServerStatus(context.Background())
	if err == nil {
		t.Error("ServerStatus() expected error, got nil")
	}
}

func TestReplicaStatus_NotConnected(t *testing.T) {
	a := New()
	_, err := a.ReplicaStatus(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("ReplicaStatus() error = %v, want ErrNotConnected", err)
	}
}

func TestReplicaStatus_Success(t *testing.T) {
	result := map[string]any{"set": "rs0", "myState": float64(1), "ok": float64(1)}
	r := &mockRunner{runResults: []map[string]any{result}}
	a := NewWithRunner(r)

	got, err := a.ReplicaStatus(context.Background())
	if err != nil {
		t.Fatalf("ReplicaStatus() error = %v", err)
	}
	if got["set"] != "rs0" {
		t.Errorf("set = %v, want rs0", got["set"])
	}
}

func TestReplicaStatus_Error(t *testing.T) {
	r := &mockRunner{runErr: errors.New("not a replica set")}
	a := NewWithRunner(r)
	_, err := a.ReplicaStatus(context.Background())
	if err == nil {
		t.Error("ReplicaStatus() expected error, got nil")
	}
}

func TestCurrentOp_NotConnected(t *testing.T) {
	a := New()
	_, err := a.CurrentOp(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("CurrentOp() error = %v, want ErrNotConnected", err)
	}
}

func TestCurrentOp_Success(t *testing.T) {
	result := map[string]any{"inprog": []any{}, "ok": float64(1)}
	r := &mockRunner{runResults: []map[string]any{result}}
	a := NewWithRunner(r)

	got, err := a.CurrentOp(context.Background())
	if err != nil {
		t.Fatalf("CurrentOp() error = %v", err)
	}
	if _, ok := got["inprog"]; !ok {
		t.Error("CurrentOp() result missing 'inprog' key")
	}
}

func TestCurrentOp_Error(t *testing.T) {
	r := &mockRunner{runErr: errors.New("unauthorized")}
	a := NewWithRunner(r)
	_, err := a.CurrentOp(context.Background())
	if err == nil {
		t.Error("CurrentOp() expected error, got nil")
	}
}

func TestDisconnect_NoRunner(t *testing.T) {
	// Disconnect when runner is nil should not panic.
	a := New()
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestStatus_ConnectedMessage(t *testing.T) {
	r := &mockRunner{}
	a := NewWithRunner(r)
	s := a.Status()
	if s.Message == "" {
		t.Error("Status().Message should not be empty when connected")
	}
}

func TestServerStatus_MultipleCallsSequential(t *testing.T) {
	// Verify that runner is called once per method, not shared state.
	r1 := map[string]any{"ok": float64(1), "version": "6.0.0"}
	r2 := map[string]any{"set": "rs0", "ok": float64(1)}
	r3 := map[string]any{"inprog": []any{}, "ok": float64(1)}

	r := &mockRunner{runResults: []map[string]any{r1, r2, r3}}
	a := NewWithRunner(r)

	ss, err := a.ServerStatus(context.Background())
	if err != nil {
		t.Fatalf("ServerStatus() error = %v", err)
	}
	if ss["version"] != "6.0.0" {
		t.Errorf("ServerStatus version = %v, want 6.0.0", ss["version"])
	}

	rs, err := a.ReplicaStatus(context.Background())
	if err != nil {
		t.Fatalf("ReplicaStatus() error = %v", err)
	}
	if rs["set"] != "rs0" {
		t.Errorf("ReplicaStatus set = %v, want rs0", rs["set"])
	}

	co, err := a.CurrentOp(context.Background())
	if err != nil {
		t.Fatalf("CurrentOp() error = %v", err)
	}
	if _, ok := co["inprog"]; !ok {
		t.Error("CurrentOp result missing inprog key")
	}
}
