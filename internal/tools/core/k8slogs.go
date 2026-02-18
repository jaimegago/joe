package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/constants"
	"github.com/jaimegago/joe/internal/llm"
)

// K8sLogsClient defines the subset of client.Client needed for K8sLogsTool.
type K8sLogsClient interface {
	K8sGetLogs(ctx context.Context, sourceID, namespace, pod, container string, tailLines int) (string, error)
}

// K8sLogsTool retrieves pod logs from a Kubernetes cluster via joecored.
type K8sLogsTool struct {
	client K8sLogsClient
}

// NewK8sLogsTool creates a new k8s_logs tool.
func NewK8sLogsTool(c K8sLogsClient) *K8sLogsTool {
	return &K8sLogsTool{client: c}
}

func (t *K8sLogsTool) Name() string { return "k8s_logs" }

func (t *K8sLogsTool) Description() string {
	return "Get logs from a Kubernetes pod. Returns the most recent log lines from a pod, optionally from a specific container. If you don't know the source_id, call list_sources first to discover available Kubernetes clusters."
}

func (t *K8sLogsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source/cluster.",
			},
			"namespace": {
				Type:        "string",
				Description: "Kubernetes namespace of the pod.",
			},
			"pod": {
				Type:        "string",
				Description: "Name of the pod to get logs from.",
			},
			"container": {
				Type:        "string",
				Description: "Container name within the pod. Required for multi-container pods.",
			},
			"tail": {
				Type:        "number",
				Description: "Number of log lines to return from the end. Defaults to 100.",
			},
		},
		Required: []string{"source_id", "namespace", "pod"},
	}
}

func (t *K8sLogsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	namespace, ok := args["namespace"].(string)
	if !ok || namespace == "" {
		return nil, fmt.Errorf("missing required parameter: namespace")
	}

	pod, ok := args["pod"].(string)
	if !ok || pod == "" {
		return nil, fmt.Errorf("missing required parameter: pod")
	}

	container, _ := args["container"].(string)

	tailLines := constants.DefaultK8sTailLines
	if t, ok := args["tail"].(float64); ok && t > 0 {
		tailLines = int(t)
	}

	logs, err := t.client.K8sGetLogs(ctx, sourceID, namespace, pod, container, tailLines)
	if err != nil {
		return nil, fmt.Errorf("k8s get logs failed: %w", err)
	}

	return map[string]any{
		"logs":      logs,
		"pod":       pod,
		"namespace": namespace,
		"source_id": sourceID,
	}, nil
}
