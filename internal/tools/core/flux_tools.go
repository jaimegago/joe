package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// FluxCRDTypes maps Flux resource short names to their full K8s resource identifiers.
var FluxCRDTypes = map[string]string{
	"GitRepository":  "gitrepositories.source.toolkit.fluxcd.io",
	"Kustomization":  "kustomizations.kustomize.toolkit.fluxcd.io",
	"HelmRelease":    "helmreleases.helm.toolkit.fluxcd.io",
	"HelmRepository": "helmrepositories.source.toolkit.fluxcd.io",
	"HelmChart":      "helmcharts.source.toolkit.fluxcd.io",
	"OCIRepository":  "ocirepositories.source.toolkit.fluxcd.io",
	"Bucket":         "buckets.source.toolkit.fluxcd.io",
}

// FluxK8sClient defines what the Flux tools need from the K8s client.
type FluxK8sClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
	K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error)
}

// --- flux_status ---

// FluxStatusTool lists all Flux resources with reconciliation status.
type FluxStatusTool struct {
	Client FluxK8sClient
}

func NewFluxStatusTool(c FluxK8sClient) *FluxStatusTool {
	return &FluxStatusTool{Client: c}
}

func (t *FluxStatusTool) Name() string { return "flux_status" }

func (t *FluxStatusTool) Description() string {
	return "List all Flux CD resources (GitRepository, Kustomization, HelmRelease, HelmRepository) " +
		"with their reconciliation status. Shows ready condition, last applied revision, " +
		"and any error messages. Use component_id of a Kubernetes component. " +
		"If you don't know the component_id, call list_components first."
}

func (t *FluxStatusTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Kubernetes component (where Flux is installed).",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to list Flux resources from. Omit to list across all namespaces.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *FluxStatusTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, _ := args["namespace"].(string)

	result := map[string]any{}

	for kind, crdName := range FluxCRDTypes {
		resources, err := t.Client.K8sListResources(ctx, sourceID, crdName, namespace)
		if err != nil {
			// If a Flux CRD type is not installed, skip it gracefully.
			continue
		}
		if len(resources) > 0 {
			result[kind] = extractFluxStatus(resources)
		}
	}

	return map[string]any{
		"flux_resources": result,
		"component_id":   sourceID,
		"namespace":      namespace,
	}, nil
}

// --- flux_resource ---

// FluxResourceTool gets details for a specific Flux resource.
type FluxResourceTool struct {
	Client FluxK8sClient
}

func NewFluxResourceTool(c FluxK8sClient) *FluxResourceTool {
	return &FluxResourceTool{Client: c}
}

func (t *FluxResourceTool) Name() string { return "flux_resource" }

func (t *FluxResourceTool) Description() string {
	return "Get details for a specific Flux CD resource. " +
		"Supported kinds: GitRepository, Kustomization, HelmRelease, HelmRepository, " +
		"HelmChart, OCIRepository, Bucket. " +
		"Returns full status including conditions, last applied revision, and error messages. " +
		"If you don't know the component_id, call list_components first."
}

func (t *FluxResourceTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Kubernetes component (where Flux is installed).",
			},
			"kind": {
				Type:        "string",
				Description: "Flux resource kind: GitRepository, Kustomization, HelmRelease, HelmRepository, HelmChart, OCIRepository, or Bucket.",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace of the Flux resource.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the Flux resource.",
			},
		},
		Required: []string{"component_id", "kind", "namespace", "name"},
	}
}

func (t *FluxResourceTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	kind, ok := args["kind"].(string)
	if !ok || kind == "" {
		return nil, fmt.Errorf("missing required parameter: kind")
	}
	namespace, ok := args["namespace"].(string)
	if !ok || namespace == "" {
		return nil, fmt.Errorf("missing required parameter: namespace")
	}
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	crdName, ok := FluxCRDTypes[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported Flux resource kind %q; supported: GitRepository, Kustomization, HelmRelease, HelmRepository, HelmChart, OCIRepository, Bucket", kind)
	}

	resource, err := t.Client.K8sGetResource(ctx, sourceID, crdName, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("get flux %s %s/%s: %w", kind, namespace, name, err)
	}

	return map[string]any{
		"resource":     resource,
		"kind":         kind,
		"namespace":    namespace,
		"name":         name,
		"component_id": sourceID,
	}, nil
}

// extractFluxStatus extracts a concise status summary from a list of Flux CRD objects.
func extractFluxStatus(resources []map[string]any) []map[string]any {
	var out []map[string]any
	for _, r := range resources {
		summary := map[string]any{}

		if meta, ok := r["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
			summary["namespace"] = meta["namespace"]
		}

		if status, ok := r["status"].(map[string]any); ok {
			summary["observed_revision"] = status["lastAppliedRevision"]
			summary["artifact_revision"] = status["artifact"]

			if conditions, ok := status["conditions"].([]any); ok {
				for _, c := range conditions {
					if cm, ok := c.(map[string]any); ok {
						if cm["type"] == "Ready" {
							summary["ready"] = cm["status"]
							summary["reason"] = cm["reason"]
							summary["message"] = cm["message"]
							break
						}
					}
				}
			}
		}
		out = append(out, summary)
	}
	return out
}
