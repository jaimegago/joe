package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// --- OCI mock ---

type mockOCIAdapter struct {
	repos    []string
	tags     []string
	manifest *ociadapter.Manifest
	err      error
}

func (m *mockOCIAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockOCIAdapter) Disconnect() error                                  { return nil }
func (m *mockOCIAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (m *mockOCIAdapter) ListRepositories(_ context.Context) ([]string, error) {
	return m.repos, m.err
}
func (m *mockOCIAdapter) ListTags(_ context.Context, _ string) ([]string, error) {
	return m.tags, m.err
}
func (m *mockOCIAdapter) GetManifest(_ context.Context, _, _ string) (*ociadapter.Manifest, error) {
	return m.manifest, m.err
}

// --- ECR mock ---

type mockECRAdapter struct {
	repos  []ecradapter.Repository
	images []ecradapter.ImageDetail
	image  *ecradapter.ImageDetail
	err    error
}

func (m *mockECRAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockECRAdapter) Disconnect() error                                  { return nil }
func (m *mockECRAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (m *mockECRAdapter) ListRepositories(_ context.Context) ([]ecradapter.Repository, error) {
	return m.repos, m.err
}
func (m *mockECRAdapter) ListImages(_ context.Context, _ string) ([]ecradapter.ImageDetail, error) {
	return m.images, m.err
}
func (m *mockECRAdapter) GetImageDetails(_ context.Context, _, _ string) (*ecradapter.ImageDetail, error) {
	return m.image, m.err
}

// --- Artifactory mock ---

type mockArtifactoryAdapter struct {
	repos    []artifactoryadapter.Repository
	tags     []string
	artifact *artifactoryadapter.ArtifactInfo
	err      error
}

func (m *mockArtifactoryAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockArtifactoryAdapter) Disconnect() error                                  { return nil }
func (m *mockArtifactoryAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (m *mockArtifactoryAdapter) ListRepositories(_ context.Context) ([]artifactoryadapter.Repository, error) {
	return m.repos, m.err
}
func (m *mockArtifactoryAdapter) ListDockerTags(_ context.Context, _, _ string) ([]string, error) {
	return m.tags, m.err
}
func (m *mockArtifactoryAdapter) GetArtifactInfo(_ context.Context, _, _ string) (*artifactoryadapter.ArtifactInfo, error) {
	return m.artifact, m.err
}

// --- setup helper ---

func setupRegistryServer(t *testing.T, sourceID string, mock adapters.Adapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register(sourceID, mock)
	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}
	server := api.New(services, api.TestingPolicyEngine(services))
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

// --- OCI tests ---

func TestHandleOCIListRepos_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/oci/nonexistent/repos", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleOCIListRepos(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockOCIAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockOCIAdapter{repos: []string{"library/nginx", "library/redis"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockOCIAdapter{err: fmt.Errorf("registry unavailable")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupRegistryServer(t, "oci-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/registry/oci/oci-src/repos", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleOCIListTags_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/oci/nonexistent/repos/nginx/tags", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleOCIGetManifest_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/oci/nonexistent/repos/nginx/manifest?reference=latest", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- ECR tests ---

func TestHandleECRListRepos_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/ecr/nonexistent/repos", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleECRListRepos(t *testing.T) {
	mock := &mockECRAdapter{repos: []ecradapter.Repository{{Name: "my-app", URI: "123.dkr.ecr.us-east-1.amazonaws.com/my-app"}}}
	mux := setupRegistryServer(t, "ecr-src", mock)
	req := httptest.NewRequest("GET", "/api/v1/registry/ecr/ecr-src/repos", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleECRListImages_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/ecr/nonexistent/repos/my-app/images", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Artifactory tests ---

func TestHandleArtifactoryListRepos_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/artifactory/nonexistent/repos", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleArtifactoryListRepos(t *testing.T) {
	mock := &mockArtifactoryAdapter{repos: []artifactoryadapter.Repository{{Key: "docker-local", Type: "local"}}}
	mux := setupRegistryServer(t, "art-src", mock)
	req := httptest.NewRequest("GET", "/api/v1/registry/artifactory/art-src/repos", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleArtifactoryListTags_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/artifactory/nonexistent/repos/my-repo/tags?image=my-image", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleOCIListTags(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockOCIAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockOCIAdapter{tags: []string{"latest", "v1.0", "v1.1"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised",
			mock:       &mockOCIAdapter{tags: nil},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockOCIAdapter{err: fmt.Errorf("registry error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupRegistryServer(t, "oci-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/registry/oci/oci-src/repos/nginx/tags", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleOCIGetManifest(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		mock       *mockOCIAdapter
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/registry/oci/oci-src/repos/nginx/manifest?reference=latest",
			mock:       &mockOCIAdapter{manifest: &ociadapter.Manifest{MediaType: "application/vnd.docker.distribution.manifest.v2+json"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing reference",
			url:        "/api/v1/registry/oci/oci-src/repos/nginx/manifest",
			mock:       &mockOCIAdapter{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/registry/oci/oci-src/repos/nginx/manifest?reference=latest",
			mock:       &mockOCIAdapter{err: fmt.Errorf("registry error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupRegistryServer(t, "oci-src", tt.mock)
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleECRListImages(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockECRAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockECRAdapter{images: []ecradapter.ImageDetail{{Tags: []string{"latest"}, Digest: "sha256:abc"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised",
			mock:       &mockECRAdapter{images: nil},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockECRAdapter{err: fmt.Errorf("ecr error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupRegistryServer(t, "ecr-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/registry/ecr/ecr-src/repos/my-app/images", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleECRGetImage(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockECRAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockECRAdapter{image: &ecradapter.ImageDetail{Tags: []string{"v1.0"}, Digest: "sha256:abc"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockECRAdapter{err: fmt.Errorf("ecr error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupRegistryServer(t, "ecr-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/registry/ecr/ecr-src/repos/my-app/images/v1.0", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleECRGetImage_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/ecr/nonexistent/repos/my-app/images/v1.0", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleArtifactoryListTags(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		mock       *mockArtifactoryAdapter
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/registry/artifactory/art-src/repos/my-repo/tags?image=my-image",
			mock:       &mockArtifactoryAdapter{tags: []string{"latest", "1.0"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing image param",
			url:        "/api/v1/registry/artifactory/art-src/repos/my-repo/tags",
			mock:       &mockArtifactoryAdapter{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/registry/artifactory/art-src/repos/my-repo/tags?image=my-image",
			mock:       &mockArtifactoryAdapter{err: fmt.Errorf("artifactory error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupRegistryServer(t, "art-src", tt.mock)
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleArtifactoryGetArtifact(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		mock       *mockArtifactoryAdapter
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/registry/artifactory/art-src/repos/my-repo/artifact?path=com/example/lib/1.0/lib-1.0.jar",
			mock:       &mockArtifactoryAdapter{artifact: &artifactoryadapter.ArtifactInfo{Repo: "my-repo", Path: "com/example"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing path param",
			url:        "/api/v1/registry/artifactory/art-src/repos/my-repo/artifact",
			mock:       &mockArtifactoryAdapter{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/registry/artifactory/art-src/repos/my-repo/artifact?path=some/path",
			mock:       &mockArtifactoryAdapter{err: fmt.Errorf("artifactory error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupRegistryServer(t, "art-src", tt.mock)
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleArtifactoryGetArtifact_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/registry/artifactory/nonexistent/repos/my-repo/artifact?path=some/path", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
