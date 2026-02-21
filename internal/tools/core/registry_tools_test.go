package core_test

import (
	"context"
	"fmt"
	"testing"

	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
	"github.com/jaimegago/joe/internal/tools/core"
)

// --- OCI mock ---

type mockOCIClient struct {
	listReposFn   func(ctx context.Context, sourceID string) ([]string, error)
	listTagsFn    func(ctx context.Context, sourceID, repo string) ([]string, error)
	getManifestFn func(ctx context.Context, sourceID, repo, reference string) (*ociadapter.Manifest, error)
}

func (m *mockOCIClient) OCIListRepos(ctx context.Context, sourceID string) ([]string, error) {
	if m.listReposFn != nil {
		return m.listReposFn(ctx, sourceID)
	}
	return nil, nil
}

func (m *mockOCIClient) OCIListTags(ctx context.Context, sourceID, repo string) ([]string, error) {
	if m.listTagsFn != nil {
		return m.listTagsFn(ctx, sourceID, repo)
	}
	return nil, nil
}

func (m *mockOCIClient) OCIGetManifest(ctx context.Context, sourceID, repo, reference string) (*ociadapter.Manifest, error) {
	if m.getManifestFn != nil {
		return m.getManifestFn(ctx, sourceID, repo, reference)
	}
	return nil, nil
}

func TestRegistryQueryTool_Name(t *testing.T) {
	tool := core.NewRegistryQueryTool(&mockOCIClient{})
	if tool.Name() != "registry_query" {
		t.Errorf("Name() = %q, want registry_query", tool.Name())
	}
}

func TestRegistryQueryTool_Execute(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockOCIClient
		wantErr bool
		wantOp  string
	}{
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockOCIClient{},
			wantErr: true,
		},
		{
			name: "list repos (no repo param)",
			args: map[string]any{"source_id": "reg-1"},
			mock: &mockOCIClient{
				listReposFn: func(_ context.Context, _ string) ([]string, error) {
					return []string{"myorg/app", "myorg/worker"}, nil
				},
			},
			wantOp: "list_repos",
		},
		{
			name: "list tags (repo set, no reference)",
			args: map[string]any{"source_id": "reg-1", "repo": "myorg/app"},
			mock: &mockOCIClient{
				listTagsFn: func(_ context.Context, _, _ string) ([]string, error) {
					return []string{"latest", "v1.0"}, nil
				},
			},
			wantOp: "list_tags",
		},
		{
			name: "get manifest (repo + reference)",
			args: map[string]any{"source_id": "reg-1", "repo": "myorg/app", "reference": "latest"},
			mock: &mockOCIClient{
				getManifestFn: func(_ context.Context, _, _, _ string) (*ociadapter.Manifest, error) {
					return &ociadapter.Manifest{Digest: "sha256:abc"}, nil
				},
			},
			wantOp: "get_manifest",
		},
		{
			name: "list repos error propagated",
			args: map[string]any{"source_id": "reg-1"},
			mock: &mockOCIClient{
				listReposFn: func(_ context.Context, _ string) ([]string, error) {
					return nil, fmt.Errorf("connection refused")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewRegistryQueryTool(tt.mock)
			got, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			result, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("result is not map[string]any")
			}
			if result["operation"] != tt.wantOp {
				t.Errorf("operation = %q, want %q", result["operation"], tt.wantOp)
			}
		})
	}
}

// --- Artifactory mock ---

type mockArtifactoryClient struct {
	listReposFn   func(ctx context.Context, sourceID string) ([]artifactoryadapter.Repository, error)
	listTagsFn    func(ctx context.Context, sourceID, repo, image string) ([]string, error)
	getArtifactFn func(ctx context.Context, sourceID, repo, path string) (*artifactoryadapter.ArtifactInfo, error)
}

func (m *mockArtifactoryClient) ArtifactoryListRepos(ctx context.Context, sourceID string) ([]artifactoryadapter.Repository, error) {
	if m.listReposFn != nil {
		return m.listReposFn(ctx, sourceID)
	}
	return nil, nil
}

func (m *mockArtifactoryClient) ArtifactoryListDockerTags(ctx context.Context, sourceID, repo, image string) ([]string, error) {
	if m.listTagsFn != nil {
		return m.listTagsFn(ctx, sourceID, repo, image)
	}
	return nil, nil
}

func (m *mockArtifactoryClient) ArtifactoryGetArtifactInfo(ctx context.Context, sourceID, repo, path string) (*artifactoryadapter.ArtifactInfo, error) {
	if m.getArtifactFn != nil {
		return m.getArtifactFn(ctx, sourceID, repo, path)
	}
	return nil, nil
}

func TestArtifactoryQueryTool_Name(t *testing.T) {
	tool := core.NewArtifactoryQueryTool(&mockArtifactoryClient{})
	if tool.Name() != "artifactory_query" {
		t.Errorf("Name() = %q, want artifactory_query", tool.Name())
	}
}

