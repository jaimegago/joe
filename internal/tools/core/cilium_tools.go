package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// CiliumCRDTypes maps Cilium resource kind names to their full K8s resource identifiers.
var CiliumCRDTypes = map[string]string{
	"CiliumNetworkPolicy":            "ciliumnetworkpolicies.cilium.io",
	"CiliumClusterwideNetworkPolicy": "ciliumclusterwidenetworkpolicies.cilium.io",
	"CiliumEndpoint":                 "ciliumendpoints.cilium.io",
}

// CiliumK8sClient defines what Cilium tools need from the K8s client.
type CiliumK8sClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
}

// --- cilium_policies ---

// CiliumPoliciesTool lists Cilium network policies.
type CiliumPoliciesTool struct {
	Client CiliumK8sClient
}

func NewCiliumPoliciesTool(c CiliumK8sClient) *CiliumPoliciesTool {
	return &CiliumPoliciesTool{Client: c}
}

func (t *CiliumPoliciesTool) Name() string { return "cilium_policies" }

func (t *CiliumPoliciesTool) Description() string {
	return "List Cilium network policies (CiliumNetworkPolicy and CiliumClusterwideNetworkPolicy) " +
		"from a Kubernetes component. " +
		"Shows policy rules, selectors, and enforcement status. " +
		"Use to understand what network traffic is allowed or denied between workloads. " +
		"Use component_id of a Kubernetes component where Cilium is installed. " +
		"If you don't know the component_id, call list_components first."
}

func (t *CiliumPoliciesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Kubernetes component (where Cilium is installed).",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to filter namespaced policies. Omit for all namespaces. Clusterwide policies are always included.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *CiliumPoliciesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, _ := args["namespace"].(string)

	result := map[string][]map[string]any{}

	// Namespaced policies.
	if ns, err := t.Client.K8sListResources(ctx, sourceID, CiliumCRDTypes["CiliumNetworkPolicy"], namespace); err == nil {
		result["CiliumNetworkPolicy"] = ns
	}

	// Clusterwide policies (not namespace-scoped).
	if cw, err := t.Client.K8sListResources(ctx, sourceID, CiliumCRDTypes["CiliumClusterwideNetworkPolicy"], ""); err == nil {
		result["CiliumClusterwideNetworkPolicy"] = cw
	}

	total := len(result["CiliumNetworkPolicy"]) + len(result["CiliumClusterwideNetworkPolicy"])
	return map[string]any{
		"policies":     result,
		"count":        total,
		"namespace":    namespace,
		"component_id": sourceID,
	}, nil
}

// --- cilium_endpoints ---

// CiliumEndpointsTool lists Cilium endpoints with identity and health status.
type CiliumEndpointsTool struct {
	Client CiliumK8sClient
}

func NewCiliumEndpointsTool(c CiliumK8sClient) *CiliumEndpointsTool {
	return &CiliumEndpointsTool{Client: c}
}

func (t *CiliumEndpointsTool) Name() string { return "cilium_endpoints" }

func (t *CiliumEndpointsTool) Description() string {
	return "List Cilium endpoints with their identity labels and health status. " +
		"Each endpoint corresponds to a pod managed by Cilium. " +
		"Use to check endpoint health, identity, and policy enforcement state. " +
		"Use component_id of a Kubernetes component where Cilium is installed. " +
		"If you don't know the component_id, call list_components first."
}

func (t *CiliumEndpointsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Kubernetes component (where Cilium is installed).",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to filter endpoints. Omit for all namespaces.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *CiliumEndpointsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, _ := args["namespace"].(string)

	endpoints, err := t.Client.K8sListResources(ctx, sourceID, CiliumCRDTypes["CiliumEndpoint"], namespace)
	if err != nil {
		return nil, fmt.Errorf("list cilium endpoints: %w", err)
	}
	if endpoints == nil {
		endpoints = []map[string]any{}
	}

	return map[string]any{
		"endpoints":    extractCiliumEndpointSummaries(endpoints),
		"count":        len(endpoints),
		"namespace":    namespace,
		"component_id": sourceID,
	}, nil
}

// extractCiliumEndpointSummaries extracts a concise summary from CiliumEndpoint objects.
func extractCiliumEndpointSummaries(endpoints []map[string]any) []map[string]any {
	var out []map[string]any
	for _, ep := range endpoints {
		summary := map[string]any{}
		if meta, ok := ep["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
			summary["namespace"] = meta["namespace"]
		}
		if status, ok := ep["status"].(map[string]any); ok {
			summary["id"] = status["id"]
			summary["state"] = status["state"]
			if identity, ok := status["identity"].(map[string]any); ok {
				summary["identity_id"] = identity["id"]
				summary["labels"] = identity["labels"]
			}
			if health, ok := status["health"].(map[string]any); ok {
				summary["health"] = health
			}
		}
		out = append(out, summary)
	}
	return out
}
