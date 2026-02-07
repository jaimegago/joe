package echo

import (
	"context"

	"github.com/jaimegago/joe/internal/llm"
)

// Tool implements a simple echo tool for testing the agentic loop
type Tool struct{}

// NewTool creates a new echo tool
func NewTool() *Tool {
	return &Tool{}
}

// Name returns the tool's name
func (t *Tool) Name() string {
	return "echo"
}

// Description returns a description for the LLM
func (t *Tool) Description() string {
	return "Echoes back the input message. Useful for testing."
}

// Parameters returns the parameter schema
func (t *Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"message": {
				Type:        "string",
				Description: "The message to echo back",
			},
		},
		Required: []string{"message"},
	}
}

// Execute runs the echo tool
func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	message, ok := args["message"].(string)
	if !ok {
		message = ""
	}

	return map[string]string{
		"echoed": message,
	}, nil
}
