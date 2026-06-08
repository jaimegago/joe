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

func (f *fakeOCIAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeOCIAdapter) Disconnect() error                                  { return nil }
func (f *fakeOCIAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
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

func (f *fakeArtifactoryAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeArtifactoryAdapter) Disconnect() error                                  { return nil }
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

func (f *fakeECRAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeECRAdapter) Disconnect() error                                  { return nil }
func (f *fakeECRAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
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

func TestRefreshOCIComponent_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-oci-1", Type: store.ComponentTypeOCIRegistry, Name: "my-registry"}

	if err := r.refreshOCIComponent(context.Background(), src, &fakeOCIAdapter{}); err != nil {
		t.Fatalf("refreshOCIComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshOCIComponent_ReposError(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-oci-2", Type: store.ComponentTypeOCIRegistry, Name: "my-registry"}
	adapter := &fakeOCIAdapter{err: errors.New("registry unreachable")}

	// Should succeed — error listing repos is non-fatal.
	if err := r.refreshOCIComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshOCIComponent should not error on ListRepositories failure, got: %v", err)
	}
}

func TestRefreshOCIComponent_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-oci-3", Type: store.ComponentTypeOCIRegistry, Name: "my-registry"}
	adapter := &fakeOCIAdapter{repos: []string{"payment", "api-gateway", "auth"}}

	if err := r.refreshOCIComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshOCIComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	// 1 source node + 3 repo nodes.
	if len(nodes) != 4 {
		t.Errorf("want 4 nodes (1 source + 3 repos), got %d", len(nodes))
	}
}

// ---- DockerHub ----

func TestRefreshDockerHubComponent_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-dh-1", Type: store.ComponentTypeDockerHub, Name: "dockerhub"}

	if err := r.refreshDockerHubComponent(context.Background(), src, &fakeOCIAdapter{}); err != nil {
		t.Fatalf("refreshDockerHubComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshDockerHubComponent_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-dh-2", Type: store.ComponentTypeDockerHub, Name: "dockerhub"}
	adapter := &fakeOCIAdapter{repos: []string{"myorg/app", "myorg/worker"}}

	if err := r.refreshDockerHubComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshDockerHubComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	// 1 source + 2 repo nodes.
	if len(nodes) != 3 {
		t.Errorf("want 3 nodes, got %d", len(nodes))
	}
}

// ---- Artifactory ----

func TestRefreshArtifactoryComponent_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-art-1", Type: store.ComponentTypeArtifactory, Name: "artifactory"}

	if err := r.refreshArtifactoryComponent(context.Background(), src, &fakeArtifactoryAdapter{}); err != nil {
		t.Fatalf("refreshArtifactoryComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshArtifactoryComponent_ReposError(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-art-2", Type: store.ComponentTypeArtifactory, Name: "artifactory"}
	adapter := &fakeArtifactoryAdapter{err: errors.New("auth failure")}

	if err := r.refreshArtifactoryComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshArtifactoryComponent should not error on ListRepositories failure, got: %v", err)
	}
}

func TestRefreshArtifactoryComponent_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-art-3", Type: store.ComponentTypeArtifactory, Name: "artifactory"}
	adapter := &fakeArtifactoryAdapter{
		repos: []artifactoryadapter.Repository{
			{Key: "docker-local", PackageType: "Docker", Description: "Local Docker repo"},
			{Key: "helm-local", PackageType: "Helm", Description: "Local Helm repo"},
		},
	}

	if err := r.refreshArtifactoryComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshArtifactoryComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	// 1 source + 2 repo nodes.
	if len(nodes) != 3 {
		t.Errorf("want 3 nodes (1 source + 2 repos), got %d", len(nodes))
	}
}

// ---- ECR ----

func TestRefreshECRComponent_NoRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-ecr-1", Type: store.ComponentTypeECR, Name: "ecr"}

	if err := r.refreshECRComponent(context.Background(), src, &fakeECRAdapter{}); err != nil {
		t.Fatalf("refreshECRComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "artifact_registry" {
		t.Errorf("want 1 artifact_registry node, got %v", nodes)
	}
}

func TestRefreshECRComponent_ReposError(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-ecr-2", Type: store.ComponentTypeECR, Name: "ecr"}
	adapter := &fakeECRAdapter{err: errors.New("aws credentials error")}

	if err := r.refreshECRComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshECRComponent should not error on ListRepositories failure, got: %v", err)
	}
}

