package core

import (
	"context"
	"fmt"

	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	"github.com/jaimegago/joe/internal/llm"
)

// EnvoyClient defines what Envoy tools need from the HTTP client.
type EnvoyClient interface {
	EnvoyClusters(ctx context.Context, sourceID string) ([]envoyadapter.ClusterStatus, error)
	EnvoyConfigDump(ctx context.Context, sourceID, section string) (map[string]any, error)
	EnvoyStats(ctx context.Context, sourceID, filter string) ([]envoyadapter.Stat, error)
}

// --- envoy_clusters ---

// EnvoyClustersTool lists Envoy cluster health summaries.
type EnvoyClustersTool struct {
	Client EnvoyClient
}

func NewEnvoyClustersTool(c EnvoyClient) *EnvoyClustersTool {
	return &EnvoyClustersTool{Client: c}
}

func (t *EnvoyClustersTool) Name() string { return "envoy_clusters" }

func (t *EnvoyClustersTool) Description() string {
	return "List Envoy upstream clusters with host health status and circuit breaker info. " +
		"Use to check upstream connectivity, health, and load distribution. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *EnvoyClustersTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Envoy source.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *EnvoyClustersTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	clusters, err := t.Client.EnvoyClusters(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("envoy clusters: %w", err)
	}
	if clusters == nil {
		clusters = []envoyadapter.ClusterStatus{}
	}
	return map[string]any{
		"clusters":  clusters,
		"count":     len(clusters),
		"source_id": sourceID,
	}, nil
}

// --- envoy_config ---

// EnvoyConfigTool dumps Envoy configuration sections.
type EnvoyConfigTool struct {
	Client EnvoyClient
}

func NewEnvoyConfigTool(c EnvoyClient) *EnvoyConfigTool {
	return &EnvoyConfigTool{Client: c}
}

func (t *EnvoyConfigTool) Name() string { return "envoy_config" }

func (t *EnvoyConfigTool) Description() string {
	return "Dump Envoy configuration. " +
		"Use section to filter: 'listeners', 'clusters', 'routes', 'endpoints', 'secrets'. " +
		"Leave section empty for the full config dump. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *EnvoyConfigTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Envoy source.",
			},
			"section": {
				Type:        "string",
				Description: "Config section: listeners, clusters, routes, endpoints, secrets. Omit for full dump.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *EnvoyConfigTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	section, _ := args["section"].(string)

	dump, err := t.Client.EnvoyConfigDump(ctx, sourceID, section)
	if err != nil {
		return nil, fmt.Errorf("envoy config: %w", err)
	}
	return map[string]any{
		"config":    dump,
		"section":   section,
		"source_id": sourceID,
	}, nil
}

// --- envoy_stats ---

// EnvoyStatsTool returns Envoy statistics.
type EnvoyStatsTool struct {
	Client EnvoyClient
}

func NewEnvoyStatsTool(c EnvoyClient) *EnvoyStatsTool {
	return &EnvoyStatsTool{Client: c}
}

func (t *EnvoyStatsTool) Name() string { return "envoy_stats" }

func (t *EnvoyStatsTool) Description() string {
	return "Get Envoy statistics filtered by an optional prefix. " +
		"Use filter to narrow results, e.g. 'cluster.backend' or 'http.ingress'. " +
		"Returns stat names and values. Useful for diagnosing request rates, errors, and retries. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *EnvoyStatsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Envoy source.",
			},
			"filter": {
				Type:        "string",
				Description: "Optional stat name prefix filter, e.g. 'cluster.backend' or 'http.ingress'.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *EnvoyStatsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	filter, _ := args["filter"].(string)

	stats, err := t.Client.EnvoyStats(ctx, sourceID, filter)
	if err != nil {
		return nil, fmt.Errorf("envoy stats: %w", err)
	}
	if stats == nil {
		stats = []envoyadapter.Stat{}
	}
	return map[string]any{
		"stats":     stats,
		"count":     len(stats),
		"filter":    filter,
		"source_id": sourceID,
	}, nil
}
