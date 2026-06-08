package datadog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/datadog"
	"github.com/jaimegago/joe/internal/store"
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func httpResponse(code int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(code)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

type mockHTTPDoer struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

// --- ParseConfig ---

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"api_key": "abc", "app_key": "def"},
			wantErr: false,
		},
		{
			name:    "missing api_key",
			config:  map[string]any{"app_key": "def"},
			wantErr: true,
		},
		{
			name:    "missing app_key",
			config:  map[string]any{"api_key": "abc"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := datadog.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- Connect ---

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := datadog.New()
	source := store.Component{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_NetworkError(t *testing.T) {
	// Use a closed server to trigger a network error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	_ = srv // Closed, so connections will fail.

	adapter := datadog.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{
		"api_key": "key",
		"app_key": "app",
	})}
	// This will fail at the DNS / network level (datadoghq.com is unreachable in CI).
	// We simply assert no panic; the error itself may be network-related.
	_ = adapter.Connect(context.Background(), source)
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := datadog.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
	if st.Message == "" {
		t.Error("Status().Message should not be empty")
	}
}

func TestAdapter_NewWithClient_Connected(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if adapter.Status().Connected {
		t.Error("Status().Connected = true after Disconnect, want false")
	}
}

// --- MetricsQuery ---

func TestAdapter_MetricsQuery(t *testing.T) {
	respBody := `{
		"status": "ok",
		"series": [
			{
				"metric": "system.cpu.user",
				"expression": "system.cpu.user{*}",
				"scope": "host:web-1",
				"tag_set": ["service:api", "env:prod"],
				"pointlist": [[1700000000000.0, 42.5], [1700000060000.0, 43.1]]
			}
		]
	}`

	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.MetricsQuery(context.Background(), "system.cpu.user{*}", 1700000000, 1700003600)
	if err != nil {
		t.Fatalf("MetricsQuery() error = %v", err)
	}

	if len(result.Series) != 1 {
		t.Errorf("len(Series) = %d, want 1", len(result.Series))
	}
	if result.Series[0].Metric != "system.cpu.user" {
		t.Errorf("Metric = %q, want %q", result.Series[0].Metric, "system.cpu.user")
	}
	if len(result.Series[0].Points) != 2 {
		t.Errorf("len(Points) = %d, want 2", len(result.Series[0].Points))
	}
	if len(result.Series[0].Tags) == 0 || result.Series[0].Tags[0] != "service:api" {
		t.Errorf("Tags[0] = %v, want %q", result.Series[0].Tags, "service:api")
	}
}

func TestAdapter_MetricsQuery_NotConnected(t *testing.T) {
	adapter := datadog.New()
	_, err := adapter.MetricsQuery(context.Background(), "system.cpu.user{*}", 0, 0)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_MetricsQuery_ServerError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusInternalServerError, `{"errors":["server error"]}`),
	})
	_, err := adapter.MetricsQuery(context.Background(), "foo{*}", 0, 0)
	if err == nil {
		t.Error("expected error for server error response, got nil")
	}
}

func TestAdapter_MetricsQuery_DatadogErrorStatus(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"status":"error","series":[]}`),
	})
	_, err := adapter.MetricsQuery(context.Background(), "foo{*}", 0, 0)
	if err == nil {
		t.Error("expected error for datadog error status, got nil")
	}
}

func TestAdapter_MetricsQuery_EmptySeries(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"status":"ok","series":[]}`),
	})
	result, err := adapter.MetricsQuery(context.Background(), "foo{*}", 0, 0)
	if err != nil {
		t.Fatalf("MetricsQuery() error = %v", err)
	}
	if len(result.Series) != 0 {
		t.Errorf("len(Series) = %d, want 0", len(result.Series))
	}
}

// --- LogsSearch ---

func TestAdapter_LogsSearch(t *testing.T) {
	respBody := `{
		"data": [
			{
				"id": "log-1",
				"attributes": {
					"timestamp": "2024-01-01T00:00:00Z",
					"host": "web-1",
					"service": "api",
					"status": "error",
					"message": "connection refused",
					"attributes": {"env": "prod"}
				}
			}
		]
	}`

	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.LogsSearch(context.Background(), "service:api", 1700000000, 1700003600, 25)
	if err != nil {
		t.Fatalf("LogsSearch() error = %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Count = %d, want 1", result.Count)
	}
	if result.Logs[0].Service != "api" {
		t.Errorf("Service = %q, want %q", result.Logs[0].Service, "api")
	}
	if result.Logs[0].Status != "error" {
		t.Errorf("Status = %q, want %q", result.Logs[0].Status, "error")
	}
	if result.Logs[0].Attributes["env"] != "prod" {
		t.Errorf("Attributes[env] = %q, want %q", result.Logs[0].Attributes["env"], "prod")
	}
}

