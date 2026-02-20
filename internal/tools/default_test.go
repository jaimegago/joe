package tools

import (
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

func TestNewDefaultRegistry(t *testing.T) {
	registry := NewDefaultRegistry(nil)

	if registry == nil {
		t.Fatal("NewDefaultRegistry() returned nil")
	}

	// Define expected tools (local tools + shared diagnostic tools)
	expectedTools := map[string]bool{
		"echo":             true,
		"ask_user":         true,
		"read_file":        true,
		"write_file":       true,
		"local_git_status": true,
		"local_git_diff":   true,
		"run_command":      true,
		// Shared diagnostic tools (Phase 6.6)
		"tcp_connect":  true,
		"port_scan":    true,
		"dns_lookup":   true,
		"http_request": true,
		"system_info":  true,
		"trace_route":  true,
	}

	// Test that all expected tools are registered
	for toolName := range expectedTools {
		tool, err := registry.Get(toolName)
		if err != nil {
			t.Errorf("NewDefaultRegistry() missing '%s' tool: %v", toolName, err)
		}
		if tool == nil {
			t.Errorf("NewDefaultRegistry() '%s' tool is nil", toolName)
		}
	}

	// Test that we have exactly the expected number of tools
	allTools := registry.GetAll()
	if len(allTools) != len(expectedTools) {
		t.Errorf("NewDefaultRegistry() has %d tools, want %d", len(allTools), len(expectedTools))
	}

	// Test that tool definitions can be generated
	definitions := registry.ToDefinitions()
	if len(definitions) != len(expectedTools) {
		t.Errorf("NewDefaultRegistry().ToDefinitions() returned %d definitions, want %d", len(definitions), len(expectedTools))
	}

	// Verify all tool names in definitions are expected
	for _, def := range definitions {
		if !expectedTools[def.Name] {
			t.Errorf("Unexpected tool in definitions: %s", def.Name)
		}
	}
}

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
	coreClient := client.New("http://localhost:7777")
	registry := NewDefaultRegistryWithClient(coreClient, nil)

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

func TestNewDefaultRegistryWithClient(t *testing.T) {
	coreClient := client.New("http://localhost:7777")
	registry := NewDefaultRegistryWithClient(coreClient, nil)

	if registry == nil {
		t.Fatal("NewDefaultRegistryWithClient() returned nil")
	}

	// Should have all local tools + shared diagnostic tools + core tools
	expectedTools := map[string]bool{
		"echo":             true,
		"ask_user":         true,
		"read_file":        true,
		"write_file":       true,
		"local_git_status": true,
		"local_git_diff":   true,
		"run_command":      true,
		// Shared diagnostic tools (Phase 6.6)
		"tcp_connect":  true,
		"port_scan":    true,
		"dns_lookup":   true,
		"http_request": true,
		"system_info":  true,
		"trace_route":  true,
		// Core tools (call joecored API)
		"list_sources":        true,
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
