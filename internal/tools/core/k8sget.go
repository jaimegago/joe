package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// K8sGetTool queries Kubernetes resources via joecored.
type K8sGetTool struct {
	client *client.Client
}

// NewK8sGetTool creates a new k8s_get tool.
func NewK8sGetTool(c *client.Client) *K8sGetTool {
	return &K8sGetTool{client: c}
}

func (t *K8sGetTool) Name() string { return "k8s_get" }

func (t *K8sGetTool) Description() string {
	return "Query Kubernetes resources from a connected cluster. Lists resources by type, or gets a single resource by name. Supports pods, deployments, services, configmaps, secrets, namespaces, nodes, ingresses, statefulsets, daemonsets, replicasets, jobs, cronjobs, and events."
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

	return map[string]any{
		"resources": items,
		"count":     len(items),
		"source_id": sourceID,
	}, nil
}
