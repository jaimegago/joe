package tools

import (
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	coretools "github.com/jaimegago/joe/internal/tools/core"
)

// stubCoreClient satisfies coretools.CoreToolsClient for registry-construction
// tests. These tests only inspect tool schemas/registration and never invoke a
// client method, so the embedded nil interface is sufficient. (In production the
// registry is wired to the in-process accessor client; see internal/api.)
type stubCoreClient struct{ coretools.CoreToolsClient }

// validatePropertySchema checks a single property for schema correctness.
// Returns a non-nil error describing the exact problem so the developer knows
// what to fix in the tool's Parameters() definition.
func validatePropertySchema(toolName, propName string, prop llm.Property) error {
	if prop.Type == "array" && prop.Items == nil {
		return fmt.Errorf(
			"tool %q: parameter %q has type \"array\" but no Items defined — "+
				"LLM providers (e.g. Gemini) reject array properties without an item type; "+
				"add Items: &llm.Property{Type: \"...\", Description: \"...\"} in Parameters()",
			toolName, propName,
		)
	}
	return nil
}

// TestToolSchemaValidity ensures every registered tool produces a schema that
// is valid for all LLM providers.  It acts as a compile-time-level gate so
// schema bugs are caught by `go test` rather than at runtime against a live API.
func TestToolSchemaValidity(t *testing.T) {
	coreClient := stubCoreClient{}
	registry := NewCoreRegistry(coreClient, nil, nil)

	for _, tool := range registry.GetAll() {
		t.Run(tool.Name(), func(t *testing.T) {
			params := tool.Parameters()
			for propName, prop := range params.Properties {
				if err := validatePropertySchema(tool.Name(), propName, prop); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func TestNewCoreRegistry(t *testing.T) {
	coreClient := stubCoreClient{}
	registry := NewCoreRegistry(coreClient, nil, nil)

	if registry == nil {
		t.Fatal("NewCoreRegistry() returned nil")
	}

	// Should have shared diagnostic tools + core tools (no local tools)
	expectedTools := map[string]bool{
		// Shared diagnostic tools (Phase 6.6)
		"tcp_connect":  true,
		"port_scan":    true,
		"dns_lookup":   true,
		"http_request": true,
		"trace_route":  true,
		"web_search":   true,
		// Core tools (call joecored API)
		"list_components":     true,
		"graph_query":         true,
		"graph_related":       true,
		"k8s_get":             true,
		"k8s_logs":            true,
		"git_read":            true,
		"git_log":             true,
		"git_diff":            true,
		"aws_ec2":             true,
		"aws_eks":             true,
		"aws_rds":             true,
		"aws_vpc":             true,
		"prometheus_query":    true,
		"loki_query":          true,
		"tempo_search":        true,
		"jaeger_traces":       true,
		"alertmanager_alerts": true,
		"pagerduty_incidents": true,
		"grafana_dashboards":  true,
		// Data store tools (Phase 6.7)
		"postgres_stat":         true,
		"postgres_query":        true,
		"mysql_stat":            true,
		"mysql_query":           true,
		"redis_info":            true,
		"redis_slowlog":         true,
		"mongodb_stat":          true,
		"kafka_topics":          true,
		"kafka_brokers":         true,
		"kafka_consumers":       true,
		"elasticsearch_health":  true,
		"elasticsearch_indices": true,
		// Networking & Ingress tools (Phase 6.9)
		"nginx_ingresses":  true,
		"nginx_status":     true,
		"nginx_config":     true,
		"envoy_clusters":   true,
		"envoy_config":     true,
		"envoy_stats":      true,
		"istio_config":     true,
		"istio_resource":   true,
		"cilium_policies":  true,
		"cilium_endpoints": true,
		// GitOps, CD & IaC tools (Phase 6.8)
		"argocd_apps":        true,
		"argocd_app":         true,
		"argocd_diff":        true,
		"argocd_history":     true,
		"flux_status":        true,
		"flux_resource":      true,
		"terraform_state":    true,
		"terraform_resource": true,
		"terraform_outputs":  true,
		"helm_releases":      true,
		"helm_release":       true,
		"helm_history":       true,
		// K8s CRD-based tools (Phase 6.10)
		"certmanager_certs":    true,
		"certmanager_issuers":  true,
		"keda_scaledobjects":   true,
		"opa_constraints":      true,
		"opa_violations":       true,
		"crossplane_providers": true,
		"crossplane_resources": true,
		"falco_alerts":         true,
		"falco_rules":          true,
		// Artifact registry tools (Phase 6.13)
		"registry_query":    true,
		"artifactory_query": true,
		"ecr_query":         true,
		// Code review tools (Phase 10)
		"github_pr_get":          true,
		"github_pr_diff":         true,
		"github_comment":         true,
		"github_request_changes": true,
		"gitlab_mr_get":          true,
		"gitlab_mr_diff":         true,
		"gitlab_comment":         true,
	}

	for toolName := range expectedTools {
		tool, err := registry.Get(toolName)
		if err != nil {
			t.Errorf("missing '%s' tool: %v", toolName, err)
		}
		if tool == nil {
			t.Errorf("'%s' tool is nil", toolName)
		}
	}

	allTools := registry.GetAll()
	if len(allTools) != len(expectedTools) {
		t.Errorf("has %d tools, want %d", len(allTools), len(expectedTools))
	}
}
