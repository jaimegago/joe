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
		case r.URL.Path == "/api/v1/observe/k8s":
			json.NewEncoder(w).Encode(map[string]any{
				"source":       "kubernetes",
				"source_id":    "k8s-prod",
				"native_query": "service=payment-svc",
				"data":         map[string]any{"pods": []any{}, "count": 0},
			})
		case r.URL.Path == "/api/v1/observe/metrics":
			json.NewEncoder(w).Encode(map[string]any{
				"source":       "prometheus",
				"source_id":    "prom-prod",
				"native_query": `rate(http_requests_total{job="payment-svc"}[5m])`,
				"data":         []any{},
			})
		case r.URL.Path == "/api/v1/observe/logs":
			json.NewEncoder(w).Encode(map[string]any{
				"source":       "loki",
				"source_id":    "loki-prod",
				"native_query": `{app="payment-svc"} |= "error"`,
				"data":         []any{},
			})
		case r.URL.Path == "/api/v1/observe/traces":
			json.NewEncoder(w).Encode(map[string]any{
				"source":       "tempo",
				"source_id":    "tempo-prod",
				"native_query": "service=payment-svc",
				"data":         []any{},
			})
		case r.URL.Path == "/api/v1/observe/alerts":
			json.NewEncoder(w).Encode(map[string]any{
				"source":    "alertmanager",
				"source_id": "am-prod",
				"alerts":    []any{},
				"count":     0,
			})
		case r.URL.Path == "/api/v1/knowledge/search":
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{"id": "entry-1", "content": "payment runbook"}},
				"count":   1,
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

func TestDispatcher_K8s(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleK8s(context.Background(), makeRequest("joe_k8s", map[string]any{
		"service":  "payment-svc",
		"question": "show me the running pods",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_K8s_MissingService(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleK8s(context.Background(), makeRequest("joe_k8s", map[string]any{
		"question": "show pods",
	}))
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}

func TestDispatcher_K8s_MissingQuestion(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleK8s(context.Background(), makeRequest("joe_k8s", map[string]any{
		"service": "payment-svc",
	}))
	if err == nil {
		t.Fatal("expected error for missing question")
	}
}

func TestDispatcher_Metrics(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleMetrics(context.Background(), makeRequest("joe_metrics", map[string]any{
		"service":  "payment-svc",
		"question": "p99 latency over the last 10 minutes",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_Metrics_MissingService(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleMetrics(context.Background(), makeRequest("joe_metrics", map[string]any{
		"question": "p99 latency",
	}))
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}

func TestDispatcher_Metrics_ErrorResult(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
	d := mcp.NewDispatcher(c)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := d.HandleMetrics(ctx, makeRequest("joe_metrics", map[string]any{
		"service":  "payment-svc",
		"question": "p99 latency",
	}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend unreachable")
	}
}

func TestDispatcher_Logs(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleLogs(context.Background(), makeRequest("joe_logs", map[string]any{
		"service":  "payment-svc",
		"question": "errors in the last hour",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_Logs_MissingService(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleLogs(context.Background(), makeRequest("joe_logs", map[string]any{
		"question": "errors",
	}))
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}

func TestDispatcher_Logs_ErrorResult(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
	d := mcp.NewDispatcher(c)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := d.HandleLogs(ctx, makeRequest("joe_logs", map[string]any{
		"service":  "payment-svc",
		"question": "errors",
	}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend unreachable")
	}
}

func TestDispatcher_Traces(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleTraces(context.Background(), makeRequest("joe_traces", map[string]any{
		"service":  "payment-svc",
		"question": "slow requests in the last 30 minutes",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_Traces_MissingService(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleTraces(context.Background(), makeRequest("joe_traces", map[string]any{
		"question": "slow requests",
	}))
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}

func TestDispatcher_Alerts(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleAlerts(context.Background(), makeRequest("joe_alerts", map[string]any{
		"service": "payment-svc",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_Alerts_WithQuestion(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	result, err := d.HandleAlerts(context.Background(), makeRequest("joe_alerts", map[string]any{
		"service":  "payment-svc",
		"question": "severity=critical",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("got error result: %v", result.Content)
	}
}

func TestDispatcher_Alerts_MissingService(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleAlerts(context.Background(), makeRequest("joe_alerts", map[string]any{}))
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}

func TestDispatcher_Alerts_ErrorResult(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
	d := mcp.NewDispatcher(c)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := d.HandleAlerts(ctx, makeRequest("joe_alerts", map[string]any{
		"service": "payment-svc",
	}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend unreachable")
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

func TestDispatcher_KnowledgeSearch_MissingQuery(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)

	_, err := d.HandleKnowledgeSearch(context.Background(), makeRequest("joe_knowledge_search", map[string]any{}))
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestDispatcher_KnowledgeSearch_ErrorResult(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
	d := mcp.NewDispatcher(c)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := d.HandleKnowledgeSearch(ctx, makeRequest("joe_knowledge_search", map[string]any{
		"query": "payment errors",
	}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend unreachable")
	}
}

func TestDispatcher_ErrorResult_OnBackendFailure(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
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

func TestDispatcher_GraphRelated_MissingNodeID(t *testing.T) {
	srv := mockJoecored(t)
	c := client.New(srv.URL)
	d := mcp.NewDispatcher(c)
	_, err := d.HandleGraphRelated(context.Background(), makeRequest("joe_graph_related", map[string]any{}))
	if err == nil {
		t.Fatal("expected error for missing node_id")
	}
}

func TestDispatcher_GraphRelated_ErrorResult(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
	d := mcp.NewDispatcher(c)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	result, err := d.HandleGraphRelated(ctx, makeRequest("joe_graph_related", map[string]any{"node_id": "svc"}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend unreachable")
	}
}

func TestDispatcher_K8s_ErrorResult(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
	d := mcp.NewDispatcher(c)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	result, err := d.HandleK8s(ctx, makeRequest("joe_k8s", map[string]any{
		"service":  "payment-svc",
		"question": "show pods",
	}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend unreachable")
	}
}

func TestDispatcher_Traces_ErrorResult(t *testing.T) {
	c := client.New("http://127.0.0.1:1")
	d := mcp.NewDispatcher(c)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	result, err := d.HandleTraces(ctx, makeRequest("joe_traces", map[string]any{
		"service":  "payment-svc",
		"question": "slow requests",
	}))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when backend unreachable")
	}
}

// Verify the mock server routes contain the expected paths.
func TestMockServer_ObservePaths(t *testing.T) {
	srv := mockJoecored(t)
	paths := []string{
		"/api/v1/observe/metrics",
		"/api/v1/observe/logs",
		"/api/v1/observe/traces",
		"/api/v1/observe/alerts",
		"/api/v1/observe/k8s",
	}
	for _, path := range paths {
		resp, err := http.Post(srv.URL+path, "application/json",
			strings.NewReader(`{"service":"payment-svc","question":"test"}`))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("POST %s: expected 200, got %d", path, resp.StatusCode)
		}
	}
}
