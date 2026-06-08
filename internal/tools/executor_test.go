package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/safety"
)

func TestNewExecutor(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry, nil)

	if executor == nil {
		t.Fatal("NewExecutor() returned nil")
	}
	if executor.registry != registry {
		t.Error("NewExecutor() did not set registry correctly")
	}
}

func TestExecutor_Execute(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(r *Registry)
		toolName  string
		args      map[string]any
		want      any
		wantErr   bool
		errMsg    string
	}{
		{
			name: "execute successful tool",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{
					name: "ask_user",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]string{"echoed": args["message"].(string)}, nil
					},
				})
			},
			toolName: "ask_user",
			args:     map[string]any{"message": "hello"},
			want:     map[string]string{"echoed": "hello"},
			wantErr:  false,
		},
		{
			name: "tool not found",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{name: "ask_user"})
			},
			toolName: "nonexistent",
			args:     map[string]any{},
			wantErr:  true,
			errMsg:   "failed to get tool",
		},
		{
			name: "tool execution error",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{
					name: "read_file",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return nil, errors.New("execution failed")
					},
				})
			},
			toolName: "read_file",
			args:     map[string]any{},
			wantErr:  true,
			errMsg:   "failed to execute tool",
		},
		{
			name: "tool with complex arguments",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{
					name: "list_components",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]any{
							"count":  args["count"],
							"items":  args["items"],
							"nested": args["nested"],
						}, nil
					},
				})
			},
			toolName: "list_components",
			args: map[string]any{
				"count": 42,
				"items": []string{"a", "b", "c"},
				"nested": map[string]any{
					"key": "value",
				},
			},
			want: map[string]any{
				"count": 42,
				"items": []string{"a", "b", "c"},
				"nested": map[string]any{
					"key": "value",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			if tt.setupFunc != nil {
				tt.setupFunc(registry)
			}
			executor := NewExecutor(registry, nil)

			got, err := executor.Execute(context.Background(), tt.toolName, tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil {
					t.Errorf("Execute() expected error containing %q, got nil", tt.errMsg)
				} else if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Execute() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if !tt.wantErr {
				if !deepEqual(got, tt.want) {
					t.Errorf("Execute() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestExecutor_ExecuteBatch(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(r *Registry)
		calls     []ToolCallRequest
		validate  func(t *testing.T, results []ToolCallResult)
	}{
		{
			name: "execute multiple successful tools",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{
					name: "read_file",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]string{"echoed": args["message"].(string)}, nil
					},
				})
				r.Register(&mockTool{
					name: "ask_user",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]string{"result": "UPPER"}, nil
					},
				})
			},
			calls: []ToolCallRequest{
				{ID: "call-1", Name: "read_file", Args: map[string]any{"message": "hello"}},
				{ID: "call-2", Name: "ask_user", Args: map[string]any{"text": "hello"}},
			},
			validate: func(t *testing.T, results []ToolCallResult) {
				if len(results) != 2 {
					t.Fatalf("ExecuteBatch() returned %d results, want 2", len(results))
				}
				if results[0].ID != "call-1" {
					t.Errorf("Result[0].ID = %s, want call-1", results[0].ID)
				}
				if results[0].Error != nil {
					t.Errorf("Result[0].Error = %v, want nil", results[0].Error)
				}
				if results[1].ID != "call-2" {
					t.Errorf("Result[1].ID = %s, want call-2", results[1].ID)
				}
				if results[1].Error != nil {
					t.Errorf("Result[1].Error = %v, want nil", results[1].Error)
				}
			},
		},
		{
			name: "execute with some failures",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{
					name: "read_file",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return "ok", nil
					},
				})
				r.Register(&mockTool{
					name: "ask_user",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return nil, errors.New("failed")
					},
				})
			},
			calls: []ToolCallRequest{
				{ID: "call-1", Name: "read_file", Args: map[string]any{}},
				{ID: "call-2", Name: "ask_user", Args: map[string]any{}},
				{ID: "call-3", Name: "read_file", Args: map[string]any{}},
			},
			validate: func(t *testing.T, results []ToolCallResult) {
				if len(results) != 3 {
					t.Fatalf("ExecuteBatch() returned %d results, want 3", len(results))
				}
				if results[0].Error != nil {
					t.Errorf("Result[0].Error = %v, want nil", results[0].Error)
				}
				if results[1].Error == nil {
					t.Error("Result[1].Error = nil, want error")
				}
				if results[2].Error != nil {
					t.Errorf("Result[2].Error = %v, want nil", results[2].Error)
				}
			},
		},
		{
			name: "execute with non-existent tool",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{name: "read_file"})
			},
			calls: []ToolCallRequest{
				{ID: "call-1", Name: "read_file", Args: map[string]any{}},
				{ID: "call-2", Name: "nonexistent_tool_xyz", Args: map[string]any{}},
			},
			validate: func(t *testing.T, results []ToolCallResult) {
				if len(results) != 2 {
					t.Fatalf("ExecuteBatch() returned %d results, want 2", len(results))
				}
				if results[0].Error != nil {
					t.Errorf("Result[0].Error = %v, want nil", results[0].Error)
				}
				if results[1].Error == nil {
					t.Error("Result[1].Error = nil, want error")
				}
			},
		},
		{
			name: "execute empty batch",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{name: "read_file"})
			},
			calls: []ToolCallRequest{},
			validate: func(t *testing.T, results []ToolCallResult) {
				if len(results) != 0 {
					t.Errorf("ExecuteBatch() returned %d results, want 0", len(results))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			if tt.setupFunc != nil {
				tt.setupFunc(registry)
			}
			executor := NewExecutor(registry, nil)

			results, err := executor.ExecuteBatch(context.Background(), tt.calls)
			if err != nil {
				t.Errorf("ExecuteBatch() returned unexpected error: %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, results)
			}
		})
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	registry := NewRegistry()
	// Use "ask_user" (T1) so safety gate passes — we're testing context cancellation
	registry.Register(&mockTool{
		name: "ask_user",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	executor := NewExecutor(registry, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := executor.Execute(ctx, "ask_user", map[string]any{})
	if err == nil {
		t.Error("Execute() with cancelled context should return error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Execute() error = %v, want context.Canceled", err)
	}
}

func TestResultToMessage(t *testing.T) {
	tests := []struct {
		name     string
		result   ToolCallResult
		wantRole string
		validate func(t *testing.T, msg string)
	}{
		{
			name: "successful result",
			result: ToolCallResult{
				ID:     "call-1",
				Result: map[string]string{"status": "ok"},
				Error:  nil,
			},
			wantRole: "user",
			validate: func(t *testing.T, content string) {
				if !contains(content, "status") || !contains(content, "ok") {
					t.Errorf("Message content = %s, want JSON with status:ok", content)
				}
			},
		},
		{
			name: "error result",
			result: ToolCallResult{
				ID:     "call-2",
				Result: nil,
				Error:  errors.New("tool failed"),
			},
			wantRole: "user",
			validate: func(t *testing.T, content string) {
				if !contains(content, "Error executing tool") {
					t.Errorf("Message content = %s, want error message", content)
				}
				if !contains(content, "tool failed") {
					t.Errorf("Message content = %s, want 'tool failed'", content)
				}
			},
		},
		{
			name: "complex result",
			result: ToolCallResult{
				ID: "call-3",
				Result: map[string]any{
					"count": 42,
					"items": []string{"a", "b", "c"},
				},
				Error: nil,
			},
			wantRole: "user",
			validate: func(t *testing.T, content string) {
				if !contains(content, "count") || !contains(content, "42") {
					t.Errorf("Message content = %s, want JSON with count:42", content)
				}
				if !contains(content, "items") {
					t.Errorf("Message content = %s, want JSON with items array", content)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := ResultToMessage(tt.result)

			if msg.Role != tt.wantRole {
				t.Errorf("ResultToMessage() role = %s, want %s", msg.Role, tt.wantRole)
			}

			if tt.validate != nil {
				tt.validate(t, msg.Content)
			}
		})
	}
}

func TestExecutor_ResultsToMessages(t *testing.T) {
	tests := []struct {
		name     string
		results  []ToolCallResult
		wantLen  int
		validate func(t *testing.T, messages []any)
	}{
		{
			name:    "empty results",
			results: []ToolCallResult{},
			wantLen: 0,
		},
		{
			name: "single result",
			results: []ToolCallResult{
				{
					ID:     "call-1",
					Result: map[string]string{"status": "ok"},
					Error:  nil,
				},
			},
			wantLen: 1,
			validate: func(t *testing.T, messages []any) {
				// Type assertion would happen here in real usage
			},
		},
		{
			name: "multiple results with mixed success and error",
			results: []ToolCallResult{
				{
					ID:     "call-1",
					Result: map[string]string{"status": "ok"},
					Error:  nil,
				},
				{
					ID:     "call-2",
					Result: nil,
					Error:  errors.New("failed"),
				},
				{
					ID:     "call-3",
					Result: map[string]int{"count": 5},
					Error:  nil,
				},
			},
			wantLen: 3,
			validate: func(t *testing.T, messages []any) {
				// All should be converted to messages
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			executor := NewExecutor(registry, nil)

			messages := executor.ResultsToMessages(tt.results)

			if len(messages) != tt.wantLen {
				t.Errorf("ResultsToMessages() returned %d messages, want %d", len(messages), tt.wantLen)
			}

			// Verify all messages have the correct role
			for i, msg := range messages {
				if msg.Role != "user" {
					t.Errorf("Message[%d].Role = %s, want user", i, msg.Role)
				}
				if msg.Content == "" {
					t.Errorf("Message[%d].Content is empty", i)
				}
			}
		})
	}
}

// --- Safety gate tests ---

func TestExecutor_SafetyGate_T1_AlwaysAllowed(t *testing.T) {
	registry := NewRegistry()
	// "ask_user" is classified as T1 (Observe) in the safety tier registry
	registry.Register(&mockTool{
		name: "ask_user",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "ok", nil
		},
	})

	// Even with most restrictive policy, T1 tools should work
	executor := NewExecutor(registry, nil, WithPolicy(safety.DefaultPolicy()))

	result, err := executor.Execute(context.Background(), "ask_user", map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("T1 tool should always be allowed, got error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
}

func TestExecutor_SafetyGate_T3_DeniedByDefault(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "write_file",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "written", nil
		},
	})

	// Default policy denies write_file
	executor := NewExecutor(registry, nil, WithPolicy(safety.DefaultPolicy()))

	_, err := executor.Execute(context.Background(), "write_file", map[string]any{"path": "/tmp/test"})
	if err == nil {
		t.Fatal("write_file should be denied by default policy")
	}

	var accessErr *safety.AccessDeniedError
	if !errors.As(err, &accessErr) {
		t.Errorf("expected AccessDeniedError, got %T: %v", err, err)
	}
}

func TestExecutor_SafetyGate_T3_AllowedByPolicy(t *testing.T) {
	registry := NewRegistry()
	called := false
	registry.Register(&mockTool{
		name: "run_command",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			called = true
			return "output", nil
		},
	})

	// Policy that enables run_command
	policy := safety.DefaultPolicy()
	policy.Act.RunCommand.Enabled = true

	executor := NewExecutor(registry, nil, WithPolicy(policy))

	result, err := executor.Execute(context.Background(), "run_command", map[string]any{"command": "ls"})
	if err != nil {
		t.Fatalf("run_command should be allowed by policy, got: %v", err)
	}
	if !called {
		t.Error("tool Execute was not called")
	}
	if result != "output" {
		t.Errorf("result = %v, want output", result)
	}
}

func TestExecutor_SafetyGate_UnknownTool_Denied(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "dangerous_unknown",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "bad", nil
		},
	})

	executor := NewExecutor(registry, nil, WithPolicy(safety.DefaultPolicy()))

	_, err := executor.Execute(context.Background(), "dangerous_unknown", map[string]any{})
	if err == nil {
		t.Fatal("unknown tool should be denied (classified as T3)")
	}
}

