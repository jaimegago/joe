package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOCIURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repositories": []string{"library/nginx", "library/redis"},
			})
		case strings.HasSuffix(r.URL.Path, "/tags"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tags": []string{"latest", "1.25"},
			})
		case strings.HasSuffix(r.URL.Path, "/manifest"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"manifest": map[string]any{"schema_version": 2, "media_type": "application/vnd.docker.distribution.manifest.v2+json"},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	repos, err := c.OCIListRepos(ctx, "oci-1")
	if err != nil {
		t.Fatalf("OCIListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("OCIListRepos: got %d repos, want 2", len(repos))
	}

	tags, err := c.OCIListTags(ctx, "oci-1", "library/nginx")
	if err != nil {
		t.Fatalf("OCIListTags: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("OCIListTags: got %d tags, want 2", len(tags))
	}

	manifest, err := c.OCIGetManifest(ctx, "oci-1", "library/nginx", "latest")
	if err != nil {
		t.Fatalf("OCIGetManifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("OCIGetManifest returned nil")
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/registry/oci/oci-1/repos")
	assertContains(t, joined, "/api/v1/registry/oci/oci-1/repos/library%2Fnginx/tags")
	assertContains(t, joined, "/api/v1/registry/oci/oci-1/repos/library%2Fnginx/manifest?reference=latest")
}

func TestArtifactoryURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repositories": []map[string]any{{"key": "docker-local", "type": "local"}},
			})
		case strings.Contains(r.URL.Path, "/repos/") && strings.HasSuffix(r.URL.Path, "/tags"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tags": []string{"latest", "v1.0"},
			})
		case strings.Contains(r.URL.Path, "/artifact"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifact": map[string]any{"repo": "docker-local", "path": "nginx/latest"},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	repos, err := c.ArtifactoryListRepos(ctx, "art-1")
	if err != nil {
		t.Fatalf("ArtifactoryListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("ArtifactoryListRepos: got %d repos, want 1", len(repos))
	}

	tags, err := c.ArtifactoryListDockerTags(ctx, "art-1", "docker-local", "nginx")
	if err != nil {
		t.Fatalf("ArtifactoryListDockerTags: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("ArtifactoryListDockerTags: got %d tags, want 2", len(tags))
	}

	artifact, err := c.ArtifactoryGetArtifactInfo(ctx, "art-1", "docker-local", "nginx/latest")
	if err != nil {
		t.Fatalf("ArtifactoryGetArtifactInfo: %v", err)
	}
	if artifact == nil {
		t.Fatal("ArtifactoryGetArtifactInfo returned nil")
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/registry/artifactory/art-1/repos")
	assertContains(t, joined, "/api/v1/registry/artifactory/art-1/repos/docker-local/tags")
	assertContains(t, joined, "image=nginx")
	assertContains(t, joined, "/api/v1/registry/artifactory/art-1/repos/docker-local/artifact")
}

func TestECRURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repositories": []map[string]any{{"name": "my-app", "uri": "123.dkr.ecr.us-east-1.amazonaws.com/my-app"}},
			})
		case strings.HasSuffix(r.URL.Path, "/images"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"images": []map[string]any{{"image_tag": "latest", "image_digest": "sha256:abc"}},
			})
		case strings.Contains(r.URL.Path, "/images/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image": map[string]any{"image_tag": "latest", "image_digest": "sha256:abc"},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	repos, err := c.ECRListRepos(ctx, "ecr-1")
	if err != nil {
		t.Fatalf("ECRListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("ECRListRepos: got %d repos, want 1", len(repos))
	}

	images, err := c.ECRListImages(ctx, "ecr-1", "my-app")
	if err != nil {
		t.Fatalf("ECRListImages: %v", err)
	}
	if len(images) != 1 {
		t.Errorf("ECRListImages: got %d images, want 1", len(images))
	}

	image, err := c.ECRGetImage(ctx, "ecr-1", "my-app", "latest")
	if err != nil {
		t.Fatalf("ECRGetImage: %v", err)
	}
	if image == nil {
		t.Fatal("ECRGetImage returned nil")
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/registry/ecr/ecr-1/repos")
	assertContains(t, joined, "/api/v1/registry/ecr/ecr-1/repos/my-app/images")
	assertContains(t, joined, "/api/v1/registry/ecr/ecr-1/repos/my-app/images/latest")
}
