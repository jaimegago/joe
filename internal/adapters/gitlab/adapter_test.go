package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/store"
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

func TestAdapter_PostNote_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	err := adapter.PostNote(context.Background(), "42", 7, "comment")
	if err == nil {
		t.Error("expected error for 403, got nil")
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

func TestAdapter_GetMRDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/42/merge_requests/7/diffs" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"diff": "@@ -1 +1 @@\n-old\n+new\n", "old_path": "file.go", "new_path": "file.go"},
			{"diff": "", "old_path": "no_change.go", "new_path": "no_change.go"}
		]`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	diff, err := adapter.GetMRDiff(context.Background(), "42", 7)
	if err != nil {
		t.Fatalf("GetMRDiff: %v", err)
	}
	if !strings.Contains(diff, "file.go") {
		t.Errorf("expected diff to contain file.go, got %q", diff)
	}
}

func TestAdapter_GetMRDiff_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.GetMRDiff(context.Background(), "42", 99)
	if err == nil {
		t.Error("expected error for 404 diff, got nil")
	}
}

func TestAdapter_RequestChanges(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		capturedBody = payload["body"]
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	if err := adapter.RequestChanges(context.Background(), "42", 7, "please fix the issue"); err != nil {
		t.Fatalf("RequestChanges: %v", err)
	}
	if !strings.Contains(capturedBody, "Changes Requested") {
		t.Errorf("expected body to contain Changes Requested prefix, got %q", capturedBody)
	}
	if !strings.Contains(capturedBody, "please fix the issue") {
		t.Errorf("expected body to contain original message, got %q", capturedBody)
	}
}

func TestAdapter_ListMRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"iid": 1, "title": "MR one", "state": "opened", "sha": "aaa", "source_branch": "feat/a", "target_branch": "main", "web_url": "https://gl/1", "author": {"username": "alice"}},
			{"iid": 2, "title": "MR two", "state": "opened", "sha": "bbb", "source_branch": "feat/b", "target_branch": "main", "web_url": "https://gl/2", "author": {"username": "bob"}}
		]`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	mrs, err := adapter.ListMRs(context.Background(), "42", "opened")
	if err != nil {
		t.Fatalf("ListMRs: %v", err)
	}
	if len(mrs) != 2 {
		t.Errorf("len(mrs) = %d, want 2", len(mrs))
	}
	if mrs[0].Title != "MR one" {
		t.Errorf("mrs[0].Title = %q, want MR one", mrs[0].Title)
	}
	if mrs[0].ProjectID != "42" {
		t.Errorf("ProjectID = %q, want 42", mrs[0].ProjectID)
	}
}

func TestAdapter_ListMRs_DefaultState(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.ListMRs(context.Background(), "42", "")
	if err != nil {
		t.Fatalf("ListMRs: %v", err)
	}
	if !strings.Contains(capturedURL, "state=opened") {
		t.Errorf("expected default state=opened in URL, got %q", capturedURL)
	}
}

func TestAdapter_ListMRs_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Token: "tok"}
	adapter := NewWithConfig(cfg, srv.Client())

	_, err := adapter.ListMRs(context.Background(), "42", "opened")
	if err == nil {
		t.Error("expected error for 500, got nil")
	}
}

func TestAdapter_Status(t *testing.T) {
	a := NewWithConfig(Config{BaseURL: "https://gitlab.com", Token: "tok"}, nil)
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
	a := NewWithConfig(Config{BaseURL: "https://gitlab.com", Token: "tok"}, nil)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected disconnected after Disconnect()")
	}
}

func TestAdapter_WebhookSecret(t *testing.T) {
	cfg := Config{BaseURL: "https://gitlab.com", Token: "tok", WebhookSecret: "gl-secret"}
	a := NewWithConfig(cfg, nil)
	if a.WebhookSecret() != "gl-secret" {
		t.Errorf("WebhookSecret() = %q, want gl-secret", a.WebhookSecret())
	}
}

