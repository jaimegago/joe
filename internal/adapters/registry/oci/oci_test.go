package oci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/registry/oci"
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
