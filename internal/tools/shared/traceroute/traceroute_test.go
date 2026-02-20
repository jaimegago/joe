package traceroute_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/tools/shared/traceroute"
)

// mockHopProber simulates per-hop responses without real ICMP sockets.
type mockHopProber struct {
	// hops maps TTL → (respondIP, latencyMS, err)
	hops map[int]hopResponse
}

type hopResponse struct {
	ip        string
	latencyMS float64
	err       error
}

func (m *mockHopProber) ProbeHop(_ context.Context, _ string, ttl int, _ time.Duration) (string, float64, error) {
	if r, ok := m.hops[ttl]; ok {
		return r.ip, r.latencyMS, r.err
	}
	// Default: timeout (empty IP, no error).
	return "", 0, nil
}

func TestTraceRouteTool_Name(t *testing.T) {
	tool := traceroute.NewTraceRouteTool()
	if tool.Name() != "trace_route" {
		t.Errorf("Name() = %q, want trace_route", tool.Name())
	}
}

func TestTraceRouteTool_Description(t *testing.T) {
	tool := traceroute.NewTraceRouteTool()
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestTraceRouteTool_Parameters(t *testing.T) {
	tool := traceroute.NewTraceRouteTool()
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["host"]; !ok {
		t.Error("Parameters() missing 'host'")
	}
	if len(params.Required) == 0 {
		t.Error("Parameters() should have required fields")
	}
}

func TestTraceRouteTool_Execute_MissingHost(t *testing.T) {
	tool := traceroute.NewTraceRouteTool()
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

func TestTraceRouteTool_Execute_ReachedDestination(t *testing.T) {
	// Simulate 3 hops: TTL 1 and 2 are intermediate, TTL 3 reaches dst "8.8.8.8".
	mock := &mockHopProber{
		hops: map[int]hopResponse{
			1: {ip: "10.0.0.1", latencyMS: 1.0},
			2: {ip: "192.168.1.1", latencyMS: 5.0},
			3: {ip: "8.8.8.8", latencyMS: 10.0},
		},
	}

	tool := &traceroute.TraceRouteTool{Prober: mock}
	result, err := tool.Execute(context.Background(), map[string]any{
		"host": "8.8.8.8",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(traceroute.TraceResult)
	if !r.Reached {
		t.Error("Reached = false, want true")
	}
	if r.TotalHops != 3 {
		t.Errorf("TotalHops = %d, want 3", r.TotalHops)
	}
	if len(r.Hops) != 3 {
		t.Errorf("len(Hops) = %d, want 3", len(r.Hops))
	}
}

func TestTraceRouteTool_Execute_Timeout(t *testing.T) {
	// All hops timeout (empty IP returned).
	mock := &mockHopProber{hops: map[int]hopResponse{}}

	tool := &traceroute.TraceRouteTool{Prober: mock}
	result, err := tool.Execute(context.Background(), map[string]any{
		"host":     "unreachable.example.com",
		"max_hops": float64(5),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(traceroute.TraceResult)
	if r.Reached {
		t.Error("Reached = true, want false for timeout")
	}
	if len(r.Hops) != 5 {
		t.Errorf("len(Hops) = %d, want 5 (max_hops)", len(r.Hops))
	}
	for _, h := range r.Hops {
		if h.Error == "" {
			t.Errorf("Hop %d: Error should not be empty for timeout", h.Hop)
		}
	}
}

func TestTraceRouteTool_Execute_PrivilegeError(t *testing.T) {
	// Simulate ICMP permission denied on all hops.
	mock := &mockHopProber{
		hops: map[int]hopResponse{
			1: {err: errors.New("insufficient privileges for ICMP traceroute (requires root or CAP_NET_RAW)")},
			2: {err: errors.New("insufficient privileges for ICMP traceroute (requires root or CAP_NET_RAW)")},
			3: {err: errors.New("insufficient privileges for ICMP traceroute (requires root or CAP_NET_RAW)")},
		},
	}

	tool := &traceroute.TraceRouteTool{Prober: mock}
	result, err := tool.Execute(context.Background(), map[string]any{
		"host":     "example.com",
		"max_hops": float64(3),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(traceroute.TraceResult)
	if r.Reached {
		t.Error("Reached should be false when all hops error")
	}
	// Each hop should carry the privilege error message.
	for _, h := range r.Hops {
		if h.Error == "" {
			t.Errorf("Hop %d: expected privilege error message, got empty", h.Hop)
		}
	}
}

func TestTraceRouteTool_Execute_MaxHops(t *testing.T) {
	// No hop ever reaches the destination.
	mock := &mockHopProber{
		hops: map[int]hopResponse{
			1: {ip: "10.0.0.1", latencyMS: 1.0},
			2: {ip: "10.0.0.2", latencyMS: 2.0},
			3: {ip: "10.0.0.3", latencyMS: 3.0},
		},
	}

	tool := &traceroute.TraceRouteTool{Prober: mock}
	result, err := tool.Execute(context.Background(), map[string]any{
		"host":     "192.0.2.1", // TEST-NET — never matches
		"max_hops": float64(3),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(traceroute.TraceResult)
	if r.Reached {
		t.Error("Reached = true, want false — destination never matched")
	}
	if len(r.Hops) != 3 {
		t.Errorf("len(Hops) = %d, want 3 (max_hops)", len(r.Hops))
	}
}

func TestTraceRouteTool_Execute_CustomOptions(t *testing.T) {
	mock := &mockHopProber{
		hops: map[int]hopResponse{
			1: {ip: "1.1.1.1", latencyMS: 1.5},
		},
	}

	tool := &traceroute.TraceRouteTool{Prober: mock}
	_, err := tool.Execute(context.Background(), map[string]any{
		"host":       "1.1.1.1",
		"max_hops":   float64(10),
		"timeout_ms": float64(3000),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

// TestTraceRouteTool_RealProber_Smoke uses the real icmpProber to confirm the tool
// handles the privilege-denied path gracefully. On most systems without root,
// net.ListenPacket("ip4:icmp") fails with "operation not permitted". The tool
// must return a TraceResult (not an error) regardless.
func TestTraceRouteTool_RealProber_Smoke(t *testing.T) {
	tool := traceroute.NewTraceRouteTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"host":       "127.0.0.1",
		"max_hops":   float64(1),
		"timeout_ms": float64(500),
	})
	// Execute must never return a hard error — failures are in HopResult.Error.
	if err != nil {
		t.Fatalf("Execute() should not return error even when ICMP fails, got: %v", err)
	}
	if _, ok := result.(traceroute.TraceResult); !ok {
		t.Fatalf("result type = %T, want TraceResult", result)
	}
}

func TestTraceRouteTool_Execute_HopFields(t *testing.T) {
	mock := &mockHopProber{
		hops: map[int]hopResponse{
			1: {ip: "8.8.8.8", latencyMS: 12.5},
		},
	}

	tool := &traceroute.TraceRouteTool{Prober: mock}
	result, err := tool.Execute(context.Background(), map[string]any{
		"host":     "8.8.8.8",
		"max_hops": float64(3),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(traceroute.TraceResult)
	if len(r.Hops) == 0 {
		t.Fatal("no hops returned")
	}
	firstHop := r.Hops[0]
	if firstHop.Hop != 1 {
		t.Errorf("first Hop.Hop = %d, want 1", firstHop.Hop)
	}
	if firstHop.IP != "8.8.8.8" {
		t.Errorf("first Hop.IP = %q, want 8.8.8.8", firstHop.IP)
	}
	if firstHop.LatencyMS != 12.5 {
		t.Errorf("first Hop.LatencyMS = %f, want 12.5", firstHop.LatencyMS)
	}
}