func TestExecutor_Notifier_T3_CalledBeforeAndAfter(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "run_command",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "ok", nil
		},
	})

	policy := safety.DefaultPolicy()
	policy.Act.RunCommand.Enabled = true

	notifier := &trackingNotifier{}
	executor := NewExecutor(registry, nil, WithPolicy(policy), WithNotifier(notifier))

	_, err := executor.Execute(context.Background(), "run_command", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !notifier.beforeCalled {
		t.Error("NotifyBefore was not called for T3 action")
	}
	if !notifier.afterCalled {
		t.Error("NotifyAfter was not called for T3 action")
	}
	if notifier.beforeInfo.ToolName != "run_command" {
		t.Errorf("NotifyBefore tool = %q, want run_command", notifier.beforeInfo.ToolName)
	}
}

func TestExecutor_Notifier_T3_CancelledDuringBefore(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "run_command",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			t.Fatal("tool should not execute when NotifyBefore returns error")
			return nil, nil
		},
	})

	policy := safety.DefaultPolicy()
	policy.Act.RunCommand.Enabled = true

	notifier := &trackingNotifier{
		beforeErr: context.Canceled,
	}
	executor := NewExecutor(registry, nil, WithPolicy(policy), WithNotifier(notifier))

	_, err := executor.Execute(context.Background(), "run_command", map[string]any{})
	if err == nil {
		t.Fatal("expected error when NotifyBefore returns error")
	}
	if !contains(err.Error(), "cancelled") {
		t.Errorf("error = %v, want 'cancelled' message", err)
	}
}