func TestAdapter_LogsSearch_NotConnected(t *testing.T) {
	adapter := datadog.New()
	_, err := adapter.LogsSearch(context.Background(), "*", 0, 0, 25)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_LogsSearch_ServerError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusBadRequest, `{"errors":["bad query"]}`),
	})
	_, err := adapter.LogsSearch(context.Background(), "bad", 0, 0, 25)
	if err == nil {
		t.Error("expected error for bad request, got nil")
	}
}

func TestAdapter_LogsSearch_DefaultLimit(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"data":[]}`),
	})
	result, err := adapter.LogsSearch(context.Background(), "*", 0, 0, 0)
	if err != nil {
		t.Fatalf("LogsSearch() error = %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}

// --- ListActiveServices ---

func TestAdapter_ListActiveServices(t *testing.T) {
	respBody := `{
		"host_list": [
			{
				"host_name": "web-1",
				"tags_by_source": {
					"Datadog": ["service:api", "env:prod"],
					"AWS": ["region:us-east-1"]
				}
			},
			{
				"host_name": "worker-1",
				"tags_by_source": {
					"Datadog": ["service:worker", "service:api", "env:prod"]
				}
			}
		],
		"total_matching": 2,
		"total_returned": 2
	}`

	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	services, err := adapter.ListActiveServices(context.Background())
	if err != nil {
		t.Fatalf("ListActiveServices() error = %v", err)
	}

	// "api" appears twice but should be deduplicated; "worker" once.
	if len(services) != 2 {
		t.Errorf("len(services) = %d, want 2, got %v", len(services), services)
	}
	found := make(map[string]bool)
	for _, s := range services {
		found[s] = true
	}
	if !found["api"] {
		t.Error("expected service 'api' in results")
	}
	if !found["worker"] {
		t.Error("expected service 'worker' in results")
	}
}

func TestAdapter_ListActiveServices_NoServiceTags(t *testing.T) {
	respBody := `{
		"host_list": [
			{
				"host_name": "web-1",
				"tags_by_source": {
					"AWS": ["region:us-east-1", "env:prod"]
				}
			}
		],
		"total_matching": 1,
		"total_returned": 1
	}`

	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	services, err := adapter.ListActiveServices(context.Background())
	if err != nil {
		t.Fatalf("ListActiveServices() error = %v", err)
	}
	if len(services) != 0 {
		t.Errorf("len(services) = %d, want 0", len(services))
	}
}

func TestAdapter_ListActiveServices_NotConnected(t *testing.T) {
	adapter := datadog.New()
	_, err := adapter.ListActiveServices(context.Background())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_ListActiveServices_ServerError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusUnauthorized, `{"errors":["forbidden"]}`),
	})
	_, err := adapter.ListActiveServices(context.Background())
	if err == nil {
		t.Error("expected error for server error response, got nil")
	}
}

func TestAdapter_ListActiveServices_EmptyHostList(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"host_list":[],"total_matching":0,"total_returned":0}`),
	})
	services, err := adapter.ListActiveServices(context.Background())
	if err != nil {
		t.Fatalf("ListActiveServices() error = %v", err)
	}
	if len(services) != 0 {
		t.Errorf("len(services) = %d, want 0", len(services))
	}
}

// --- ListLogServices ---

func TestAdapter_ListLogServices(t *testing.T) {
	respBody := `{
		"data": [
			{"attributes": {"service": "api", "message": "ok"}},
			{"attributes": {"service": "worker", "message": "ok"}},
			{"attributes": {"service": "api", "message": "err"}}
		]
	}`

	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	services, err := adapter.ListLogServices(context.Background())
	if err != nil {
		t.Fatalf("ListLogServices() error = %v", err)
	}

	// "api" duplicated → dedup; "worker" once.
	if len(services) != 2 {
		t.Errorf("len(services) = %d, want 2, got %v", len(services), services)
	}
	found := make(map[string]bool)
	for _, s := range services {
		found[s] = true
	}
	if !found["api"] {
		t.Error("expected service 'api' in results")
	}
	if !found["worker"] {
		t.Error("expected service 'worker' in results")
	}
}

func TestAdapter_ListLogServices_EmptyService(t *testing.T) {
	// Log entries with no service field should be skipped.
	respBody := `{
		"data": [
			{"attributes": {"service": "", "message": "no service"}},
			{"attributes": {"service": "payment", "message": "ok"}}
		]
	}`

	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	services, err := adapter.ListLogServices(context.Background())
	if err != nil {
		t.Fatalf("ListLogServices() error = %v", err)
	}
	if len(services) != 1 || services[0] != "payment" {
		t.Errorf("services = %v, want [payment]", services)
	}
}

