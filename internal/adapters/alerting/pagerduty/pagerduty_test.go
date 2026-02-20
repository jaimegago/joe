package pagerduty_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/store"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantKey string
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"api_key": "test-key-123"},
			wantKey: "test-key-123",
		},
		{
			name:    "missing api_key",
			config:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := pagerduty.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.APIKey != tt.wantKey {
				t.Errorf("APIKey = %q, want %q", cfg.APIKey, tt.wantKey)
			}
		})
	}
}

func TestAdapter_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/abilities" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"abilities":["teams"]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := pagerduty.New()
	// Override the base URL via the config's api_url field.
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "test-key",
		"api_url": srv.URL,
	})}

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

	adapter := pagerduty.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "bad-key",
		"api_url": srv.URL,
	})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_ListIncidents(t *testing.T) {
	incidentsJSON := `{
		"incidents": [
			{
				"id": "INC001",
				"title": "Payment service down",
				"status": "triggered",
				"urgency": "high",
				"service": {"id": "SVC001", "name": "payment"},
				"created_at": "2024-01-01T00:00:00Z",
				"html_url": "https://acme.pagerduty.com/incidents/INC001"
			}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/abilities":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"abilities":[]}`))
		case "/incidents":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(incidentsJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := pagerduty.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "test-key",
		"api_url": srv.URL,
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	incidents, err := adapter.ListIncidents(context.Background(), "", "triggered", 10)
	if err != nil {
		t.Fatalf("ListIncidents() error = %v", err)
	}

	if len(incidents) != 1 {
		t.Errorf("len(incidents) = %d, want 1", len(incidents))
	}
	if incidents[0].ID != "INC001" {
		t.Errorf("ID = %q, want %q", incidents[0].ID, "INC001")
	}
	if incidents[0].Service.Name != "payment" {
		t.Errorf("Service.Name = %q, want %q", incidents[0].Service.Name, "payment")
	}
}

func TestAdapter_ListServices(t *testing.T) {
	servicesJSON := `{
		"services": [
			{"id": "SVC001", "name": "payment"},
			{"id": "SVC002", "name": "order"}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/abilities":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"abilities":[]}`))
		case "/services":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(servicesJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := pagerduty.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "test-key",
		"api_url": srv.URL,
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	services, err := adapter.ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}

	if len(services) != 2 {
		t.Errorf("len(services) = %d, want 2", len(services))
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	adapter := pagerduty.New()
	_, err := adapter.ListIncidents(context.Background(), "", "", 10)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestNewWithClient(t *testing.T) {
	adapter := pagerduty.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	}, "http://localhost")
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := pagerduty.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"abilities":[]}`))
	}))
	defer srv.Close()

	adapter := pagerduty.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "key",
		"api_url": srv.URL,
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if adapter.Status().Connected {
		t.Error("Status().Connected = true after Disconnect, want false")
	}
	_, err := adapter.ListIncidents(context.Background(), "", "", 10)
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := pagerduty.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	adapter := pagerduty.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "key",
		"api_url": srv.URL,
	})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_ListIncidents_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"abilities":[]}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := pagerduty.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "key",
		"api_url": srv.URL,
	})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListIncidents(context.Background(), "", "", 10)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_ListServices_NotConnected(t *testing.T) {
	adapter := pagerduty.New()
	_, err := adapter.ListServices(context.Background())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_ListServices_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"abilities":[]}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := pagerduty.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"api_key": "key",
		"api_url": srv.URL,
	})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListServices(context.Background())
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
