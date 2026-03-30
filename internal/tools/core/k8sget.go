package core

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jaimegago/joe/internal/llm"
)

// K8sGetClient defines the subset of client.Client needed for K8sGetTool.
type K8sGetClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
	K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error)
}

// K8sGetTool queries Kubernetes resources via joecored.
type K8sGetTool struct {
	client K8sGetClient
}

// NewK8sGetTool creates a new k8s_get tool.
func NewK8sGetTool(c K8sGetClient) *K8sGetTool {
	return &K8sGetTool{client: c}
}

func (t *K8sGetTool) Name() string { return "k8s_get" }

func (t *K8sGetTool) Description() string {
	return "Query Kubernetes resources from a connected cluster. Lists resources by type, or gets a single resource by name. Supports pods, deployments, services, configmaps, secrets, namespaces, nodes, ingresses, statefulsets, daemonsets, replicasets, jobs, cronjobs, and events. If you don't know the source_id, call list_sources first to discover available Kubernetes clusters."
}

func (t *K8sGetTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source/cluster to query.",
			},
			"resource": {
				Type:        "string",
				Description: "Kubernetes resource type (e.g. pods, deployments, services, configmaps).",
			},
			"namespace": {
				Type:        "string",
				Description: "Kubernetes namespace. Omit or leave empty for all namespaces.",
			},
			"name": {
				Type:        "string",
				Description: "Name of a specific resource to get. Omit to list all resources of the type.",
			},
		},
		Required: []string{"source_id", "resource"},
	}
}

func (t *K8sGetTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	resource, ok := args["resource"].(string)
	if !ok || resource == "" {
		return nil, fmt.Errorf("missing required parameter: resource")
	}

	namespace, _ := args["namespace"].(string)
	name, _ := args["name"].(string)

	// Single resource get
	if name != "" {
		obj, err := t.client.K8sGetResource(ctx, sourceID, resource, namespace, name)
		if err != nil {
			return nil, fmt.Errorf("k8s get resource failed: %w", err)
		}
		redactSecretData(obj)
		return map[string]any{
			"resource":  obj,
			"source_id": sourceID,
		}, nil
	}

	// List resources
	items, err := t.client.K8sListResources(ctx, sourceID, resource, namespace)
	if err != nil {
		return nil, fmt.Errorf("k8s list resources failed: %w", err)
	}

	for _, item := range items {
		redactSecretData(item)
	}

	return map[string]any{
		"resources": items,
		"count":     len(items),
		"source_id": sourceID,
	}, nil
}

// redactSecretData replaces the values in a Kubernetes Secret's data and
// stringData fields with "[REDACTED]". This prevents secret values from
// entering the LLM context. The check uses the "kind" field so it works
// regardless of whether the resource type was requested as "secrets" or
// fetched indirectly.
func redactSecretData(obj map[string]any) {
	kind, _ := obj["kind"].(string)
	if kind != "Secret" {
		return
	}

	redacted := false

	if data, ok := obj["data"].(map[string]any); ok {
		for k := range data {
			data[k] = "[REDACTED]"
		}
		redacted = true
	}

	if stringData, ok := obj["stringData"].(map[string]any); ok {
		for k := range stringData {
			stringData[k] = "[REDACTED]"
		}
		redacted = true
	}

	if redacted {
		slog.Info("k8s_get: redacted secret data values",
			"name", obj["metadata"],
			"kind", kind,
		)
	}
}
