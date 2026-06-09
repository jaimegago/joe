package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// =========================
// GitHub PR operations
// =========================

func TestGitHubGetPR_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GitHubGetPR: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 42,
			"title":  "Fix the bug",
			"state":  "open",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	pr, err := c.GitHubGetPR(context.Background(), "gh-src", "myorg", "myrepo", 42)
	if err != nil {
		t.Fatalf("GitHubGetPR() error: %v", err)
	}
	if pr == nil {
		t.Fatal("GitHubGetPR(): got nil PR")
	}
}

func TestGitHubGetPR_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "message": "pr not found"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitHubGetPR(context.Background(), "gh-src", "org", "repo", 1)
	if err == nil {
		t.Fatal("GitHubGetPR(): expected error for 404 response")
	}
}

func TestGitHubGetPRDiff_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"diff": "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	diff, err := c.GitHubGetPRDiff(context.Background(), "gh-src", "myorg", "myrepo", 42)
	if err != nil {
		t.Fatalf("GitHubGetPRDiff() error: %v", err)
	}
	if !strings.Contains(diff, "main.go") {
		t.Errorf("GitHubGetPRDiff(): unexpected diff %q", diff)
	}
}

func TestGitHubGetPRDiff_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "diff failed"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitHubGetPRDiff(context.Background(), "gh-src", "org", "repo", 1)
	if err == nil {
		t.Fatal("GitHubGetPRDiff(): expected error for 500 response")
	}
}

func TestGitHubListPRs_Success(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"prs": []map[string]any{
				{"number": 1, "title": "first PR", "state": "open"},
			},
			"count": 1,
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	prs, err := c.GitHubListPRs(context.Background(), "gh-src", "myorg", "myrepo", "open")
	if err != nil {
		t.Fatalf("GitHubListPRs() error: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("GitHubListPRs(): got %d PRs, want 1", len(prs))
	}
	assertContains(t, capturedURI, "owner=myorg")
	assertContains(t, capturedURI, "repo=myrepo")
	assertContains(t, capturedURI, "state=open")
}

func TestGitHubListPRs_NoState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"prs":   []map[string]any{},
			"count": 0,
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitHubListPRs(context.Background(), "gh-src", "org", "repo", "")
	if err != nil {
		t.Fatalf("GitHubListPRs() no-state error: %v", err)
	}
}

func TestGitHubListPRs_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "gh error"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitHubListPRs(context.Background(), "gh-src", "org", "repo", "open")
	if err == nil {
		t.Fatal("GitHubListPRs(): expected error for 500 response")
	}
}

func TestGitHubPostComment_Success(t *testing.T) {
	var capturedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("GitHubPostComment: expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.GitHubPostComment(context.Background(), "gh-src", "myorg", "myrepo", 42, "LGTM!")
	if err != nil {
		t.Fatalf("GitHubPostComment() error: %v", err)
	}
	if capturedBody["body"] != "LGTM!" {
		t.Errorf("GitHubPostComment(): unexpected body %q", capturedBody["body"])
	}
}

func TestGitHubPostComment_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "forbidden", "message": "no write access"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.GitHubPostComment(context.Background(), "gh-src", "org", "repo", 1, "comment")
	if err == nil {
		t.Fatal("GitHubPostComment(): expected error for 403 response")
	}
}

func TestGitHubRequestChanges_Success(t *testing.T) {
	var capturedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("GitHubRequestChanges: expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.GitHubRequestChanges(context.Background(), "gh-src", "myorg", "myrepo", 42, "Needs tests")
	if err != nil {
		t.Fatalf("GitHubRequestChanges() error: %v", err)
	}
	if capturedBody["body"] != "Needs tests" {
		t.Errorf("GitHubRequestChanges(): unexpected body %q", capturedBody["body"])
	}
}

func TestGitHubRequestChanges_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "review failed"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.GitHubRequestChanges(context.Background(), "gh-src", "org", "repo", 1, "needs work")
	if err == nil {
		t.Fatal("GitHubRequestChanges(): expected error for 500 response")
	}
}

// =========================
// GitLab MR operations
// =========================

func TestGitLabGetMR_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GitLabGetMR: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"iid":   7,
			"title": "Add feature",
			"state": "opened",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	mr, err := c.GitLabGetMR(context.Background(), "gl-src", "group/project", 7)
	if err != nil {
		t.Fatalf("GitLabGetMR() error: %v", err)
	}
	if mr == nil {
		t.Fatal("GitLabGetMR(): got nil MR")
	}
}

func TestGitLabGetMR_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "message": "mr not found"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitLabGetMR(context.Background(), "gl-src", "project", 99)
	if err == nil {
		t.Fatal("GitLabGetMR(): expected error for 404 response")
	}
}

func TestGitLabGetMRDiff_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"diff": "--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	diff, err := c.GitLabGetMRDiff(context.Background(), "gl-src", "group/project", 7)
	if err != nil {
		t.Fatalf("GitLabGetMRDiff() error: %v", err)
	}
	if !strings.Contains(diff, "file.go") {
		t.Errorf("GitLabGetMRDiff(): unexpected diff %q", diff)
	}
}

func TestGitLabGetMRDiff_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "diff failed"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitLabGetMRDiff(context.Background(), "gl-src", "project", 1)
	if err == nil {
		t.Fatal("GitLabGetMRDiff(): expected error for 500 response")
	}
}

func TestGitLabListMRs_Success(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mrs": []map[string]any{
				{"iid": 1, "title": "First MR", "state": "opened"},
			},
			"count": 1,
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	mrs, err := c.GitLabListMRs(context.Background(), "gl-src", "group/project", "opened")
	if err != nil {
		t.Fatalf("GitLabListMRs() error: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("GitLabListMRs(): got %d MRs, want 1", len(mrs))
	}
	assertContains(t, capturedURI, "state=opened")
}

func TestGitLabListMRs_NoState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mrs":   []map[string]any{},
			"count": 0,
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitLabListMRs(context.Background(), "gl-src", "project", "")
	if err != nil {
		t.Fatalf("GitLabListMRs() no-state error: %v", err)
	}
}

func TestGitLabListMRs_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "gl error"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GitLabListMRs(context.Background(), "gl-src", "project", "opened")
	if err == nil {
		t.Fatal("GitLabListMRs(): expected error for 500 response")
	}
}

func TestGitLabPostNote_Success(t *testing.T) {
	var capturedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("GitLabPostNote: expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.GitLabPostNote(context.Background(), "gl-src", "group/project", 7, "Looks good!")
	if err != nil {
		t.Fatalf("GitLabPostNote() error: %v", err)
	}
	if capturedBody["body"] != "Looks good!" {
		t.Errorf("GitLabPostNote(): unexpected body %q", capturedBody["body"])
	}
}

func TestGitLabPostNote_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "forbidden", "message": "no access"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.GitLabPostNote(context.Background(), "gl-src", "project", 1, "comment")
	if err == nil {
		t.Fatal("GitLabPostNote(): expected error for 403 response")
	}
}
