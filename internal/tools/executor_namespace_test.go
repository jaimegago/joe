package tools

import (
	"context"
	"errors"
	"testing"
)

func TestExecutor_NamespaceScope_AllowedNamespace(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "pods", nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedSources([]string{"cluster-a"}),
		WithAllowedNamespaces([]string{"frontend", "backend"}),
	)

	result, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "frontend",
	})
	if err != nil {
		t.Fatalf("tool targeting allowed namespace should succeed, got: %v", err)
	}
	if result != "pods" {
		t.Errorf("result = %v, want pods", result)
	}
}

func TestExecutor_NamespaceScope_DeniedNamespace(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			t.Fatal("tool should not execute for unauthorized namespace")
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedSources([]string{"cluster-a"}),
		WithAllowedNamespaces([]string{"frontend"}),
	)

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "payments",
	})
	if err == nil {
		t.Fatal("expected namespace violation error for unauthorized namespace")
	}

	var nsErr *NamespaceViolationError
	if !errors.As(err, &nsErr) {
		t.Fatalf("expected NamespaceViolationError, got %T: %v", err, err)
	}
	if nsErr.Namespace != "payments" {
		t.Errorf("NamespaceViolationError.Namespace = %q, want %q", nsErr.Namespace, "payments")
	}
}

func TestExecutor_NamespaceScope_ReadAlsoBlocked(t *testing.T) {
	// k8s_get is a read (T1) tool — namespace enforcement must block reads too
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			t.Fatal("read should not execute for unauthorized namespace")
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedNamespaces([]string{"frontend"}),
	)

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "deployments",
		"namespace": "payments",
	})
	if err == nil {
		t.Fatal("k8s_get (read) targeting unauthorized namespace should be blocked")
	}

	var nsErr *NamespaceViolationError
	if !errors.As(err, &nsErr) {
		t.Fatalf("expected NamespaceViolationError, got %T: %v", err, err)
	}
}

func TestExecutor_NamespaceScope_NoNamespaceArg_Allowed(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "graph_query",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "ok", nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedNamespaces([]string{"frontend"}),
	)

	result, err := executor.Execute(context.Background(), "graph_query", map[string]any{
		"query": "services",
	})
	if err != nil {
		t.Fatalf("tool without namespace arg should not be blocked: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
}

func TestExecutor_NamespaceScope_NilAllowedNamespaces_NoRestriction(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "ok", nil
		},
	})

	// Default (nil allowedNamespaces) should not restrict
	executor := NewExecutor(registry, nil)

	result, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "any-namespace",
	})
	if err != nil {
		t.Fatalf("nil allowedNamespaces should not restrict: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
}

func TestExecutor_NamespaceScope_EmptyAllowedNamespaces_DeniesAll(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			t.Fatal("should not execute")
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedNamespaces([]string{}),
	)

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "anything",
	})
	if err == nil {
		t.Fatal("empty allowedNamespaces should deny all namespace-scoped calls")
	}
	var nsErr *NamespaceViolationError
	if !errors.As(err, &nsErr) {
		t.Fatalf("expected NamespaceViolationError, got %T: %v", err, err)
	}
}

func TestExecutor_NamespaceScope_EnforcedRegardlessOfLLM(t *testing.T) {
	// Even if the LLM says "I'll check the payments namespace", the executor blocks it.
	// This test verifies the executor check is deterministic — it doesn't matter what
	// the LLM "intended", only what namespace the tool call targets.
	registry := NewRegistry()
	callCount := 0
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			callCount++
			return "ok", nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedNamespaces([]string{"frontend"}),
	)

	// Run multiple times to verify determinism
	for i := 0; i < 10; i++ {
		_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
			"source_id": "cluster-a",
			"resource":  "pods",
			"namespace": "payments",
		})
		if err == nil {
			t.Fatalf("iteration %d: expected namespace violation, got nil", i)
		}
	}

	if callCount != 0 {
		t.Errorf("tool was called %d times, expected 0 (all should be blocked)", callCount)
	}
}

func TestExecutor_NamespaceScope_ErrorContainsZoneNames(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedNamespaces([]string{"frontend"}),
		WithScopeZoneNames("zone-a (Frontend)"),
	)

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "payments",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Failure 3: error message must contain zone names
	if !contains(err.Error(), "zone-a") {
		t.Errorf("error message should contain zone name, got: %s", err.Error())
	}
	if !contains(err.Error(), "payments") {
		t.Errorf("error message should contain blocked namespace, got: %s", err.Error())
	}
	if !contains(err.Error(), "frontend") {
		t.Errorf("error message should contain allowed namespace, got: %s", err.Error())
	}
}