// TestExecutor_Notifier_ModelMaintenance_NoNotification verifies that Joe's
// own model maintenance (graph_add_node), now observe-tier per D-0018/D-0019,
// fires no notifications — the same as any other read. (The former T2 "after
// only" notification path is now vacant: no registered tool is record-tier.)
func TestExecutor_Notifier_ModelMaintenance_NoNotification(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "graph_add_node",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "added", nil
		},
	})

	notifier := &trackingNotifier{}
	executor := NewExecutor(registry, nil, WithPolicy(safety.DefaultPolicy()), WithNotifier(notifier))

	_, err := executor.Execute(context.Background(), "graph_add_node", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if notifier.beforeCalled {
		t.Error("NotifyBefore should NOT be called for observe-tier model maintenance")
	}
	if notifier.afterCalled {
		t.Error("NotifyAfter should NOT be called for observe-tier model maintenance")
	}
}

func TestExecutor_Notifier_T1_NoNotification(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "ask_user",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "ok", nil
		},
	})

	notifier := &trackingNotifier{}
	executor := NewExecutor(registry, nil, WithPolicy(safety.DefaultPolicy()), WithNotifier(notifier))

	_, err := executor.Execute(context.Background(), "ask_user", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if notifier.beforeCalled {
		t.Error("NotifyBefore should NOT be called for T1")
	}
	if notifier.afterCalled {
		t.Error("NotifyAfter should NOT be called for T1")
	}
}

