package tools

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

// mockTool is a test tool implementation
type mockTool struct {
	name        string
	description string
	params      llm.ParameterSchema
	executeFunc func(ctx context.Context, args map[string]any) (any, error)
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) Parameters() llm.ParameterSchema {
	return m.params
}

func (m *mockTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, args)
	}
	return map[string]string{"result": "ok"}, nil
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if registry.tools == nil {
		t.Fatal("NewRegistry() did not initialize tools map")
	}
}

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name  string
		tools []Tool
		want  int
	}{
		{
			name: "register single tool",
			tools: []Tool{
				&mockTool{name: "test1"},
			},
			want: 1,
		},
		{
			name: "register multiple tools",
			tools: []Tool{
				&mockTool{name: "test1"},
				&mockTool{name: "test2"},
				&mockTool{name: "test3"},
			},
			want: 3,
		},
		{
			name: "register duplicate tool overwrites",
			tools: []Tool{
				&mockTool{name: "test1", description: "first"},
				&mockTool{name: "test1", description: "second"},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, tool := range tt.tools {
				registry.Register(tool)
			}
			if len(registry.tools) != tt.want {
				t.Errorf("Registry has %d tools, want %d", len(registry.tools), tt.want)
			}
		})
	}
}

func TestRegistry_Get(t *testing.T) {
	tests := []struct {
		name     string
		register []Tool
		get      string
		wantErr  bool
	}{
		{
			name: "get existing tool",
			register: []Tool{
				&mockTool{name: "test1"},
			},
			get:     "test1",
			wantErr: false,
		},
		{
			name: "get non-existing tool",
			register: []Tool{
				&mockTool{name: "test1"},
			},
			get:     "test2",
			wantErr: true,
		},
		{
			name:     "get from empty registry",
			register: []Tool{},
			get:      "test1",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, tool := range tt.register {
				registry.Register(tool)
			}

			got, err := registry.Get(tt.get)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Error("Get() returned nil tool without error")
			}
			if !tt.wantErr && got.Name() != tt.get {
				t.Errorf("Get() returned tool with name %s, want %s", got.Name(), tt.get)
			}
		})
	}
}

func TestRegistry_GetAll(t *testing.T) {
	tests := []struct {
		name     string
		register []Tool
		wantLen  int
	}{
		{
			name:     "empty registry",
			register: []Tool{},
			wantLen:  0,
		},
		{
			name: "single tool",
			register: []Tool{
				&mockTool{name: "test1"},
			},
			wantLen: 1,
		},
		{
			name: "multiple tools",
			register: []Tool{
				&mockTool{name: "test1"},
				&mockTool{name: "test2"},
				&mockTool{name: "test3"},
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, tool := range tt.register {
				registry.Register(tool)
			}

			got := registry.GetAll()
			if len(got) != tt.wantLen {
				t.Errorf("GetAll() returned %d tools, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestRegistry_ToDefinitions(t *testing.T) {
	tests := []struct {
		name     string
		register []Tool
		wantLen  int
		validate func(t *testing.T, defs []llm.ToolDefinition)
	}{
		{
			name:     "empty registry",
			register: []Tool{},
			wantLen:  0,
		},
		{
			name: "single tool with complete definition",
			register: []Tool{
				&mockTool{
					name:        "echo",
					description: "Echoes back the input",
					params: llm.ParameterSchema{
						Type: "object",
						Properties: map[string]llm.Property{
							"message": {
								Type:        "string",
								Description: "Message to echo",
							},
						},
						Required: []string{"message"},
					},
				},
			},
			wantLen: 1,
			validate: func(t *testing.T, defs []llm.ToolDefinition) {
				if defs[0].Name != "echo" {
					t.Errorf("Definition name = %s, want echo", defs[0].Name)
				}
				if defs[0].Description != "Echoes back the input" {
					t.Errorf("Definition description = %s, want 'Echoes back the input'", defs[0].Description)
				}
				if defs[0].Parameters.Type != "object" {
					t.Errorf("Definition parameters type = %s, want object", defs[0].Parameters.Type)
				}
				if len(defs[0].Parameters.Properties) != 1 {
					t.Errorf("Definition has %d properties, want 1", len(defs[0].Parameters.Properties))
				}
			},
		},
		{
			name: "multiple tools",
			register: []Tool{
				&mockTool{name: "test1", description: "Test 1"},
				&mockTool{name: "test2", description: "Test 2"},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			for _, tool := range tt.register {
				registry.Register(tool)
			}

			got := registry.ToDefinitions()
			if len(got) != tt.wantLen {
				t.Errorf("ToDefinitions() returned %d definitions, want %d", len(got), tt.wantLen)
			}

			if tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}
