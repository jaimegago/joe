package agent

import (
	"context"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
)

// Agent runs the agentic loop: LLM → tool calls → LLM → ...
type Agent struct {
	llm      llm.LLMAdapter
	executor *tools.Executor
	registry *tools.Registry
}

// NewAgent creates a new agent
func NewAgent(llmAdapter llm.LLMAdapter, executor *tools.Executor, registry *tools.Registry) *Agent {
	return &Agent{
		llm:      llmAdapter,
		executor: executor,
		registry: registry,
	}
}

// Run executes the agentic loop for a user message
func (a *Agent) Run(ctx context.Context, sessionID string, message string) (<-chan string, error) {
	// TODO: Implement agentic loop
	// This is a placeholder that will be implemented in Phase 2

	responseChan := make(chan string, 1)
	responseChan <- "Agent loop not yet implemented"
	close(responseChan)

	return responseChan, nil
}
