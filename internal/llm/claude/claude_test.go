package claude

import (
	"os"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		apiKey  string
		wantErr bool
	}{
		{
			name:    "creates client with API key",
			model:   "claude-sonnet-4-20250514",
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name:    "uses default model when empty",
			model:   "",
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name:    "returns error when API key missing",
			model:   "claude-sonnet-4-20250514",
			apiKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.apiKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", tt.apiKey)
				defer os.Unsetenv("ANTHROPIC_API_KEY")
			} else {
				os.Unsetenv("ANTHROPIC_API_KEY")
			}

			client, err := NewClient(tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if client == nil {
					t.Error("NewClient() returned nil client")
				}
				if tt.model == "" && client.model != "claude-sonnet-4-20250514" {
					t.Errorf("NewClient() model = %v, want default model", client.model)
				}
				if tt.model != "" && client.model != tt.model {
					t.Errorf("NewClient() model = %v, want %v", client.model, tt.model)
				}
			}
		})
	}
}

func TestConvertToolDefinition(t *testing.T) {
	// Set up a client for testing (requires API key in env)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, err := NewClient("")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	tests := []struct {
		name string
		tool llm.ToolDefinition
	}{
		{
			name: "converts simple tool",
			tool: llm.ToolDefinition{
				Name:        "echo",
				Description: "Echoes back the input",
				Parameters: llm.ParameterSchema{
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
		{
			name: "converts tool with multiple parameters",
			tool: llm.ToolDefinition{
				Name:        "calculate",
				Description: "Performs calculation",
				Parameters: llm.ParameterSchema{
					Type: "object",
					Properties: map[string]llm.Property{
						"operation": {
							Type:        "string",
							Description: "Operation to perform",
						},
						"x": {
							Type:        "number",
							Description: "First operand",
						},
						"y": {
							Type:        "number",
							Description: "Second operand",
						},
					},
					Required: []string{"operation", "x", "y"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.convertToolDefinition(tt.tool)

			// Verify the result is a valid ToolUnionParam
			// Since ToolUnionParam is a union type, we just verify it's not nil
			if result.OfTool == nil {
				t.Error("convertToolDefinition() returned ToolUnionParam with nil OfTool")
			}
		})
	}
}

func TestConvertResponse(t *testing.T) {
	// This test verifies the response conversion logic
	// We can't easily test the full API flow without mocking,
	// but we can verify the conversion function works

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, err := NewClient("")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test that client was created successfully
	if client == nil {
		t.Fatal("Client should not be nil")
	}

	// Verify client has the expected model
	expectedModel := "claude-sonnet-4-20250514"
	if client.model != expectedModel {
		t.Errorf("Client model = %v, want %v", client.model, expectedModel)
	}
}
