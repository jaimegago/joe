package core

import (
	"context"
	"fmt"

	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
	"github.com/jaimegago/joe/internal/llm"
)

// --- OCI Registry tool ---

// OCIRegistryClient is the interface for querying OCI-compatible registries.
type OCIRegistryClient interface {
	OCIListRepos(ctx context.Context, sourceID string) ([]string, error)
	OCIListTags(ctx context.Context, sourceID, repo string) ([]string, error)
	OCIGetManifest(ctx context.Context, sourceID, repo, reference string) (*ociadapter.Manifest, error)
}

// RegistryQueryTool queries an OCI-compatible container registry.
// Tier: T1 (Observe) — read-only.
type RegistryQueryTool struct {
	client OCIRegistryClient
}

// NewRegistryQueryTool creates a new registry_query tool.
func NewRegistryQueryTool(c OCIRegistryClient) *RegistryQueryTool {
	return &RegistryQueryTool{client: c}
}

func (t *RegistryQueryTool) Name() string { return "registry_query" }

func (t *RegistryQueryTool) Description() string {
	return "Query an OCI-compatible container registry (DockerHub, GHCR, Harbor, Quay, or any self-hosted OCI registry). " +
		"Omit repo to list all repositories. Set repo to list its tags. Set repo+reference to get the image manifest, " +
		"which includes the content digest and git commit SHA (org.opencontainers.image.revision label)."
}

func (t *RegistryQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "The registry source ID as registered in Joe.",
			},
			"repo": {
				Type:        "string",
				Description: "Repository name, e.g. \"myorg/myapp\". Omit to list all repositories.",
			},
			"reference": {
				Type:        "string",
				Description: "Tag or digest to fetch the manifest for, e.g. \"latest\" or \"sha256:abc...\". Requires repo.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *RegistryQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	repo, _ := args["repo"].(string)
	reference, _ := args["reference"].(string)

	switch {
	case repo != "" && reference != "":
		manifest, err := t.client.OCIGetManifest(ctx, sourceID, repo, reference)
		if err != nil {
			return nil, fmt.Errorf("get manifest for %s:%s: %w", repo, reference, err)
		}
		return map[string]any{
			"operation":    "get_manifest",
			"component_id": sourceID,
			"repo":         repo,
			"reference":    reference,
			"manifest":     manifest,
		}, nil

	case repo != "":
		tags, err := t.client.OCIListTags(ctx, sourceID, repo)
		if err != nil {
			return nil, fmt.Errorf("list tags for %s: %w", repo, err)
		}
		return map[string]any{
			"operation":    "list_tags",
			"component_id": sourceID,
			"repo":         repo,
			"tags":         tags,
			"count":        len(tags),
		}, nil

	default:
		repos, err := t.client.OCIListRepos(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("list repositories: %w", err)
		}
		return map[string]any{
			"operation":    "list_repos",
			"component_id": sourceID,
			"repositories": repos,
			"count":        len(repos),
		}, nil
	}
}

// --- JFrog Artifactory tool ---

// ArtifactoryClient is the interface for querying JFrog Artifactory.
type ArtifactoryClient interface {
	ArtifactoryListRepos(ctx context.Context, sourceID string) ([]artifactoryadapter.Repository, error)
	ArtifactoryListDockerTags(ctx context.Context, sourceID, repo, image string) ([]string, error)
	ArtifactoryGetArtifactInfo(ctx context.Context, sourceID, repo, path string) (*artifactoryadapter.ArtifactInfo, error)
}

// ArtifactoryQueryTool queries a JFrog Artifactory instance.
// Tier: T1 (Observe) — read-only.
type ArtifactoryQueryTool struct {
	client ArtifactoryClient
}

// NewArtifactoryQueryTool creates a new artifactory_query tool.
func NewArtifactoryQueryTool(c ArtifactoryClient) *ArtifactoryQueryTool {
	return &ArtifactoryQueryTool{client: c}
}

func (t *ArtifactoryQueryTool) Name() string { return "artifactory_query" }

func (t *ArtifactoryQueryTool) Description() string {
	return "Query a JFrog Artifactory instance. Omit repo to list Docker/Helm repositories. " +
		"Set repo+image to list Docker image tags. Set repo+path to retrieve artifact metadata " +
		"(size, checksums, timestamps)."
}

