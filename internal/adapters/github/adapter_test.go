package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdapter_GetPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/pulls/1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"number": 1,
			"title": "Fix payment timeout",
			"state": "open",
			"html_url": "https://github.com/org/repo/pull/1",
			"head": {"sha": "abc123", "ref": "fix/timeout"},
			"base": {"sha": "def456", "ref": "main"},
			"user": {"login": "alice"}
		}`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	pr, err := adapter.GetPR(context.Background(), "org", "repo", 1)
	if err != nil {
		t.Fatalf("GetPR: %v", err)
	}
	if pr.Title != "Fix payment timeout" {
		t.Errorf("title: got %q, want %q", pr.Title, "Fix payment timeout")
	}
	if pr.HeadSHA != "abc123" {
		t.Errorf("head SHA: got %q, want %q", pr.HeadSHA, "abc123")
	}
	if pr.Author != "alice" {
		t.Errorf("author: got %q, want %q", pr.Author, "alice")
	}
}

func TestAdapter_GetPRDiff(t *testing.T) {
	diffContent := "diff --git a/main.go b/main.go\n+func hello() {}\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.v3.diff" {
			http.Error(w, "wrong accept", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(diffContent))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	diff, err := adapter.GetPRDiff(context.Background(), "org", "repo", 1)
	if err != nil {
		t.Fatalf("GetPRDiff: %v", err)
	}
	if diff != diffContent {
		t.Errorf("diff: got %q, want %q", diff, diffContent)
	}
}

func TestAdapter_PostComment(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		capturedBody = payload["body"]
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	if err := adapter.PostComment(context.Background(), "org", "repo", 1, "LGTM!"); err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if capturedBody != "LGTM!" {
		t.Errorf("body: got %q, want %q", capturedBody, "LGTM!")
	}
}

func TestAdapter_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "bad-token"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.GetPR(context.Background(), "org", "repo", 1)
	if err == nil {
		t.Error("expected error for 401 response")
	}
}
