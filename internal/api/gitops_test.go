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

func (m *mockArgoCDAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockArgoCDAdapter) Disconnect() error                                  { return nil }
func (m *mockArgoCDAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
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

func (m *mockTerraformAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockTerraformAdapter) Disconnect() error                                  { return nil }
func (m *mockTerraformAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
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

func (m *mockHelmAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockHelmAdapter) Disconnect() error                                  { return nil }
func (m *mockHelmAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
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

func TestHandleTerraformGetResource_MissingAddress(t *testing.T) {
	mock := &mockTerraformAdapter{}
	mux := setupGitOpsServer(t, "tf-src", mock)
	// No ?address= query param — should 400.
	req := httptest.NewRequest("GET", "/api/v1/terraform/tf-src/state/resource", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleTerraformGetResource_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/terraform/nonexistent/state/resource?address=aws_instance.web", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleTerraformGetResource(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockTerraformAdapter
		wantStatus int
	}{
		{
			name: "success",
			mock: &mockTerraformAdapter{
				resource: &terraformadapter.Resource{Address: "aws_instance.web", Type: "aws_instance"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockTerraformAdapter{err: fmt.Errorf("state error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGitOpsServer(t, "tf-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/terraform/tf-src/state/resource?address=aws_instance.web", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
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

func TestHandleArgoCDDiff(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockArgoCDAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockArgoCDAdapter{diff: &argocdadapter.Diff{Name: "my-app", SyncStatus: "OutOfSync"}},
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
			req := httptest.NewRequest("GET", "/api/v1/argocd/argocd-src/apps/my-app/diff", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleArgoCDHistory(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockArgoCDAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockArgoCDAdapter{history: []argocdadapter.SyncOperation{{Revision: "abc123", Phase: "Succeeded"}}},
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
			req := httptest.NewRequest("GET", "/api/v1/argocd/argocd-src/apps/my-app/history", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleTerraformOutputs(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockTerraformAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockTerraformAdapter{outputs: map[string]terraformadapter.Output{"vpc_id": {Value: "vpc-123", Sensitive: false}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised",
			mock:       &mockTerraformAdapter{outputs: nil},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockTerraformAdapter{err: fmt.Errorf("terraform error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupGitOpsServer(t, "tf-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/terraform/tf-src/outputs", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleHelmGetRelease(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockHelmAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockHelmAdapter{detail: &helmadapter.ReleaseDetail{Release: helmadapter.Release{Name: "nginx", Namespace: "ingress", Status: "deployed"}}},
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
			req := httptest.NewRequest("GET", "/api/v1/helm/helm-src/releases/default/nginx", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleHelmHistory(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockHelmAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockHelmAdapter{history: []helmadapter.RevisionEntry{{Revision: 1, Status: "deployed"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised",
			mock:       &mockHelmAdapter{history: nil},
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
			req := httptest.NewRequest("GET", "/api/v1/helm/helm-src/releases/default/nginx/history", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleHelmHistory_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/helm/nonexistent/releases/default/nginx/history", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