func (t *ArtifactoryQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "The Artifactory source ID as registered in Joe.",
			},
			"repo": {
				Type:        "string",
				Description: "Artifactory repository key, e.g. \"docker-local\". Omit to list all Docker/Helm repos.",
			},
			"image": {
				Type:        "string",
				Description: "Docker image name within the repo, e.g. \"myapp\". Requires repo. Returns tags.",
			},
			"path": {
				Type:        "string",
				Description: "Artifact path within the repo, e.g. \"myapp/latest/manifest.json\". Requires repo. Returns metadata.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *ArtifactoryQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	repo, _ := args["repo"].(string)
	image, _ := args["image"].(string)
	path, _ := args["path"].(string)

	switch {
	case repo != "" && image != "":
		tags, err := t.client.ArtifactoryListDockerTags(ctx, sourceID, repo, image)
		if err != nil {
			return nil, fmt.Errorf("list docker tags for %s/%s: %w", repo, image, err)
		}
		return map[string]any{
			"operation":    "list_docker_tags",
			"component_id": sourceID,
			"repo":         repo,
			"image":        image,
			"tags":         tags,
			"count":        len(tags),
		}, nil

	case repo != "" && path != "":
		info, err := t.client.ArtifactoryGetArtifactInfo(ctx, sourceID, repo, path)
		if err != nil {
			return nil, fmt.Errorf("get artifact info for %s/%s: %w", repo, path, err)
		}
		return map[string]any{
			"operation":    "get_artifact_info",
			"component_id": sourceID,
			"repo":         repo,
			"path":         path,
			"artifact":     info,
		}, nil

	case repo != "":
		return nil, fmt.Errorf("when repo is set, also provide image (for tags) or path (for artifact info)")

	default:
		repos, err := t.client.ArtifactoryListRepos(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("list repositories: %w", err)
		}
		return map[string]any{
			"operation":    "list_repos",
			"component_id": sourceID,
			"repositories": repos,
			"count":        len(repos),
		}, nil
	}
}

// --- AWS ECR tool ---

// ECRClient is the interface for querying AWS ECR.
type ECRClient interface {
	ECRListRepos(ctx context.Context, sourceID string) ([]ecradapter.Repository, error)
	ECRListImages(ctx context.Context, sourceID, repo string) ([]ecradapter.ImageDetail, error)
	ECRGetImage(ctx context.Context, sourceID, repo, tag string) (*ecradapter.ImageDetail, error)
}

// ECRQueryTool queries AWS Elastic Container Registry.
// Tier: T1 (Observe) — read-only.
type ECRQueryTool struct {
	client ECRClient
}

// NewECRQueryTool creates a new ecr_query tool.
func NewECRQueryTool(c ECRClient) *ECRQueryTool {
	return &ECRQueryTool{client: c}
}

func (t *ECRQueryTool) Name() string { return "ecr_query" }

func (t *ECRQueryTool) Description() string {
	return "Query AWS Elastic Container Registry (ECR). Omit repo to list repositories. " +
		"Set repo to list all images (tags, digests, push dates, scan findings). " +
		"Set repo+tag to get details for a specific image."
}

func (t *ECRQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "The ECR source ID as registered in Joe.",
			},
			"repo": {
				Type:        "string",
				Description: "ECR repository name, e.g. \"my-app\". Omit to list all repositories.",
			},
			"tag": {
				Type:        "string",
				Description: "Image tag, e.g. \"latest\" or \"v1.2.3\". Requires repo. Returns single image details.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *ECRQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	repo, _ := args["repo"].(string)
	tag, _ := args["tag"].(string)

	switch {
	case repo != "" && tag != "":
		img, err := t.client.ECRGetImage(ctx, sourceID, repo, tag)
		if err != nil {
			return nil, fmt.Errorf("get ECR image %s:%s: %w", repo, tag, err)
		}
		return map[string]any{
			"operation":    "get_image",
			"component_id": sourceID,
			"repo":         repo,
			"tag":          tag,
			"image":        img,
		}, nil

	case repo != "":
		images, err := t.client.ECRListImages(ctx, sourceID, repo)
		if err != nil {
			return nil, fmt.Errorf("list ECR images in %s: %w", repo, err)
		}
		return map[string]any{
			"operation":    "list_images",
			"component_id": sourceID,
			"repo":         repo,
			"images":       images,
			"count":        len(images),
		}, nil

	default:
		repos, err := t.client.ECRListRepos(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("list ECR repositories: %w", err)
		}
		return map[string]any{
			"operation":    "list_repos",
			"component_id": sourceID,
			"repositories": repos,
			"count":        len(repos),
		}, nil
	}
}
