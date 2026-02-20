package dynatrace_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
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
			config:  map[string]any{"url": "https://dt.example.com", "token": "tok"},
			wantErr: false,
		},
		{
			name:    "missing url",
			config:  map[string]any{"token": "tok"},
			wantErr: true,
		},
		{
			name:    "missing token",
			config:  map[string]any{"url": "https://dt.example.com"},
			wantErr: true,
		},
		{
			name:    "url trailing slash stripped",
			config:  map[string]any{"url": "https://dt.example.com/", "token": "tok"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dynatrace.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- Connect ---

func TestAdapter_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/metrics" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"metrics":[],"totalCount":0}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := dynatrace.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":   srv.URL,
		"token": "testtoken",
	})}

	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !adapter.Status().Connected {
		t.Error("Status().Connected = false after Connect, want true")
	}
}

func TestAdapter_Connect_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	adapter := dynatrace.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":   srv.URL,
		"token": "bad",
	})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := dynatrace.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	adapter := dynatrace.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":   srv.URL,
		"token": "tok",
	})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := dynatrace.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
}

func TestAdapter_NewWithClient_Connected(t *testing.T) {
	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
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
		"resolution": "1m",
		"result": [
			{
				"metricId": "builtin:host.cpu.usage",
				"data": [
					{
						"dimensionMap": {"dt.entity.host": "HOST-abc"},
						"timestamps": [1700000000000, 1700000060000],
						"values": [45.2, 46.1]
					}
				]
			}
		]
	}`

	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.MetricsQuery(context.Background(), "builtin:host.cpu.usage:avg", 1700000000000, 1700003600000)
	if err != nil {
		t.Fatalf("MetricsQuery() error = %v", err)
	}

	if result.Resolution != "1m" {
		t.Errorf("Resolution = %q, want %q", result.Resolution, "1m")
	}
	if len(result.Series) != 1 {
		t.Errorf("len(Series) = %d, want 1", len(result.Series))
	}
	if result.Series[0].MetricID != "builtin:host.cpu.usage" {
		t.Errorf("MetricID = %q, want %q", result.Series[0].MetricID, "builtin:host.cpu.usage")
	}
	if len(result.Series[0].Values) != 2 {
		t.Errorf("len(Values) = %d, want 2", len(result.Series[0].Values))
	}
	if result.Series[0].Values[0].Value != 45.2 {
		t.Errorf("Values[0].Value = %v, want 45.2", result.Series[0].Values[0].Value)
	}
}

func TestAdapter_MetricsQuery_NotConnected(t *testing.T) {
	adapter := dynatrace.New()
	_, err := adapter.MetricsQuery(context.Background(), "builtin:host.cpu.usage:avg", 0, 0)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_MetricsQuery_ServerError(t *testing.T) {
	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusForbidden, `{"error":{"code":403,"message":"Forbidden"}}`),
	})
	_, err := adapter.MetricsQuery(context.Background(), "builtin:host.cpu.usage:avg", 0, 0)
	if err == nil {
		t.Error("expected error for forbidden response, got nil")
	}
}

func TestAdapter_MetricsQuery_EmptyResult(t *testing.T) {
	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"resolution":"1m","result":[]}`),
	})
	result, err := adapter.MetricsQuery(context.Background(), "builtin:host.cpu.usage:avg", 0, 0)
	if err != nil {
		t.Fatalf("MetricsQuery() error = %v", err)
	}
	if len(result.Series) != 0 {
		t.Errorf("len(Series) = %d, want 0", len(result.Series))
	}
}

// --- Events ---

func TestAdapter_Events(t *testing.T) {
	respBody := `{
		"totalCount": 1,
		"events": [
			{
				"eventId": "evt-1",
				"eventType": "AVAILABILITY_EVENT",
				"title": "Service unavailable",
				"severity": "AVAILABILITY",
				"startTime": 1700000000000,
				"endTime": 1700001000000,
				"entityId": {
					"entityId": {"id": "SERVICE-abc"},
					"name": "payment-service"
				},
				"properties": [{"key": "env", "value": "prod"}]
			}
		]
	}`

	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.Events(context.Background(), 1700000000000, 1700003600000, 50)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Count = %d, want 1", result.Count)
	}
	if result.Events[0].EventID != "evt-1" {
		t.Errorf("EventID = %q, want %q", result.Events[0].EventID, "evt-1")
	}
	if result.Events[0].EntityName != "payment-service" {
		t.Errorf("EntityName = %q, want %q", result.Events[0].EntityName, "payment-service")
	}
	if result.Events[0].Properties["env"] != "prod" {
		t.Errorf("Properties[env] = %q, want %q", result.Events[0].Properties["env"], "prod")
	}
}

func TestAdapter_Events_NotConnected(t *testing.T) {
	adapter := dynatrace.New()
	_, err := adapter.Events(context.Background(), 0, 0, 50)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_Events_ServerError(t *testing.T) {
	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusInternalServerError, `{"error":{"code":500,"message":"internal error"}}`),
	})
	_, err := adapter.Events(context.Background(), 0, 0, 50)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_Events_DefaultLimit(t *testing.T) {
	adapter := dynatrace.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"totalCount":0,"events":[]}`),
	})
	result, err := adapter.Events(context.Background(), 0, 0, 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}
