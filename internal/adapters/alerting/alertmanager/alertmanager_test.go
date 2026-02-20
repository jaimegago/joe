package alertmanager_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
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
			config:  map[string]any{"url": "http://alertmanager:9093"},
			wantURL: "http://alertmanager:9093",
		},
		{
			name:    "missing url",
			config:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := alertmanager.ParseConfig(tt.config)
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
		if r.URL.Path == "/api/v2/status" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cluster":{"status":"ready"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := alertmanager.New()
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

	adapter := alertmanager.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_ListAlerts(t *testing.T) {
	alertsJSON := `[
		{
			"fingerprint": "abc123",
			"status": {"state": "active"},
			"labels": {"alertname": "HighCPU", "severity": "critical", "service": "payment"},
			"annotations": {"summary": "CPU usage above 90%"},
			"startsAt": "2024-01-01T00:00:00Z",
			"updatedAt": "2024-01-01T00:01:00Z",
			"endsAt": "0001-01-01T00:00:00Z",
			"receivers": [{"name": "slack-critical"}]
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cluster":{"status":"ready"}}`))
		case "/api/v2/alerts":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(alertsJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := alertmanager.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	alerts, err := adapter.ListAlerts(context.Background(), "")
	if err != nil {
		t.Fatalf("ListAlerts() error = %v", err)
	}

	if len(alerts) != 1 {
		t.Errorf("len(alerts) = %d, want 1", len(alerts))
	}
	if alerts[0].Fingerprint != "abc123" {
		t.Errorf("Fingerprint = %q, want %q", alerts[0].Fingerprint, "abc123")
	}
	if alerts[0].Labels["alertname"] != "HighCPU" {
		t.Errorf("Labels[alertname] = %q, want %q", alerts[0].Labels["alertname"], "HighCPU")
	}
	if len(alerts[0].Receivers) != 1 || alerts[0].Receivers[0] != "slack-critical" {
		t.Errorf("Receivers = %v, want [slack-critical]", alerts[0].Receivers)
	}
}

func TestAdapter_ListAlerts_WithFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case "/api/v2/alerts":
			if r.URL.Query().Get("filter") != "severity=critical" {
				t.Errorf("expected filter=severity=critical, got %q", r.URL.Query().Get("filter"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := alertmanager.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	alerts, err := adapter.ListAlerts(context.Background(), "severity=critical")
	if err != nil {
		t.Fatalf("ListAlerts() error = %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	adapter := alertmanager.New()
	_, err := adapter.ListAlerts(context.Background(), "")
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestNewWithClient(t *testing.T) {
	adapter := alertmanager.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := alertmanager.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	adapter := alertmanager.New()
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
	_, err := adapter.ListAlerts(context.Background(), "")
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := alertmanager.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	adapter := alertmanager.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_Connect_WithAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	adapter := alertmanager.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "my-secret",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotAuth != "Bearer my-secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-secret")
	}
}

func TestAdapter_ListAlerts_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := alertmanager.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListAlerts(context.Background(), "")
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

// mockHTTPDoer is a minimal mock for the httpDoer interface.
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