func TestAdapter_Connect_Success(t *testing.T) {
	a := New()
	src := store.Component{
		Config: []byte(`{"token":"tok","base_url":"https://gitlab.com"}`),
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
	src := store.Component{Config: []byte(`{bad json`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for bad JSON config")
	}
}

func TestAdapter_Connect_MissingToken(t *testing.T) {
	a := New()
	src := store.Component{Config: []byte(`{"base_url":"https://gitlab.com"}`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for missing token")
	}
}

// captureTokenServer returns an httptest server that records the PRIVATE-TOKEN
// header of the most recent request and answers a minimal MR payload.
func captureTokenServer(t *testing.T, gotToken *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"iid":7,"title":"t","state":"opened","sha":"a","source_branch":"x","target_branch":"main","web_url":"u","author":{"username":"alice"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// D-0026 unit 2 (A003-W1): a component config with no discriminator still connects
// via the static provider, and the legacy "token" field is preserved as the
// per-request PRIVATE-TOKEN. This is the default-static, no-behavior-regression path.
func TestConnect_NoDiscriminator_PreservesLegacyToken(t *testing.T) {
	var gotToken string
	srv := captureTokenServer(t, &gotToken)

	a := New()
	cfg := fmt.Sprintf(`{"token":"legacy-tok","base_url":%q}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "gl-1", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := a.GetMR(context.Background(), "42", 7); err != nil {
		t.Fatalf("GetMR: %v", err)
	}
	if gotToken != "legacy-tok" {
		t.Errorf("PRIVATE-TOKEN = %q, want %q", gotToken, "legacy-tok")
	}
}

// D-0026 unit 2 (A003-W1): GitLab resolves its token through the static provider's
// inline value, which overrides the legacy field, proving the seam is exercised
// rather than bypassed.
func TestConnect_StaticProvider_ResolvesInlineValue(t *testing.T) {
	var gotToken string
	srv := captureTokenServer(t, &gotToken)

	a := New()
	cfg := fmt.Sprintf(`{"token":"placeholder","base_url":%q,"credential_provider":"static","value":"resolved-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "gl-2", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := a.GetMR(context.Background(), "42", 7); err != nil {
		t.Fatalf("GetMR: %v", err)
	}
	if gotToken != "resolved-tok" {
		t.Errorf("PRIVATE-TOKEN = %q, want %q (provider value should win)", gotToken, "resolved-tok")
	}
}

// D-0026 unit 2 (A003-W1): GitLab resolves its token from a named environment
// variable via the static provider, proving resolution (not just config decode)
// feeds the per-request token.
func TestConnect_StaticProvider_ResolvesEnvVar(t *testing.T) {
	var gotToken string
	srv := captureTokenServer(t, &gotToken)

	t.Setenv("JOE_TEST_GL_TOKEN", "env-tok")
	a := New()
	cfg := fmt.Sprintf(`{"token":"placeholder","base_url":%q,"credential_provider":"static","env_var":"JOE_TEST_GL_TOKEN"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "gl-3", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := a.GetMR(context.Background(), "42", 7); err != nil {
		t.Fatalf("GetMR: %v", err)
	}
	if gotToken != "env-tok" {
		t.Errorf("PRIVATE-TOKEN = %q, want %q", gotToken, "env-tok")
	}
}

// D-0026 unit 2 (A003-W1) break-test: a Resolve failure (named env var unset)
// surfaces through Connect's normal error path, and the credential-bearing config
// value never appears in the error.
func TestConnect_ResolveFailure_SurfacesWithoutCredential(t *testing.T) {
	a := New()
	cfg := `{"token":"SUPERSECRET","base_url":"https://gitlab.com","credential_provider":"static","env_var":"JOE_DEFINITELY_UNSET_VAR_XYZ"}`
	err := a.Connect(context.Background(), store.Component{ID: "gl-4", Config: []byte(cfg)})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("credential leaked into error: %v", err)
	}
	if a.Status().Connected {
		t.Error("adapter should not be connected after a resolve failure")
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
			wantURL: "https://gitlab.com",
		},
		{
			name:    "valid with custom base_url",
			raw:     `{"token":"tok","base_url":"https://gitlab.example.com"}`,
			wantURL: "https://gitlab.example.com",
		},
		{
			name:    "missing token",
			raw:     `{"base_url":"https://gitlab.com"}`,
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
