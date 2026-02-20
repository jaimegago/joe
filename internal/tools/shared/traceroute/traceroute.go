// Package traceroute provides a Go-native network path tracing tool.
// Replaces the traceroute/tracepath CLI commands with structured JSON output.
//
// Implementation note: ICMP-based hop identification requires either elevated
// privileges (root/sudo) or CAP_NET_RAW on Linux. When privileges are
// insufficient, each hop reports the limitation in its error field. The final
// hop connectivity is always attempted regardless of ICMP availability.
package traceroute

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jaimegago/joe/internal/llm"
)

// HopResult holds the result of probing a single TTL hop.
type HopResult struct {
	Hop       int     `json:"hop"`
	IP        string  `json:"ip,omitempty"`
	Hostname  string  `json:"hostname,omitempty"`
	LatencyMS float64 `json:"latency_ms,omitempty"`
	Reached   bool    `json:"reached,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// TraceResult is the full structured result of a trace_route invocation.
type TraceResult struct {
	Destination string      `json:"destination"`
	MaxHops     int         `json:"max_hops"`
	Hops        []HopResult `json:"hops"`
	Reached     bool        `json:"reached"`
	TotalHops   int         `json:"total_hops"`
}

// HopProber probes a single hop in the network path. Abstracted for testing.
type HopProber interface {
	// ProbeHop sends a probe with the given TTL toward dst and returns
	// the IP address that responded, the round-trip latency, and any error.
	// An empty respondIP with err==nil means the hop timed out (no response).
	ProbeHop(ctx context.Context, dst string, ttl int, timeout time.Duration) (respondIP string, latencyMS float64, err error)
}

// TraceRouteTool traces the network path to a destination.
// Replaces traceroute/tracepath with structured output.
type TraceRouteTool struct {
	Prober HopProber
}

// NewTraceRouteTool creates a TraceRouteTool using the real ICMP prober.
func NewTraceRouteTool() *TraceRouteTool {
	return &TraceRouteTool{Prober: &icmpProber{}}
}

func (t *TraceRouteTool) Name() string { return "trace_route" }

func (t *TraceRouteTool) Description() string {
	return "Trace the network path to a host, showing each hop with IP, reverse-DNS hostname, and latency. Replaces traceroute/tracepath. Requires elevated privileges (root or CAP_NET_RAW) for ICMP-based hop identification; intermediate hops show errors when privileges are absent but the final-hop check still runs."
}

func (t *TraceRouteTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"host": {
				Type:        "string",
				Description: "Hostname or IP address to trace toward.",
			},
			"max_hops": {
				Type:        "integer",
				Description: "Maximum number of hops before giving up. Default: 30.",
			},
			"timeout_ms": {
				Type:        "integer",
				Description: "Timeout per hop probe in milliseconds. Default: 5000.",
			},
		},
		Required: []string{"host"},
	}
}

func (t *TraceRouteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	host, ok := args["host"].(string)
	if !ok || host == "" {
		return nil, fmt.Errorf("missing required parameter: host")
	}

	maxHops := 30
	if mh, ok := args["max_hops"].(float64); ok && mh > 0 {
		maxHops = int(mh)
	}

	timeout := 5000.0
	if tm, ok := args["timeout_ms"].(float64); ok && tm > 0 {
		timeout = tm
	}
	timeoutDur := time.Duration(timeout) * time.Millisecond

	result := TraceResult{
		Destination: host,
		MaxHops:     maxHops,
		Hops:        make([]HopResult, 0, maxHops),
	}

	// Resolve the destination IP once for comparison.
	dstIP := resolveHost(host)

	for ttl := 1; ttl <= maxHops; ttl++ {
		hop := HopResult{Hop: ttl}

		respondIP, latency, err := t.Prober.ProbeHop(ctx, host, ttl, timeoutDur)
		if err != nil {
			hop.Error = err.Error()
			result.Hops = append(result.Hops, hop)
			// If context is done, stop early.
			if ctx.Err() != nil {
				break
			}
			continue
		}

		if respondIP == "" {
			// Timeout — no response from this hop.
			hop.Error = "timeout (no response)"
			result.Hops = append(result.Hops, hop)
			continue
		}

		hop.IP = respondIP
		hop.LatencyMS = latency

		// Attempt reverse DNS lookup (best-effort, don't fail on error).
		if names, lookupErr := net.LookupAddr(respondIP); lookupErr == nil && len(names) > 0 {
			hop.Hostname = names[0]
		}

		// Check if we've reached the destination.
		if respondIP == dstIP || respondIP == host {
			hop.Reached = true
			result.Hops = append(result.Hops, hop)
			result.Reached = true
			result.TotalHops = ttl
			break
		}

		result.Hops = append(result.Hops, hop)
	}

	if result.TotalHops == 0 {
		result.TotalHops = len(result.Hops)
	}

	return result, nil
}

// resolveHost resolves a hostname to its first IP address string.
// Returns the input unchanged if resolution fails (it may already be an IP).
func resolveHost(host string) string {
	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return host
	}
	return addrs[0]
}
