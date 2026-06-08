package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// KEDACRDTypes maps KEDA resource kind names to their full K8s resource identifiers.
var KEDACRDTypes = map[string]string{
	"ScaledObject":          "scaledobjects.keda.sh",
	"ScaledJob":             "scaledjobs.keda.sh",
	"TriggerAuthentication": "triggerauthentications.keda.sh",
}

// KEDAK8sClient defines what KEDA tools need from the K8s client.
type KEDAK8sClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
}

// --- keda_scaledobjects ---

// KEDAScaledObjectsTool lists KEDA ScaledObject and ScaledJob resources.
type KEDAScaledObjectsTool struct {
	Client KEDAK8sClient
}

func NewKEDAScaledObjectsTool(c KEDAK8sClient) *KEDAScaledObjectsTool {
	return &KEDAScaledObjectsTool{Client: c}
}

func (t *KEDAScaledObjectsTool) Name() string { return "keda_scaledobjects" }

func (t *KEDAScaledObjectsTool) Description() string {
	return "List KEDA ScaledObject and ScaledJob resources with their scaling configuration. " +
		"Shows target workload, trigger types (Kafka, Prometheus, Redis, etc.), " +
		"min/max replicas, and current scaling status. " +
		"Use to understand how workloads are scaled and whether autoscaling is healthy. " +
		"Use component_id of a Kubernetes source where KEDA is installed. " +
		"If you don't know the component_id, call list_components first."
}

func (t *KEDAScaledObjectsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source (where KEDA is installed).",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to filter. Omit for all namespaces.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *KEDAScaledObjectsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, _ := args["namespace"].(string)

	result := map[string][]map[string]any{}

	if scaledObjects, err := t.Client.K8sListResources(ctx, sourceID, KEDACRDTypes["ScaledObject"], namespace); err == nil {
		result["ScaledObject"] = extractKEDAScaledObjectSummaries(scaledObjects)
	}

	if scaledJobs, err := t.Client.K8sListResources(ctx, sourceID, KEDACRDTypes["ScaledJob"], namespace); err == nil {
		result["ScaledJob"] = extractKEDAScaledObjectSummaries(scaledJobs)
	}

	total := len(result["ScaledObject"]) + len(result["ScaledJob"])
	return map[string]any{
		"scaled_objects": result,
		"count":          total,
		"namespace":      namespace,
		"component_id":   sourceID,
	}, nil
}

// extractKEDAScaledObjectSummaries extracts a concise summary from KEDA ScaledObject/ScaledJob objects.
func extractKEDAScaledObjectSummaries(objects []map[string]any) []map[string]any {
	var out []map[string]any
	for _, obj := range objects {
		summary := map[string]any{}
		if meta, ok := obj["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
			summary["namespace"] = meta["namespace"]
		}
		if spec, ok := obj["spec"].(map[string]any); ok {
			if target, ok := spec["scaleTargetRef"].(map[string]any); ok {
				summary["target_name"] = target["name"]
				summary["target_kind"] = target["kind"]
			}
			summary["min_replicas"] = spec["minReplicaCount"]
			summary["max_replicas"] = spec["maxReplicaCount"]
			// Extract trigger types.
			if triggers, ok := spec["triggers"].([]any); ok {
				types := make([]string, 0, len(triggers))
				for _, tr := range triggers {
					if tm, ok := tr.(map[string]any); ok {
						if typ, ok := tm["type"].(string); ok {
							types = append(types, typ)
						}
					}
				}
				summary["trigger_types"] = types
			}
		}
		if status, ok := obj["status"].(map[string]any); ok {
			summary["ready_replicas"] = status["currentReplicas"]
			summary["desired_replicas"] = status["desiredReplicas"]
			if conditions, ok := status["conditions"].([]any); ok {
				for _, cond := range conditions {
					if cm, ok := cond.(map[string]any); ok && cm["type"] == "Ready" {
						summary["ready"] = cm["status"]
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
