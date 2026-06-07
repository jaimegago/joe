package httpreq_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/tools/shared/httpreq"
)

// mockHTTPClient returns a pre-built http.Response or error.
type mockHTTPClient struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

// okResponse builds a simple 200 OK response.
func okResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestHTTPRequestTool_Name(t *testing.T) {
	tool := httpreq.NewHTTPRequestTool()
	if tool.Name() != "http_request" {
		t.Errorf("Name() = %q, want http_request", tool.Name())
	}
}

func TestHTTPRequestTool_Description(t *testing.T) {
	tool := httpreq.NewHTTPRequestTool()
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestHTTPRequestTool_Parameters(t *testing.T) {
	tool := httpreq.NewHTTPRequestTool()
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["url"]; !ok {
		t.Error("Parameters() missing 'url'")
	}
	if _, ok := params.Properties["method"]; !ok {
		t.Error("Parameters() missing 'method'")
	}
}

func TestHTTPRequestTool_Execute_Success(t *testing.T) {
	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{resp: okResponse(`{"status":"ok"}`)},
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://example.com/health",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(httpreq.HTTPRequestResult)
	if r.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", r.StatusCode)
	}
	if r.URL != "http://example.com/health" {
		t.Errorf("URL = %q, want http://example.com/health", r.URL)
	}
	if r.Method != "GET" {
		t.Errorf("Method = %q, want GET", r.Method)
	}
}

func TestHTTPRequestTool_Execute_CustomMethod(t *testing.T) {
	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{resp: okResponse("")},
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"url":    "http://example.com/api",
		"method": "head", // read-only method, lower-cased to exercise normalization
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(httpreq.HTTPRequestResult)
	if r.Method != "HEAD" {
		t.Errorf("Method = %q, want HEAD (uppercased)", r.Method)
	}
}

// TestHTTPRequestTool_Execute_RejectsMutatingMethod locks in the read-only
// floor: http_request is a T1 (observe) tool, so mutating HTTP verbs must be
// rejected before any request is made. Without this, a T1 tool could mutate
// external systems via POST/PUT/DELETE — a hole in the write floor
// (D-0018/D-0019).
func TestHTTPRequestTool_Execute_RejectsMutatingMethod(t *testing.T) {
	for _, method := range []string{"POST", "put", "PATCH", "delete"} {
		t.Run(method, func(t *testing.T) {
			tool := &httpreq.HTTPRequestTool{
				Client: &mockHTTPClient{resp: okResponse("")},
			}
			_, err := tool.Execute(context.Background(), map[string]any{
				"url":    "http://example.com/api",
				"method": method,
			})
			if err == nil {
				t.Fatalf("Execute(method=%q) = nil error, want rejection (read-only tool)", method)
			}
		})
	}
}

func TestHTTPRequestTool_Execute_ClientError(t *testing.T) {
	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{err: context.DeadlineExceeded},
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://dead.example.com/",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(httpreq.HTTPRequestResult)
	if r.Error == "" {
		t.Error("expected Error in result for client error")
	}
	if r.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0 for error case", r.StatusCode)
	}
}

func TestHTTPRequestTool_Execute_MissingURL(t *testing.T) {
	tool := httpreq.NewHTTPRequestTool()
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing url, got nil")
	}
}

func TestHTTPRequestTool_Execute_BlockedMetadata(t *testing.T) {
	tool := httpreq.NewHTTPRequestTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://169.254.169.254/latest/meta-data/",
	})
	if err == nil {
		t.Error("expected error for metadata endpoint, got nil")
	}
}

func TestHTTPRequestTool_Execute_BlockedGCPMetadata(t *testing.T) {
	tool := httpreq.NewHTTPRequestTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://metadata.google.internal/computeMetadata/v1/",
	})
	if err == nil {
		t.Error("expected error for GCP metadata endpoint, got nil")
	}
}

func TestHTTPRequestTool_Execute_BodyTruncation(t *testing.T) {
	largeBody := strings.Repeat("x", 5000)
	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{resp: okResponse(largeBody)},
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://example.com/large",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(httpreq.HTTPRequestResult)
	if !r.BodyTrunc {
		t.Error("BodyTrunc = false, want true for body > 4096 bytes")
	}
	if len(r.Body) > 4096 {
		t.Errorf("len(Body) = %d, want <= 4096", len(r.Body))
	}
}

func TestHTTPRequestTool_Execute_Headers(t *testing.T) {
	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{resp: okResponse("")},
	}

	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://example.com/api",
		"headers": map[string]any{
			"Authorization": "Bearer token123",
			"Accept":        "application/json",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPRequestTool_Execute_ResponseHeaders(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"abc123"},
		},
		Body: io.NopCloser(strings.NewReader("")),
	}

	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{resp: resp},
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://example.com/",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(httpreq.HTTPRequestResult)
	if r.Headers["Content-Type"] != "application/json" {
		t.Errorf("Headers[Content-Type] = %q, want application/json", r.Headers["Content-Type"])
	}
}

func TestHTTPRequestTool_Execute_WithBody(t *testing.T) {
	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{resp: okResponse(`{"created":true}`)},
	}
	result, err := tool.Execute(context.Background(), map[string]any{
		"url":    "http://example.com/api",
		"method": "GET",
		"body":   `{"key":"value"}`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(httpreq.HTTPRequestResult)
	if r.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", r.StatusCode)
	}
}

func TestHTTPRequestTool_Execute_CustomTimeout(t *testing.T) {
	tool := &httpreq.HTTPRequestTool{
		Client: &mockHTTPClient{resp: okResponse("")},
	}
	_, err := tool.Execute(context.Background(), map[string]any{
		"url":        "http://example.com/",
		"timeout_ms": float64(500),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPRequestTool_Execute_BlockedMetadataInternal(t *testing.T) {
	tool := httpreq.NewHTTPRequestTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://metadata.internal/latest/",
	})
	if err == nil {
		t.Error("expected error for metadata.internal endpoint, got nil")
	}
}

func TestHTTPRequestTool_NewHTTPRequestTool(t *testing.T) {
	// Verify NewHTTPRequestTool returns a usable tool.
	tool := httpreq.NewHTTPRequestTool()
	if tool == nil {
		t.Fatal("NewHTTPRequestTool() returned nil")
	}
	if tool.Name() != "http_request" {
		t.Errorf("Name() = %q, want http_request", tool.Name())
	}
	// Verify Client is set.
	if tool.Client == nil {
		t.Error("Client should not be nil")
	}
}

func TestHTTPRequestTool_Execute_Non200(t *testing.T) {
	resp := &http.Response{
		StatusCode: 503,
		Status:     "503 Service Unavailable",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("service unavailable")),
	}
	tool := &httpreq.HTTPRequestTool{Client: &mockHTTPClient{resp: resp}}

	result, err := tool.Execute(context.Background(), map[string]any{
		"url": "http://example.com/",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(httpreq.HTTPRequestResult)
	if r.StatusCode != 503 {
		t.Errorf("StatusCode = %d, want 503", r.StatusCode)
	}
}