func TestRefreshECRComponent_WithRepos(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-ecr-3", Type: store.ComponentTypeECR, Name: "ecr"}
	adapter := &fakeECRAdapter{
		repos: []ecradapter.Repository{
			{Name: "payment", URI: "123456789.dkr.ecr.us-east-1.amazonaws.com/payment", CreatedAt: "2024-01-01T00:00:00Z"},
			{Name: "api-gateway", URI: "123456789.dkr.ecr.us-east-1.amazonaws.com/api-gateway"},
		},
	}

	if err := r.refreshECRComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshECRComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
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
	src := &store.Component{ID: "src1", Type: "oci_registry", Name: "my-reg"}
	meta := registryMetadata(src)
	if meta["component_id"] != "src1" || meta["component_type"] != "oci_registry" || meta["name"] != "my-reg" {
		t.Errorf("registryMetadata = %v, unexpected values", meta)
	}
}

// ---- buildImageStoredInEdges ----

func TestBuildImageStoredInEdges_EmptyRepoName(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-img-1", Type: store.ComponentTypeOCIRegistry}
	edges := r.buildImageStoredInEdges(context.Background(), src, "repo-node-id", "", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for empty repoName, got %d", len(edges))
	}
}

func TestBuildImageStoredInEdges_NoMatchingNodes(t *testing.T) {
	r := setupRegistryRefresher(t)
	src := &store.Component{ID: "src-img-2", Type: store.ComponentTypeOCIRegistry}
	edges := r.buildImageStoredInEdges(context.Background(), src, "repo-node-id", "payment", time.Now())
	// No matching nodes in graph → no edges.
	if len(edges) != 0 {
		t.Errorf("want 0 edges (no matching nodes), got %d", len(edges))
	}
}

func TestBuildImageStoredInEdges_WithMatchingDeployment(t *testing.T) {
	r := setupRegistryRefresher(t)
	ctx := context.Background()
	src := &store.Component{ID: "src-img-3", Type: store.ComponentTypeOCIRegistry}

	// Add a deployment node that matches the repo name.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "deploy/payment",
		Type:        "deployment",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "payment"},
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
	src := &store.Component{ID: "src-img-4", Type: store.ComponentTypeOCIRegistry}

	// Add a k8s_node — should NOT produce an image_stored_in edge.
	_ = r.services.Graph.AddNode(ctx, graph.Node{
		ID:          "k8snode/my-payment-node",
		Type:        "k8s_node",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "my-payment-node"},
	})

	edges := r.buildImageStoredInEdges(ctx, src, "repo-node", "my-payment-node", time.Now())
	if len(edges) != 0 {
		t.Errorf("want 0 edges for non-deployment/service node, got %d", len(edges))
	}
}

// ---- refreshComponent switch cases for registry types ----

func TestRefreshComponent_OCIType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-oci", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-oci", Type: store.ComponentTypeOCIRegistry, Name: "oci"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(oci_registry) error: %v", err)
	}
}

func TestRefreshComponent_DockerHubType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-dh", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-dh", Type: store.ComponentTypeDockerHub, Name: "dockerhub"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(dockerhub) error: %v", err)
	}
}

func TestRefreshComponent_ArtifactoryType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-art", &fakeArtifactoryAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-art", Type: store.ComponentTypeArtifactory, Name: "artifactory"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(artifactory) error: %v", err)
	}
}

func TestRefreshComponent_ECRType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-ecr", &fakeECRAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-ecr", Type: store.ComponentTypeECR, Name: "ecr"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(ecr) error: %v", err)
	}
}

func TestRefreshComponent_OCIWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	// Register wrong adapter type — adapter is ECR but source is OCI.
	reg.Register("src-oci-bad", &fakeECRAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-oci-bad", Type: store.ComponentTypeOCIRegistry, Name: "oci"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshComponent_ArtifactoryWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-art-bad", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-art-bad", Type: store.ComponentTypeArtifactory, Name: "artifactory"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshComponent_ECRWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-ecr-bad", &fakeOCIAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Component{ID: "src-ecr-bad", Type: store.ComponentTypeECR, Name: "ecr"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
