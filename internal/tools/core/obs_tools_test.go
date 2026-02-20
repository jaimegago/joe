package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	"github.com/jaimegago/joe/internal/tools/core"
)

// --- Prometheus tool tests ---

type mockPrometheusClient struct {
	queryResult  *prometheusadapter.QueryResult
	queryErr     error
	targets      []prometheusadapter.Target
	targetsErr   error
}

func (m *mockPrometheusClient) PrometheusQuery(_ context.Context, _, _ string, _ time.Time) (*prometheusadapter.QueryResult, error) {
	return m.queryResult, m.queryErr
}

func (m *mockPrometheusClient) PrometheusQueryRange(_ context.Context, _, _ string, _, _ time.Time, _ time.Duration) (*prometheusadapter.QueryResult, error) {
	return m.queryResult, m.queryErr
}

func (m *mockPrometheusClient) PrometheusTargets(_ context.Context, _ string) ([]prometheusadapter.Target, error) {
	return m.targets, m.targetsErr
}

func TestPrometheusQueryTool_Name(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{})
	if tool.Name() != "prometheus_query" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "prometheus_query")
	}
}

func TestPrometheusQueryTool_Execute_Query(t *testing.T) {
	expected := &prometheusadapter.QueryResult{
		ResultType: "vector",
		Vector: []prometheusadapter.Sample{
			{Metric: map[string]string{"job": "payment"}, Timestamp: 1234567890, Value: "1"},
		},
	}

	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{queryResult: expected})

	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"query":     "up",
		"action":    "query",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not map[string]any")
	}
	if m["source_id"] != "prom-1" {
		t.Errorf("source_id = %v, want %q", m["source_id"], "prom-1")
	}
}

func TestPrometheusQueryTool_Execute_Targets(t *testing.T) {
	targets := []prometheusadapter.Target{
		{Labels: map[string]string{"job": "payment"}, State: "active"},
	}
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{targets: targets})

	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"action":    "targets",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

func TestPrometheusQueryTool_Execute_MissingSourceID(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"query": "up"})
	if err == nil {
		t.Error("expected error for missing source_id, got nil")
	}
}

func TestPrometheusQueryTool_Execute_MissingQuery(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"source_id": "prom-1"})
	if err == nil {
		t.Error("expected error for missing query, got nil")
	}
}

// --- Loki tool tests ---

type mockLokiClient struct {
	result    *lokiadapter.QueryResult
	resultErr error
}

func (m *mockLokiClient) LokiQuery(_ context.Context, _, _ string, _ int, _ time.Duration) (*lokiadapter.QueryResult, error) {
	return m.result, m.resultErr
}

func (m *mockLokiClient) LokiQueryRange(_ context.Context, _, _ string, _, _ time.Time, _ int) (*lokiadapter.QueryResult, error) {
	return m.result, m.resultErr
}

func TestLokiQueryTool_Name(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{})
	if tool.Name() != "loki_query" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "loki_query")
	}
}

func TestLokiQueryTool_Execute(t *testing.T) {
	expected := &lokiadapter.QueryResult{
		ResultType: "streams",
		Entries: []lokiadapter.LogEntry{
			{Timestamp: "1609459200000000000", Line: "error: timeout", Labels: map[string]string{"app": "payment"}},
		},
	}

	tool := core.NewLokiQueryTool(&mockLokiClient{result: expected})

	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "loki-1",
		"query":     `{app="payment"} |= "error"`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["source_id"] != "loki-1" {
		t.Errorf("source_id = %v, want %q", m["source_id"], "loki-1")
	}
}

func TestLokiQueryTool_Execute_MissingQuery(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"source_id": "loki-1"})
	if err == nil {
		t.Error("expected error for missing query, got nil")
	}
}

// --- Tempo tool tests ---

type mockTempoClient struct {
	traces    []tempoadapter.TraceSearchResult
	tracesErr error
	trace     *tempoadapter.Trace
	traceErr  error
}

func (m *mockTempoClient) TempoSearch(_ context.Context, _, _, _ string, _, _, _ int) ([]tempoadapter.TraceSearchResult, error) {
	return m.traces, m.tracesErr
}

func (m *mockTempoClient) TempoGetTrace(_ context.Context, _, _ string) (*tempoadapter.Trace, error) {
	return m.trace, m.traceErr
}

func TestTempoSearchTool_Name(t *testing.T) {
	tool := core.NewTempoSearchTool(&mockTempoClient{})
	if tool.Name() != "tempo_search" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "tempo_search")
	}
}

