package loki_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters/observability/loki"
	"github.com/jaimegago/joe/internal/store"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantURL string
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"url": "http://loki:3100"},
			wantURL: "http://loki:3100",
		},
		{
			name:    "missing url",
			config:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loki.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", cfg.URL, tt.wantURL)
			}
		})
	}
}

func TestAdapter_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loki/api/v1/labels" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":["app","job","namespace"]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if !adapter.Status().Connected {
		t.Error("Status().Connected = false, want true")
	}
}

func TestAdapter_Connect_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_Query(t *testing.T) {
	queryResp := `{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [
				{
					"stream": {"app": "payment", "namespace": "prod"},
					"values": [
						["1609459200000000000", "payment service started"],
						["1609459260000000000", "processed 100 requests"]
					]
				}
			]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/query":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(queryResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	result, err := adapter.Query(context.Background(), `{app="payment"}`, 100, time.Hour)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if result.ResultType != "streams" {
		t.Errorf("ResultType = %q, want %q", result.ResultType, "streams")
	}
	if len(result.Entries) != 2 {
		t.Errorf("len(Entries) = %d, want 2", len(result.Entries))
	}
}

func TestAdapter_QueryRange(t *testing.T) {
	queryResp := `{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [
				{
					"stream": {"job": "api"},
					"values": [["1609459200000000000", "error: connection refused"]]
				}
			]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/query_range":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(queryResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	now := time.Now()
	result, err := adapter.QueryRange(context.Background(), `{job="api"} |= "error"`,
		now.Add(-1*time.Hour), now, 50)
	if err != nil {
		t.Fatalf("QueryRange() error = %v", err)
	}

	if len(result.Entries) != 1 {
		t.Errorf("len(Entries) = %d, want 1", len(result.Entries))
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	adapter := loki.New()
	_, err := adapter.Query(context.Background(), `{app="test"}`, 10, time.Hour)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestNewWithClient(t *testing.T) {
	adapter := loki.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := loki.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
	if st.Message == "" {
		t.Error("Status().Message should not be empty")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if adapter.Status().Connected {
		t.Error("Status().Connected = true after Disconnect, want false")
	}
	_, err := adapter.Query(context.Background(), `{app="test"}`, 10, time.Hour)
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := loki.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_Connect_WithHeaders(t *testing.T) {
	var gotAuth, gotOrgID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrgID = r.Header.Get("X-Scope-OrgID")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "my-key",
		"org_id":  "tenant1",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotAuth != "Bearer my-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-key")
	}
	if gotOrgID != "tenant1" {
		t.Errorf("X-Scope-OrgID = %q, want %q", gotOrgID, "tenant1")
	}
}

func TestAdapter_Query_DefaultLimit(t *testing.T) {
	queryResp := `{"status":"success","data":{"resultType":"streams","result":[]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/query":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(queryResp))
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	// limit=0 triggers default, since=0 skips start param
	result, err := adapter.Query(context.Background(), `{app="test"}`, 0, 0)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if result == nil {
		t.Error("Query() returned nil")
	}
}

func TestAdapter_QueryRange_DefaultLimit(t *testing.T) {
	queryResp := `{"status":"success","data":{"resultType":"streams","result":[]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/query_range":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(queryResp))
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	now := time.Now()
	result, err := adapter.QueryRange(context.Background(), `{app="test"}`, now.Add(-time.Hour), now, 0)
	if err != nil {
		t.Fatalf("QueryRange() error = %v", err)
	}
	if result == nil {
		t.Error("QueryRange() returned nil")
	}
}

func TestAdapter_QueryRange_NotConnected(t *testing.T) {
	adapter := loki.New()
	now := time.Now()
	_, err := adapter.QueryRange(context.Background(), `{app="test"}`, now.Add(-time.Hour), now, 10)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_Query_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.Query(context.Background(), `{app="test"}`, 10, time.Hour)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_Query_LokiErrorStatus(t *testing.T) {
	// Loki returns 200 but status != "success"
	queryResp := `{"status":"error","data":{"resultType":"","result":[]}}`
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(queryResp))
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.Query(context.Background(), `{app="test"}`, 10, time.Hour)
	if err == nil {
		t.Error("expected error for loki error status, got nil")
	}
}

func TestAdapter_ListServices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/label/app/values":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":["payment","orders","auth"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	services, err := adapter.ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if len(services) != 3 {
		t.Errorf("len(services) = %d, want 3", len(services))
	}
}

func TestAdapter_ListServices_NotConnected(t *testing.T) {
	adapter := loki.New()
	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_ListServices_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/label/app/values":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`error`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_ListServices_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/label/app/values":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not-json`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestAdapter_ListServices_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	// Close the server to cause a request error on subsequent calls.
	srv.Close()

	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error when server is closed, got nil")
	}
}

func TestAdapter_Query_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/loki/api/v1/query":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not-json`))
		}
	}))
	defer srv.Close()

	adapter := loki.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.Query(context.Background(), `{app="test"}`, 10, time.Hour)
	if err == nil {
		t.Error("expected error for invalid JSON response, got nil")
	}
}

func TestAdapter_Connect_EmptyConfig(t *testing.T) {
	adapter := loki.New()
	// Empty config bytes triggers the else branch (configMap = make(map[string]any))
	// then ParseConfig returns error because url is missing.
	source := store.Source{Config: nil}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for empty config, got nil")
	}
}

func TestAdapter_ParseConfig_WithOptionalFields(t *testing.T) {
	cfg, err := loki.ParseConfig(map[string]any{
		"url":     "http://loki:3100",
		"api_key": "mykey",
		"org_id":  "tenant1",
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.APIKey != "mykey" {
		t.Errorf("APIKey = %q, want mykey", cfg.APIKey)
	}
	if cfg.OrgID != "tenant1" {
		t.Errorf("OrgID = %q, want tenant1", cfg.OrgID)
	}
}

// mockHTTPDoer is a simple mock for the httpDoer interface.
type mockHTTPDoer struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
