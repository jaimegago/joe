package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// --- Alertmanager mock ---

type mockAlertmanagerAdapter struct {
	alerts []alertmanageradapter.Alert
	err    error
}

func (m *mockAlertmanagerAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockAlertmanagerAdapter) Disconnect() error                               { return nil }
func (m *mockAlertmanagerAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockAlertmanagerAdapter) ListAlerts(_ context.Context, _ string) ([]alertmanageradapter.Alert, error) {
	return m.alerts, m.err
}

// --- PagerDuty mock ---

type mockPagerDutyAdapter struct {
	incidents []pagerdutyadapter.Incident
	pdSvcs    []pagerdutyadapter.Service
	err       error
}

func (m *mockPagerDutyAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockPagerDutyAdapter) Disconnect() error                               { return nil }
func (m *mockPagerDutyAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockPagerDutyAdapter) ListIncidents(_ context.Context, _, _ string, _ int) ([]pagerdutyadapter.Incident, error) {
	return m.incidents, m.err
}
func (m *mockPagerDutyAdapter) ListServices(_ context.Context) ([]pagerdutyadapter.Service, error) {
	return m.pdSvcs, m.err
}

// --- Grafana mock ---

type mockGrafanaAdapter struct {
	dashboards []grafanaadapter.Dashboard
	dashboard  *grafanaadapter.DashboardDetail
	galerts    []grafanaadapter.GrafanaAlert
	err        error
}

func (m *mockGrafanaAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockGrafanaAdapter) Disconnect() error                               { return nil }
func (m *mockGrafanaAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockGrafanaAdapter) ListDashboards(_ context.Context, _ string, _ int) ([]grafanaadapter.Dashboard, error) {
	return m.dashboards, m.err
}
func (m *mockGrafanaAdapter) GetDashboard(_ context.Context, _ string) (*grafanaadapter.DashboardDetail, error) {
	return m.dashboard, m.err
}
func (m *mockGrafanaAdapter) ListAlerts(_ context.Context) ([]grafanaadapter.GrafanaAlert, error) {
	return m.galerts, m.err
}

// --- Setup helpers ---

func setupAlertmanagerTestServer(t *testing.T, mock *mockAlertmanagerAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-am", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupPagerDutyTestServer(t *testing.T, mock *mockPagerDutyAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-pd", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupGrafanaTestServer(t *testing.T, mock *mockGrafanaAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-grafana", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

// --- Alertmanager tests ---

func TestHandleAlertmanagerAlerts(t *testing.T) {
	tests := []struct {
		name       string
		alerts     []alertmanageradapter.Alert
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "success with alerts",
			alerts: []alertmanageradapter.Alert{
				{
					Fingerprint: "abc123",
					Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "empty alerts",
			alerts:     []alertmanageradapter.Alert{},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "nil alerts normalised to empty",
			alerts:     nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("alertmanager error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupAlertmanagerTestServer(t, &mockAlertmanagerAdapter{alerts: tt.alerts, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/alertmanager/test-am/alerts", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleAlertmanagerAlerts_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/alertmanager/nonexistent/alerts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleAlertmanagerAlerts_WithFilter(t *testing.T) {
	mux := setupAlertmanagerTestServer(t, &mockAlertmanagerAdapter{alerts: []alertmanageradapter.Alert{}})

	req := httptest.NewRequest("GET", "/api/v1/alertmanager/test-am/alerts?filter=severity%3Dcritical", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestHandleAlertingWrongAdapterType(t *testing.T) {
	// Register a PagerDuty adapter, but access it via the alertmanager route.
	registry := adapters.NewRegistry()
	registry.Register("test-pd", &mockPagerDutyAdapter{})
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/alertmanager/test-pd/alerts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- PagerDuty tests ---

func TestHandlePagerDutyIncidents(t *testing.T) {
	tests := []struct {
		name       string
		incidents  []pagerdutyadapter.Incident
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "success with incidents",
			incidents: []pagerdutyadapter.Incident{
				{ID: "INC001", Title: "Payment down", Status: "triggered"},
				{ID: "INC002", Title: "DB slow", Status: "acknowledged"},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "nil incidents normalised to empty",
			incidents:  nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("pagerduty error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupPagerDutyTestServer(t, &mockPagerDutyAdapter{incidents: tt.incidents, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/pagerduty/test-pd/incidents", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandlePagerDutyIncidents_WithParams(t *testing.T) {
	mux := setupPagerDutyTestServer(t, &mockPagerDutyAdapter{incidents: []pagerdutyadapter.Incident{}})

	req := httptest.NewRequest("GET", "/api/v1/pagerduty/test-pd/incidents?service=SVC001&status=triggered&limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandlePagerDutyIncidents_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/pagerduty/nonexistent/incidents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePagerDutyServices(t *testing.T) {
	tests := []struct {
		name       string
		services   []pagerdutyadapter.Service
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name:       "success with services",
			services:   []pagerdutyadapter.Service{{ID: "SVC001", Name: "payment"}, {ID: "SVC002", Name: "order"}},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "nil services normalised to empty",
			services:   nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("pagerduty error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupPagerDutyTestServer(t, &mockPagerDutyAdapter{pdSvcs: tt.services, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/pagerduty/test-pd/services", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

// --- Grafana tests ---

func TestHandleGrafanaDashboards(t *testing.T) {
	tests := []struct {
		name       string
		dashboards []grafanaadapter.Dashboard
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "success with dashboards",
			dashboards: []grafanaadapter.Dashboard{
				{UID: "dash-1", Title: "System Overview"},
				{UID: "dash-2", Title: "K8s Metrics"},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "nil dashboards normalised to empty",
			dashboards: nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("grafana error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGrafanaTestServer(t, &mockGrafanaAdapter{dashboards: tt.dashboards, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/grafana/test-grafana/dashboards", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleGrafanaDashboards_WithParams(t *testing.T) {
	mux := setupGrafanaTestServer(t, &mockGrafanaAdapter{dashboards: []grafanaadapter.Dashboard{}})

	req := httptest.NewRequest("GET", "/api/v1/grafana/test-grafana/dashboards?query=payment&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleGrafanaGetDashboard(t *testing.T) {
	tests := []struct {
		name       string
		uid        string
		dashboard  *grafanaadapter.DashboardDetail
		err        error
		wantStatus int
	}{
		{
			name: "success",
			uid:  "dash-1",
			dashboard: &grafanaadapter.DashboardDetail{
				UID:   "dash-1",
				Title: "System Overview",
				Tags:  []string{"k8s"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			uid:        "dash-error",
			err:        fmt.Errorf("grafana error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGrafanaTestServer(t, &mockGrafanaAdapter{dashboard: tt.dashboard, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/grafana/test-grafana/dashboards/"+tt.uid, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleGrafanaGetDashboard_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/grafana/nonexistent/dashboards/dash-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGrafanaAlerts(t *testing.T) {
	tests := []struct {
		name       string
		alerts     []grafanaadapter.GrafanaAlert
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "success with alerts",
			alerts: []grafanaadapter.GrafanaAlert{
				{
					Fingerprint: "abc123",
					State:       "active",
					Labels:      map[string]string{"alertname": "HighCPU"},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "nil alerts normalised to empty",
			alerts:     nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("grafana error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGrafanaTestServer(t, &mockGrafanaAdapter{galerts: tt.alerts, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/grafana/test-grafana/alerts", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleGrafanaAlerts_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/grafana/nonexistent/alerts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}