func TestExecutor_NamespaceScope_ErrorContainsTargetZone(t *testing.T) {
	// When namespaceZoneMap maps the denied namespace to a zone, the error
	// message must include the target zone so the LLM can articulate the
	// boundary (OASIS zone-violation-001).
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedNamespaces([]string{"frontend"}),
		WithScopeZoneNames("Frontend Zone (zone-a)"),
		WithNamespaceZoneMap(map[string]string{
			"frontend": "Frontend Zone (zone-a)",
			"payments": "Payments Zone (zone-b)",
			"orders":   "Payments Zone (zone-b)",
		}),
	)

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "payments",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	// Must contain the authorized zone
	if !contains(errMsg, "Frontend Zone") {
		t.Errorf("error should contain authorized zone name, got: %s", errMsg)
	}
	// Must contain the TARGET zone (the zone payments belongs to)
	if !contains(errMsg, "Payments Zone") {
		t.Errorf("error should contain target zone name, got: %s", errMsg)
	}
	// Must contain the denied namespace
	if !contains(errMsg, "payments") {
		t.Errorf("error should contain denied namespace, got: %s", errMsg)
	}
}

func TestExecutor_NamespaceScope_ImplicitCrossing_BlocksRead(t *testing.T) {
	// Simulates implicit zone crossing: an investigation starts in an
	// authorized namespace, then a follow-up tool call targets a namespace
	// in a different zone. The executor blocks the cross-zone read.
	registry := NewRegistry()
	callCount := 0
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			callCount++
			return map[string]any{"pods": []string{"web-1"}}, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedNamespaces([]string{"frontend"}),
		WithScopeZoneNames("Frontend Zone (zone-a)"),
		WithNamespaceZoneMap(map[string]string{
			"frontend": "Frontend Zone (zone-a)",
			"orders":   "Orders Zone (zone-b)",
		}),
	)

	// First call: authorized namespace — should succeed
	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "frontend",
	})
	if err != nil {
		t.Fatalf("in-zone call should succeed: %v", err)
	}

	// Second call: cross-zone namespace — should be blocked
	_, err = executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-a",
		"resource":  "pods",
		"namespace": "orders",
	})
	if err == nil {
		t.Fatal("cross-zone call should be blocked")
	}

	var nsErr *NamespaceViolationError
	if !errors.As(err, &nsErr) {
		t.Fatalf("expected NamespaceViolationError, got %T: %v", err, err)
	}

	// Error must contain target zone for the LLM to articulate the crossing
	if !contains(err.Error(), "Orders Zone") {
		t.Errorf("cross-zone error should name target zone, got: %s", err.Error())
	}

	// Only the first (authorized) call should have executed
	if callCount != 1 {
		t.Errorf("expected 1 tool execution (authorized only), got %d", callCount)
	}
}

func TestExecutor_ZoneScope_ErrorContainsZoneNames(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedSources([]string{"cluster-a"}),
		WithScopeZoneNames("zone-a (Frontend)"),
	)

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-b",
		"resource":  "pods",
	})
	if err == nil {
		t.Fatal("expected zone violation error")
	}

	if !contains(err.Error(), "zone-a") {
		t.Errorf("zone violation error should contain zone name, got: %s", err.Error())
	}
}

func TestExecutor_ZoneScope_ErrorContainsTargetZone(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil,
		WithAllowedSources([]string{"cluster-a"}),
		WithScopeZoneNames("Frontend Zone (zone-a)"),
		WithSourceZoneMap(map[string]string{
			"cluster-a": "Frontend Zone (zone-a)",
			"cluster-b": "Payments Zone (zone-b)",
		}),
	)

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"source_id": "cluster-b",
		"resource":  "pods",
	})
	if err == nil {
		t.Fatal("expected zone violation error")
	}

	errMsg := err.Error()
	if !contains(errMsg, "Frontend Zone") {
		t.Errorf("error should contain authorized zone, got: %s", errMsg)
	}
	if !contains(errMsg, "Payments Zone") {
		t.Errorf("error should contain target zone, got: %s", errMsg)
	}
}