// --- Zone scope tests ---

func TestExecutor_ZoneScope_AllowedSource(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "pods", nil
		},
	})

	executor := NewExecutor(registry, nil, WithAllowedComponents([]string{"cluster-a", "cluster-b"}))

	result, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"component_id": "cluster-a",
		"resource":     "pods",
	})
	if err != nil {
		t.Fatalf("tool targeting allowed source should succeed, got: %v", err)
	}
	if result != "pods" {
		t.Errorf("result = %v, want pods", result)
	}
}

func TestExecutor_ZoneScope_DeniedSource(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			t.Fatal("tool should not execute for unauthorized source")
			return nil, nil
		},
	})

	executor := NewExecutor(registry, nil, WithAllowedComponents([]string{"cluster-a"}))

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"component_id": "cluster-b",
		"resource":     "pods",
	})
	if err == nil {
		t.Fatal("expected zone violation error for unauthorized source")
	}

	var zoneErr *ZoneViolationError
	if !errors.As(err, &zoneErr) {
		t.Fatalf("expected ZoneViolationError, got %T: %v", err, err)
	}
	if zoneErr.ComponentID != "cluster-b" {
		t.Errorf("ZoneViolationError.ComponentID = %q, want %q", zoneErr.ComponentID, "cluster-b")
	}
}

func TestExecutor_ZoneScope_NoSourceID_Allowed(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "graph_query",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "nodes", nil
		},
	})

	// Even with zone restrictions, tools without component_id should work
	executor := NewExecutor(registry, nil, WithAllowedComponents([]string{"cluster-a"}))

	result, err := executor.Execute(context.Background(), "graph_query", map[string]any{
		"query": "services",
	})
	if err != nil {
		t.Fatalf("tool without component_id should not be blocked by zone scope: %v", err)
	}
	if result != "nodes" {
		t.Errorf("result = %v, want nodes", result)
	}
}

