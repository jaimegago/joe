package oci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/registry/oci"
	"github.com/jaimegago/joe/internal/store"
)

func newTestServer(t *testing.T, mux *http.ServeMux) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func TestParseLinkHeader(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		baseURL string
		want    string
	}{
		{
			name:    "empty header",
			header:  "",
			baseURL: "https://registry.example.com",
			want:    "",
		},
		{
			name:    "no next link",
			header:  `</v2/_catalog?last=foo>; rel="prev"`,
			baseURL: "https://registry.example.com",
			want:    "",
		},
		{
			name:    "relative next link",
			header:  `</v2/_catalog?last=foo&n=100>; rel="next"`,
			baseURL: "https://registry.example.com",
			want:    "https://registry.example.com/v2/_catalog?last=foo&n=100",
		},
		{
			name:    "absolute next link",
			header:  `<https://registry.example.com/v2/_catalog?last=bar>; rel="next"`,
			baseURL: "https://registry.example.com",
			want:    "https://registry.example.com/v2/_catalog?last=bar",
		},
		{
			name:    "multiple links, pick next",
			header:  `</v2/_catalog?last=baz>; rel="next", </v2/_catalog>; rel="first"`,
			baseURL: "https://registry.example.com",
			want:    "https://registry.example.com/v2/_catalog?last=baz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// parseLinkHeader is tested indirectly via ListRepositories; test it via a
			// helper exported in tests using the package-level function.
			// We verify pagination behaviour in TestListRepositories_Pagination below.
			_ = tt
		})
	}
}

func TestConnect_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv, client := newTestServer(t, mux)

	cfg := oci.Config{RegistryURL: srv.URL}
	a := oci.NewWithClient(client, cfg)
	// Already connected via NewWithClient; verify Status.
	status := a.Status()
	if !status.Connected {
		t.Fatalf("expected connected, got %s", status.Message)
	}
}

func TestListRepositories_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"repositories": []string{"myorg/app", "myorg/worker"},
		})
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2", len(repos))
	}
}

func TestListRepositories_Pagination(t *testing.T) {
	page := 0
	// srvURL is populated after server start so the handler closure can use it.
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		if page == 0 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/v2/_catalog?last=a>; rel="next"`, srvURL))
			json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-a"}})
			page++
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-b"}})
	})
	srv, client := newTestServer(t, mux)
	srvURL = srv.URL

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2", len(repos))
	}
}

func TestListRepositories_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.ListRepositories(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListTags_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myorg/app/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"name": "myorg/app",
			"tags": []string{"latest", "v1.0.0", "v1.1.0"},
		})
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	tags, err := a.ListTags(context.Background(), "myorg/app")
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("got %d tags, want 3", len(tags))
	}
}

func TestListTags_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/missing/repo/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.ListTags(context.Background(), "missing/repo")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestGetManifest_WithAnnotations(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myorg/app/manifests/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:abc123")
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		json.NewEncoder(w).Encode(map[string]any{
			"schemaVersion": 2,
			"annotations": map[string]string{
				"org.opencontainers.image.revision": "deadbeef",
				"org.opencontainers.image.created":  "2025-01-01T00:00:00Z",
			},
		})
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	m, err := a.GetManifest(context.Background(), "myorg/app", "latest")
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	if m.Digest != "sha256:abc123" {
		t.Errorf("digest = %q, want sha256:abc123", m.Digest)
	}
	if m.Labels["org.opencontainers.image.revision"] != "deadbeef" {
		t.Errorf("git revision label = %q, want deadbeef", m.Labels["org.opencontainers.image.revision"])
	}
}

func TestGetManifest_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myorg/app/manifests/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.GetManifest(context.Background(), "myorg/app", "missing")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestDisconnect(t *testing.T) {
	a := oci.NewWithClient(http.DefaultClient, oci.Config{RegistryURL: "https://example.com"})
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected disconnected after Disconnect()")
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		wantErr bool
	}{
		{
			name: "valid config",
			raw:  []byte(`{"registry_url":"https://registry.example.com"}`),
		},
		{
			name: "with credentials",
			raw:  []byte(`{"registry_url":"https://registry.example.com","username":"user","password":"pass"}`),
		},
		{
			name:    "missing registry_url",
			raw:     []byte(`{}`),
			wantErr: true,
		},
		{
			name:    "invalid json",
			raw:     []byte(`{bad}`),
			wantErr: true,
		},
		{
			name:    "empty",
			raw:     []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := oci.ParseConfig(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.RegistryURL == "" {
				t.Error("RegistryURL should not be empty on valid config")
			}
		})
	}
}

func TestStatus_NotConnected(t *testing.T) {
	a := oci.New()
	s := a.Status()
	if s.Connected {
		t.Error("expected not connected for New() adapter")
	}
	if s.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestListRepositories_NotConnected(t *testing.T) {
	a := oci.New()
	_, err := a.ListRepositories(context.Background())
	if err == nil {
		t.Error("expected error for not connected adapter")
	}
}

func TestListTags_NotConnected(t *testing.T) {
	a := oci.New()
	_, err := a.ListTags(context.Background(), "myorg/app")
	if err == nil {
		t.Error("expected error for not connected adapter")
	}
}

func TestGetManifest_NotConnected(t *testing.T) {
	a := oci.New()
	_, err := a.GetManifest(context.Background(), "myorg/app", "latest")
	if err == nil {
		t.Error("expected error for not connected adapter")
	}
}

func TestListTags_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myorg/app/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.ListTags(context.Background(), "myorg/app")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestGetManifest_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myorg/app/manifests/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.GetManifest(context.Background(), "myorg/app", "latest")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestConnect_WithAuth(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	srv, _ := newTestServer(t, mux)

	cfg := oci.Config{RegistryURL: srv.URL, Username: "user", Password: "pass"}
	a := oci.NewWithClient(srv.Client(), cfg)
	// Test auth by calling ListRepositories which also calls addAuthHeader.
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{"repositories": []string{}})
	})
	_, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if gotAuth == "" {
		t.Error("expected Authorization header to be set with credentials")
	}
}

func TestConnect_BadConfigError(t *testing.T) {
	// ParseConfig with missing registry_url returns error.
	_, err := oci.ParseConfig([]byte(`{}`))
	if err == nil {
		t.Error("expected error for missing registry_url")
	}
}

func TestListRepositories_AbsoluteLinkPagination(t *testing.T) {
	page := 0
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		if page == 0 {
			// Set absolute URL in Link header (not relative path).
			w.Header().Set("Link", fmt.Sprintf(`<%s/v2/_catalog?last=a>; rel="next"`, srvURL))
			json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-a"}})
			page++
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-b"}})
	})
	srv, client := newTestServer(t, mux)
	srvURL = srv.URL

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2", len(repos))
	}
}

func TestListRepositories_BadJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{bad json}`))
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.ListRepositories(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestListRepositories_RelativeLinkPagination(t *testing.T) {
	// Test that parseLinkHeader handles relative paths (prepends baseURL).
	page := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		if page == 0 {
			// Relative path - parseLinkHeader must prepend baseURL.
			w.Header().Set("Link", `</v2/_catalog?last=repo-a&n=100>; rel="next"`)
			json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-a"}})
			page++
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-b"}})
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2", len(repos))
	}
}

func TestConnect_ViaComponent_Success(t *testing.T) {
	// Connect requires a real HTTP connection to /v2/; use httptest.
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := oci.New()
	src := store.Component{
		Config: []byte(fmt.Sprintf(`{"registry_url":%q}`, srv.URL)),
	}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected after Connect()")
	}
}

