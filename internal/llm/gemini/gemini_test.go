package gemini

import (
	"context"
	"os"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		geminiKey string
		googleKey string
		wantErr   bool
		wantModel string
	}{
		{
			name:      "creates client with GEMINI_API_KEY",
			model:     "gemini-2.0-flash-exp",
			geminiKey: "test-gemini-key",
			wantErr:   false,
			wantModel: "gemini-2.0-flash-exp",
		},
		{
			name:      "creates client with GOOGLE_API_KEY fallback",
			model:     "gemini-2.0-flash-exp",
			googleKey: "test-google-key",
			wantErr:   false,
			wantModel: "gemini-2.0-flash-exp",
		},
		{
			name:      "uses default model when empty",
			model:     "",
			geminiKey: "test-key",
			wantErr:   false,
			wantModel: "gemini-1.5-flash",
		},
		{
			name:    "returns error when no API key",
			model:   "gemini-2.0-flash-exp",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean environment
			os.Unsetenv("GEMINI_API_KEY")
			os.Unsetenv("GOOGLE_API_KEY")

			// Set up environment
			if tt.geminiKey != "" {
				os.Setenv("GEMINI_API_KEY", tt.geminiKey)
				defer os.Unsetenv("GEMINI_API_KEY")
			}
			if tt.googleKey != "" {
				os.Setenv("GOOGLE_API_KEY", tt.googleKey)
				defer os.Unsetenv("GOOGLE_API_KEY")
			}

			ctx := context.Background()
			client, err := NewClient(ctx, tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if client == nil {
					t.Error("NewClient() returned nil client")
					return
				}
				if client.model != tt.wantModel {
					t.Errorf("NewClient() model = %v, want %v", client.model, tt.wantModel)
				}
				// Clean up
				client.Close()
			}
		})
	}
}

func TestConvertToolDefinition(t *testing.T) {
	// Set up a client for testing
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, err := NewClient(ctx, "")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

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

			// Verify the tool was created
			if result == nil {
				t.Fatal("convertToolDefinition() returned nil")
			}

			if len(result.FunctionDeclarations) == 0 {
				t.Fatal("convertToolDefinition() returned no function declarations")
			}

			funcDecl := result.FunctionDeclarations[0]
			if funcDecl.Name != tt.tool.Name {
				t.Errorf("Function name = %v, want %v", funcDecl.Name, tt.tool.Name)
			}

			if funcDecl.Description != tt.tool.Description {
				t.Errorf("Function description = %v, want %v", funcDecl.Description, tt.tool.Description)
			}

			if funcDecl.Parameters == nil {
				t.Error("Function parameters is nil")
			}
		})
	}
}

func TestClose(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, err := NewClient(ctx, "")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test that Close doesn't error
	if err := client.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}
