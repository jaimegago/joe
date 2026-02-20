package jaeger_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/jaeger"
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
			config:  map[string]any{"url": "http://jaeger:16686"},
			wantURL: "http://jaeger:16686",
		},
		{
			name:    "missing url",
			config:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := jaeger.ParseConfig(tt.config)
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
		if r.URL.Path == "/api/services" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment","order"]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := jaeger.New()
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
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_ListServices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/services":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment","order","inventory"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
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

func TestAdapter_SearchTraces(t *testing.T) {
	tracesResp := `{
		"data": [
			{
				"traceID": "trace001",
				"spans": [
					{
						"operationName": "POST /checkout",
						"startTime": 1609459200000000,
						"duration": 150000
					}
				],
				"processes": {
					"p1": {"serviceName": "payment"}
				}
			}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/services":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment"]}`))
		case "/api/traces":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(tracesResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	results, err := adapter.SearchTraces(context.Background(), "payment", "", 10)
	if err != nil {
		t.Fatalf("SearchTraces() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
	if results[0].TraceID != "trace001" {
		t.Errorf("TraceID = %q, want %q", results[0].TraceID, "trace001")
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	adapter := jaeger.New()
	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestNewWithClient(t *testing.T) {
	adapter := jaeger.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
		},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := jaeger.New()
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
		_, _ = w.Write([]byte(`{"data":["svc"]}`))
	}))
	defer srv.Close()

	adapter := jaeger.New()
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
	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := jaeger.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // Close immediately so connection fails

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_Connect_WithAPIKey(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":["payment"]}`))
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "my-secret-key",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if receivedAuth != "Bearer my-secret-key" {
		t.Errorf("Authorization = %q, want %q", receivedAuth, "Bearer my-secret-key")
	}
}

func TestAdapter_ListServices_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["svc"]}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_SearchTraces_WithOperation_And_DefaultLimit(t *testing.T) {
	var gotOperation string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/services":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment"]}`))
		case "/api/traces":
			gotOperation = r.URL.Query().Get("operation")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	results, err := adapter.SearchTraces(context.Background(), "payment", "POST /checkout", 0) // limit=0 → uses default
	if err != nil {
		t.Fatalf("SearchTraces() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	if gotOperation != "POST /checkout" {
		t.Errorf("operation = %q, want %q", gotOperation, "POST /checkout")
	}
}

func TestAdapter_SearchTraces_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/services":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment"]}`))
		case "/api/traces":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`error`))
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.SearchTraces(context.Background(), "payment", "", 5)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_GetTrace_Success(t *testing.T) {
	traceJSON := `{"data":[{"traceID":"trace001"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/services":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment"]}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(traceJSON))
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	result, err := adapter.GetTrace(context.Background(), "trace001")
	if err != nil {
		t.Fatalf("GetTrace() error = %v", err)
	}
	if result == nil {
		t.Error("GetTrace() returned nil")
	}
}

func TestAdapter_GetTrace_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/services":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.GetTrace(context.Background(), "notexist")
	if err == nil {
		t.Error("expected error for 404 trace, got nil")
	}
}

func TestAdapter_GetTrace_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/services":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":["payment"]}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal"}`))
		}
	}))
	defer srv.Close()

	adapter := jaeger.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.GetTrace(context.Background(), "trace001")
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_GetTrace_NotConnected(t *testing.T) {
	adapter := jaeger.New()
	_, err := adapter.GetTrace(context.Background(), "trace001")
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_SearchTraces_NotConnected(t *testing.T) {
	adapter := jaeger.New()
	_, err := adapter.SearchTraces(context.Background(), "svc", "", 10)
	if err == nil {
		t.Error("expected error when not connected, got nil")
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
