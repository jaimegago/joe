package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/adapters/packaging/helm"
	"github.com/jaimegago/joe/internal/llm"
)

// HelmClient defines what the Helm tools need from the HTTP client.
type HelmClient interface {
	HelmReleases(ctx context.Context, sourceID, namespace string) ([]helm.Release, error)
	HelmGetRelease(ctx context.Context, sourceID, namespace, name string) (*helm.ReleaseDetail, error)
	HelmHistory(ctx context.Context, sourceID, namespace, name string, limit int) ([]helm.RevisionEntry, error)
}

// --- helm_releases ---

// HelmReleasesTool lists Helm releases.
type HelmReleasesTool struct {
	Client HelmClient
}

func NewHelmReleasesTool(c HelmClient) *HelmReleasesTool {
	return &HelmReleasesTool{Client: c}
}

func (t *HelmReleasesTool) Name() string { return "helm_releases" }

func (t *HelmReleasesTool) Description() string {
	return "List Helm releases with their status, chart version, and last updated time. " +
		"Shows name, namespace, chart, chart_version, app_version, status (deployed/failed/pending), " +
		"revision, and updated timestamp. " +
		"Optionally filter by namespace. " +
		"If you don't know the component_id, call list_components first."
}

func (t *HelmReleasesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Helm source.",
			},
			"namespace": {
				Type:        "string",
				Description: "Kubernetes namespace to list releases from. Omit to list all namespaces.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *HelmReleasesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, _ := args["namespace"].(string)

	releases, err := t.Client.HelmReleases(ctx, sourceID, namespace)
	if err != nil {
		return nil, fmt.Errorf("helm releases: %w", err)
	}
	if releases == nil {
		releases = []helm.Release{}
	}
	return map[string]any{
		"releases":     releases,
		"count":        len(releases),
		"component_id": sourceID,
	}, nil
}

// --- helm_release ---

// HelmGetReleaseTool gets details for one Helm release.
type HelmGetReleaseTool struct {
	Client HelmClient
}

func NewHelmGetReleaseTool(c HelmClient) *HelmGetReleaseTool {
	return &HelmGetReleaseTool{Client: c}
}

func (t *HelmGetReleaseTool) Name() string { return "helm_release" }

func (t *HelmGetReleaseTool) Description() string {
	return "Get full details for a specific Helm release: values (user-provided chart configuration) " +
		"and release notes. Use helm_releases first to find the release name and namespace. " +
		"If you don't know the component_id, call list_components first."
}

func (t *HelmGetReleaseTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Helm source.",
			},
			"namespace": {
				Type:        "string",
				Description: "Kubernetes namespace of the release.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the Helm release.",
			},
		},
		Required: []string{"component_id", "namespace", "name"},
	}
}

func (t *HelmGetReleaseTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, ok := args["namespace"].(string)
	if !ok || namespace == "" {
		return nil, fmt.Errorf("missing required parameter: namespace")
	}
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	detail, err := t.Client.HelmGetRelease(ctx, sourceID, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("helm release: %w", err)
	}
	return map[string]any{
		"detail":       detail,
		"component_id": sourceID,
	}, nil
}

// --- helm_history ---

// HelmHistoryTool lists revision history for a Helm release.
type HelmHistoryTool struct {
	Client HelmClient
}

func NewHelmHistoryTool(c HelmClient) *HelmHistoryTool {
	return &HelmHistoryTool{Client: c}
}

func (t *HelmHistoryTool) Name() string { return "helm_history" }

func (t *HelmHistoryTool) Description() string {
	return "Get the revision history for a Helm release. " +
		"Each entry shows revision number, status, chart version, and deployment time. " +
		"Ordered newest first. Use this to see what changed across upgrades or rollbacks. " +
		"If you don't know the component_id, call list_components first."
}

func (t *HelmHistoryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Helm source.",
			},
			"namespace": {
				Type:        "string",
				Description: "Kubernetes namespace of the release.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the Helm release.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of history entries to return. Defaults to 10.",
			},
		},
		Required: []string{"component_id", "namespace", "name"},
	}
}

func (t *HelmHistoryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, ok := args["namespace"].(string)
	if !ok || namespace == "" {
		return nil, fmt.Errorf("missing required parameter: namespace")
	}
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	history, err := t.Client.HelmHistory(ctx, sourceID, namespace, name, limit)
	if err != nil {
		return nil, fmt.Errorf("helm history: %w", err)
	}
	if history == nil {
		history = []helm.RevisionEntry{}
	}
	return map[string]any{
		"history":      history,
		"count":        len(history),
		"component_id": sourceID,
	}, nil
}
