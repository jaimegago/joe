package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// IstioCRDTypes maps Istio resource kind names to their full K8s resource identifiers.
var IstioCRDTypes = map[string]string{
	"VirtualService":        "virtualservices.networking.istio.io",
	"DestinationRule":       "destinationrules.networking.istio.io",
	"Gateway":               "gateways.networking.istio.io",
	"ServiceEntry":          "serviceentries.networking.istio.io",
	"PeerAuthentication":    "peerauthentications.security.istio.io",
	"AuthorizationPolicy":   "authorizationpolicies.security.istio.io",
	"RequestAuthentication": "requestauthentications.security.istio.io",
}

// IstioK8sClient defines what Istio tools need from the K8s client.
// It is the same interface shape as FluxK8sClient.
type IstioK8sClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
	K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error)
}

// --- istio_config ---

// IstioConfigTool lists Istio service mesh CRDs from a Kubernetes source.
type IstioConfigTool struct {
	Client IstioK8sClient
}

func NewIstioConfigTool(c IstioK8sClient) *IstioConfigTool {
	return &IstioConfigTool{Client: c}
}

func (t *IstioConfigTool) Name() string { return "istio_config" }

func (t *IstioConfigTool) Description() string {
	return "List Istio service mesh resources (VirtualService, DestinationRule, Gateway, " +
		"ServiceEntry, PeerAuthentication, AuthorizationPolicy) from a Kubernetes source. " +
		"Shows traffic policies, mTLS settings, routing rules, and auth policies. " +
		"Use source_id of a Kubernetes source where Istio is installed. " +
		"Supported kinds: VirtualService, DestinationRule, Gateway, ServiceEntry, " +
		"PeerAuthentication, AuthorizationPolicy, RequestAuthentication. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *IstioConfigTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source (where Istio is installed).",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to filter by. Omit to list from all namespaces.",
			},
			"kind": {
				Type:        "string",
				Description: "Optional Istio resource kind to filter by: VirtualService, DestinationRule, Gateway, etc. Omit for all kinds.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *IstioConfigTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	namespace, _ := args["namespace"].(string)
	kind, _ := args["kind"].(string)

	// If a specific kind is requested, validate and list only that type.
	if kind != "" {
		crdName, ok := IstioCRDTypes[kind]
		if !ok {
			supported := make([]string, 0, len(IstioCRDTypes))
			for k := range IstioCRDTypes {
				supported = append(supported, k)
			}
			return nil, fmt.Errorf("unsupported Istio kind %q; supported: %v", kind, supported)
		}
		resources, err := t.Client.K8sListResources(ctx, sourceID, crdName, namespace)
		if err != nil {
			return nil, fmt.Errorf("list istio %s: %w", kind, err)
		}
		return map[string]any{
			"resources": resources,
			"kind":      kind,
			"namespace": namespace,
			"source_id": sourceID,
		}, nil
	}

	// List all Istio CRD types, skip missing ones gracefully.
	result := map[string]any{}
	for kindName, crdName := range IstioCRDTypes {
		resources, err := t.Client.K8sListResources(ctx, sourceID, crdName, namespace)
		if err != nil {
			continue
		}
		if len(resources) > 0 {
			result[kindName] = resources
		}
	}
	return map[string]any{
		"resources": result,
		"namespace": namespace,
		"source_id": sourceID,
	}, nil
}

// --- istio_resource ---

// IstioResourceTool gets details for a specific Istio resource.
type IstioResourceTool struct {
	Client IstioK8sClient
}

func NewIstioResourceTool(c IstioK8sClient) *IstioResourceTool {
	return &IstioResourceTool{Client: c}
}

func (t *IstioResourceTool) Name() string { return "istio_resource" }

func (t *IstioResourceTool) Description() string {
	return "Get full details for a specific Istio resource. " +
		"Returns the complete spec and status, including routing rules, traffic policies, " +
		"mTLS settings, and conditions. " +
		"Supported kinds: VirtualService, DestinationRule, Gateway, ServiceEntry, " +
		"PeerAuthentication, AuthorizationPolicy, RequestAuthentication. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *IstioResourceTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source (where Istio is installed).",
			},
			"kind": {
				Type:        "string",
				Description: "Istio resource kind: VirtualService, DestinationRule, Gateway, ServiceEntry, PeerAuthentication, AuthorizationPolicy, or RequestAuthentication.",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace of the Istio resource.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the Istio resource.",
			},
		},
		Required: []string{"source_id", "kind", "namespace", "name"},
	}
}

func (t *IstioResourceTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
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

	crdName, ok := IstioCRDTypes[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported Istio kind %q", kind)
	}

	resource, err := t.Client.K8sGetResource(ctx, sourceID, crdName, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("get istio %s %s/%s: %w", kind, namespace, name, err)
	}
	return map[string]any{
		"resource":  resource,
		"kind":      kind,
		"namespace": namespace,
		"name":      name,
		"source_id": sourceID,
	}, nil
}
