package coreagent

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// ---- fake OCI adapter ----

type fakeOCIAdapter struct {
	repos []string
	err   error
}

func (f *fakeOCIAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeOCIAdapter) Disconnect() error                               { return nil }
func (f *fakeOCIAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (f *fakeOCIAdapter) ListRepositories(_ context.Context) ([]string, error) {
	return f.repos, f.err
}
func (f *fakeOCIAdapter) ListTags(_ context.Context, _ string) ([]string, error) {
	return nil, f.err
}
func (f *fakeOCIAdapter) GetManifest(_ context.Context, _, _ string) (*ociadapter.Manifest, error) {
	return nil, f.err
}

// ---- fake Artifactory adapter ----

type fakeArtifactoryAdapter struct {
	repos []artifactoryadapter.Repository
	err   error
}

func (f *fakeArtifactoryAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeArtifactoryAdapter) Disconnect() error                               { return nil }
func (f *fakeArtifactoryAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeArtifactoryAdapter) ListRepositories(_ context.Context) ([]artifactoryadapter.Repository, error) {
	return f.repos, f.err
}
func (f *fakeArtifactoryAdapter) ListDockerTags(_ context.Context, _, _ string) ([]string, error) {
	return nil, f.err
}
func (f *fakeArtifactoryAdapter) GetArtifactInfo(_ context.Context, _, _ string) (*artifactoryadapter.ArtifactInfo, error) {
	return nil, f.err
}

// ---- fake ECR adapter ----

type fakeECRAdapter struct {
	repos []ecradapter.Repository
	err   error
}

func (f *fakeECRAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeECRAdapter) Disconnect() error                               { return nil }
func (f *fakeECRAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (f *fakeECRAdapter) ListRepositories(_ context.Context) ([]ecradapter.Repository, error) {
	return f.repos, f.err
}
func (f *fakeECRAdapter) ListImages(_ context.Context, _ string) ([]ecradapter.ImageDetail, error) {
	return nil, f.err
}
func (f *fakeECRAdapter) GetImageDetails(_ context.Context, _, _ string) (*ecradapter.ImageDetail, error) {
	return nil, f.err
}

// ---- helper ----

func setupRegistryRefresher(t *testing.T) *Refresher {
	t.Helper()
	gs := setupGraphStore(t)
	return &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
}

// ---- OCI ----

func TestRefreshOCISource_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-oci-1", Type: store.SourceTypeOCIRegistry, Name: "my-registry"}

	if err := r.refreshOCISource(context.Background(), src, &fakeOCIAdapter{}); err != nil {
		t.Fatalf("refreshOCISource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshOCISource_ReposError(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-oci-2", Type: store.SourceTypeOCIRegistry, Name: "my-registry"}
	adapter := &fakeOCIAdapter{err: errors.New("registry unreachable")}

	// Should succeed — error listing repos is non-fatal.
	if err := r.refreshOCISource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshOCISource should not error on ListRepositories failure, got: %v", err)
	}
}

func TestRefreshOCISource_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-oci-3", Type: store.SourceTypeOCIRegistry, Name: "my-registry"}
	adapter := &fakeOCIAdapter{repos: []string{"payment", "api-gateway", "auth"}}

	if err := r.refreshOCISource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshOCISource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source node + 3 repo nodes.
	if len(nodes) != 4 {
		t.Errorf("want 4 nodes (1 source + 3 repos), got %d", len(nodes))
	}
}

// ---- DockerHub ----

func TestRefreshDockerHubSource_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-dh-1", Type: store.SourceTypeDockerHub, Name: "dockerhub"}

	if err := r.refreshDockerHubSource(context.Background(), src, &fakeOCIAdapter{}); err != nil {
		t.Fatalf("refreshDockerHubSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshDockerHubSource_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-dh-2", Type: store.SourceTypeDockerHub, Name: "dockerhub"}
	adapter := &fakeOCIAdapter{repos: []string{"myorg/app", "myorg/worker"}}

	if err := r.refreshDockerHubSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshDockerHubSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source + 2 repo nodes.
	if len(nodes) != 3 {
		t.Errorf("want 3 nodes, got %d", len(nodes))
	}
}

// ---- Artifactory ----

func TestRefreshArtifactorySource_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-art-1", Type: store.SourceTypeArtifactory, Name: "artifactory"}

	if err := r.refreshArtifactorySource(context.Background(), src, &fakeArtifactoryAdapter{}); err != nil {
		t.Fatalf("refreshArtifactorySource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshArtifactorySource_ReposError(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-art-2", Type: store.SourceTypeArtifactory, Name: "artifactory"}
	adapter := &fakeArtifactoryAdapter{err: errors.New("auth failure")}

	if err := r.refreshArtifactorySource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshArtifactorySource should not error on ListRepositories failure, got: %v", err)
	}
}

func TestRefreshArtifactorySource_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-art-3", Type: store.SourceTypeArtifactory, Name: "artifactory"}
	adapter := &fakeArtifactoryAdapter{
		repos: []artifactoryadapter.Repository{
			{Key: "docker-local", PackageType: "Docker", Description: "Local Docker repo"},
			{Key: "helm-local", PackageType: "Helm", Description: "Local Helm repo"},
		},
	}

	if err := r.refreshArtifactorySource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshArtifactorySource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source + 2 repo nodes.
	if len(nodes) != 3 {
		t.Errorf("want 3 nodes (1 source + 2 repos), got %d", len(nodes))
	}
}

// ---- ECR ----

