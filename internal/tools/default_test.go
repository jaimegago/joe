package tools

import (
	"testing"

	"github.com/jaimegago/joe/internal/client"
)

func TestNewDefaultRegistry(t *testing.T) {
	registry := NewDefaultRegistry()

	if registry == nil {
		t.Fatal("NewDefaultRegistry() returned nil")
	}

	// Define expected tools
	expectedTools := map[string]bool{
		"echo":             true,
		"ask_user":         true,
		"read_file":        true,
		"write_file":       true,
		"local_git_status": true,
		"local_git_diff":   true,
		"run_command":      true,
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

func TestNewDefaultRegistryWithClient(t *testing.T) {
	coreClient := client.New("http://localhost:7777")
	registry := NewDefaultRegistryWithClient(coreClient)

	if registry == nil {
		t.Fatal("NewDefaultRegistryWithClient() returned nil")
	}

	// Should have all local tools plus core tools
	expectedTools := map[string]bool{
		"echo":             true,
		"ask_user":         true,
		"read_file":        true,
		"write_file":       true,
		"local_git_status": true,
		"local_git_diff":   true,
		"run_command":      true,
		"graph_query":      true,
		"graph_related":    true,
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
