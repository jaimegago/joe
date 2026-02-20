package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
)

// ======================
// Argo CD client methods
// ======================

// ArgoCDApps lists Argo CD applications, optionally filtered by project.
func (c *Client) ArgoCDApps(ctx context.Context, sourceID, project string) ([]argocdadapter.App, error) {
	u := fmt.Sprintf("%s%s/%s/apps", c.baseURL, apiArgoCDBasePath, url.PathEscape(sourceID))
	if project != "" {
		u += "?project=" + url.QueryEscape(project)
	}

	var result struct {
		Apps     []argocdadapter.App `json:"apps"`
		SourceID string              `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "argocd apps"); err != nil {
		return nil, err
	}
	return result.Apps, nil
}

// ArgoCDGetApp returns full details for one Argo CD application.
func (c *Client) ArgoCDGetApp(ctx context.Context, sourceID, name string) (*argocdadapter.AppDetail, error) {
	u := fmt.Sprintf("%s%s/%s/apps/%s",
		c.baseURL, apiArgoCDBasePath, url.PathEscape(sourceID), url.PathEscape(name))

	var result struct {
		Detail   *argocdadapter.AppDetail `json:"detail"`
		SourceID string                   `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "argocd get app"); err != nil {
		return nil, err
	}
	return result.Detail, nil
}

// ArgoCDGetDiff returns the sync diff for an Argo CD application.
func (c *Client) ArgoCDGetDiff(ctx context.Context, sourceID, name string) (*argocdadapter.Diff, error) {
	u := fmt.Sprintf("%s%s/%s/apps/%s/diff",
		c.baseURL, apiArgoCDBasePath, url.PathEscape(sourceID), url.PathEscape(name))

	var result struct {
		Diff     *argocdadapter.Diff `json:"diff"`
		SourceID string              `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "argocd diff"); err != nil {
		return nil, err
	}
	return result.Diff, nil
}

// ArgoCDGetHistory returns the sync history for an Argo CD application.
func (c *Client) ArgoCDGetHistory(ctx context.Context, sourceID, name string, limit int) ([]argocdadapter.SyncOperation, error) {
	u := fmt.Sprintf("%s%s/%s/apps/%s/history?limit=%d",
		c.baseURL, apiArgoCDBasePath, url.PathEscape(sourceID), url.PathEscape(name), limit)

	var result struct {
		History  []argocdadapter.SyncOperation `json:"history"`
		SourceID string                        `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "argocd history"); err != nil {
		return nil, err
	}
	return result.History, nil
}

// =========================
// Terraform client methods
// =========================

// TerraformResources lists managed resources from a Terraform state.
func (c *Client) TerraformResources(ctx context.Context, sourceID, resourceType string) ([]terraformadapter.Resource, error) {
	u := fmt.Sprintf("%s%s/%s/state", c.baseURL, apiTerraformBasePath, url.PathEscape(sourceID))
	if resourceType != "" {
		u += "?type=" + url.QueryEscape(resourceType)
	}

	var result struct {
		Resources []terraformadapter.Resource `json:"resources"`
		SourceID  string                      `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "terraform resources"); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

// TerraformGetResource returns details for a specific Terraform resource by address.
func (c *Client) TerraformGetResource(ctx context.Context, sourceID, address string) (*terraformadapter.Resource, error) {
	u := fmt.Sprintf("%s%s/%s/state/resource?address=%s",
		c.baseURL, apiTerraformBasePath, url.PathEscape(sourceID), url.QueryEscape(address))

	var result struct {
		Resource *terraformadapter.Resource `json:"resource"`
		SourceID string                     `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "terraform get resource"); err != nil {
		return nil, err
	}
	return result.Resource, nil
}

// TerraformOutputs returns output values from a Terraform state.
func (c *Client) TerraformOutputs(ctx context.Context, sourceID string) (map[string]terraformadapter.Output, error) {
	u := fmt.Sprintf("%s%s/%s/outputs", c.baseURL, apiTerraformBasePath, url.PathEscape(sourceID))

	var result struct {
		Outputs  map[string]terraformadapter.Output `json:"outputs"`
		SourceID string                             `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "terraform outputs"); err != nil {
		return nil, err
	}
	return result.Outputs, nil
}

// ===================
// Helm client methods
// ===================

// HelmReleases lists Helm releases, optionally filtered by namespace.
func (c *Client) HelmReleases(ctx context.Context, sourceID, namespace string) ([]helmadapter.Release, error) {
	u := fmt.Sprintf("%s%s/%s/releases", c.baseURL, apiHelmBasePath, url.PathEscape(sourceID))
	if namespace != "" {
		u += "?namespace=" + url.QueryEscape(namespace)
	}

	var result struct {
		Releases []helmadapter.Release `json:"releases"`
		SourceID string                `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "helm releases"); err != nil {
		return nil, err
	}
	return result.Releases, nil
}

// HelmGetRelease returns full details for one Helm release.
func (c *Client) HelmGetRelease(ctx context.Context, sourceID, namespace, name string) (*helmadapter.ReleaseDetail, error) {
	u := fmt.Sprintf("%s%s/%s/releases/%s/%s",
		c.baseURL, apiHelmBasePath,
		url.PathEscape(sourceID), url.PathEscape(namespace), url.PathEscape(name))

	var result struct {
		Detail   *helmadapter.ReleaseDetail `json:"detail"`
		SourceID string                     `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "helm get release"); err != nil {
		return nil, err
	}
	return result.Detail, nil
}

// HelmHistory returns the revision history for a Helm release.
func (c *Client) HelmHistory(ctx context.Context, sourceID, namespace, name string, limit int) ([]helmadapter.RevisionEntry, error) {
	u := fmt.Sprintf("%s%s/%s/releases/%s/%s/history?limit=%s",
		c.baseURL, apiHelmBasePath,
		url.PathEscape(sourceID), url.PathEscape(namespace), url.PathEscape(name),
		strconv.Itoa(limit))

	var result struct {
		History  []helmadapter.RevisionEntry `json:"history"`
		SourceID string                      `json:"source_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "helm history"); err != nil {
		return nil, err
	}
	return result.History, nil
}
