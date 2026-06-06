// Package netcheck provides Go-native TCP connectivity checking tools.
// No external CLI dependencies — uses net.DialTimeout from the standard library.
package netcheck

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/llm"
)

// Dialer creates network connections. Abstracted for testing.
type Dialer interface {
	DialTimeout(network, address string, timeout time.Duration) (net.Conn, error)
}

// defaultDialer wraps net.DialTimeout.
type defaultDialer struct{}

func (d *defaultDialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, address, timeout)
}

// TCPConnectResult is the structured result of a single TCP connectivity check.
type TCPConnectResult struct {
	Host      string  `json:"host"`
	Port      int     `json:"port"`
	Reachable bool    `json:"reachable"`
	LatencyMS float64 `json:"latency_ms,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// TCPConnectTool checks whether a host:port is reachable via TCP.
// Replaces nc/telnet for quick connectivity checks.
type TCPConnectTool struct {
	Dial Dialer
}

// NewTCPConnectTool creates a TCPConnectTool using the real net.DialTimeout dialer.
func NewTCPConnectTool() *TCPConnectTool {
	return &TCPConnectTool{Dial: &defaultDialer{}}
}

func (t *TCPConnectTool) Name() string { return "tcp_connect" }

func (t *TCPConnectTool) Description() string {
	return "Check if a host:port is reachable via TCP. Returns reachable status and round-trip latency in milliseconds. Useful for diagnosing connectivity issues — replaces nc/telnet. Use from joe to check from your machine, or from joecored to check from the cluster."
}

func (t *TCPConnectTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"host":       {Type: "string", Description: "Hostname or IP address to connect to."},
			"port":       {Type: "integer", Description: "TCP port number to connect to."},
			"timeout_ms": {Type: "integer", Description: "Connection timeout in milliseconds. Default: 5000."},
		},
		Required: []string{"host", "port"},
	}
}

func (t *TCPConnectTool) Execute(_ context.Context, args map[string]any) (any, error) {
	host, ok := args["host"].(string)
	if !ok || host == "" {
		return nil, fmt.Errorf("missing required parameter: host")
	}

	portF, ok := args["port"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing required parameter: port")
	}
	port := int(portF)

	timeout := 5000.0
	if t, ok := args["timeout_ms"].(float64); ok && t > 0 {
		timeout = t
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	start := time.Now()
	conn, err := t.Dial.DialTimeout("tcp", addr, time.Duration(timeout)*time.Millisecond)
	latency := float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		return TCPConnectResult{
			Host:      host,
			Port:      port,
			Reachable: false,
			Error:     err.Error(),
		}, nil
	}
	conn.Close()
	return TCPConnectResult{
		Host:      host,
		Port:      port,
		Reachable: true,
		LatencyMS: latency,
	}, nil
}

// PortResult is the result for a single port in a port scan.
type PortResult struct {
	Port      int     `json:"port"`
	Open      bool    `json:"open"`
	LatencyMS float64 `json:"latency_ms,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// PortScanResult is the structured result of scanning multiple ports.
type PortScanResult struct {
	Host  string       `json:"host"`
	Ports []PortResult `json:"ports"`
	Open  int          `json:"open"`
}

// PortScanTool checks multiple ports on a host concurrently.
type PortScanTool struct {
	Dial Dialer
}

// NewPortScanTool creates a PortScanTool using the real net.DialTimeout dialer.
func NewPortScanTool() *PortScanTool {
	return &PortScanTool{Dial: &defaultDialer{}}
}

func (t *PortScanTool) Name() string { return "port_scan" }

func (t *PortScanTool) Description() string {
	return "Check which of the specified ports are open on a host. Probes all ports concurrently and returns open/closed status with latency for each. Useful for service discovery and firewall auditing."
}

func (t *PortScanTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"host":       {Type: "string", Description: "Hostname or IP address to scan."},
			"ports":      {Type: "array", Description: "Array of integer port numbers to check. Example: [80, 443, 8080].", Items: &llm.Property{Type: "integer", Description: "TCP port number."}},
			"timeout_ms": {Type: "integer", Description: "Connection timeout per port in milliseconds. Default: 5000."},
		},
		Required: []string{"host", "ports"},
	}
}

func (t *PortScanTool) Execute(_ context.Context, args map[string]any) (any, error) {
	host, ok := args["host"].(string)
	if !ok || host == "" {
		return nil, fmt.Errorf("missing required parameter: host")
	}

	portsRaw, ok := args["ports"].([]any)
	if !ok || len(portsRaw) == 0 {
		return nil, fmt.Errorf("missing required parameter: ports (must be a non-empty array)")
	}

	timeout := 5000.0
	if tm, ok := args["timeout_ms"].(float64); ok && tm > 0 {
		timeout = tm
	}

	// Each goroutine writes to its own disjoint index in results, so no
	// channel or lock is needed to collect them — only a WaitGroup to know
	// when every probe has finished.
	results := make([]PortResult, len(portsRaw))
	var wg sync.WaitGroup

	for i, p := range portsRaw {
		portF, ok := p.(float64)
		if !ok {
			results[i] = PortResult{Port: 0, Error: "invalid port value"}
			continue
		}
		port := int(portF)
		wg.Add(1)
		go func(idx, port int) {
			defer wg.Done()
			addr := fmt.Sprintf("%s:%d", host, port)
			start := time.Now()
			conn, err := t.Dial.DialTimeout("tcp", addr, time.Duration(timeout)*time.Millisecond)
			latency := float64(time.Since(start).Microseconds()) / 1000.0
			r := PortResult{Port: port}
			if err != nil {
				r.Open = false
				r.Error = err.Error()
			} else {
				conn.Close()
				r.Open = true
				r.LatencyMS = latency
			}
			results[idx] = r
		}(i, port)
	}

	wg.Wait()

	open := 0
	for _, r := range results {
		if r.Open {
			open++
		}
	}

	return PortScanResult{
		Host:  host,
		Ports: results,
		Open:  open,
	}, nil
}
