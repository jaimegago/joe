package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
)

// --- OCI Registry ---

// OCIListRepos returns all repository names from an OCI-compatible registry source.
func (c *Client) OCIListRepos(ctx context.Context, sourceID string) ([]string, error) {
	u := fmt.Sprintf("%s%s/%s/repos", c.baseURL, apiRegistryOCIBasePath, url.PathEscape(sourceID))

	var result struct {
		Repositories []string `json:"repositories"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "oci list repos"); err != nil {
		return nil, err
	}
	return result.Repositories, nil
}

// OCIListTags returns tags for a repository in an OCI-compatible registry.
func (c *Client) OCIListTags(ctx context.Context, sourceID, repo string) ([]string, error) {
	u := fmt.Sprintf("%s%s/%s/repos/%s/tags",
		c.baseURL, apiRegistryOCIBasePath, url.PathEscape(sourceID), url.PathEscape(repo))

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "oci list tags"); err != nil {
		return nil, err
	}
	return result.Tags, nil
}

// OCIGetManifest retrieves the manifest for a given repo and reference (tag or digest).
func (c *Client) OCIGetManifest(ctx context.Context, sourceID, repo, reference string) (*ociadapter.Manifest, error) {
	u := fmt.Sprintf("%s%s/%s/repos/%s/manifest?reference=%s",
		c.baseURL, apiRegistryOCIBasePath,
		url.PathEscape(sourceID), url.PathEscape(repo), url.QueryEscape(reference))

	var result struct {
		Manifest *ociadapter.Manifest `json:"manifest"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "oci get manifest"); err != nil {
		return nil, err
	}
	return result.Manifest, nil
}

// --- JFrog Artifactory ---

// ArtifactoryListRepos returns Docker/Helm repositories from an Artifactory source.
func (c *Client) ArtifactoryListRepos(ctx context.Context, sourceID string) ([]artifactoryadapter.Repository, error) {
	u := fmt.Sprintf("%s%s/%s/repos", c.baseURL, apiRegistryArtifactoryBasePath, url.PathEscape(sourceID))

	var result struct {
		Repositories []artifactoryadapter.Repository `json:"repositories"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "artifactory list repos"); err != nil {
		return nil, err
	}
	return result.Repositories, nil
}

// ArtifactoryListDockerTags returns Docker image tags in an Artifactory repository.
func (c *Client) ArtifactoryListDockerTags(ctx context.Context, sourceID, repo, image string) ([]string, error) {
	u := fmt.Sprintf("%s%s/%s/repos/%s/tags?image=%s",
		c.baseURL, apiRegistryArtifactoryBasePath,
		url.PathEscape(sourceID), url.PathEscape(repo), url.QueryEscape(image))

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "artifactory list docker tags"); err != nil {
		return nil, err
	}
	return result.Tags, nil
}

// ArtifactoryGetArtifactInfo retrieves metadata for a specific artifact path.
func (c *Client) ArtifactoryGetArtifactInfo(ctx context.Context, sourceID, repo, path string) (*artifactoryadapter.ArtifactInfo, error) {
	u := fmt.Sprintf("%s%s/%s/repos/%s/artifact?path=%s",
		c.baseURL, apiRegistryArtifactoryBasePath,
		url.PathEscape(sourceID), url.PathEscape(repo), url.QueryEscape(path))

	var result struct {
		Artifact *artifactoryadapter.ArtifactInfo `json:"artifact"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "artifactory get artifact"); err != nil {
		return nil, err
	}
	return result.Artifact, nil
}

// --- AWS ECR ---

// ECRListRepos returns all ECR repositories for a source.
func (c *Client) ECRListRepos(ctx context.Context, sourceID string) ([]ecradapter.Repository, error) {
	u := fmt.Sprintf("%s%s/%s/repos", c.baseURL, apiRegistryECRBasePath, url.PathEscape(sourceID))

	var result struct {
		Repositories []ecradapter.Repository `json:"repositories"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "ecr list repos"); err != nil {
		return nil, err
	}
	return result.Repositories, nil
}

// ECRListImages returns all images in an ECR repository.
func (c *Client) ECRListImages(ctx context.Context, sourceID, repo string) ([]ecradapter.ImageDetail, error) {
	u := fmt.Sprintf("%s%s/%s/repos/%s/images",
		c.baseURL, apiRegistryECRBasePath, url.PathEscape(sourceID), url.PathEscape(repo))

	var result struct {
		Images []ecradapter.ImageDetail `json:"images"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "ecr list images"); err != nil {
		return nil, err
	}
	return result.Images, nil
}

// ECRGetImage returns details for a specific tagged image in an ECR repository.
func (c *Client) ECRGetImage(ctx context.Context, sourceID, repo, tag string) (*ecradapter.ImageDetail, error) {
	u := fmt.Sprintf("%s%s/%s/repos/%s/images/%s",
		c.baseURL, apiRegistryECRBasePath,
		url.PathEscape(sourceID), url.PathEscape(repo), url.PathEscape(tag))

	var result struct {
		Image *ecradapter.ImageDetail `json:"image"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "ecr get image"); err != nil {
		return nil, err
	}
	return result.Image, nil
}
