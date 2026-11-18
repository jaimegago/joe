package access

import (
	"context"

	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- OCI ---

func (a *Accessor) OCIListRepositories(ctx context.Context, principal rbac.Principal, sourceID string) ([]string, error) {
	ad, err := guard[ociadapter.OCIAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "repositories"}, "oci")
	if err != nil {
		return nil, err
	}
	return ad.ListRepositories(ctx)
}

func (a *Accessor) OCIListTags(ctx context.Context, principal rbac.Principal, sourceID, repo string) ([]string, error) {
	ad, err := guard[ociadapter.OCIAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "repositories", Subresource: "tags", Name: repo}, "oci")
	if err != nil {
		return nil, err
	}
	return ad.ListTags(ctx, repo)
}

func (a *Accessor) OCIGetManifest(ctx context.Context, principal rbac.Principal, sourceID, repo, reference string) (*ociadapter.Manifest, error) {
	ad, err := guard[ociadapter.OCIAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "repositories", Subresource: "manifests", Name: repo + ":" + reference}, "oci")
	if err != nil {
		return nil, err
	}
	return ad.GetManifest(ctx, repo, reference)
}

// --- Artifactory ---

func (a *Accessor) ArtifactoryListRepositories(ctx context.Context, principal rbac.Principal, sourceID string) ([]artifactoryadapter.Repository, error) {
	ad, err := guard[artifactoryadapter.ArtifactoryAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "repositories"}, "artifactory")
	if err != nil {
		return nil, err
	}
	return ad.ListRepositories(ctx)
}

func (a *Accessor) ArtifactoryListDockerTags(ctx context.Context, principal rbac.Principal, sourceID, repo, image string) ([]string, error) {
	ad, err := guard[artifactoryadapter.ArtifactoryAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "repositories", Subresource: "tags", Name: repo + "/" + image}, "artifactory")
	if err != nil {
		return nil, err
	}
	return ad.ListDockerTags(ctx, repo, image)
}

func (a *Accessor) ArtifactoryGetArtifactInfo(ctx context.Context, principal rbac.Principal, sourceID, repo, path string) (*artifactoryadapter.ArtifactInfo, error) {
	ad, err := guard[artifactoryadapter.ArtifactoryAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "artifacts", Name: repo + "/" + path}, "artifactory")
	if err != nil {
		return nil, err
	}
	return ad.GetArtifactInfo(ctx, repo, path)
}

// --- ECR ---

func (a *Accessor) ECRListRepositories(ctx context.Context, principal rbac.Principal, sourceID string) ([]ecradapter.Repository, error) {
	ad, err := guard[ecradapter.ECRAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "repositories"}, "ecr")
	if err != nil {
		return nil, err
	}
	return ad.ListRepositories(ctx)
}

func (a *Accessor) ECRListImages(ctx context.Context, principal rbac.Principal, sourceID, repo string) ([]ecradapter.ImageDetail, error) {
	ad, err := guard[ecradapter.ECRAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "repositories", Subresource: "images", Name: repo}, "ecr")
	if err != nil {
		return nil, err
	}
	return ad.ListImages(ctx, repo)
}

func (a *Accessor) ECRGetImageDetails(ctx context.Context, principal rbac.Principal, sourceID, repo, tag string) (*ecradapter.ImageDetail, error) {
	ad, err := guard[ecradapter.ECRAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "repositories", Subresource: "images", Name: repo + ":" + tag}, "ecr")
	if err != nil {
		return nil, err
	}
	return ad.GetImageDetails(ctx, repo, tag)
}
