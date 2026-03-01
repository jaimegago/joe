package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdapter_GetMR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/42/merge_requests/7" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("PRIVATE-TOKEN") == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"iid": 7,
			"title": "Add caching layer",
			"state": "opened",
			"sha": "bbb222",
			"source_branch": "feature/cache",
			"target_branch": "main",
			"web_url": "https://gitlab.com/group/project/-/merge_requests/7",
			"author": {"username": "bob"}
		}`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "mytoken"}
	adapter := NewWithConfig(cfg, srv.Client())

	mr, err := adapter.GetMR(context.Background(), "42", 7)
	if err != nil {
		t.Fatalf("GetMR: %v", err)
	}
	if mr.Title != "Add caching layer" {
		t.Errorf("title: got %q, want %q", mr.Title, "Add caching layer")
	}
	if mr.SHA != "bbb222" {
		t.Errorf("SHA: got %q, want %q", mr.SHA, "bbb222")
	}
	if mr.Author != "bob" {
		t.Errorf("author: got %q, want %q", mr.Author, "bob")
	}
}

func TestAdapter_PostNote(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"id":1,"body":"note"}`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	if err := adapter.PostNote(context.Background(), "42", 7, "LGTM!"); err != nil {
		t.Fatalf("PostNote: %v", err)
	}
	if capturedBody != "LGTM!" {
		t.Errorf("body: got %q, want %q", capturedBody, "LGTM!")
	}
}

func TestAdapter_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "bad"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.GetMR(context.Background(), "42", 1)
	if err == nil {
		t.Error("expected error for 401 response")
	}
}
