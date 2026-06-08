package core

import (
	"context"
	"fmt"

	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	"github.com/jaimegago/joe/internal/llm"
)

// NginxClient defines what nginx tools need from the HTTP client.
type NginxClient interface {
	NginxIngresses(ctx context.Context, sourceID, namespace string) ([]nginxadapter.Ingress, error)
	NginxStatus(ctx context.Context, sourceID string) (*nginxadapter.NginxStatus, error)
	NginxConfigMaps(ctx context.Context, sourceID, namespace string) ([]nginxadapter.ConfigMapSummary, error)
}

// --- nginx_ingresses ---

// NginxIngressesTool lists Ingress resources managed by NGINX Ingress Controller.
type NginxIngressesTool struct {
	Client NginxClient
}

func NewNginxIngressesTool(c NginxClient) *NginxIngressesTool {
	return &NginxIngressesTool{Client: c}
}

func (t *NginxIngressesTool) Name() string { return "nginx_ingresses" }

func (t *NginxIngressesTool) Description() string {
	return "List Kubernetes Ingress resources from an NGINX Ingress Controller source. " +
		"Shows hosts, paths, backend services, TLS settings, and load balancer addresses. " +
		"Use to answer 'what is exposed?', 'which service handles /api?', or 'which ingress uses TLS?'. " +
		"If you don't know the component_id, call list_components first."
}

func (t *NginxIngressesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the nginx-ingress source.",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to filter by. Omit to list from all namespaces.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *NginxIngressesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, _ := args["namespace"].(string)

	ingresses, err := t.Client.NginxIngresses(ctx, sourceID, namespace)
	if err != nil {
		return nil, fmt.Errorf("nginx ingresses: %w", err)
	}
	if ingresses == nil {
		ingresses = []nginxadapter.Ingress{}
	}
	return map[string]any{
		"ingresses":    ingresses,
		"count":        len(ingresses),
		"component_id": sourceID,
		"namespace":    namespace,
	}, nil
}

// --- nginx_status ---

// NginxStatusTool fetches NGINX connection statistics.
type NginxStatusTool struct {
	Client NginxClient
}

func NewNginxStatusTool(c NginxClient) *NginxStatusTool {
	return &NginxStatusTool{Client: c}
}

func (t *NginxStatusTool) Name() string { return "nginx_status" }

func (t *NginxStatusTool) Description() string {
	return "Get NGINX Ingress Controller connection statistics from the status endpoint " +
		"(active connections, reading/writing/waiting workers, total accepts/requests). " +
		"Requires status_url to be configured in the source. " +
		"Use to check if NGINX is under load or has connection issues. " +
		"If you don't know the component_id, call list_components first."
}

func (t *NginxStatusTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the nginx-ingress source.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *NginxStatusTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	status, err := t.Client.NginxStatus(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("nginx status: %w", err)
	}
	return map[string]any{
		"status":       status,
		"component_id": sourceID,
	}, nil
}

// --- nginx_config ---

// NginxConfigTool lists NGINX ConfigMaps containing controller configuration.
type NginxConfigTool struct {
	Client NginxClient
}

func NewNginxConfigTool(c NginxClient) *NginxConfigTool {
	return &NginxConfigTool{Client: c}
}

func (t *NginxConfigTool) Name() string { return "nginx_config" }

func (t *NginxConfigTool) Description() string {
	return "List ConfigMaps containing NGINX Ingress Controller configuration. " +
		"Shows proxy settings, rate limiting, and custom NGINX directives. " +
		"Typical namespace is 'ingress-nginx'. " +
		"If you don't know the component_id, call list_components first."
}

func (t *NginxConfigTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the nginx-ingress source.",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to search for ConfigMaps. Defaults to all namespaces.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *NginxConfigTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	namespace, _ := args["namespace"].(string)

	cms, err := t.Client.NginxConfigMaps(ctx, sourceID, namespace)
	if err != nil {
		return nil, fmt.Errorf("nginx configmaps: %w", err)
	}
	if cms == nil {
		cms = []nginxadapter.ConfigMapSummary{}
	}
	return map[string]any{
		"config_maps":  cms,
		"count":        len(cms),
		"component_id": sourceID,
		"namespace":    namespace,
	}, nil
}
