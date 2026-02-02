package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// ErrAllToolsFailed is returned when all tools in a batch fail
var ErrAllToolsFailed = errors.New("all tools in batch failed")

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
// Returns results for all tools (successful or not) and an error only if ALL tools failed.
// Individual tool errors are stored in each ToolCallResult.Error field.
// This allows partial success - the caller can inspect individual results.
func (e *Executor) ExecuteBatch(ctx context.Context, calls []ToolCallRequest) ([]ToolCallResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	results := make([]ToolCallResult, len(calls))
	errorCount := 0

	for i, call := range calls {
		result, err := e.Execute(ctx, call.Name, call.Args)
		results[i] = ToolCallResult{
			ID:     call.ID,
			Result: result,
			Error:  err,
		}
		if err != nil {
			errorCount++
		}
	}

	// Return error only if ALL tools failed
	if errorCount == len(calls) {
		return results, fmt.Errorf("%w: %d tool(s) failed", ErrAllToolsFailed, errorCount)
	}

	return results, nil
}

// ResultsToMessages converts tool call results to LLM messages
// This formats the results in a way that can be appended to the conversation history
func (e *Executor) ResultsToMessages(results []ToolCallResult) []llm.Message {
	messages := make([]llm.Message, len(results))

	for i, result := range results {
		messages[i] = ResultToMessage(result)
	}

	return messages
}

// ResultToMessage converts a single tool call result to an LLM message
func ResultToMessage(result ToolCallResult) llm.Message {
	var content string

	if result.Error != nil {
		content = fmt.Sprintf("Error executing tool: %v", result.Error)
	} else {
		// Format the result as JSON for the LLM
		jsonBytes, err := json.Marshal(result.Result)
		if err != nil {
			content = fmt.Sprintf("Error marshaling result: %v", err)
		} else {
			content = string(jsonBytes)
		}
	}

	return llm.Message{
		Role:    "user", // Tool results are sent as user messages in the conversation
		Content: content,
	}
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