func TestArtifactoryQueryTool_Execute(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockArtifactoryClient
		wantErr bool
		wantOp  string
	}{
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockArtifactoryClient{},
			wantErr: true,
		},
		{
			name: "list repos",
			args: map[string]any{"source_id": "art-1"},
			mock: &mockArtifactoryClient{
				listReposFn: func(_ context.Context, _ string) ([]artifactoryadapter.Repository, error) {
					return []artifactoryadapter.Repository{{Key: "docker-local"}}, nil
				},
			},
			wantOp: "list_repos",
		},
		{
			name: "list docker tags",
			args: map[string]any{"source_id": "art-1", "repo": "docker-local", "image": "myapp"},
			mock: &mockArtifactoryClient{
				listTagsFn: func(_ context.Context, _, _, _ string) ([]string, error) {
					return []string{"latest"}, nil
				},
			},
			wantOp: "list_docker_tags",
		},
		{
			name: "get artifact info",
			args: map[string]any{"source_id": "art-1", "repo": "docker-local", "path": "myapp/latest/manifest.json"},
			mock: &mockArtifactoryClient{
				getArtifactFn: func(_ context.Context, _, _, _ string) (*artifactoryadapter.ArtifactInfo, error) {
					return &artifactoryadapter.ArtifactInfo{Repo: "docker-local"}, nil
				},
			},
			wantOp: "get_artifact_info",
		},
		{
			name:    "repo set without image or path",
			args:    map[string]any{"source_id": "art-1", "repo": "docker-local"},
			mock:    &mockArtifactoryClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewArtifactoryQueryTool(tt.mock)
			got, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			result, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("result is not map[string]any")
			}
			if result["operation"] != tt.wantOp {
				t.Errorf("operation = %q, want %q", result["operation"], tt.wantOp)
			}
		})
	}
}

// --- ECR mock ---

type mockECRQueryClient struct {
	listReposFn  func(ctx context.Context, sourceID string) ([]ecradapter.Repository, error)
	listImagesFn func(ctx context.Context, sourceID, repo string) ([]ecradapter.ImageDetail, error)
	getImageFn   func(ctx context.Context, sourceID, repo, tag string) (*ecradapter.ImageDetail, error)
}

func (m *mockECRQueryClient) ECRListRepos(ctx context.Context, sourceID string) ([]ecradapter.Repository, error) {
	if m.listReposFn != nil {
		return m.listReposFn(ctx, sourceID)
	}
	return nil, nil
}

func (m *mockECRQueryClient) ECRListImages(ctx context.Context, sourceID, repo string) ([]ecradapter.ImageDetail, error) {
	if m.listImagesFn != nil {
		return m.listImagesFn(ctx, sourceID, repo)
	}
	return nil, nil
}

func (m *mockECRQueryClient) ECRGetImage(ctx context.Context, sourceID, repo, tag string) (*ecradapter.ImageDetail, error) {
	if m.getImageFn != nil {
		return m.getImageFn(ctx, sourceID, repo, tag)
	}
	return nil, nil
}

func TestECRQueryTool_Name(t *testing.T) {
	tool := core.NewECRQueryTool(&mockECRQueryClient{})
	if tool.Name() != "ecr_query" {
		t.Errorf("Name() = %q, want ecr_query", tool.Name())
	}
}

func TestECRQueryTool_Execute(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockECRQueryClient
		wantErr bool
		wantOp  string
	}{
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockECRQueryClient{},
			wantErr: true,
		},
		{
			name: "list repos",
			args: map[string]any{"source_id": "ecr-1"},
			mock: &mockECRQueryClient{
				listReposFn: func(_ context.Context, _ string) ([]ecradapter.Repository, error) {
					return []ecradapter.Repository{{Name: "my-app"}}, nil
				},
			},
			wantOp: "list_repos",
		},
		{
			name: "list images (repo, no tag)",
			args: map[string]any{"source_id": "ecr-1", "repo": "my-app"},
			mock: &mockECRQueryClient{
				listImagesFn: func(_ context.Context, _, _ string) ([]ecradapter.ImageDetail, error) {
					return []ecradapter.ImageDetail{{Digest: "sha256:abc", Tags: []string{"latest"}}}, nil
				},
			},
			wantOp: "list_images",
		},
		{
			name: "get image (repo + tag)",
			args: map[string]any{"source_id": "ecr-1", "repo": "my-app", "tag": "v1.0"},
			mock: &mockECRQueryClient{
				getImageFn: func(_ context.Context, _, _, _ string) (*ecradapter.ImageDetail, error) {
					return &ecradapter.ImageDetail{Digest: "sha256:abc", Tags: []string{"v1.0"}}, nil
				},
			},
			wantOp: "get_image",
		},
		{
			name: "list repos error propagated",
			args: map[string]any{"source_id": "ecr-1"},
			mock: &mockECRQueryClient{
				listReposFn: func(_ context.Context, _ string) ([]ecradapter.Repository, error) {
					return nil, fmt.Errorf("credentials expired")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewECRQueryTool(tt.mock)
			got, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			result, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("result is not map[string]any")
			}
			if result["operation"] != tt.wantOp {
				t.Errorf("operation = %q, want %q", result["operation"], tt.wantOp)
			}
		})
	}
}
