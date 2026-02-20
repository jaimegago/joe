package netcheck_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/tools/shared/netcheck"
)

// mockDialer simulates network connections without actually dialing.
type mockDialer struct {
	conn net.Conn
	err  error
}

func (m *mockDialer) DialTimeout(_, _ string, _ time.Duration) (net.Conn, error) {
	return m.conn, m.err
}

// mockConn is a no-op net.Conn for tests.
type mockConn struct{}

func (m *mockConn) Read(_ []byte) (int, error)         { return 0, nil }
func (m *mockConn) Write(_ []byte) (int, error)        { return 0, nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

// --- TCPConnectTool tests ---

func TestTCPConnectTool_Name(t *testing.T) {
	tool := netcheck.NewTCPConnectTool()
	if tool.Name() != "tcp_connect" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "tcp_connect")
	}
}

func TestTCPConnectTool_Description(t *testing.T) {
	tool := netcheck.NewTCPConnectTool()
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestTCPConnectTool_Parameters(t *testing.T) {
	tool := netcheck.NewTCPConnectTool()
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["host"]; !ok {
		t.Error("Parameters() missing 'host'")
	}
	if _, ok := params.Properties["port"]; !ok {
		t.Error("Parameters() missing 'port'")
	}
}

func TestTCPConnectTool_Execute_Reachable(t *testing.T) {
	tool := &netcheck.TCPConnectTool{Dial: &mockDialer{conn: &mockConn{}}}

	result, err := tool.Execute(context.Background(), map[string]any{
		"host": "example.com",
		"port": float64(80),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r, ok := result.(netcheck.TCPConnectResult)
	if !ok {
		t.Fatalf("result type = %T, want TCPConnectResult", result)
	}
	if !r.Reachable {
		t.Error("Reachable = false, want true")
	}
	if r.Host != "example.com" {
		t.Errorf("Host = %q, want example.com", r.Host)
	}
	if r.Port != 80 {
		t.Errorf("Port = %d, want 80", r.Port)
	}
}

func TestTCPConnectTool_Execute_Unreachable(t *testing.T) {
	tool := &netcheck.TCPConnectTool{Dial: &mockDialer{err: errors.New("connection refused")}}

	result, err := tool.Execute(context.Background(), map[string]any{
		"host": "dead.example.com",
		"port": float64(9999),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(netcheck.TCPConnectResult)
	if r.Reachable {
		t.Error("Reachable = true, want false")
	}
	if r.Error == "" {
		t.Error("Error should not be empty for unreachable host")
	}
}

func TestTCPConnectTool_Execute_MissingHost(t *testing.T) {
	tool := &netcheck.TCPConnectTool{Dial: &mockDialer{}}
	_, err := tool.Execute(context.Background(), map[string]any{"port": float64(80)})
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

func TestTCPConnectTool_Execute_MissingPort(t *testing.T) {
	tool := &netcheck.TCPConnectTool{Dial: &mockDialer{}}
	_, err := tool.Execute(context.Background(), map[string]any{"host": "example.com"})
	if err == nil {
		t.Error("expected error for missing port, got nil")
	}
}

func TestTCPConnectTool_Execute_CustomTimeout(t *testing.T) {
	tool := &netcheck.TCPConnectTool{Dial: &mockDialer{conn: &mockConn{}}}
	_, err := tool.Execute(context.Background(), map[string]any{
		"host":       "example.com",
		"port":       float64(443),
		"timeout_ms": float64(1000),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

// --- PortScanTool tests ---

func TestPortScanTool_Name(t *testing.T) {
	tool := netcheck.NewPortScanTool()
	if tool.Name() != "port_scan" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "port_scan")
	}
}

func TestPortScanTool_Description(t *testing.T) {
	tool := netcheck.NewPortScanTool()
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestPortScanTool_Parameters(t *testing.T) {
	tool := netcheck.NewPortScanTool()
	params := tool.Parameters()
	if _, ok := params.Properties["host"]; !ok {
		t.Error("Parameters() missing 'host'")
	}
	if _, ok := params.Properties["ports"]; !ok {
		t.Error("Parameters() missing 'ports'")
	}
}

func TestPortScanTool_Execute_AllOpen(t *testing.T) {
	tool := &netcheck.PortScanTool{Dial: &mockDialer{conn: &mockConn{}}}

	result, err := tool.Execute(context.Background(), map[string]any{
		"host":  "example.com",
		"ports": []any{float64(80), float64(443), float64(8080)},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(netcheck.PortScanResult)
	if r.Open != 3 {
		t.Errorf("Open = %d, want 3", r.Open)
	}
	if len(r.Ports) != 3 {
		t.Errorf("len(Ports) = %d, want 3", len(r.Ports))
	}
}

func TestPortScanTool_Execute_AllClosed(t *testing.T) {
	tool := &netcheck.PortScanTool{Dial: &mockDialer{err: errors.New("connection refused")}}

	result, err := tool.Execute(context.Background(), map[string]any{
		"host":  "example.com",
		"ports": []any{float64(80), float64(443)},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(netcheck.PortScanResult)
	if r.Open != 0 {
		t.Errorf("Open = %d, want 0", r.Open)
	}
}

func TestPortScanTool_Execute_MissingHost(t *testing.T) {
	tool := &netcheck.PortScanTool{Dial: &mockDialer{}}
	_, err := tool.Execute(context.Background(), map[string]any{"ports": []any{float64(80)}})
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

func TestPortScanTool_Execute_MissingPorts(t *testing.T) {
	tool := &netcheck.PortScanTool{Dial: &mockDialer{}}
	_, err := tool.Execute(context.Background(), map[string]any{"host": "example.com"})
	if err == nil {
		t.Error("expected error for missing ports, got nil")
	}
}

func TestPortScanTool_Execute_EmptyPorts(t *testing.T) {
	tool := &netcheck.PortScanTool{Dial: &mockDialer{}}
	_, err := tool.Execute(context.Background(), map[string]any{
		"host":  "example.com",
		"ports": []any{},
	})
	if err == nil {
		t.Error("expected error for empty ports array, got nil")
	}
}
