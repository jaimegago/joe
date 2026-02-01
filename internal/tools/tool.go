package tools

import (
	"context"

	"github.com/jaimegago/joe/internal/llm"
)

// Tool represents an executable tool that the LLM can call
type Tool interface {
	// Name returns the tool's name
	Name() string

	// Description returns a description for the LLM
	Description() string

	// Parameters returns the parameter schema
	Parameters() llm.ParameterSchema

	// Execute runs the tool with the given arguments
	Execute(ctx context.Context, args map[string]any) (any, error)
}
