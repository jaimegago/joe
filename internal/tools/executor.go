package tools

import (
	"context"
	"fmt"
)

// Executor executes tool calls from the LLM
type Executor struct {
	registry *Registry
}

// NewExecutor creates a new tool executor
func NewExecutor(registry *Registry) *Executor {
	return &Executor{
		registry: registry,
	}
}

// Execute executes a single tool call
func (e *Executor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	tool, err := e.registry.Get(name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool %s: %w", name, err)
	}

	result, err := tool.Execute(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tool %s: %w", name, err)
	}

	return result, nil
}

// ExecuteBatch executes multiple tool calls
func (e *Executor) ExecuteBatch(ctx context.Context, calls []ToolCallRequest) ([]ToolCallResult, error) {
	results := make([]ToolCallResult, len(calls))

	for i, call := range calls {
		result, err := e.Execute(ctx, call.Name, call.Args)
		results[i] = ToolCallResult{
			ID:     call.ID,
			Result: result,
			Error:  err,
		}
	}

	return results, nil
}

// ToolCallRequest represents a request to execute a tool
type ToolCallRequest struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolCallResult represents the result of executing a tool
type ToolCallResult struct {
	ID     string
	Result any
	Error  error
}
