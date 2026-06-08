package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/review"
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

// setupReviewServer creates a test server with a full store (including review jobs table)
// and optional adapter registry entries.
func setupReviewServer(t *testing.T, regFn func(*adapters.Registry)) (*api.Server, *http.ServeMux) {
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

	reviewRepo := review.NewRepository(sqlStore.DB(), store.DriverSQLite)
	reviewSvc := review.NewService(reviewRepo)

	services := &core.Services{
		Config:   &config.Config{},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: registry,
		Review:   reviewSvc,
	}

	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

func reviewJSON(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// --- Webhook tests ---

func TestReviewHandler_GitHubWebhook_NotPREvent(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/github", reviewJSON(map[string]any{
		"action": "opened",
	}))
	req.Header.Set("X-GitHub-Event", "push") // not pull_request
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ignored" {
		t.Errorf("status = %v, want 'ignored'", resp["status"])
	}
}

func TestReviewHandler_GitHubWebhook_ActionNotReviewed(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/github", reviewJSON(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number": 1,
			"head":   map[string]any{"sha": "abc123"},
		},
		"repository": map[string]any{
			"name":  "repo",
			"owner": map[string]any{"login": "org"},
		},
	}))
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ignored" {
		t.Errorf("status = %v, want 'ignored'", resp["status"])
	}
}

func TestReviewHandler_GitHubWebhook_Success(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 42,
			"head":   map[string]any{"sha": "deadbeef"},
		},
		"repository": map[string]any{
			"name":  "myrepo",
			"owner": map[string]any{"login": "myorg"},
		},
	}
	req := httptest.NewRequest("POST", "/api/v1/webhooks/github?component_id=gh-src", reviewJSON(payload))
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var job review.ReviewJob
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if job.PRNumber != 42 {
		t.Errorf("pr_number = %d, want 42", job.PRNumber)
	}
}