func TestRefreshECRSource_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-ecr-1", Type: store.SourceTypeECR, Name: "ecr"}

	if err := r.refreshECRSource(context.Background(), src, &fakeECRAdapter{}); err != nil {
		t.Fatalf("refreshECRSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshECRSource_ReposError(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-ecr-2", Type: store.SourceTypeECR, Name: "ecr"}
	adapter := &fakeECRAdapter{err: errors.New("aws credentials error")}

	if err := r.refreshECRSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshECRSource should not error on ListRepositories failure, got: %v", err)
	}
}

func TestRefreshECRSource_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-ecr-3", Type: store.SourceTypeECR, Name: "ecr"}
	adapter := &fakeECRAdapter{
		repos: []ecradapter.Repository{
			{Name: "payment", URI: "123456789.dkr.ecr.us-east-1.amazonaws.com/payment", CreatedAt: "2024-01-01T00:00:00Z"},
			{Name: "api-gateway", URI: "123456789.dkr.ecr.us-east-1.amazonaws.com/api-gateway"},
		},
	}

	if err := r.refreshECRSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshECRSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source + 2 repo nodes.
	if len(nodes) != 3 {
		t.Errorf("want 3 nodes (1 source + 2 repos), got %d", len(nodes))
	}
}

// ---- registryNodeID, repoNodeID, registryMetadata ----

func TestRegistryNodeID(t *testing.T) {
	id := registryNodeID("src1", "oci_registry")
	want := "registry/oci_registry/src1"
	if id != want {
		t.Errorf("registryNodeID = %q, want %q", id, want)
	}
}

func TestRepoNodeID(t *testing.T) {
	id := repoNodeID("src1", "payment")
	want := "registry/src1/repo/payment"
	if id != want {
		t.Errorf("repoNodeID = %q, want %q", id, want)
	}
}

func TestRegistryMetadata(t *testing.T) {
	src := &store.Source{ID: "src1", Type: "oci_registry", Name: "my-reg"}
	meta := registryMetadata(src)
	if meta["source_id"] != "src1" || meta["source_type"] != "oci_registry" || meta["name"] != "my-reg" {
		t.Errorf("registryMetadata = %v, unexpected values", meta)
	}
}

// ---- buildImageStoredInEdges ----

func TestBuildImageStoredInEdges_EmptyRepoName(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-img-1", Type: store.SourceTypeOCIRegistry}
	edges := r.buildImageStoredInEdges(context.Background(), src, "repo-node-id", "", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for empty repoName, got %d", len(edges))
	}
}

func TestBuildImageStoredInEdges_NoMatchingNodes(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Source{ID: "src-img-2", Type: store.SourceTypeOCIRegistry}
	edges := r.buildImageStoredInEdges(context.Background(), src, "repo-node-id", "payment", time.Now())
	// No matching nodes in graph → no edges.
	if len(edges) != 0 {
		t.Errorf("want 0 edges (no matching nodes), got %d", len(edges))
	}
}

func TestBuildImageStoredInEdges_WithMatchingDeployment(t *testing.T) {
	r := setupRegistryRefresher(t)
	ctx := context.Background()
	src := &store.Source{ID: "src-img-3", Type: store.SourceTypeOCIRegistry}

	// Add a deployment node that matches the repo name.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:       "deploy/payment",
		Type:     "deployment",
		SourceID: "src-k8s",
		Metadata: map[string]any{"name": "payment"},
	})

	edges := r.buildImageStoredInEdges(ctx, src, "registry/src-img-3/repo/payment", "payment", time.Now())
	if len(edges) != 1 {
		t.Errorf("want 1 image_stored_in edge, got %d", len(edges))
	}
	if len(edges) > 0 && edges[0].Relation != graph.RelationImageStoredIn {
		t.Errorf("edge relation = %v, want RelationImageStoredIn", edges[0].Relation)
	}
}

func TestBuildImageStoredInEdges_SkipsNonDeploymentNodes(t *testing.T) {
	r := setupRegistryRefresher(t)
	ctx := context.Background()
	src := &store.Source{ID: "src-img-4", Type: store.SourceTypeOCIRegistry}

	// Add a k8s_node — should NOT produce an image_stored_in edge.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:       "k8snode/my-payment-node",
		Type:     "k8s_node",
		SourceID: "src-k8s",
		Metadata: map[string]any{"name": "my-payment-node"},
	})

	edges := r.buildImageStoredInEdges(ctx, src, "repo-node", "my-payment-node", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-deployment/service node, got %d", len(edges))
	}
}

// ---- refreshSource switch cases for registry types ----

func TestRefreshSource_OCIType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-oci", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-oci", Type: store.SourceTypeOCIRegistry, Name: "oci"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(oci_registry) error: %v", err)
	}
}

func TestRefreshSource_DockerHubType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-dh", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-dh", Type: store.SourceTypeDockerHub, Name: "dockerhub"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(dockerhub) error: %v", err)
	}
}

func TestRefreshSource_ArtifactoryType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-art", &fakeArtifactoryAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-art", Type: store.SourceTypeArtifactory, Name: "artifactory"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(artifactory) error: %v", err)
	}
}

func TestRefreshSource_ECRType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-ecr", &fakeECRAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-ecr", Type: store.SourceTypeECR, Name: "ecr"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(ecr) error: %v", err)
	}
}

func TestRefreshSource_OCIWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	// Register wrong adapter type — adapter is ECR but source is OCI.
	reg.Register("src-oci-bad", &fakeECRAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-oci-bad", Type: store.SourceTypeOCIRegistry, Name: "oci"}
	if err := r.refreshSource(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshSource_ArtifactoryWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-art-bad", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-art-bad", Type: store.SourceTypeArtifactory, Name: "artifactory"}
	if err := r.refreshSource(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshSource_ECRWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-ecr-bad", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-ecr-bad", Type: store.SourceTypeECR, Name: "ecr"}
	if err := r.refreshSource(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
