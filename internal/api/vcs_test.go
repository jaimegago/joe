package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// --- Mock GitHub adapter ---

type mockGitHubAdapter struct {
	secret        string
	pr            *githubadapter.PRInfo
	diff          string
	prs           []*githubadapter.PRInfo
	prErr         error
	diffErr       error
	commentErr    error
	requestChgErr error
	listPRsErr    error
}

func (m *mockGitHubAdapter) Connect(_ context.Context, _ store.Component) error {
	return nil
}
func (m *mockGitHubAdapter) Disconnect() error { return nil }
func (m *mockGitHubAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockGitHubAdapter) WebhookSecret() string { return m.secret }
func (m *mockGitHubAdapter) GetPR(_ context.Context, _, _ string, _ int) (*githubadapter.PRInfo, error) {
	return m.pr, m.prErr
}
func (m *mockGitHubAdapter) GetPRDiff(_ context.Context, _, _ string, _ int) (string, error) {
	return m.diff, m.diffErr
}
func (m *mockGitHubAdapter) PostComment(_ context.Context, _, _ string, _ int, _ string) error {
	return m.commentErr
}
func (m *mockGitHubAdapter) RequestChanges(_ context.Context, _, _ string, _ int, _ string) error {
	return m.requestChgErr
}
func (m *mockGitHubAdapter) ListPRs(_ context.Context, _, _, _ string) ([]*githubadapter.PRInfo, error) {
	return m.prs, m.listPRsErr
}

// --- Mock GitLab adapter ---

type mockGitLabAdapter struct {
	secret        string
	mr            *gitlabadapter.MRInfo
	diff          string
	mrs           []*gitlabadapter.MRInfo
	mrErr         error
	diffErr       error
	noteErr       error
	requestChgErr error
	listMRsErr    error
}

func (m *mockGitLabAdapter) Connect(_ context.Context, _ store.Component) error {
	return nil
}
func (m *mockGitLabAdapter) Disconnect() error { return nil }
func (m *mockGitLabAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockGitLabAdapter) WebhookSecret() string { return m.secret }
func (m *mockGitLabAdapter) GetMR(_ context.Context, _ string, _ int) (*gitlabadapter.MRInfo, error) {
	return m.mr, m.mrErr
}
func (m *mockGitLabAdapter) GetMRDiff(_ context.Context, _ string, _ int) (string, error) {
	return m.diff, m.diffErr
}
func (m *mockGitLabAdapter) PostNote(_ context.Context, _ string, _ int, _ string) error {
	return m.noteErr
}
func (m *mockGitLabAdapter) RequestChanges(_ context.Context, _ string, _ int, _ string) error {
	return m.requestChgErr
}
func (m *mockGitLabAdapter) ListMRs(_ context.Context, _, _ string) ([]*gitlabadapter.MRInfo, error) {
	return m.mrs, m.listMRsErr
}

// --- Setup helpers ---

// setupVCSServer creates a test server with a full store and optional adapter
// registry entries for exercising the GitHub PR / GitLab MR operation routes.
func setupVCSServer(t *testing.T, regFn func(*adapters.Registry)) (*api.Server, *http.ServeMux) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	registry := adapters.NewRegistry()
	if regFn != nil {
		regFn(registry)
	}

	services := &core.Services{
		Config:   &config.Config{},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: registry,
	}

	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

func vcsJSON(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// --- GitHub PR operation tests ---

func TestVCSHandler_GitHubGetPR_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{
		pr: &githubadapter.PRInfo{Number: 1, Title: "test PR", Author: "alice"},
	}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1?owner=org&repo=repo", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVCSHandler_GitHubGetPR_MissingOwnerRepo(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1", nil) // no owner/repo
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubGetPR_NotFound(t *testing.T) {
	// No adapter registered → 404.
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/unknown-src/pulls/1?owner=o&repo=r", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubGetPRDiff_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{diff: "diff --git a/file.go b/file.go"}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1/diff?owner=org&repo=repo", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVCSHandler_GitHubGetPRDiff_MissingOwnerRepo(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubListPRs_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{
		prs: []*githubadapter.PRInfo{{Number: 1, Title: "PR1"}},
	}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls?owner=org&repo=repo", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", resp["count"])
	}
}

func TestVCSHandler_GitHubListPRs_MissingOwnerRepo(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubPostComment_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	body := map[string]any{"owner": "org", "repo": "repo", "body": "LGTM!"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/comments", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVCSHandler_GitHubPostComment_MissingBody(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo"} // missing body
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/comments", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubRequestChanges_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo", "body": "Needs work"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/reviews", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVCSHandler_GitHubRequestChanges_MissingFields(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org"} // missing repo + body
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/reviews", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubRequestChanges_InvalidNumber(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/abc/reviews", vcsJSON(map[string]any{}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric PR number, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubRequestChanges_AdapterError(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{requestChgErr: fmt.Errorf("api error")}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo", "body": "Needs work"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/reviews", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on adapter error, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubPostComment_InvalidNumber(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/zero/comments", vcsJSON(map[string]any{}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric PR number, got %d", w.Code)
	}
}

func TestVCSHandler_GitHubPostComment_AdapterError(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{commentErr: fmt.Errorf("api error")}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo", "body": "LGTM"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/comments", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on adapter error, got %d", w.Code)
	}
}

// --- GitLab MR operation tests ---

func TestVCSHandler_GitLabGetMR_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{
		mr: &gitlabadapter.MRInfo{IID: 3, Title: "Fix bug", Author: "bob"},
	}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVCSHandler_GitLabGetMR_NotFound(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/gitlab/unknown-src/projects/proj1/mrs/1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestVCSHandler_GitLabGetMR_InvalidIID(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/abc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitLabGetMRDiff_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{diff: "diff --git a/f.go b/f.go"}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVCSHandler_GitLabGetMRDiff_InvalidIID(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/bad/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitLabGetMRDiff_AdapterError(t *testing.T) {
	glAdapter := &mockGitLabAdapter{diffErr: fmt.Errorf("connection refused")}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVCSHandler_GitLabListMRs_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{
		mrs: []*gitlabadapter.MRInfo{{IID: 1, Title: "MR1"}},
	}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", resp["count"])
	}
}

func TestVCSHandler_GitLabListMRs_AdapterError(t *testing.T) {
	glAdapter := &mockGitLabAdapter{listMRsErr: fmt.Errorf("api error")}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestVCSHandler_GitLabPostNote_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	body := map[string]any{"body": "LGTM"}
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/notes", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVCSHandler_GitLabPostNote_MissingBody(t *testing.T) {
	glAdapter := &mockGitLabAdapter{}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	body := map[string]any{} // missing body
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/notes", vcsJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitLabPostNote_InvalidIID(t *testing.T) {
	_, mux := setupVCSServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/bad/notes", vcsJSON(map[string]any{"body": "x"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVCSHandler_GitLabPostNote_AdapterError(t *testing.T) {
	glAdapter := &mockGitLabAdapter{noteErr: fmt.Errorf("api error")}
	_, mux := setupVCSServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/notes", vcsJSON(map[string]any{"body": "LGTM"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}
