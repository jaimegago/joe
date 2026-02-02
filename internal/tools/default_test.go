package tools

import (
	"testing"
)

func TestNewDefaultRegistry(t *testing.T) {
	registry := NewDefaultRegistry()

	if registry == nil {
		t.Fatal("NewDefaultRegistry() returned nil")
	}

	// Test that echo tool is registered
	echoTool, err := registry.Get("echo")
	if err != nil {
		t.Errorf("NewDefaultRegistry() missing 'echo' tool: %v", err)
	}
	if echoTool == nil {
		t.Error("NewDefaultRegistry() 'echo' tool is nil")
	}

	// Test that ask_user tool is registered
	askUserTool, err := registry.Get("ask_user")
	if err != nil {
		t.Errorf("NewDefaultRegistry() missing 'ask_user' tool: %v", err)
	}
	if askUserTool == nil {
		t.Error("NewDefaultRegistry() 'ask_user' tool is nil")
	}

	// Test that we have exactly 2 tools
	allTools := registry.GetAll()
	if len(allTools) != 2 {
		t.Errorf("NewDefaultRegistry() has %d tools, want 2", len(allTools))
	}

	// Test that tool definitions can be generated
	definitions := registry.ToDefinitions()
	if len(definitions) != 2 {
		t.Errorf("NewDefaultRegistry().ToDefinitions() returned %d definitions, want 2", len(definitions))
	}

	// Verify tool names in definitions
	toolNames := make(map[string]bool)
	for _, def := range definitions {
		toolNames[def.Name] = true
	}

	if !toolNames["echo"] {
		t.Error("NewDefaultRegistry() definitions missing 'echo'")
	}
	if !toolNames["ask_user"] {
		t.Error("NewDefaultRegistry() definitions missing 'ask_user'")
	}
}
