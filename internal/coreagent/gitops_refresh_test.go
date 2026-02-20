package coreagent

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// ---- fake adapters ----

type fakeArgoCDAdapter struct {
	apps []argocdadapter.App
	err  error
}

func (f *fakeArgoCDAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeArgoCDAdapter) Disconnect() error                               { return nil }
func (f *fakeArgoCDAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (f *fakeArgoCDAdapter) Apps(_ context.Context, _ string) ([]argocdadapter.App, error) {
	return f.apps, f.err
}
func (f *fakeArgoCDAdapter) GetApp(_ context.Context, _ string) (*argocdadapter.AppDetail, error) {
	return nil, f.err
}
func (f *fakeArgoCDAdapter) GetDiff(_ context.Context, _ string) (*argocdadapter.Diff, error) {
	return nil, f.err
}
func (f *fakeArgoCDAdapter) GetHistory(_ context.Context, _ string, _ int) ([]argocdadapter.SyncOperation, error) {
	return nil, f.err
}

type fakeHelmAdapter struct {
	releases []helmadapter.Release
	err      error
}

func (f *fakeHelmAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeHelmAdapter) Disconnect() error                               { return nil }
func (f *fakeHelmAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (f *fakeHelmAdapter) Releases(_ context.Context, _ string) ([]helmadapter.Release, error) {
	return f.releases, f.err
}
func (f *fakeHelmAdapter) GetRelease(_ context.Context, _, _ string) (*helmadapter.ReleaseDetail, error) {
	return nil, f.err
}
func (f *fakeHelmAdapter) History(_ context.Context, _, _ string, _ int) ([]helmadapter.RevisionEntry, error) {
	return nil, f.err
}

type fakeTerraformAdapter struct {
	resources []terraformadapter.Resource
	err       error
}

func (f *fakeTerraformAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeTerraformAdapter) Disconnect() error                               { return nil }
func (f *fakeTerraformAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (f *fakeTerraformAdapter) Resources(_ context.Context, _ string) ([]terraformadapter.Resource, error) {
	return f.resources, f.err
}
func (f *fakeTerraformAdapter) GetResource(_ context.Context, _ string) (*terraformadapter.Resource, error) {
	return nil, f.err
}
func (f *fakeTerraformAdapter) Outputs(_ context.Context) (map[string]terraformadapter.Output, error) {
	return nil, f.err
}

// ---- helper ----

func setupGitOpsRefresher(t *testing.T) *Refresher {
	t.Helper()
	gs := setupGraphStore(t)
	return &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
}

// ---- Argo CD ----

func TestRefreshArgoCDSource_NoApps(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-argocd-1", Type: store.SourceTypeArgoCd, Name: "argo"}

	if err := r.refreshArgoCDSource(context.Background(), src, &fakeArgoCDAdapter{}); err != nil {
		t.Fatalf("refreshArgoCDSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// Expect 1 source node, no app nodes.
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].Type != "argocd_source" {
		t.Errorf("node type = %q, want argocd_source", nodes[0].Type)
	}
}

func TestRefreshArgoCDSource_AppsError(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-argocd-2", Type: store.SourceTypeArgoCd, Name: "argo"}
	adapter := &fakeArgoCDAdapter{err: errors.New("connection refused")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshArgoCDSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshArgoCDSource should not error on Apps failure, got: %v", err)
	}
}

func TestRefreshArgoCDSource_WithApps(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-argocd-3", Type: store.SourceTypeArgoCd, Name: "argo"}
	adapter := &fakeArgoCDAdapter{
		apps: []argocdadapter.App{
			{Name: "payment", Namespace: "default", SyncStatus: "Synced", Health: "Healthy"},
			{Name: "api-gateway", Namespace: "default", SyncStatus: "OutOfSync", Health: "Degraded"},
		},
	}

	if err := r.refreshArgoCDSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshArgoCDSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source node + 2 app nodes.
	if len(nodes) != 3 {
		t.Errorf("want 3 nodes (1 source + 2 apps), got %d", len(nodes))
	}
}

// ---- Helm ----

func TestRefreshHelmSource_NoReleases(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-helm-1", Type: store.SourceTypeHelm, Name: "prod-helm"}

	if err := r.refreshHelmSource(context.Background(), src, &fakeHelmAdapter{}); err != nil {
		t.Fatalf("refreshHelmSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "helm_source" {
		t.Errorf("want 1 helm_source node, got %v", nodes)
	}
}

func TestRefreshHelmSource_ReleasesError(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-helm-2", Type: store.SourceTypeHelm, Name: "helm"}
	adapter := &fakeHelmAdapter{err: errors.New("not connected")}

	if err := r.refreshHelmSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshHelmSource should not error on Releases failure, got: %v", err)
	}
}

func TestRefreshHelmSource_WithReleases(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-helm-3", Type: store.SourceTypeHelm, Name: "helm"}
	adapter := &fakeHelmAdapter{
		releases: []helmadapter.Release{
			{Name: "payment", Namespace: "default", Status: "deployed", Revision: 5},
		},
	}

	if err := r.refreshHelmSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshHelmSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source node + 1 release node.
	if len(nodes) != 2 {
		t.Errorf("want 2 nodes, got %d", len(nodes))
	}
}

// ---- Terraform ----

func TestRefreshTerraformSource_NoResources(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-tf-1", Type: store.SourceTypeTerraform, Name: "infra-tf"}

	if err := r.refreshTerraformSource(context.Background(), src, &fakeTerraformAdapter{}); err != nil {
		t.Fatalf("refreshTerraformSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "terraform_source" {
		t.Errorf("want 1 terraform_source node, got %v", nodes)
	}
}

func TestRefreshTerraformSource_ResourcesError(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-tf-2", Type: store.SourceTypeTerraform, Name: "tf"}
	adapter := &fakeTerraformAdapter{err: errors.New("state file not found")}

	if err := r.refreshTerraformSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshTerraformSource should not error on Resources failure, got: %v", err)
	}
}

func TestRefreshTerraformSource_WithManagedResources(t *testing.T) {
	r := setupGitOpsRefresher(t)
	src := &store.Source{ID: "src-tf-3", Type: store.SourceTypeTerraform, Name: "tf"}
	adapter := &fakeTerraformAdapter{
		resources: []terraformadapter.Resource{
			{Address: "aws_instance.web", Type: "aws_instance", Name: "web", Provider: "aws", Mode: "managed"},
			{Address: "data.aws_ami.ubuntu", Type: "aws_ami", Name: "ubuntu", Provider: "aws", Mode: "data"}, // skipped
		},
	}

	if err := r.refreshTerraformSource(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshTerraformSource: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), r.services.Graph, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource: %v", err)
	}
	// 1 source + 1 managed resource (data source skipped).
	if len(nodes) != 2 {
		t.Errorf("want 2 nodes, got %d: %v", len(nodes), nodes)
	}
}

// ---- gitopsNodeID ----

func TestGitopsNodeID(t *testing.T) {
	id := gitopsNodeID("src1", "argocd")
	want := "gitops/argocd/src1"
	if id != want {
		t.Errorf("gitopsNodeID = %q, want %q", id, want)
	}
}

// ---- refreshSource switch cases for GitOps types ----

func TestRefreshSource_ArgoCDType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-argocd", &fakeArgoCDAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-argocd", Type: store.SourceTypeArgoCd, Name: "argo"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(argocd) error: %v", err)
	}
}

func TestRefreshSource_HelmType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-helm", &fakeHelmAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-helm", Type: store.SourceTypeHelm, Name: "helm"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(helm) error: %v", err)
	}
}

func TestRefreshSource_TerraformType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-tf", &fakeTerraformAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-tf", Type: store.SourceTypeTerraform, Name: "tf"}
	if err := r.refreshSource(context.Background(), src); err != nil {
		t.Fatalf("refreshSource(terraform) error: %v", err)
	}
}

func TestRefreshSource_ArgoCDWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-argocd-bad", &fakeHelmAdapter{})

	r := &Refresher{services: svc, logger: slog.Default()}
	src := &store.Source{ID: "src-argocd-bad", Type: store.SourceTypeArgoCd, Name: "argo"}
	if err := r.refreshSource(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
