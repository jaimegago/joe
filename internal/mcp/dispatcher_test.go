package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/mcp"
)

// mockJoecored creates a test HTTP server that handles all joecored API routes
// needed by the MCP dispatcher tests.
func mockJoecored(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/graph/query":
			json.NewEncoder(w).Encode(map[string]any{
				"nodes": []map[string]any{{"id": "service:payment-svc", "type": "service"}},
				"count": 1,
			})
		case r.URL.Path == "/api/v1/graph/related":
			json.NewEncoder(w).Encode(map[string]any{
				"nodes": []map[string]any{{"id": "db:payments-db", "type": "database"}},
				"edges": []map[string]any{},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/k8s/") && strings.HasSuffix(r.URL.Path, "/resources"):
			json.NewEncoder(w).Encode(map[string]any{
				"resources": []map[string]any{{"metadata": map[string]any{"name": "pod-1"}}},
				"count":     1,
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/k8s/") && strings.Contains(r.URL.Path, "/logs/"):
			json.NewEncoder(w).Encode(map[string]any{
				"logs": "2026-01-01 log line 1\n2026-01-01 log line 2",
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/prometheus/") && strings.HasSuffix(r.URL.Path, "/query"):
			json.NewEncoder(w).Encode(map[string]any{
				"result":    map[string]any{"type": "vector", "result": []any{}},
				"source_id": "prom-1",
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/loki/") && strings.HasSuffix(r.URL.Path, "/query"):
			json.NewEncoder(w).Encode(map[string]any{
				"result":    map[string]any{"streams": []any{}},
				"source_id": "loki-1",
			})
		case r.URL.Path == "/api/v1/knowledge/search":
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{"id": "entry-1", "content": "payment runbook"}},
				"count":   1,
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/alertmanager/") && strings.HasSuffix(r.URL.Path, "/alerts"):
			json.NewEncoder(w).Encode(map[string]any{
				"alerts": []map[string]any{{"fingerprint": "abc123", "status": map[string]any{"state": "firing"}}},
				"count":  1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func makeRequest(tool string, args map[string]any) mcpgo.CallToolRequest {
	return mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      tool,
			Arguments: args,
		},
	}
}

func TestDispatcher_GraphQuery(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleGraphQuery(context.Background(), makeRequest("joe_graph_query", map[string]any{
		"query": "payment-svc",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

func TestDispatcher_GraphQuery_MissingQuery(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleGraphQuery(context.Background(), makeRequest("joe_graph_query", map[string]any{}))
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
}

func TestDispatcher_GraphRelated(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleGraphRelated(context.Background(), makeRequest("joe_graph_related", map[string]any{
		"node_id": "service:payment-svc",
		"depth":   float64(1),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_K8sGet_List(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleK8sGet(context.Background(), makeRequest("joe_k8s_get", map[string]any{
		"source_id": "k8s-prod",
		"resource":  "pods",
		"namespace": "default",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_K8sLogs(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleK8sLogs(context.Background(), makeRequest("joe_k8s_logs", map[string]any{
		"source_id": "k8s-prod",
		"namespace": "default",
		"pod":       "payment-svc-abc123",
		"tail":      float64(50),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_MetricsQuery(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleMetricsQuery(context.Background(), makeRequest("joe_metrics_query", map[string]any{
		"source_id": "prom-1",
		"query":     "up",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_LogsSearch(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleLogsSearch(context.Background(), makeRequest("joe_logs_search", map[string]any{
		"source_id":     "loki-1",
		"query":         `{app="payment-svc"}`,
		"limit":         float64(50),
		"since_seconds": float64(1800),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_KnowledgeSearch(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleKnowledgeSearch(context.Background(), makeRequest("joe_knowledge_search", map[string]any{
		"query": "payment service errors",
		"top_k": float64(3),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_Incidents(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleIncidents(context.Background(), makeRequest("joe_incidents", map[string]any{
		"source_id": "alertmanager-prod",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_Incidents_WithFilter(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleIncidents(context.Background(), makeRequest("joe_incidents", map[string]any{
		"source_id": "alertmanager-prod",
		"filter":    "severity=critical",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_ErrorResult_OnBackendFailure(t *testing.T) {
	// Use an invalid server to force a connection error.
	c := client.New("http://127.0.0.1:1") // nothing listening here
	d := mcp.NewDispatcher(c)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := d.HandleGraphQuery(ctx, makeRequest("joe_graph_query", map[string]any{
		"query": "payment-svc",
	}))
	if err != nil {
		t.Fatalf("unexpected hard error (should return IsError result): %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend is unreachable")
	}
}
