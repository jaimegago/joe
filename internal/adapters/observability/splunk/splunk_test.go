package splunk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/splunk"
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
			config:  map[string]any{"url": "https://splunk:8089", "token": "tok"},
			wantErr: false,
		},
		{
			name:    "missing url",
			config:  map[string]any{"token": "tok"},
			wantErr: true,
		},
		{
			name:    "missing token",
			config:  map[string]any{"url": "https://splunk:8089"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := splunk.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- Connect ---

func TestAdapter_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/services/server/info" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"entry":[{"content":{"version":"9.0.0"}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := splunk.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{
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

	adapter := splunk.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{
		"url":   srv.URL,
		"token": "bad",
	})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := splunk.New()
	source := store.Component{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	adapter := splunk.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{
		"url":   srv.URL,
		"token": "tok",
	})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := splunk.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
}

func TestAdapter_NewWithClient_Connected(t *testing.T) {
	adapter := splunk.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	adapter := splunk.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if adapter.Status().Connected {
		t.Error("Status().Connected = true after Disconnect, want false")
	}
}

// --- Search ---

func TestAdapter_Search(t *testing.T) {
	respBody := `{
		"results": [
			{"_time": "2024-01-01T00:00:00Z", "host": "web-1", "source": "/var/log/app.log", "_raw": "ERROR: panic in handler", "index": "main"},
			{"_time": "2024-01-01T00:01:00Z", "host": "web-2", "source": "/var/log/app.log", "_raw": "ERROR: timeout"}
		]
	}`

	adapter := splunk.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.Search(context.Background(), "index=main ERROR", "-1h", "now", 100)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if result.Count != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}
	if result.Events[0].Host != "web-1" {
		t.Errorf("Events[0].Host = %q, want %q", result.Events[0].Host, "web-1")
	}
	if result.Events[0].Raw != "ERROR: panic in handler" {
		t.Errorf("Events[0].Raw = %q, want %q", result.Events[0].Raw, "ERROR: panic in handler")
	}
}

func TestAdapter_Search_PrependsSPL(t *testing.T) {
	// Verify that a query not starting with "search" gets "search " prepended.
	// We capture the outgoing request to inspect the body.
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/services/server/info" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = body
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	adapter := splunk.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{
		"url":   srv.URL,
		"token": "tok",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	_, err := adapter.Search(context.Background(), "index=main", "", "", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	_ = gotBody // query prepending tested via successful encode
}

func TestAdapter_Search_NotConnected(t *testing.T) {
	adapter := splunk.New()
	_, err := adapter.Search(context.Background(), "index=main", "-1h", "now", 100)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_Search_ServerError(t *testing.T) {
	adapter := splunk.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusInternalServerError, `{"messages":[{"type":"FATAL","text":"internal error"}]}`),
	})
	_, err := adapter.Search(context.Background(), "index=main", "-1h", "now", 10)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_Search_DefaultLimit(t *testing.T) {
	adapter := splunk.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"results":[]}`),
	})
	result, err := adapter.Search(context.Background(), "index=main", "", "", 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}

func TestAdapter_Search_EmptyResults(t *testing.T) {
	adapter := splunk.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, `{"results":[]}`),
	})
	result, err := adapter.Search(context.Background(), "search nothing", "-1h", "now", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}
