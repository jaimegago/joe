package artifactory_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/registry/artifactory"
)

func newTestServer(t *testing.T, mux *http.ServeMux) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func TestConnect_Ping_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/system/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{BaseURL: srv.URL})
	if !a.Status().Connected {
		t.Fatal("expected connected status")
	}
}

func TestListRepositories_FilterDockerHelm(t *testing.T) {
	allRepos := []artifactory.Repository{
		{Key: "docker-local", PackageType: "Docker", Type: "LOCAL"},
		{Key: "helm-local", PackageType: "Helm", Type: "LOCAL"},
		{Key: "generic-local", PackageType: "Generic", Type: "LOCAL"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/repositories", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(allRepos)
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{BaseURL: srv.URL})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2 (Docker+Helm only)", len(repos))
	}
}

func TestListRepositories_KeyFilter(t *testing.T) {
	allRepos := []artifactory.Repository{
		{Key: "docker-local", PackageType: "Docker", Type: "LOCAL"},
		{Key: "helm-local", PackageType: "Helm", Type: "LOCAL"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/repositories", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(allRepos)
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{
		BaseURL:      srv.URL,
		Repositories: []string{"docker-local"},
	})
	repos, err := a.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 1 || repos[0].Key != "docker-local" {
		t.Errorf("expected only docker-local, got %v", repos)
	}
}

func TestListDockerTags_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/docker/docker-local/v2/myapp/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tags": []string{"latest", "v1.2.3"},
		})
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{BaseURL: srv.URL})
	tags, err := a.ListDockerTags(context.Background(), "docker-local", "myapp")
	if err != nil {
		t.Fatalf("ListDockerTags() error = %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("got %d tags, want 2", len(tags))
	}
}

func TestListDockerTags_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/docker/docker-local/v2/missing/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{BaseURL: srv.URL})
	_, err := a.ListDockerTags(context.Background(), "docker-local", "missing")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestGetArtifactInfo_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/storage/docker-local/myapp/latest/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(artifactory.ArtifactInfo{
			Repo:    "docker-local",
			Path:    "myapp/latest/manifest.json",
			Created: "2025-01-01T00:00:00.000Z",
		})
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{BaseURL: srv.URL})
	info, err := a.GetArtifactInfo(context.Background(), "docker-local", "myapp/latest/manifest.json")
	if err != nil {
		t.Fatalf("GetArtifactInfo() error = %v", err)
	}
	if info.Repo != "docker-local" {
		t.Errorf("Repo = %q, want docker-local", info.Repo)
	}
}

func TestGetArtifactInfo_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/storage/docker-local/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{BaseURL: srv.URL})
	_, err := a.GetArtifactInfo(context.Background(), "docker-local", "missing")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestAPIKeyAuthHeader(t *testing.T) {
	var gotAPIKey string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/repositories", func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-JFrog-Art-Api")
		json.NewEncoder(w).Encode([]artifactory.Repository{})
	})
	srv, client := newTestServer(t, mux)

	a := artifactory.NewWithClient(client, artifactory.Config{
		BaseURL: srv.URL,
		APIKey:  "my-secret-key",
	})
	a.ListRepositories(context.Background())

	if gotAPIKey != "my-secret-key" {
		t.Errorf("API key header = %q, want my-secret-key", gotAPIKey)
	}
}

func TestDisconnect(t *testing.T) {
	a := artifactory.NewWithClient(http.DefaultClient, artifactory.Config{BaseURL: "https://example.com"})
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected disconnected after Disconnect()")
	}
}
