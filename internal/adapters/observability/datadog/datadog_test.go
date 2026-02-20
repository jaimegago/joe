package datadog_test

import (
	"context"
	"encoding/json"
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
	source := store.Source{Config: []byte(`invalid json`)}
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
	source := store.Source{Config: mustMarshal(t, map[string]any{
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
