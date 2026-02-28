package tools

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/safety"
)

// safemodeTestTool is a minimal Tool implementation for safe mode tests.
type safemodeTestTool struct{ name string }

func (f *safemodeTestTool) Name() string        { return f.name }
func (f *safemodeTestTool) Description() string { return "safe mode test tool" }
func (f *safemodeTestTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{Type: "object"}
}
func (f *safemodeTestTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return "ok", nil
}

func TestExecutor_SafeMode_BlocksT2(t *testing.T) {
	safety.ActivateSafeMode()
	safety.Trigger(safety.PanicSourceAPI, "test")
	t.Cleanup(func() {
		safety.DeactivateSafeMode()
		safety.Reset()
	})

	reg := NewRegistry()
	// graph_add_node is T2 — should be blocked in safe mode
	reg.Register(&safemodeTestTool{name: "graph_add_node"})

	e := NewExecutor(reg, nil)
	_, err := e.Execute(context.Background(), "graph_add_node", nil)
	if err == nil {
		t.Fatal("expected error in safe mode for T2 tool")
	}
}

func TestExecutor_SafeMode_AllowsT1(t *testing.T) {
	safety.ActivateSafeMode()
	safety.Trigger(safety.PanicSourceAPI, "test")
	t.Cleanup(func() {
		safety.DeactivateSafeMode()
		safety.Reset()
	})

	reg := NewRegistry()
	// read_file is T1 — should succeed even in safe mode
	reg.Register(&safemodeTestTool{name: "read_file"})

	e := NewExecutor(reg, nil)
	_, err := e.Execute(context.Background(), "read_file", nil)
	if err != nil {
		t.Fatalf("expected T1 tool to succeed in safe mode, got: %v", err)
	}
}

func TestExecutor_NormalMode_AllowsT2(t *testing.T) {
	safety.DeactivateSafeMode()
	safety.Reset()
	t.Cleanup(func() {
		safety.DeactivateSafeMode()
		safety.Reset()
	})

	reg := NewRegistry()
	reg.Register(&safemodeTestTool{name: "graph_add_node"})

	policy := safety.DefaultPolicy()
	e := NewExecutor(reg, nil, WithPolicy(policy))
	// T2 should pass the safe mode check (policy allows graph_mutations by default)
	_, err := e.Execute(context.Background(), "graph_add_node", nil)
	if err != nil {
		t.Fatalf("expected T2 tool to succeed in normal mode: %v", err)
	}
}
