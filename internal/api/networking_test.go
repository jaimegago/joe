package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	envoyidapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// --- NGINX mock ---

type mockNginxAdapter struct {
	ingresses  []nginxadapter.Ingress
	status     *nginxadapter.NginxStatus
	configMaps []nginxadapter.ConfigMapSummary
	err        error
}

func (m *mockNginxAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockNginxAdapter) Disconnect() error                               { return nil }
func (m *mockNginxAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockNginxAdapter) ListIngresses(_ context.Context, _ string) ([]nginxadapter.Ingress, error) {
	return m.ingresses, m.err
}
func (m *mockNginxAdapter) GetNginxStatus(_ context.Context) (*nginxadapter.NginxStatus, error) {
	return m.status, m.err
}
func (m *mockNginxAdapter) ListConfigMaps(_ context.Context, _ string) ([]nginxadapter.ConfigMapSummary, error) {
	return m.configMaps, m.err
}

// --- Envoy mock ---

type mockEnvoyAdapter struct {
	clusters []envoyidapter.ClusterStatus
	config   map[string]any
	stats    []envoyidapter.Stat
	err      error
}

func (m *mockEnvoyAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockEnvoyAdapter) Disconnect() error                               { return nil }
func (m *mockEnvoyAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockEnvoyAdapter) Clusters(_ context.Context) ([]envoyidapter.ClusterStatus, error) {
	return m.clusters, m.err
}
func (m *mockEnvoyAdapter) ConfigDump(_ context.Context, _ string) (map[string]any, error) {
	return m.config, m.err
}
func (m *mockEnvoyAdapter) Stats(_ context.Context, _ string) ([]envoyidapter.Stat, error) {
	return m.stats, m.err
}

// --- setup helper ---

func setupNetworkingServer(t *testing.T, sourceID string, mock adapters.Adapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register(sourceID, mock)
	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

// --- NGINX tests ---

func TestHandleNginxIngresses_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/nginx/nonexistent/ingresses", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleNginxIngresses(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockNginxAdapter
		wantStatus int
	}{
		{
			name: "success",
			mock: &mockNginxAdapter{ingresses: []nginxadapter.Ingress{
				{Name: "frontend", Namespace: "default"},
			}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockNginxAdapter{err: fmt.Errorf("k8s error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupNetworkingServer(t, "nginx-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/nginx/nginx-src/ingresses", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleNginxStatus(t *testing.T) {
	mock := &mockNginxAdapter{status: &nginxadapter.NginxStatus{ActiveConnections: 5}}
	mux := setupNetworkingServer(t, "nginx-src", mock)
	req := httptest.NewRequest("GET", "/api/v1/nginx/nginx-src/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleNginxConfigMaps_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/nginx/nonexistent/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Envoy tests ---

func TestHandleEnvoyClusters_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/envoy/nonexistent/clusters", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleEnvoyClusters(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockEnvoyAdapter
		wantStatus int
	}{
		{
			name: "success",
			mock: &mockEnvoyAdapter{clusters: []envoyidapter.ClusterStatus{
				{Name: "payment-svc"},
			}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockEnvoyAdapter{err: fmt.Errorf("envoy unreachable")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupNetworkingServer(t, "envoy-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/envoy/envoy-src/clusters", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleEnvoyConfigDump_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/envoy/nonexistent/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleEnvoyStats_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/envoy/nonexistent/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
