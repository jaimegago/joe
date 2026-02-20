package tempo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/tempo"
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
			config:  map[string]any{"url": "http://tempo:3200"},
			wantURL: "http://tempo:3200",
		},
		{
			name:    "missing url",
			config:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := tempo.ParseConfig(tt.config)
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
		if r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := tempo.New()
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
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_Search(t *testing.T) {
	searchResp := `{
		"traces": [
			{
				"traceID": "abc123",
				"rootServiceName": "payment",
				"rootTraceName": "POST /checkout",
				"startTimeUnixNano": "1609459200000000000",
				"durationMs": 152.5,
				"spanCount": 8
			}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		case "/api/search":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	results, err := adapter.Search(context.Background(), "payment", "", 0, 0, 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
	if results[0].TraceID != "abc123" {
		t.Errorf("TraceID = %q, want %q", results[0].TraceID, "abc123")
	}
	if results[0].RootServiceName != "payment" {
		t.Errorf("RootServiceName = %q, want %q", results[0].RootServiceName, "payment")
	}
}

func TestAdapter_GetTrace(t *testing.T) {
	traceResp := `{"batches": [{"resource": {}, "scope_spans": []}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		case "/api/traces/abc123":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(traceResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	trace, err := adapter.GetTrace(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetTrace() error = %v", err)
	}

	if trace.TraceID != "abc123" {
		t.Errorf("TraceID = %q, want %q", trace.TraceID, "abc123")
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	adapter := tempo.New()
	_, err := adapter.Search(context.Background(), "payment", "", 0, 0, 10)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestNewWithClient(t *testing.T) {
	adapter := tempo.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := tempo.New()
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
		_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
	}))
	defer srv.Close()

	adapter := tempo.New()
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
	_, err := adapter.Search(context.Background(), "payment", "", 0, 0, 10)
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := tempo.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	adapter := tempo.New()
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
		_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "secret",
		"org_id":  "tenant1",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret")
	}
	if gotOrgID != "tenant1" {
		t.Errorf("X-Scope-OrgID = %q, want %q", gotOrgID, "tenant1")
	}
}

func TestAdapter_Search_TagsOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		case "/api/search":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"traces":[]}`))
		}
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	results, err := adapter.Search(context.Background(), "", "http.status_code=500", 0, 0, 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestAdapter_Search_ServiceAndTags_With_Duration(t *testing.T) {
	var gotTags, gotMin, gotMax string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		case "/api/search":
			gotTags = r.URL.Query().Get("tags")
			gotMin = r.URL.Query().Get("minDuration")
			gotMax = r.URL.Query().Get("maxDuration")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"traces":[]}`))
		}
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.Search(context.Background(), "payment", "http.method=POST", 100, 5000, 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if gotTags != "service.name=payment http.method=POST" {
		t.Errorf("tags = %q", gotTags)
	}
	if gotMin != "100ms" {
		t.Errorf("minDuration = %q, want 100ms", gotMin)
	}
	if gotMax != "5000ms" {
		t.Errorf("maxDuration = %q, want 5000ms", gotMax)
	}
}

func TestAdapter_Search_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		case "/api/search":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`error`))
		}
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.Search(context.Background(), "payment", "", 0, 0, 10)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_GetTrace_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := tempo.New()
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
		case "/api/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal"}`))
		}
	}))
	defer srv.Close()

	adapter := tempo.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.GetTrace(context.Background(), "trace001")
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_GetTrace_NotConnected(t *testing.T) {
	adapter := tempo.New()
	_, err := adapter.GetTrace(context.Background(), "trace001")
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
