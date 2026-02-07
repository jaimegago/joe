package useragent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
)

// AdapterFactory creates a new LLM adapter for the given provider and model.
// Used by SwitchModel to hot-swap the underlying LLM without restarting.
type AdapterFactory func(ctx context.Context, provider, model string) (llm.LLMAdapter, error)

// AgentOption configures optional Agent settings.
type AgentOption func(*Agent)

// WithAdapterFactory sets the adapter factory for hot-swapping models.
func WithAdapterFactory(f AdapterFactory) AgentOption {
	return func(a *Agent) { a.adapterFactory = f }
}

// WithCurrentModelName sets the display name of the active model.
func WithCurrentModelName(name string) AgentOption {
	return func(a *Agent) { a.currentModel = name }
}

// Agent runs the agentic loop: LLM → tool calls → LLM → ...
type Agent struct {
	mu             sync.RWMutex // protects llm and currentModel
	llm            llm.LLMAdapter
	executor       *tools.Executor
	registry       *tools.Registry
	systemPrompt   string
	maxIterations  int
	adapterFactory AdapterFactory // optional, for hot-swap
	currentModel   string         // display name of active model
}

// NewAgent creates a new agent. Options are applied after defaults.
func NewAgent(llmAdapter llm.LLMAdapter, executor *tools.Executor, registry *tools.Registry, systemPrompt string, opts ...AgentOption) *Agent {
	a := &Agent{
		llm:           llmAdapter,
		executor:      executor,
		registry:      registry,
		systemPrompt:  systemPrompt,
		maxIterations: 10, // Prevent infinite loops
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// SwitchModel hot-swaps the LLM adapter to a different provider/model.
// Requires an AdapterFactory to have been set via WithAdapterFactory.
func (a *Agent) SwitchModel(ctx context.Context, provider, model, displayName string) error {
	if a.adapterFactory == nil {
		return fmt.Errorf("no adapter factory configured; cannot switch models")
	}
	newAdapter, err := a.adapterFactory(ctx, provider, model)
	if err != nil {
		return fmt.Errorf("failed to create adapter for %s/%s: %w", provider, model, err)
	}
	a.mu.Lock()
	a.llm = newAdapter
	a.currentModel = displayName
	a.mu.Unlock()
	return nil
}

// CurrentModelName returns the display name of the active model.
func (a *Agent) CurrentModelName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentModel
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

		// Call LLM (under read lock so SwitchModel can't swap mid-call)
		a.mu.RLock()
		resp, err := a.llm.Chat(ctx, req)
		a.mu.RUnlock()
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
