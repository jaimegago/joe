package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
)

// Agent runs the agentic loop: LLM → tool calls → LLM → ...
type Agent struct {
	llm           llm.LLMAdapter
	executor      *tools.Executor
	registry      *tools.Registry
	systemPrompt  string
	maxIterations int
}

// NewAgent creates a new agent
func NewAgent(llmAdapter llm.LLMAdapter, executor *tools.Executor, registry *tools.Registry, systemPrompt string) *Agent {
	return &Agent{
		llm:           llmAdapter,
		executor:      executor,
		registry:      registry,
		systemPrompt:  systemPrompt,
		maxIterations: 10, // Prevent infinite loops
	}
}

// Run executes the agentic loop for a user message
// The loop:
// 1. Adds user message to session history
// 2. Calls LLM with system prompt, tools, and conversation history
// 3. If LLM returns tool calls, executes them and loops back to step 2
// 4. If LLM returns no tool calls, returns the final response
func (a *Agent) Run(ctx context.Context, session *Session, userMessage string) (string, error) {
	// Reset per-run token tracking
	session.ResetRunStats()

	// Add user message to history
	session.AddMessage(llm.Message{
		Role:    "user",
		Content: userMessage,
	})

	// Get tool definitions for the LLM
	toolDefs := a.registry.ToDefinitions()

	// Agentic loop
	for i := 0; i < a.maxIterations; i++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// Build request with current conversation history
		req := llm.ChatRequest{
			SystemPrompt: a.systemPrompt,
			Messages:     session.Messages,
			Tools:        toolDefs,
		}

		// Call LLM
		resp, err := a.llm.Chat(ctx, req)
		if err != nil {
			return "", fmt.Errorf("llm chat failed: %w", err)
		}

		// Track token usage
		session.AddTokenUsage(resp.Usage)

		// If no tool calls, we have the final response
		if len(resp.ToolCalls) == 0 {
			// Add assistant's final response to history
			if resp.Content != "" {
				session.AddMessage(llm.Message{
					Role:    "assistant",
					Content: resp.Content,
				})
			}

			return resp.Content, nil
		}

		// Add assistant's response (with tool calls) to history
		// The tool calls must be preserved so the LLM sees them on the next iteration
		session.AddMessage(llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute tool calls
		toolCallRequests := make([]tools.ToolCallRequest, len(resp.ToolCalls))
		for i, tc := range resp.ToolCalls {
			toolCallRequests[i] = tools.ToolCallRequest{
				ID:   tc.ID,
				Name: tc.Name,
				Args: tc.Args,
			}
		}

		results, err := a.executor.ExecuteBatch(ctx, toolCallRequests)
		if err != nil && !errors.Is(err, tools.ErrAllToolsFailed) {
			// Only return fatal errors, not tool execution failures
			// Tool failures are added to conversation for LLM to handle
			return "", fmt.Errorf("tool execution failed: %w", err)
		}

		// Convert tool results to messages and add to history
		// This includes error messages for failed tools, which the LLM can respond to
		resultMessages := a.executor.ResultsToMessages(results)
		session.AddMessages(resultMessages)
	}

	return "", fmt.Errorf("max iterations (%d) reached without final response", a.maxIterations)
}
