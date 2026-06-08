package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArgoCDURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/apps") && !strings.Contains(r.URL.Path, "/apps/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"apps":         []map[string]any{{"name": "my-app", "namespace": "argocd"}},
				"component_id": "argo-1",
			})
		case strings.Contains(r.URL.Path, "/apps/") && strings.HasSuffix(r.URL.Path, "/diff"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"diff":         map[string]any{"has_changes": true},
				"component_id": "argo-1",
			})
		case strings.Contains(r.URL.Path, "/apps/") && strings.HasSuffix(r.URL.Path, "/history"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"history":      []map[string]any{{"revision": "abc123"}},
				"component_id": "argo-1",
			})
		case strings.Contains(r.URL.Path, "/apps/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"detail":       map[string]any{"app": map[string]any{"name": "my-app"}},
				"component_id": "argo-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	apps, err := c.ArgoCDApps(ctx, "argo-1", "")
	if err != nil {
		t.Fatalf("ArgoCDApps: %v", err)
	}
	if len(apps) != 1 {
		t.Errorf("ArgoCDApps: got %d apps, want 1", len(apps))
	}

	_, err = c.ArgoCDApps(ctx, "argo-1", "my-project")
	if err != nil {
		t.Fatalf("ArgoCDApps with project: %v", err)
	}

	_, err = c.ArgoCDGetApp(ctx, "argo-1", "my-app")
	if err != nil {
		t.Fatalf("ArgoCDGetApp: %v", err)
	}

	_, err = c.ArgoCDGetDiff(ctx, "argo-1", "my-app")
	if err != nil {
		t.Fatalf("ArgoCDGetDiff: %v", err)
	}

	history, err := c.ArgoCDGetHistory(ctx, "argo-1", "my-app", 10)
	if err != nil {
		t.Fatalf("ArgoCDGetHistory: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("ArgoCDGetHistory: got %d entries, want 1", len(history))
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/argocd/argo-1/apps")
	assertContains(t, joined, "project=my-project")
	assertContains(t, joined, "/api/v1/argocd/argo-1/apps/my-app")
	assertContains(t, joined, "/api/v1/argocd/argo-1/apps/my-app/diff")
	assertContains(t, joined, "/api/v1/argocd/argo-1/apps/my-app/history?limit=10")
}

func TestTerraformURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/outputs"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"outputs":      map[string]any{"vpc_id": map[string]any{"value": "vpc-123"}},
				"component_id": "tf-1",
			})
		case strings.Contains(r.URL.Path, "/state/resource"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":     map[string]any{"address": "aws_vpc.main", "type": "aws_vpc"},
				"component_id": "tf-1",
			})
		case strings.HasSuffix(r.URL.Path, "/state"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resources":    []map[string]any{{"address": "aws_vpc.main", "type": "aws_vpc"}},
				"component_id": "tf-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	resources, err := c.TerraformResources(ctx, "tf-1", "")
	if err != nil {
		t.Fatalf("TerraformResources: %v", err)
	}
	if len(resources) != 1 {
		t.Errorf("TerraformResources: got %d, want 1", len(resources))
	}

	_, err = c.TerraformResources(ctx, "tf-1", "aws_vpc")
	if err != nil {
		t.Fatalf("TerraformResources with type filter: %v", err)
	}

	resource, err := c.TerraformGetResource(ctx, "tf-1", "aws_vpc.main")
	if err != nil {
		t.Fatalf("TerraformGetResource: %v", err)
	}
	if resource == nil {
		t.Fatal("TerraformGetResource returned nil")
	}

	outputs, err := c.TerraformOutputs(ctx, "tf-1")
	if err != nil {
		t.Fatalf("TerraformOutputs: %v", err)
	}
	if len(outputs) != 1 {
		t.Errorf("TerraformOutputs: got %d, want 1", len(outputs))
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/terraform/tf-1/state")
	assertContains(t, joined, "type=aws_vpc")
	assertContains(t, joined, "/api/v1/terraform/tf-1/state/resource?address=aws_vpc.main")
	assertContains(t, joined, "/api/v1/terraform/tf-1/outputs")
}

func TestHelmURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.Contains(r.URL.Path, "/history"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"history":      []map[string]any{{"revision": 1, "status": "deployed"}},
				"component_id": "helm-1",
			})
		case strings.Contains(r.URL.Path, "/releases/") && strings.Count(r.URL.Path, "/") > 5:
			// get release (has namespace and name)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"detail":       map[string]any{"release": map[string]any{"name": "my-release"}},
				"component_id": "helm-1",
			})
		case strings.HasSuffix(r.URL.Path, "/releases"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"releases":     []map[string]any{{"name": "my-release", "namespace": "default"}},
				"component_id": "helm-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	releases, err := c.HelmReleases(ctx, "helm-1", "")
	if err != nil {
		t.Fatalf("HelmReleases: %v", err)
	}
	if len(releases) != 1 {
		t.Errorf("HelmReleases: got %d, want 1", len(releases))
	}

	_, err = c.HelmReleases(ctx, "helm-1", "default")
	if err != nil {
		t.Fatalf("HelmReleases with namespace: %v", err)
	}

	_, err = c.HelmGetRelease(ctx, "helm-1", "default", "my-release")
	if err != nil {
		t.Fatalf("HelmGetRelease: %v", err)
	}

	history, err := c.HelmHistory(ctx, "helm-1", "default", "my-release", 5)
	if err != nil {
		t.Fatalf("HelmHistory: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("HelmHistory: got %d, want 1", len(history))
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/helm/helm-1/releases")
	assertContains(t, joined, "namespace=default")
	assertContains(t, joined, "/api/v1/helm/helm-1/releases/default/my-release")
	assertContains(t, joined, "/api/v1/helm/helm-1/releases/default/my-release/history?limit=5")
}
