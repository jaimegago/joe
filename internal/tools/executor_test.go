package tools

import (
	"context"
	"errors"
	"testing"
)

func TestNewExecutor(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

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
					name: "echo",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]string{"echoed": args["message"].(string)}, nil
					},
				})
			},
			toolName: "echo",
			args:     map[string]any{"message": "hello"},
			want:     map[string]string{"echoed": "hello"},
			wantErr:  false,
		},
		{
			name: "tool not found",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{name: "echo"})
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
					name: "failing",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return nil, errors.New("execution failed")
					},
				})
			},
			toolName: "failing",
			args:     map[string]any{},
			wantErr:  true,
			errMsg:   "failed to execute tool",
		},
		{
			name: "tool with complex arguments",
			setupFunc: func(r *Registry) {
				r.Register(&mockTool{
					name: "complex",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]any{
							"count":  args["count"],
							"items":  args["items"],
							"nested": args["nested"],
						}, nil
					},
				})
			},
			toolName: "complex",
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
			executor := NewExecutor(registry)

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
					name: "echo",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]string{"echoed": args["message"].(string)}, nil
					},
				})
				r.Register(&mockTool{
					name: "upper",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return map[string]string{"result": "UPPER"}, nil
					},
				})
			},
			calls: []ToolCallRequest{
				{ID: "call-1", Name: "echo", Args: map[string]any{"message": "hello"}},
				{ID: "call-2", Name: "upper", Args: map[string]any{"text": "hello"}},
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
					name: "success",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return "ok", nil
					},
				})
				r.Register(&mockTool{
					name: "failing",
					executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
						return nil, errors.New("failed")
					},
				})
			},
			calls: []ToolCallRequest{
				{ID: "call-1", Name: "success", Args: map[string]any{}},
				{ID: "call-2", Name: "failing", Args: map[string]any{}},
				{ID: "call-3", Name: "success", Args: map[string]any{}},
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
				r.Register(&mockTool{name: "real"})
			},
			calls: []ToolCallRequest{
				{ID: "call-1", Name: "real", Args: map[string]any{}},
				{ID: "call-2", Name: "fake", Args: map[string]any{}},
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
				r.Register(&mockTool{name: "test"})
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
			executor := NewExecutor(registry)

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
	registry.Register(&mockTool{
		name: "slow",
		executeFunc: func(ctx context.Context, args map[string]any) (any, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	executor := NewExecutor(registry)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := executor.Execute(ctx, "slow", map[string]any{})
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
			executor := NewExecutor(registry)

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