func TestTempoSearchTool_Execute_Search(t *testing.T) {
	traces := []tempoadapter.TraceSearchResult{
		{TraceID: "abc123", RootServiceName: "payment", DurationMs: 152.5},
	}
	tool := core.NewTempoSearchTool(&mockTempoClient{traces: traces})

	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "tempo-1",
		"service":   "payment",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

func TestTempoSearchTool_Execute_Get(t *testing.T) {
	trace := &tempoadapter.Trace{TraceID: "abc123"}
	tool := core.NewTempoSearchTool(&mockTempoClient{trace: trace})

	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "tempo-1",
		"action":    "get",
		"trace_id":  "abc123",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["source_id"] != "tempo-1" {
		t.Errorf("source_id = %v, want %q", m["source_id"], "tempo-1")
	}
}

// --- Jaeger tool tests ---

type mockJaegerClient struct {
	services    []string
	servicesErr error
	traces      []jaegeradapter.TraceSearchResult
	tracesErr   error
}

func (m *mockJaegerClient) JaegerServices(_ context.Context, _ string) ([]string, error) {
	return m.services, m.servicesErr
}

func (m *mockJaegerClient) JaegerTraces(_ context.Context, _, _, _ string, _ int) ([]jaegeradapter.TraceSearchResult, error) {
	return m.traces, m.tracesErr
}

func TestJaegerTracesTool_Name(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{})
	if tool.Name() != "jaeger_traces" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "jaeger_traces")
	}
}

func TestJaegerTracesTool_Execute_Services(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{
		services: []string{"payment", "order", "inventory"},
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "jaeger-1",
		"action":    "services",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 3 {
		t.Errorf("count = %v, want 3", m["count"])
	}
}

func TestJaegerTracesTool_Execute_Traces(t *testing.T) {
	traces := []jaegeradapter.TraceSearchResult{
		{TraceID: "trace001", Service: "payment", Operation: "POST /checkout"},
	}
	tool := core.NewJaegerTracesTool(&mockJaegerClient{traces: traces})

	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "jaeger-1",
		"service":   "payment",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

func TestJaegerTracesTool_Execute_MissingService(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "jaeger-1",
		"action":    "traces",
	})
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

// --- Additional Prometheus tests ---

func TestPrometheusQueryTool_Description(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestPrometheusQueryTool_Parameters(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["source_id"]; !ok {
		t.Error("Parameters() missing source_id")
	}
	if _, ok := params.Properties["query"]; !ok {
		t.Error("Parameters() missing query")
	}
}

func TestPrometheusQueryTool_Execute_QueryRange(t *testing.T) {
	expected := &prometheusadapter.QueryResult{ResultType: "matrix"}
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{queryResult: expected})
	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"query":     "rate(http_requests_total[5m])",
		"action":    "query_range",
		"start":     float64(1700000000),
		"end":       float64(1700003600),
		"step":      float64(60),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["source_id"] != "prom-1" {
		t.Errorf("source_id = %v, want prom-1", m["source_id"])
	}
}

func TestPrometheusQueryTool_Execute_QueryRangeDefaults(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{queryResult: &prometheusadapter.QueryResult{}})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"query":     "up",
		"action":    "query_range",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestPrometheusQueryTool_Execute_QueryRangeMissingQuery(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"action":    "query_range",
	})
	if err == nil {
		t.Error("expected error for missing query in query_range, got nil")
	}
}

func TestPrometheusQueryTool_Execute_QueryError(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{queryErr: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"query":     "up",
	})
	if err == nil {
		t.Error("expected error from query, got nil")
	}
}

func TestPrometheusQueryTool_Execute_TargetsError(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{targetsErr: errors.New("server down")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"action":    "targets",
	})
	if err == nil {
		t.Error("expected error from targets, got nil")
	}
}

func TestPrometheusQueryTool_Execute_QueryRangeError(t *testing.T) {
	tool := core.NewPrometheusQueryTool(&mockPrometheusClient{queryErr: errors.New("timeout")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "prom-1",
		"query":     "up",
		"action":    "query_range",
	})
	if err == nil {
		t.Error("expected error from query_range, got nil")
	}
}

// --- Additional Loki tests ---

func TestLokiQueryTool_Description(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestLokiQueryTool_Parameters(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["source_id"]; !ok {
		t.Error("Parameters() missing source_id")
	}
}

func TestLokiQueryTool_Execute_MissingSourceID(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"query": `{app="payment"}`})
	if err == nil {
		t.Error("expected error for missing source_id, got nil")
	}
}

func TestLokiQueryTool_Execute_QueryRange(t *testing.T) {
	expected := &lokiadapter.QueryResult{ResultType: "streams"}
	tool := core.NewLokiQueryTool(&mockLokiClient{result: expected})
	result, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "loki-1",
		"query":     `{app="payment"}`,
		"action":    "query_range",
		"start":     float64(1700000000),
		"end":       float64(1700003600),
		"limit":     float64(50),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["source_id"] != "loki-1" {
		t.Errorf("source_id = %v, want loki-1", m["source_id"])
	}
}

