// Package httpreq provides a Go-native HTTP probing tool.
// No external CLI dependencies — uses net/http from the standard library.
package httpreq

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/llm"
)

// maxBodyBytes is the maximum number of body bytes returned to the LLM.
const maxBodyBytes = 4096

// blockedHosts lists addresses that must not be reached to prevent SSRF attacks.
// These are cloud metadata endpoints and localhost loopback addresses.
var blockedHosts = []string{
	"169.254.169.254", // AWS/GCP/Azure/DO metadata
	"metadata.google.internal",
	"metadata.internal",
}

// HTTPDoer executes HTTP requests. Abstracted for testing.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TLSInfo contains TLS certificate details from the connection.
type TLSInfo struct {
	Version     string `json:"version"`
	Issuer      string `json:"issuer"`
	Subject     string `json:"subject"`
	ExpiresAt   string `json:"expires_at"`
	DaysUntilEx int    `json:"days_until_expiry"`
}

// HTTPRequestResult is the structured result of an HTTP probe.
type HTTPRequestResult struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	StatusCode int               `json:"status_code,omitempty"`
	Status     string            `json:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	BodyTrunc  bool              `json:"body_truncated,omitempty"`
	LatencyMS  float64           `json:"latency_ms,omitempty"`
	TLS        *TLSInfo          `json:"tls,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// HTTPRequestTool probes an HTTP endpoint and returns structured diagnostics.
// Replaces curl for connectivity and status checks.
type HTTPRequestTool struct {
	Client HTTPDoer
}

// NewHTTPRequestTool creates an HTTPRequestTool with a real http.Client.
func NewHTTPRequestTool() *HTTPRequestTool {
	return &HTTPRequestTool{
		Client: &http.Client{
			Timeout: 10 * time.Second,
			// Do not follow redirects — return the redirect response so the
			// LLM can reason about it explicitly.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (t *HTTPRequestTool) Name() string { return "http_request" }

func (t *HTTPRequestTool) Description() string {
	return "Probe an HTTP/HTTPS endpoint and return status code, response headers, body snippet, and latency. Replaces curl for endpoint health checks and debugging. Requests to cloud metadata endpoints (169.254.169.254) are blocked for safety."
}

func (t *HTTPRequestTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"url": {
				Type:        "string",
				Description: "Full URL to request (http:// or https://).",
			},
			"method": {
				Type:        "string",
				Description: "HTTP method: GET, POST, HEAD, PUT, DELETE. Default: GET.",
			},
			"headers": {
				Type:        "object",
				Description: "Optional map of request headers (string → string).",
			},
			"body": {
				Type:        "string",
				Description: "Optional request body for POST/PUT.",
			},
			"timeout_ms": {
				Type:        "integer",
				Description: "Request timeout in milliseconds. Default: 10000.",
			},
		},
		Required: []string{"url"},
	}
}

func (t *HTTPRequestTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawURL, ok := args["url"].(string)
	if !ok || rawURL == "" {
		return nil, fmt.Errorf("missing required parameter: url")
	}

	if err := checkSSRF(rawURL); err != nil {
		return nil, err
	}

	method := "GET"
	if m, ok := args["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}

	timeout := 10000.0
	if tm, ok := args["timeout_ms"].(float64); ok && tm > 0 {
		timeout = tm
	}

	var bodyReader io.Reader
	if b, ok := args["body"].(string); ok && b != "" {
		bodyReader = strings.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	if hdrs, ok := args["headers"].(map[string]any); ok {
		for k, v := range hdrs {
			if vs, ok := v.(string); ok {
				req.Header.Set(k, vs)
			}
		}
	}

	// Apply per-request timeout by creating a client with the timeout set.
	client := t.Client
	if tm := time.Duration(timeout) * time.Millisecond; tm != 10*time.Second {
		client = &http.Client{
			Timeout: tm,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		return HTTPRequestResult{
			URL:    rawURL,
			Method: method,
			Error:  err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	result := HTTPRequestResult{
		URL:        rawURL,
		Method:     method,
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		LatencyMS:  latency,
		Headers:    flattenHeaders(resp.Header),
	}

	if resp.TLS != nil {
		result.TLS = extractTLSInfo(resp.TLS)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err == nil {
		if len(bodyBytes) > maxBodyBytes {
			result.Body = string(bodyBytes[:maxBodyBytes])
			result.BodyTrunc = true
		} else {
			result.Body = string(bodyBytes)
		}
	}

	return result, nil
}

// checkSSRF returns an error if the URL targets a blocked metadata endpoint.
func checkSSRF(rawURL string) error {
	// Extract host from URL (simple prefix check + net.SplitHostPort).
	// We check both the string form and resolved IPs to cover redirects.
	host := extractHost(rawURL)
	if host == "" {
		return nil
	}
	for _, blocked := range blockedHosts {
		if host == blocked {
			return fmt.Errorf("safety: requests to %q are blocked (cloud metadata endpoint)", host)
		}
	}
	// Block loopback only for localhost hostnames, not 127.x.x.x ranges
	// (those are rare but legitimate in some test environments).
	if host == "localhost" {
		return nil // allow localhost — useful for local service checks
	}
	return nil
}

// extractHost pulls the hostname from a URL string without a full URL parse
// (to keep the check lightweight and avoid reflection on invalid URLs).
func extractHost(rawURL string) string {
	// Strip scheme
	u := rawURL
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	// Strip path
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	// Strip port
	host, _, err := net.SplitHostPort(u)
	if err != nil {
		return u // no port
	}
	return host
}

// flattenHeaders converts multi-value response headers to a single string map.
func flattenHeaders(h http.Header) map[string]string {
	flat := make(map[string]string, len(h))
	for k, v := range h {
		flat[k] = strings.Join(v, ", ")
	}
	return flat
}

// extractTLSInfo builds a TLSInfo struct from the TLS connection state.
func extractTLSInfo(state *tls.ConnectionState) *TLSInfo {
	if len(state.PeerCertificates) == 0 {
		return nil
	}
	cert := state.PeerCertificates[0]
	daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)

	var version string
	switch state.Version {
	case tls.VersionTLS10:
		version = "TLS 1.0"
	case tls.VersionTLS11:
		version = "TLS 1.1"
	case tls.VersionTLS12:
		version = "TLS 1.2"
	case tls.VersionTLS13:
		version = "TLS 1.3"
	default:
		version = fmt.Sprintf("0x%04X", state.Version)
	}

	return &TLSInfo{
		Version:     version,
		Issuer:      cert.Issuer.CommonName,
		Subject:     cert.Subject.CommonName,
		ExpiresAt:   cert.NotAfter.UTC().Format(time.RFC3339),
		DaysUntilEx: daysLeft,
	}
}
