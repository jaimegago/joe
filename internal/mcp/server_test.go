package mcp_test

import (
	"testing"

	"github.com/jaimegago/joe/internal/mcp"
)

// TestNewServer_RegistersAllTools verifies that NewServer registers
// all 8 expected Joe tools.
func TestNewServer_RegistersAllTools(t *testing.T) {
	s := mcp.NewServer(nil) // nil client is OK for registration test

	toolMap := s.ListTools()

	expected := []string{
		"joe_graph_query",
		"joe_graph_related",
		"joe_k8s",
		"joe_metrics",
		"joe_logs",
		"joe_traces",
		"joe_alerts",
		"joe_knowledge_search",
	}

	for _, name := range expected {
		if _, ok := toolMap[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

// TestNewServer_ToolCount verifies exactly 8 tools are registered.
func TestNewServer_ToolCount(t *testing.T) {
	s := mcp.NewServer(nil)
	toolMap := s.ListTools()
	if len(toolMap) != 8 {
		t.Errorf("expected 8 tools, got %d", len(toolMap))
	}
}

// TestToolDefs_HaveDescriptions verifies each tool has a non-empty description.
func TestToolDefs_HaveDescriptions(t *testing.T) {
	s := mcp.NewServer(nil)
	toolMap := s.ListTools()

	for name, st := range toolMap {
		if st.Tool.Description == "" {
			t.Errorf("tool %q has empty description", name)
		}
	}
}

// TestToolDefs_RequiredParams verifies required parameters are declared on each tool.
func TestToolDefs_RequiredParams(t *testing.T) {
	tests := []struct {
		tool     string
		required []string
	}{
		{"joe_graph_query", []string{"query"}},
		{"joe_graph_related", []string{"node_id"}},
		{"joe_k8s", []string{"service", "question"}},
		{"joe_metrics", []string{"service", "question"}},
		{"joe_logs", []string{"service", "question"}},
		{"joe_traces", []string{"service", "question"}},
		{"joe_alerts", []string{"service"}},
		{"joe_knowledge_search", []string{"query"}},
	}

	s := mcp.NewServer(nil)
	toolMap := s.ListTools()

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			st, ok := toolMap[tt.tool]
			if !ok {
				t.Fatalf("tool %q not found", tt.tool)
			}

			reqSet := map[string]bool{}
			for _, r := range st.Tool.InputSchema.Required {
				reqSet[r] = true
			}

			for _, param := range tt.required {
				if !reqSet[param] {
					t.Errorf("tool %q: expected required param %q", tt.tool, param)
				}
			}
		})
	}
}