func TestLokiQueryTool_Execute_QueryRangeDefaults(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{result: &lokiadapter.QueryResult{}})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "loki-1",
		"query":     `{app="payment"}`,
		"action":    "query_range",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestLokiQueryTool_Execute_QueryError(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{resultErr: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "loki-1",
		"query":     `{app="payment"}`,
	})
	if err == nil {
		t.Error("expected error from query, got nil")
	}
}

func TestLokiQueryTool_Execute_QueryRangeError(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{resultErr: errors.New("timeout")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "loki-1",
		"query":     `{app="payment"}`,
		"action":    "query_range",
	})
	if err == nil {
		t.Error("expected error from query_range, got nil")
	}
}

func TestLokiQueryTool_Execute_CustomLimit(t *testing.T) {
	tool := core.NewLokiQueryTool(&mockLokiClient{result: &lokiadapter.QueryResult{}})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "loki-1",
		"query":     `{app="payment"}`,
		"limit":     float64(200),
		"since":     float64(7200),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

// --- Additional Tempo tests ---

func TestTempoSearchTool_Description(t *testing.T) {
	tool := core.NewTempoSearchTool(&mockTempoClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestTempoSearchTool_Parameters(t *testing.T) {
	tool := core.NewTempoSearchTool(&mockTempoClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["source_id"]; !ok {
		t.Error("Parameters() missing source_id")
	}
}

func TestTempoSearchTool_Execute_MissingSourceID(t *testing.T) {
	tool := core.NewTempoSearchTool(&mockTempoClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"service": "payment"})
	if err == nil {
		t.Error("expected error for missing source_id, got nil")
	}
}

func TestTempoSearchTool_Execute_SearchError(t *testing.T) {
	tool := core.NewTempoSearchTool(&mockTempoClient{tracesErr: errors.New("timeout")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "tempo-1",
		"service":   "payment",
	})
	if err == nil {
		t.Error("expected error from search, got nil")
	}
}

func TestTempoSearchTool_Execute_SearchWithOptions(t *testing.T) {
	traces := []tempoadapter.TraceSearchResult{{TraceID: "abc"}}
	tool := core.NewTempoSearchTool(&mockTempoClient{traces: traces})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id":    "tempo-1",
		"service":      "payment",
		"tags":         "http.status_code=500",
		"min_duration": float64(100),
		"max_duration": float64(5000),
		"limit":        float64(10),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestTempoSearchTool_Execute_GetMissingTraceID(t *testing.T) {
	tool := core.NewTempoSearchTool(&mockTempoClient{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "tempo-1",
		"action":    "get",
	})
	if err == nil {
		t.Error("expected error for missing trace_id, got nil")
	}
}

func TestTempoSearchTool_Execute_GetError(t *testing.T) {
	tool := core.NewTempoSearchTool(&mockTempoClient{traceErr: errors.New("not found")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "tempo-1",
		"action":    "get",
		"trace_id":  "abc123",
	})
	if err == nil {
		t.Error("expected error from get, got nil")
	}
}

// --- Additional Jaeger tests ---

func TestJaegerTracesTool_Description(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestJaegerTracesTool_Parameters(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["source_id"]; !ok {
		t.Error("Parameters() missing source_id")
	}
}

func TestJaegerTracesTool_Execute_MissingSourceID(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"service": "payment"})
	if err == nil {
		t.Error("expected error for missing source_id, got nil")
	}
}

func TestJaegerTracesTool_Execute_ServicesError(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{servicesErr: errors.New("timeout")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "jaeger-1",
		"action":    "services",
	})
	if err == nil {
		t.Error("expected error from services, got nil")
	}
}

func TestJaegerTracesTool_Execute_TracesError(t *testing.T) {
	tool := core.NewJaegerTracesTool(&mockJaegerClient{tracesErr: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "jaeger-1",
		"service":   "payment",
	})
	if err == nil {
		t.Error("expected error from traces, got nil")
	}
}

func TestJaegerTracesTool_Execute_TracesWithOptions(t *testing.T) {
	traces := []jaegeradapter.TraceSearchResult{{TraceID: "t1", Service: "payment"}}
	tool := core.NewJaegerTracesTool(&mockJaegerClient{traces: traces})
	_, err := tool.Execute(context.Background(), map[string]any{
		"source_id": "jaeger-1",
		"service":   "payment",
		"operation": "POST /checkout",
		"limit":     float64(5),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}
