package grafana_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/alerting/grafana"
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
			config:  map[string]any{"url": "http://grafana:3000", "api_key": "glsa_abc123"},
			wantURL: "http://grafana:3000",
		},
		{
			name:    "missing url",
			config:  map[string]any{"api_key": "glsa_abc123"},
			wantErr: true,
		},
		{
			name:    "missing api_key",
			config:  map[string]any{"url": "http://grafana:3000"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := grafana.ParseConfig(tt.config)
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
		if r.URL.Path == "/api/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"commit":"abc","database":"ok","version":"10.0.0"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "test-key",
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

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "bad-key",
	})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_ListDashboards(t *testing.T) {
	dashboardsJSON := `[
		{
			"id": 1,
			"uid": "abc123",
			"title": "Payment Service Overview",
			"uri": "db/payment-service-overview",
			"url": "/d/abc123/payment-service-overview",
			"slug": "payment-service-overview",
			"tags": ["payment", "service"],
			"type": "dash-db"
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"database":"ok"}`))
		case "/api/search":
			if r.URL.Query().Get("type") != "dash-db" {
				t.Errorf("expected type=dash-db, got %q", r.URL.Query().Get("type"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(dashboardsJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "test-key",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	dashboards, err := adapter.ListDashboards(context.Background(), "", 50)
	if err != nil {
		t.Fatalf("ListDashboards() error = %v", err)
	}

	if len(dashboards) != 1 {
		t.Errorf("len(dashboards) = %d, want 1", len(dashboards))
	}
	if dashboards[0].UID != "abc123" {
		t.Errorf("UID = %q, want %q", dashboards[0].UID, "abc123")
	}
	if dashboards[0].Title != "Payment Service Overview" {
		t.Errorf("Title = %q, want %q", dashboards[0].Title, "Payment Service Overview")
	}
}

func TestAdapter_GetDashboard(t *testing.T) {
	dashboardDetailJSON := `{
		"dashboard": {
			"id": 1,
			"uid": "abc123",
			"title": "Payment Service Overview",
			"tags": ["payment"],
			"version": 5,
			"panels": [
				{"id": 1, "title": "Request Rate", "type": "graph"},
				{"id": 2, "title": "Error Rate", "type": "graph"}
			]
		},
		"meta": {
			"url": "/d/abc123/payment-service-overview",
			"updated": "2024-01-01T00:00:00Z"
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"database":"ok"}`))
		case "/api/dashboards/uid/abc123":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(dashboardDetailJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "test-key",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	detail, err := adapter.GetDashboard(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetDashboard() error = %v", err)
	}

	if detail.UID != "abc123" {
		t.Errorf("UID = %q, want %q", detail.UID, "abc123")
	}
	if len(detail.Panels) != 2 {
		t.Errorf("len(Panels) = %d, want 2", len(detail.Panels))
	}
}

func TestAdapter_ListAlerts(t *testing.T) {
	alertsJSON := `[
		{
			"fingerprint": "gf001",
			"status": {"state": "active"},
			"labels": {"alertname": "HighLatency", "service": "payment"},
			"annotations": {"summary": "P99 latency > 500ms"},
			"startsAt": "2024-01-01T00:00:00Z",
			"updatedAt": "2024-01-01T00:01:00Z"
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"database":"ok"}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(alertsJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "test-key",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	alerts, err := adapter.ListAlerts(context.Background())
	if err != nil {
		t.Fatalf("ListAlerts() error = %v", err)
	}

	if len(alerts) != 1 {
		t.Errorf("len(alerts) = %d, want 1", len(alerts))
	}
	if alerts[0].Labels["alertname"] != "HighLatency" {
		t.Errorf("Labels[alertname] = %q, want %q", alerts[0].Labels["alertname"], "HighLatency")
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	adapter := grafana.New()
	_, err := adapter.ListDashboards(context.Background(), "", 10)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestNewWithClient(t *testing.T) {
	adapter := grafana.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := grafana.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"database":"ok"}`))
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "key",
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
	_, err := adapter.ListDashboards(context.Background(), "", 10)
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := grafana.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "key",
	})}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_Connect_WithAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"database":"ok"}`))
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "glsa_mysecret",
	})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotAuth != "Bearer glsa_mysecret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer glsa_mysecret")
	}
}

func TestAdapter_ListDashboards_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"database":"ok"}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "key",
	})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListDashboards(context.Background(), "", 10)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_GetDashboard_NotConnected(t *testing.T) {
	adapter := grafana.New()
	_, err := adapter.GetDashboard(context.Background(), "abc123")
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_GetDashboard_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"database":"ok"}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "key",
	})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.GetDashboard(context.Background(), "abc123")
	if err == nil {
		t.Error("expected error for not found, got nil")
	}
}

func TestAdapter_ListAlerts_NotConnected(t *testing.T) {
	adapter := grafana.New()
	_, err := adapter.ListAlerts(context.Background())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_ListAlerts_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"database":"ok"}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "key",
	})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListAlerts(context.Background())
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_ListDashboards_WithQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"database":"ok"}`))
		case "/api/search":
			gotQuery = r.URL.Query().Get("query")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	adapter := grafana.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "key",
	})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListDashboards(context.Background(), "payment", 25)
	if err != nil {
		t.Fatalf("ListDashboards() error = %v", err)
	}
	if gotQuery != "payment" {
		t.Errorf("query = %q, want payment", gotQuery)
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
