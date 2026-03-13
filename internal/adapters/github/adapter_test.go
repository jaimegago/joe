package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/store"
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

func TestAdapter_GetPRDiff_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.GetPRDiff(context.Background(), "org", "repo", 99)
	if err == nil {
		t.Error("expected error for 404 diff, got nil")
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

func TestAdapter_PostComment_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	err := adapter.PostComment(context.Background(), "org", "repo", 1, "comment")
	if err == nil {
		t.Error("expected error for 403, got nil")
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

func TestAdapter_RequestChanges(t *testing.T) {
	var capturedPath string
	var capturedEvent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		capturedEvent = payload["event"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	if err := adapter.RequestChanges(context.Background(), "org", "repo", 42, "needs fixes"); err != nil {
		t.Fatalf("RequestChanges: %v", err)
	}
	if capturedPath != "/repos/org/repo/pulls/42/reviews" {
		t.Errorf("path: got %q, want /repos/org/repo/pulls/42/reviews", capturedPath)
	}
	if capturedEvent != "REQUEST_CHANGES" {
		t.Errorf("event: got %q, want REQUEST_CHANGES", capturedEvent)
	}
}

func TestAdapter_ListPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number": 1, "title": "PR one", "state": "open", "head": {"sha": "aaa", "ref": "feat/a"}, "base": {"sha": "bbb", "ref": "main"}, "user": {"login": "alice"}},
			{"number": 2, "title": "PR two", "state": "open", "head": {"sha": "ccc", "ref": "feat/b"}, "base": {"sha": "ddd", "ref": "main"}, "user": {"login": "bob"}}
		]`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	prs, err := adapter.ListPRs(context.Background(), "org", "repo", "open")
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 2 {
		t.Errorf("len(prs) = %d, want 2", len(prs))
	}
	if prs[0].Title != "PR one" {
		t.Errorf("prs[0].Title = %q, want PR one", prs[0].Title)
	}
	if prs[0].RepoOwner != "org" {
		t.Errorf("RepoOwner = %q, want org", prs[0].RepoOwner)
	}
}

func TestAdapter_ListPRs_DefaultState(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.ListPRs(context.Background(), "org", "repo", "")
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if !strings.Contains(capturedURL, "state=open") {
		t.Errorf("expected default state=open in URL, got %q", capturedURL)
	}
}

func TestAdapter_ListPRs_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.ListPRs(context.Background(), "org", "repo", "open")
	if err == nil {
		t.Error("expected error for 500, got nil")
	}
}

func TestAdapter_Status(t *testing.T) {
	a := NewWithConfig(Config{BaseURL: "https://api.github.com", Token: "tok"}, nil)
	s := a.Status()
	if !s.Connected {
		t.Error("expected connected status")
	}
}

func TestAdapter_Status_NotConnected(t *testing.T) {
	a := New()
	s := a.Status()
	if s.Connected {
		t.Error("expected not connected")
	}
	if s.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	a := NewWithConfig(Config{BaseURL: "https://api.github.com", Token: "tok"}, nil)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected disconnected after Disconnect()")
	}
}

func TestAdapter_WebhookSecret(t *testing.T) {
	cfg := Config{BaseURL: "https://api.github.com", Token: "tok", WebhookSecret: "my-secret"}
	a := NewWithConfig(cfg, nil)
	if a.WebhookSecret() != "my-secret" {
		t.Errorf("WebhookSecret() = %q, want my-secret", a.WebhookSecret())
	}
}

func TestAdapter_Connect_Success(t *testing.T) {
	a := New()
	src := store.Source{
		Config: []byte(`{"token":"tok","base_url":"https://api.github.com"}`),
	}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected after Connect()")
	}
}

func TestAdapter_Connect_BadConfig(t *testing.T) {
	a := New()
	src := store.Source{Config: []byte(`{bad json`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for bad JSON config")
	}
}

func TestAdapter_Connect_MissingToken(t *testing.T) {
	a := New()
	src := store.Source{Config: []byte(`{"base_url":"https://api.github.com"}`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for missing token")
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		wantURL string
	}{
		{
			name:    "valid with token",
			raw:     `{"token":"tok"}`,
			wantURL: "https://api.github.com",
		},
		{
			name:    "valid with custom base_url",
			raw:     `{"token":"tok","base_url":"https://ghes.example.com"}`,
			wantURL: "https://ghes.example.com",
		},
		{
			name:    "missing token",
			raw:     `{"base_url":"https://api.github.com"}`,
			wantErr: true,
		},
		{
			name:    "empty config",
			raw:     ``,
			wantErr: true,
		},
		{
			name:    "invalid json",
			raw:     `{bad}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.BaseURL != tt.wantURL {
				t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, tt.wantURL)
			}
		})
	}
}
