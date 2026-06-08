package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
)

// ======================
// NGINX client methods
// ======================

// NginxIngresses lists Ingress resources from the NGINX Ingress Controller source.
func (c *Client) NginxIngresses(ctx context.Context, sourceID, namespace string) ([]nginxadapter.Ingress, error) {
	u := fmt.Sprintf("%s%s/%s/ingresses", c.baseURL, apiNginxBasePath, url.PathEscape(sourceID))
	if namespace != "" {
		u += "?namespace=" + url.QueryEscape(namespace)
	}

	var result struct {
		Ingresses   []nginxadapter.Ingress `json:"ingresses"`
		ComponentID string                 `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "nginx ingresses"); err != nil {
		return nil, err
	}
	return result.Ingresses, nil
}

// NginxStatus returns connection stats from the NGINX status endpoint.
func (c *Client) NginxStatus(ctx context.Context, sourceID string) (*nginxadapter.NginxStatus, error) {
	u := fmt.Sprintf("%s%s/%s/status", c.baseURL, apiNginxBasePath, url.PathEscape(sourceID))

	var result struct {
		Status      *nginxadapter.NginxStatus `json:"status"`
		ComponentID string                    `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "nginx status"); err != nil {
		return nil, err
	}
	return result.Status, nil
}

// NginxConfigMaps returns NGINX controller ConfigMaps.
func (c *Client) NginxConfigMaps(ctx context.Context, sourceID, namespace string) ([]nginxadapter.ConfigMapSummary, error) {
	u := fmt.Sprintf("%s%s/%s/config", c.baseURL, apiNginxBasePath, url.PathEscape(sourceID))
	if namespace != "" {
		u += "?namespace=" + url.QueryEscape(namespace)
	}

	var result struct {
		ConfigMaps  []nginxadapter.ConfigMapSummary `json:"config_maps"`
		ComponentID string                          `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "nginx configmaps"); err != nil {
		return nil, err
	}
	return result.ConfigMaps, nil
}

// ======================
// Envoy client methods
// ======================

// EnvoyClusters returns Envoy cluster health summaries.
func (c *Client) EnvoyClusters(ctx context.Context, sourceID string) ([]envoyadapter.ClusterStatus, error) {
	u := fmt.Sprintf("%s%s/%s/clusters", c.baseURL, apiEnvoyBasePath, url.PathEscape(sourceID))

	var result struct {
		Clusters    []envoyadapter.ClusterStatus `json:"clusters"`
		ComponentID string                       `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "envoy clusters"); err != nil {
		return nil, err
	}
	return result.Clusters, nil
}

// EnvoyConfigDump returns the Envoy config dump, optionally filtered by section.
func (c *Client) EnvoyConfigDump(ctx context.Context, sourceID, section string) (map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/config", c.baseURL, apiEnvoyBasePath, url.PathEscape(sourceID))
	if section != "" {
		u += "?section=" + url.QueryEscape(section)
	}

	var result struct {
		Config      map[string]any `json:"config"`
		ComponentID string         `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "envoy config"); err != nil {
		return nil, err
	}
	return result.Config, nil
}

// EnvoyStats returns Envoy stats, optionally filtered by a prefix.
func (c *Client) EnvoyStats(ctx context.Context, sourceID, filter string) ([]envoyadapter.Stat, error) {
	u := fmt.Sprintf("%s%s/%s/stats", c.baseURL, apiEnvoyBasePath, url.PathEscape(sourceID))
	if filter != "" {
		u += "?filter=" + url.QueryEscape(filter)
	}

	var result struct {
		Stats       []envoyadapter.Stat `json:"stats"`
		ComponentID string              `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "envoy stats"); err != nil {
		return nil, err
	}
	return result.Stats, nil
}