func TestReviewHandler_GitHubWebhook_DuplicateEvent(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 1,
			"head":   map[string]any{"sha": "aaabbb"},
		},
		"repository": map[string]any{
			"name":  "repo",
			"owner": map[string]any{"login": "org"},
		},
	}
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/webhooks/github?component_id=s1", reviewJSON(payload))
		req.Header.Set("X-GitHub-Event", "pull_request")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	first := send()
	if first.Code != http.StatusAccepted {
		t.Fatalf("first: expected 202, got %d", first.Code)
	}
	second := send()
	if second.Code != http.StatusOK {
		t.Fatalf("duplicate: expected 200, got %d: %s", second.Code, second.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(second.Body).Decode(&resp)
	if resp["status"] != "skipped" {
		t.Errorf("status = %v, want 'skipped'", resp["status"])
	}
}

func TestReviewHandler_GitHubWebhook_BadJSON(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/webhooks/github", strings.NewReader("{bad json"))
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubWebhook_ServiceUnavailable(t *testing.T) {
	// Server without Review service configured.
	sqlStore, _ := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	sqlStore.Migrate()
	t.Cleanup(func() { sqlStore.Close() })
	services := &core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/github", reviewJSON(map[string]any{}))
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubWebhook_HMACValidation(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{secret: "mysecret"}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	// Send without a valid signature — should be rejected.
	payload := reviewJSON(map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 1, "head": map[string]any{"sha": "abc"},
		},
		"repository": map[string]any{
			"name": "r", "owner": map[string]any{"login": "o"},
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/webhooks/github?component_id=gh-src", payload)
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalidsig")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid HMAC, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabWebhook_NotMRHook(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/gitlab", reviewJSON(map[string]any{}))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ignored" {
		t.Errorf("status = %v, want 'ignored'", resp["status"])
	}
}

func TestReviewHandler_GitLabWebhook_Success(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	payload := map[string]any{
		"object_kind": "merge_request",
		"object_attributes": map[string]any{
			"iid":         7,
			"action":      "open",
			"last_commit": map[string]any{"id": "cafe1234"},
		},
		"project": map[string]any{"id": 99, "name": "myproject"},
	}
	req := httptest.NewRequest("POST", "/api/v1/webhooks/gitlab?component_id=gl-src", reviewJSON(payload))
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GitLabWebhook_ActionIgnored(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	payload := map[string]any{
		"object_attributes": map[string]any{
			"iid":         1,
			"action":      "close",
			"last_commit": map[string]any{"id": "aaa"},
		},
		"project": map[string]any{"id": 1, "name": "proj"},
	}
	req := httptest.NewRequest("POST", "/api/v1/webhooks/gitlab", reviewJSON(payload))
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ignored" {
		t.Errorf("status = %v, want 'ignored'", resp["status"])
	}
}

func TestReviewHandler_GitLabWebhook_TokenValidation(t *testing.T) {
	glAdapter := &mockGitLabAdapter{secret: "mytoken"}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	payload := map[string]any{
		"object_attributes": map[string]any{
			"iid": 1, "action": "open",
			"last_commit": map[string]any{"id": "abc"},
		},
		"project": map[string]any{"id": 1, "name": "p"},
	}
	req := httptest.NewRequest("POST", "/api/v1/webhooks/gitlab?component_id=gl-src", reviewJSON(payload))
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	req.Header.Set("X-Gitlab-Token", "wrongtoken")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d", w.Code)
	}
}

// --- Review job management tests ---

func TestReviewHandler_EnqueueReview_Success(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	body := map[string]any{
		"component_id": "src-1",
		"owner":        "org",
		"repo":         "repo",
		"pr_number":    10,
		"head_sha":     "abcdef",
		"platform":     "github",
	}
	req := httptest.NewRequest("POST", "/api/v1/reviews", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var job review.ReviewJob
	json.NewDecoder(w.Body).Decode(&job)
	if job.PRNumber != 10 {
		t.Errorf("pr_number = %d, want 10", job.PRNumber)
	}
}

func TestReviewHandler_EnqueueReview_MissingFields(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	body := map[string]any{"component_id": "src-1"} // missing required fields
	req := httptest.NewRequest("POST", "/api/v1/reviews", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_EnqueueReview_InvalidJSON(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	req := httptest.NewRequest("POST", "/api/v1/reviews", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_EnqueueReview_Duplicate(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	body := map[string]any{
		"component_id": "s", "owner": "o", "repo": "r",
		"pr_number": 5, "head_sha": "sha5", "platform": "github",
	}
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/reviews", reviewJSON(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	first := send()
	if first.Code != http.StatusCreated {
		t.Fatalf("first: expected 201, got %d", first.Code)
	}
	second := send()
	if second.Code != http.StatusConflict {
		t.Errorf("duplicate: expected 409, got %d", second.Code)
	}
}

func TestReviewHandler_EnqueueReview_ServiceUnavailable(t *testing.T) {
	services := &core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/reviews", reviewJSON(map[string]any{}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestReviewHandler_ListReviews_Empty(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	req := httptest.NewRequest("GET", "/api/v1/reviews", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["count"].(float64) != 0 {
		t.Errorf("count = %v, want 0", resp["count"])
	}
}

func TestReviewHandler_ListReviews_WithFilter(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	// Enqueue one GitHub job.
	enqReq := httptest.NewRequest("POST", "/api/v1/reviews", reviewJSON(map[string]any{
		"component_id": "s", "owner": "o", "repo": "r",
		"pr_number": 1, "head_sha": "sha1", "platform": "github",
	}))
	enqW := httptest.NewRecorder()
	mux.ServeHTTP(enqW, enqReq)

	// List all.
	req := httptest.NewRequest("GET", "/api/v1/reviews?platform=github", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", resp["count"])
	}
}

func TestReviewHandler_GetReview_Success(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	// Enqueue then get by ID.
	enqReq := httptest.NewRequest("POST", "/api/v1/reviews", reviewJSON(map[string]any{
		"component_id": "s", "owner": "o", "repo": "r",
		"pr_number": 2, "head_sha": "sha2", "platform": "github",
	}))
	enqW := httptest.NewRecorder()
	mux.ServeHTTP(enqW, enqReq)

	var created review.ReviewJob
	json.NewDecoder(enqW.Body).Decode(&created)

	req := httptest.NewRequest("GET", "/api/v1/reviews/"+created.ID, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var job review.ReviewJob
	json.NewDecoder(w.Body).Decode(&job)
	if job.ID != created.ID {
		t.Errorf("id = %s, want %s", job.ID, created.ID)
	}
}

func TestReviewHandler_GetReview_NotFound(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	req := httptest.NewRequest("GET", "/api/v1/reviews/nonexistent-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// --- GitHub PR operation tests ---

func TestReviewHandler_GitHubGetPR_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{
		pr: &githubadapter.PRInfo{Number: 1, Title: "test PR", Author: "alice"},
	}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1?owner=org&repo=repo", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GitHubGetPR_MissingOwnerRepo(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1", nil) // no owner/repo
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubGetPR_NotFound(t *testing.T) {
	// No adapter registered → 404.
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/unknown-src/pulls/1?owner=o&repo=r", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubGetPRDiff_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{diff: "diff --git a/file.go b/file.go"}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1/diff?owner=org&repo=repo", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GitHubGetPRDiff_MissingOwnerRepo(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls/1/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubListPRs_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{
		prs: []*githubadapter.PRInfo{{Number: 1, Title: "PR1"}},
	}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
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

func TestReviewHandler_GitHubListPRs_MissingOwnerRepo(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/github/gh-src/pulls", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubPostComment_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})

	body := map[string]any{"owner": "org", "repo": "repo", "body": "LGTM!"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/comments", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GitHubPostComment_MissingBody(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo"} // missing body
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/comments", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubRequestChanges_Success(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo", "body": "Needs work"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/reviews", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// --- GitLab MR operation tests ---

func TestReviewHandler_GitLabGetMR_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{
		mr: &gitlabadapter.MRInfo{IID: 3, Title: "Fix bug", Author: "bob"},
	}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GitLabGetMRDiff_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{diff: "diff --git a/f.go b/f.go"}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GitLabListMRs_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{
		mrs: []*gitlabadapter.MRInfo{{IID: 1, Title: "MR1"}},
	}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
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

func TestReviewHandler_GitLabPostNote_Success(t *testing.T) {
	glAdapter := &mockGitLabAdapter{}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})

	body := map[string]any{"body": "LGTM"}
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/notes", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GitLabPostNote_MissingBody(t *testing.T) {
	glAdapter := &mockGitLabAdapter{}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	body := map[string]any{} // missing body
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/notes", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabGetMR_NotFound(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/gitlab/unknown-src/projects/proj1/mrs/1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabWebhook_BadJSON(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/webhooks/gitlab", strings.NewReader("{bad"))
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubRequestChanges_MissingFields(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org"} // missing repo + body
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/reviews", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubRequestChanges_InvalidNumber(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/abc/reviews", reviewJSON(map[string]any{}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric PR number, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubRequestChanges_AdapterError(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{requestChgErr: fmt.Errorf("api error")}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo", "body": "Needs work"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/reviews", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on adapter error, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubPostComment_InvalidNumber(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/zero/comments", reviewJSON(map[string]any{}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric PR number, got %d", w.Code)
	}
}

func TestReviewHandler_GitHubPostComment_AdapterError(t *testing.T) {
	ghAdapter := &mockGitHubAdapter{commentErr: fmt.Errorf("api error")}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gh-src", ghAdapter)
	})
	body := map[string]any{"owner": "org", "repo": "repo", "body": "LGTM"}
	req := httptest.NewRequest("POST", "/api/v1/github/gh-src/pulls/1/comments", reviewJSON(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on adapter error, got %d", w.Code)
	}
}

// setupReviewServerNoService creates a test server without a review service (services.Review == nil).
func setupReviewServerNoService(t *testing.T) (*api.Server, *http.ServeMux) {
	t.Helper()
	services := &core.Services{
		Config:   &config.Config{},
		Adapters: adapters.NewRegistry(),
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

func TestReviewHandler_ListReviews_ServiceUnavailable(t *testing.T) {
	_, mux := setupReviewServerNoService(t)
	req := httptest.NewRequest("GET", "/api/v1/reviews", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestReviewHandler_ListReviews_WithLimit(t *testing.T) {
	_, mux := setupReviewServer(t, nil)

	// Enqueue a job so we have something to list.
	enqReq := httptest.NewRequest("POST", "/api/v1/reviews", reviewJSON(map[string]any{
		"component_id": "s", "owner": "o", "repo": "r",
		"pr_number": 99, "head_sha": "shaX", "platform": "github",
	}))
	mux.ServeHTTP(httptest.NewRecorder(), enqReq)

	req := httptest.NewRequest("GET", "/api/v1/reviews?limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReviewHandler_GetReview_ServiceUnavailable(t *testing.T) {
	_, mux := setupReviewServerNoService(t)
	req := httptest.NewRequest("GET", "/api/v1/reviews/some-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabGetMR_InvalidIID(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/abc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabGetMRDiff_InvalidIID(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/bad/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabGetMRDiff_AdapterError(t *testing.T) {
	glAdapter := &mockGitLabAdapter{diffErr: fmt.Errorf("connection refused")}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/diff", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabListMRs_AdapterError(t *testing.T) {
	glAdapter := &mockGitLabAdapter{listMRsErr: fmt.Errorf("api error")}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	req := httptest.NewRequest("GET", "/api/v1/gitlab/gl-src/projects/proj1/mrs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabPostNote_InvalidIID(t *testing.T) {
	_, mux := setupReviewServer(t, nil)
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/bad/notes", reviewJSON(map[string]any{"body": "x"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReviewHandler_GitLabPostNote_AdapterError(t *testing.T) {
	glAdapter := &mockGitLabAdapter{noteErr: fmt.Errorf("api error")}
	_, mux := setupReviewServer(t, func(r *adapters.Registry) {
		r.Register("gl-src", glAdapter)
	})
	req := httptest.NewRequest("POST", "/api/v1/gitlab/gl-src/projects/proj1/mrs/3/notes", reviewJSON(map[string]any{"body": "LGTM"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// Ensure fmt is used.
var _ = fmt.Sprintf