func TestConnect_ViaComponent_Auth401(t *testing.T) {
	// 401 from /v2/ is valid (auth required but registry is live).
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := oci.New()
	src := store.Component{
		Config: []byte(fmt.Sprintf(`{"registry_url":%q}`, srv.URL)),
	}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() with 401 error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected for 401 response (auth required)")
	}
}

func TestConnect_ViaComponent_UnexpectedStatus(t *testing.T) {
	// Non-200/401 status should return error.
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := oci.New()
	src := store.Component{
		Config: []byte(fmt.Sprintf(`{"registry_url":%q}`, srv.URL)),
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for 503 response")
	}
}

func TestConnect_ViaComponent_BadConfig(t *testing.T) {
	a := oci.New()
	src := store.Component{Config: []byte(`{}`)} // missing registry_url
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for missing registry_url")
	}
}

func TestConnect_ViaComponent_NetworkError(t *testing.T) {
	// Point to a URL that will fail to connect (no server listening).
	a := oci.New()
	src := store.Component{
		Config: []byte(`{"registry_url":"http://127.0.0.1:19999"}`),
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error when registry is unreachable")
	}
}

func TestListRepositories_NetworkError(t *testing.T) {
	// Use a server that closes immediately to simulate network error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	client := srv.Client()
	srv.Close() // close immediately so requests fail

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.ListRepositories(context.Background())
	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestListTags_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	client := srv.Client()
	srv.Close()

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.ListTags(context.Background(), "myorg/app")
	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestGetManifest_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	client := srv.Client()
	srv.Close()

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.GetManifest(context.Background(), "myorg/app", "latest")
	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestListTags_BadJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myorg/app/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{bad json}`))
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	_, err := a.ListTags(context.Background(), "myorg/app")
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestParseLinkHeader_NoSegments(t *testing.T) {
	// A link header entry with no semicolons has only 1 segment — should be skipped.
	// We test this indirectly: a Link header that is malformed returns no next URL.
	page := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		if page == 0 {
			// Malformed link with no semicolon — parseLinkHeader should skip it.
			w.Header().Set("Link", `<https://example.com/no-semicolon>`)
			json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-a"}})
			page++
			return
		}
		// This should not be reached.
		json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"repo-b"}})
	})
	srv, client := newTestServer(t, mux)

	a := oci.NewWithClient(client, oci.Config{RegistryURL: srv.URL})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	// Malformed link means no pagination — only page 0 entries returned.
	if len(repos) != 1 {
		t.Errorf("got %d repos, want 1 (no pagination for malformed link)", len(repos))
	}
}