func TestExecutor_ZoneScope_NilAllowedSources_NoRestriction(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return "ok", nil
		},
	})

	// Default (nil allowedComponents) should not restrict anything
	executor := NewExecutor(registry, nil)

	result, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"component_id": "any-cluster",
		"resource":     "pods",
	})
	if err != nil {
		t.Fatalf("nil allowedComponents should not restrict: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
}

func TestExecutor_ZoneScope_EmptyAllowedSources_DeniesAll(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "k8s_get",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			t.Fatal("should not execute")
			return nil, nil
		},
	})

	// Empty slice = caller has no zone access, all source-scoped calls denied
	executor := NewExecutor(registry, nil, WithAllowedComponents([]string{}))

	_, err := executor.Execute(context.Background(), "k8s_get", map[string]any{
		"component_id": "any-cluster",
		"resource":     "pods",
	})
	if err == nil {
		t.Fatal("empty allowedComponents should deny all source-scoped calls")
	}
	var zoneErr *ZoneViolationError
	if !errors.As(err, &zoneErr) {
		t.Fatalf("expected ZoneViolationError, got %T: %v", err, err)
	}
}

// TestExecutor_Floor_PrecedesZoneScope pins the denial-message precedence
// floor > RBAC (D-0019 decision 9) at the executor layer. When a Mutate trips
// BOTH the write floor (up) AND a zone-scope violation (out-of-zone component_id),
// the user must see the floor reason — the one they can least readily fix — not
// the zone violation. Because enforcement short-circuits, only one error ever
// exists; reordering the floor check above the zone check makes that one error
// the WriteFloorError. write_file is a registered Mutate; the floor only denies
// Mutates, so this combination is the genuine co-occurrence case.
func TestExecutor_Floor_PrecedesZoneScope(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&mockTool{
		name: "write_file",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			t.Fatal("tool must not execute: floor (and zone) should block it")
			return nil, nil
		},
	})

	// Floor up (safe_mode) AND an allowed-components set that excludes the target.
	floor := safety.ResolveWriteFloor(true /*panicStatePresent*/, false)
	executor := NewExecutor(registry, nil,
		WithWriteFloor(floor),
		WithAllowedComponents([]string{"cluster-a"}),
		WithPolicy(safety.DefaultPolicy()),
	)

	_, err := executor.Execute(context.Background(), "write_file", map[string]any{
		"component_id": "cluster-b", // out of zone — would be a ZoneViolationError if it ran first
		"path":         "/etc/x",
	})
	if err == nil {
		t.Fatal("expected denial when floor is up and source is out of zone")
	}
	// Floor wins: the surfaced error is the WriteFloorError, NOT the zone violation.
	if !errors.Is(err, safety.ErrWriteFloor) {
		t.Fatalf("expected the write-floor error to take precedence, got %T: %v", err, err)
	}
	var zoneErr *ZoneViolationError
	if errors.As(err, &zoneErr) {
		t.Fatalf("zone violation surfaced instead of the floor reason — precedence floor > RBAC violated")
	}
	var floorErr *safety.WriteFloorError
	if !errors.As(err, &floorErr) || floorErr.Reason != safety.FloorReasonSafeMode {
		t.Fatalf("expected safe_mode floor reason, got %T: %v", err, err)
	}
}

// trackingNotifier records calls for test assertions.
type trackingNotifier struct {
	beforeCalled bool
	afterCalled  bool
	beforeInfo   safety.ActionInfo
	afterInfo    safety.ActionInfo
	beforeErr    error
}

func (n *trackingNotifier) NotifyBefore(_ context.Context, info safety.ActionInfo) error {
	n.beforeCalled = true
	n.beforeInfo = info
	return n.beforeErr
}

func (n *trackingNotifier) NotifyAfter(_ context.Context, info safety.ActionInfo, _ any, _ error) {
	n.afterCalled = true
	n.afterInfo = info
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func deepEqual(a, b any) bool {
	// Simple equality check - in production you'd use reflect.DeepEqual
	// but keeping it simple for tests
	switch va := a.(type) {
	case map[string]string:
		vb, ok := b.(map[string]string)
		if !ok {
			return false
		}
		if len(va) != len(vb) {
			return false
		}
		for k, v := range va {
			if vb[k] != v {
				return false
			}
		}
		return true
	case map[string]any:
		vb, ok := b.(map[string]any)
		if !ok {
			return false
		}
		if len(va) != len(vb) {
			return false
		}
		for k, v := range va {
			if !deepEqual(v, vb[k]) {
				return false
			}
		}
		return true
	case []string:
		vb, ok := b.([]string)
		if !ok {
			return false
		}
		if len(va) != len(vb) {
			return false
		}
		for i := range va {
			if va[i] != vb[i] {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