func TestAdapter_ListLogServices_NotConnected(t *testing.T) {
	adapter := datadog.New()
	_, err := adapter.ListLogServices(context.Background())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_ListLogServices_ServerError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusBadRequest, `{"errors":["bad request"]}`),
	})
	_, err := adapter.ListLogServices(context.Background())
	if err == nil {
		t.Error("expected error for server error response, got nil")
	}
}

func TestAdapter_ListLogServices_NoData(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"data":[]}`),
	})
	services, err := adapter.ListLogServices(context.Background())
	if err != nil {
		t.Fatalf("ListLogServices() error = %v", err)
	}
	if len(services) != 0 {
		t.Errorf("len(services) = %d, want 0", len(services))
	}
}

// --- Connect: empty config (no JSON bytes) ---

func TestAdapter_Connect_EmptyConfig(t *testing.T) {
	// source.Config is empty → uses make(map[string]any), then ParseConfig fails (missing api_key).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	adapter := datadog.New()
	source := store.Component{} // empty Config bytes
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for empty config (missing api_key), got nil")
	}
}

// --- ParseConfig: explicit site value preserved ---

func TestParseConfig_ExplicitSite(t *testing.T) {
	cfg, err := datadog.ParseConfig(map[string]any{
		"api_key": "k",
		"app_key": "a",
		"site":    "datadoghq.eu",
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.Site != "datadoghq.eu" {
		t.Errorf("Site = %q, want %q", cfg.Site, "datadoghq.eu")
	}
}

// --- MetricsQuery: invalid JSON response ---

func TestAdapter_MetricsQuery_InvalidJSON(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `not-json`),
	})
	_, err := adapter.MetricsQuery(context.Background(), "foo{*}", 0, 0)
	if err == nil {
		t.Error("expected error for invalid JSON metrics response, got nil")
	}
}

// --- MetricsQuery: network/Do error ---

func TestAdapter_MetricsQuery_DoError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		err: fmt.Errorf("connection refused"),
	})
	_, err := adapter.MetricsQuery(context.Background(), "foo{*}", 0, 0)
	if err == nil {
		t.Error("expected error for Do() failure, got nil")
	}
}

// --- LogsSearch: invalid JSON response ---

func TestAdapter_LogsSearch_InvalidJSON(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `not-json`),
	})
	_, err := adapter.LogsSearch(context.Background(), "*", 0, 0, 10)
	if err == nil {
		t.Error("expected error for invalid JSON logs response, got nil")
	}
}

// --- LogsSearch: network/Do error ---

func TestAdapter_LogsSearch_DoError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		err: fmt.Errorf("connection refused"),
	})
	_, err := adapter.LogsSearch(context.Background(), "*", 0, 0, 10)
	if err == nil {
		t.Error("expected error for Do() failure, got nil")
	}
}

// --- ListActiveServices: invalid JSON response ---

func TestAdapter_ListActiveServices_InvalidJSON(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `not-json`),
	})
	_, err := adapter.ListActiveServices(context.Background())
	if err == nil {
		t.Error("expected error for invalid JSON hosts response, got nil")
	}
}

// --- ListActiveServices: network/Do error ---

func TestAdapter_ListActiveServices_DoError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		err: fmt.Errorf("connection refused"),
	})
	_, err := adapter.ListActiveServices(context.Background())
	if err == nil {
		t.Error("expected error for Do() failure, got nil")
	}
}

// --- ListActiveServices: "service:" tag with empty name (trimmed) ---

func TestAdapter_ListActiveServices_EmptyServiceName(t *testing.T) {
	// Tag "service:" → TrimPrefix leaves "", should be skipped.
	respBody := `{
		"host_list": [
			{
				"host_name": "web-1",
				"tags_by_source": {
					"Datadog": ["service:", "service:api"]
				}
			}
		]
	}`
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})
	services, err := adapter.ListActiveServices(context.Background())
	if err != nil {
		t.Fatalf("ListActiveServices() error = %v", err)
	}
	// "service:" (empty name) skipped; "service:api" kept → 1 result
	if len(services) != 1 || services[0] != "api" {
		t.Errorf("services = %v, want [api]", services)
	}
}

// --- ListLogServices: invalid JSON response ---

func TestAdapter_ListLogServices_InvalidJSON(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `not-json`),
	})
	_, err := adapter.ListLogServices(context.Background())
	if err == nil {
		t.Error("expected error for invalid JSON log services response, got nil")
	}
}

// --- ListLogServices: network/Do error ---

func TestAdapter_ListLogServices_DoError(t *testing.T) {
	adapter := datadog.NewWithClient(&mockHTTPDoer{
		err: fmt.Errorf("connection refused"),
	})
	_, err := adapter.ListLogServices(context.Background())
	if err == nil {
		t.Error("expected error for Do() failure, got nil")
	}
}
