package echo

import (
	"context"
	"testing"
)

func TestNewTool(t *testing.T) {
	tool := NewTool()
	if tool == nil {
		t.Fatal("NewTool() returned nil")
	}
}

func TestTool_Name(t *testing.T) {
	tool := NewTool()
	if got := tool.Name(); got != "echo" {
		t.Errorf("Name() = %s, want echo", got)
	}
}

func TestTool_Description(t *testing.T) {
	tool := NewTool()
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
}

func TestTool_Parameters(t *testing.T) {
	tool := NewTool()
	params := tool.Parameters()

	if params.Type != "object" {
		t.Errorf("Parameters().Type = %s, want object", params.Type)
	}

	if len(params.Properties) != 1 {
		t.Errorf("Parameters() has %d properties, want 1", len(params.Properties))
	}

	if _, ok := params.Properties["message"]; !ok {
		t.Error("Parameters() missing 'message' property")
	}

	if len(params.Required) != 1 || params.Required[0] != "message" {
		t.Errorf("Parameters().Required = %v, want [message]", params.Required)
	}
}

func TestTool_Execute(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		want    map[string]string
		wantErr bool
	}{
		{
			name: "echo simple message",
			args: map[string]any{"message": "hello"},
			want: map[string]string{"echoed": "hello"},
		},
		{
			name: "echo empty message",
			args: map[string]any{"message": ""},
			want: map[string]string{"echoed": ""},
		},
		{
			name: "echo multiline message",
			args: map[string]any{"message": "hello\nworld"},
			want: map[string]string{"echoed": "hello\nworld"},
		},
		{
			name: "echo with special characters",
			args: map[string]any{"message": "hello! @#$%^&*()"},
			want: map[string]string{"echoed": "hello! @#$%^&*()"},
		},
		{
			name: "missing message parameter",
			args: map[string]any{},
			want: map[string]string{"echoed": ""},
		},
		{
			name: "wrong type for message",
			args: map[string]any{"message": 123},
			want: map[string]string{"echoed": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewTool()
			got, err := tool.Execute(context.Background(), tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				gotMap, ok := got.(map[string]string)
				if !ok {
					t.Errorf("Execute() returned %T, want map[string]string", got)
					return
				}

				if gotMap["echoed"] != tt.want["echoed"] {
					t.Errorf("Execute() = %v, want %v", gotMap, tt.want)
				}
			}
		})
	}
}

func TestTool_Execute_ContextCancellation(t *testing.T) {
	tool := NewTool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Echo tool doesn't do I/O, so it should still work with cancelled context
	_, err := tool.Execute(ctx, map[string]any{"message": "test"})
	if err != nil {
		t.Errorf("Execute() with cancelled context returned error: %v", err)
	}
}
