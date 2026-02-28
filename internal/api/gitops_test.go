package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// --- ArgoCD mock ---

type mockArgoCDAdapter struct {
	apps    []argocdadapter.App
	appErr  error
	detail  *argocdadapter.AppDetail
	diff    *argocdadapter.Diff
	history []argocdadapter.SyncOperation
}

func (m *mockArgoCDAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockArgoCDAdapter) Disconnect() error                               { return nil }
func (m *mockArgoCDAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockArgoCDAdapter) Apps(_ context.Context, _ string) ([]argocdadapter.App, error) {
	return m.apps, m.appErr
}
func (m *mockArgoCDAdapter) GetApp(_ context.Context, _ string) (*argocdadapter.AppDetail, error) {
	return m.detail, m.appErr
}
func (m *mockArgoCDAdapter) GetDiff(_ context.Context, _ string) (*argocdadapter.Diff, error) {
	return m.diff, m.appErr
}
func (m *mockArgoCDAdapter) GetHistory(_ context.Context, _ string, _ int) ([]argocdadapter.SyncOperation, error) {
	return m.history, m.appErr
}

// --- Terraform mock ---

type mockTerraformAdapter struct {
	resources []terraformadapter.Resource
	resource  *terraformadapter.Resource
	outputs   map[string]terraformadapter.Output
	err       error
}

func (m *mockTerraformAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockTerraformAdapter) Disconnect() error                               { return nil }
func (m *mockTerraformAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockTerraformAdapter) Resources(_ context.Context, _ string) ([]terraformadapter.Resource, error) {
	return m.resources, m.err
}
func (m *mockTerraformAdapter) GetResource(_ context.Context, _ string) (*terraformadapter.Resource, error) {
	return m.resource, m.err
}
func (m *mockTerraformAdapter) Outputs(_ context.Context) (map[string]terraformadapter.Output, error) {
	return m.outputs, m.err
}

// --- Helm mock ---

type mockHelmAdapter struct {
	releases []helmadapter.Release
	detail   *helmadapter.ReleaseDetail
	history  []helmadapter.RevisionEntry
	err      error
}

func (m *mockHelmAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockHelmAdapter) Disconnect() error                               { return nil }
func (m *mockHelmAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockHelmAdapter) Releases(_ context.Context, _ string) ([]helmadapter.Release, error) {
	return m.releases, m.err
}
func (m *mockHelmAdapter) GetRelease(_ context.Context, _, _ string) (*helmadapter.ReleaseDetail, error) {
	return m.detail, m.err
}
func (m *mockHelmAdapter) History(_ context.Context, _, _ string, _ int) ([]helmadapter.RevisionEntry, error) {
	return m.history, m.err
}

// --- setup helpers ---

func setupGitOpsServer(t *testing.T, sourceID string, mock adapters.Adapter) *http.ServeMux {
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

// --- ArgoCD tests ---

func TestHandleArgoCDApps_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/argocd/nonexistent/apps", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleArgoCDApps(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockArgoCDAdapter
		wantStatus int
	}{
		{
			name:       "success empty list",
			mock:       &mockArgoCDAdapter{apps: []argocdadapter.App{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "success with apps",
			mock:       &mockArgoCDAdapter{apps: []argocdadapter.App{{Name: "my-app", Namespace: "default"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockArgoCDAdapter{appErr: fmt.Errorf("argocd error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGitOpsServer(t, "argocd-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/argocd/argocd-src/apps", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleArgoCDGetApp(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockArgoCDAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockArgoCDAdapter{detail: &argocdadapter.AppDetail{App: argocdadapter.App{Name: "my-app"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockArgoCDAdapter{appErr: fmt.Errorf("not found")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGitOpsServer(t, "argocd-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/argocd/argocd-src/apps/my-app", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleArgoCDDiff_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/argocd/nonexistent/apps/my-app/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleArgoCDHistory_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/argocd/nonexistent/apps/my-app/history", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Terraform tests ---

func TestHandleTerraformResources_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/terraform/nonexistent/state", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleTerraformResources(t *testing.T) {
	mock := &mockTerraformAdapter{resources: []terraformadapter.Resource{{Address: "aws_instance.web"}}}
	mux := setupGitOpsServer(t, "tf-src", mock)
	req := httptest.NewRequest("GET", "/api/v1/terraform/tf-src/state", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleTerraformOutputs_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/terraform/nonexistent/outputs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Helm tests ---

func TestHandleHelmReleases_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/helm/nonexistent/releases", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleHelmReleases(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockHelmAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockHelmAdapter{releases: []helmadapter.Release{{Name: "nginx", Namespace: "ingress", Status: "deployed"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockHelmAdapter{err: fmt.Errorf("helm error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGitOpsServer(t, "helm-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/helm/helm-src/releases", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleHelmGetRelease_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/helm/nonexistent/releases/default/nginx", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
