package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// CrossplaneProviderCRDTypes maps Crossplane provider resource kinds to their full K8s identifiers.
var CrossplaneProviderCRDTypes = map[string]string{
	"Provider":         "providers.pkg.crossplane.io",
	"ProviderRevision": "providerrevisions.pkg.crossplane.io",
}

// CrossplaneCompositionCRDTypes maps Crossplane composition resource kinds to their full K8s identifiers.
var CrossplaneCompositionCRDTypes = map[string]string{
	"CompositeResourceDefinition": "compositeresourcedefinitions.apiextensions.crossplane.io",
	"Composition":                 "compositions.apiextensions.crossplane.io",
}

// CrossplaneK8sClient defines what Crossplane tools need from the K8s client.
type CrossplaneK8sClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
}

// --- crossplane_providers ---

// CrossplaneProvidersTool lists Crossplane Provider resources with health status.
type CrossplaneProvidersTool struct {
	Client CrossplaneK8sClient
}

func NewCrossplaneProvidersTool(c CrossplaneK8sClient) *CrossplaneProvidersTool {
	return &CrossplaneProvidersTool{Client: c}
}

func (t *CrossplaneProvidersTool) Name() string { return "crossplane_providers" }

func (t *CrossplaneProvidersTool) Description() string {
	return "List Crossplane Provider resources with their installation and health status. " +
		"Shows provider package (e.g. provider-aws, provider-gcp), installed revision, " +
		"and whether the provider is Healthy and ready to provision resources. " +
		"Use component_id of a Kubernetes component where Crossplane is installed. " +
		"If you don't know the component_id, call list_components first."
}

func (t *CrossplaneProvidersTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Kubernetes component (where Crossplane is installed).",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *CrossplaneProvidersTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	providers, err := t.Client.K8sListResources(ctx, sourceID, CrossplaneProviderCRDTypes["Provider"], "")
	if err != nil {
		return nil, fmt.Errorf("list crossplane providers: %w", err)
	}
	if providers == nil {
		providers = []map[string]any{}
	}

	return map[string]any{
		"providers":    extractCrossplaneProviderSummaries(providers),
		"count":        len(providers),
		"component_id": sourceID,
	}, nil
}

// extractCrossplaneProviderSummaries extracts concise summaries from Provider objects.
func extractCrossplaneProviderSummaries(providers []map[string]any) []map[string]any {
	var out []map[string]any
	for _, p := range providers {
		summary := map[string]any{}
		if meta, ok := p["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
		}
		if spec, ok := p["spec"].(map[string]any); ok {
			summary["package"] = spec["package"]
		}
		if status, ok := p["status"].(map[string]any); ok {
			summary["current_revision"] = status["currentRevision"]
			summary["current_identifier"] = status["currentIdentifier"]
			if conditions, ok := status["conditions"].([]any); ok {
				for _, cond := range conditions {
					if cm, ok := cond.(map[string]any); ok {
						switch cm["type"] {
						case "Healthy":
							summary["healthy"] = cm["status"]
							summary["health_reason"] = cm["reason"]
						case "Installed":
							summary["installed"] = cm["status"]
						}
					}
				}
			}
		}
		out = append(out, summary)
	}
	return out
}

// --- crossplane_resources ---

// CrossplaneResourcesTool lists Crossplane CompositeResourceDefinitions and Compositions.
type CrossplaneResourcesTool struct {
	Client CrossplaneK8sClient
}

func NewCrossplaneResourcesTool(c CrossplaneK8sClient) *CrossplaneResourcesTool {
	return &CrossplaneResourcesTool{Client: c}
}

func (t *CrossplaneResourcesTool) Name() string { return "crossplane_resources" }

func (t *CrossplaneResourcesTool) Description() string {
	return "List Crossplane CompositeResourceDefinitions (XRDs) and Compositions. " +
		"XRDs define the API for composite resources; Compositions define how they are provisioned. " +
		"Use to understand what composite resource types are available and how they map to cloud resources. " +
		"Use component_id of a Kubernetes component where Crossplane is installed. " +
		"If you don't know the component_id, call list_components first."
}

func (t *CrossplaneResourcesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Kubernetes component (where Crossplane is installed).",
			},
			"kind": {
				Type:        "string",
				Description: "Optional kind filter: CompositeResourceDefinition or Composition. Omit for both.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *CrossplaneResourcesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	kind, _ := args["kind"].(string)

	if kind != "" {
		crdName, ok := CrossplaneCompositionCRDTypes[kind]
		if !ok {
			return nil, fmt.Errorf("unsupported Crossplane resource kind %q; supported: CompositeResourceDefinition, Composition", kind)
		}
		resources, err := t.Client.K8sListResources(ctx, sourceID, crdName, "")
		if err != nil {
			return nil, fmt.Errorf("list crossplane %s: %w", kind, err)
		}
		if resources == nil {
			resources = []map[string]any{}
		}
		return map[string]any{
			"kind":         kind,
			"resources":    extractCrossplaneResourceSummaries(resources),
			"count":        len(resources),
			"component_id": sourceID,
		}, nil
	}

	result := map[string][]map[string]any{}
	for _, k := range []string{"CompositeResourceDefinition", "Composition"} {
		if resources, err := t.Client.K8sListResources(ctx, sourceID, CrossplaneCompositionCRDTypes[k], ""); err == nil && len(resources) > 0 {
			result[k] = extractCrossplaneResourceSummaries(resources)
		}
	}

	total := len(result["CompositeResourceDefinition"]) + len(result["Composition"])
	return map[string]any{
		"resources":    result,
		"count":        total,
		"component_id": sourceID,
	}, nil
}

// extractCrossplaneResourceSummaries extracts concise summaries from XRD/Composition objects.
func extractCrossplaneResourceSummaries(resources []map[string]any) []map[string]any {
	var out []map[string]any
	for _, r := range resources {
		summary := map[string]any{}
		if meta, ok := r["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
		}
		if spec, ok := r["spec"].(map[string]any); ok {
			summary["group"] = spec["group"]
			if names, ok := spec["names"].(map[string]any); ok {
				summary["kind"] = names["kind"]
				summary["plural"] = names["plural"]
			}
			// Composition: compositeTypeRef shows which XRD it implements.
			if ctr, ok := spec["compositeTypeRef"].(map[string]any); ok {
				summary["composite_type"] = ctr["kind"]
				summary["composite_api_version"] = ctr["apiVersion"]
			}
		}
		if status, ok := r["status"].(map[string]any); ok {
			if conditions, ok := status["conditions"].([]any); ok {
				for _, cond := range conditions {
					if cm, ok := cond.(map[string]any); ok && cm["type"] == "Established" {
						summary["established"] = cm["status"]
						summary["reason"] = cm["reason"]
						break
					}
				}
			}
		}
		out = append(out, summary)
	}
	return out
}
