// Package mocks provides mock implementations for testing
package mocks

import (
	"context"
	"errors"

	"github.com/jaimegago/joe/internal/llm"
)

// MockLLM is a mock implementation of llmLLMAdapter for testing
type MockLLM struct {
	// DefaultResponse is returned if no specific scenario is set
	DefaultResponse *llm.ChatResponse

	// Responses is a map of input messages to responses
	Responses map[string]*llm.ChatResponse

	// CallCount tracks how many times Chat has been called
	CallCount int

	// LastRequest stores the most recent request for assertions
	LastRequest *llm.ChatRequest

	// ShouldError will cause the mock to return an error
	ShouldError bool

	// ErrorMessage is the error to return when ShouldError is true
	ErrorMessage string
}

// NewMockLLM creates a new mock LLM adapter
func NewMockLLM() *MockLLM {
	return &MockLLM{
		Responses: make(map[string]*llm.ChatResponse),
		DefaultResponse: &llm.ChatResponse{
			Content: "Mock response",
			Usage: llm.TokenUsage{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		},
	}
}

// Chat implements llm.LLMAdapter
func (m *MockLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.CallCount++
	reqCopy := req
	m.LastRequest = &reqCopy

	// Return error if configured
	if m.ShouldError {
		errMsg := m.ErrorMessage
		if errMsg == "" {
			errMsg = "mock LLM error"
		}
		return nil, errors.New(errMsg)
	}

	// Check for specific responses based on last user message
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if resp, ok := m.Responses[lastMsg.Content]; ok {
			return resp, nil
		}
	}

	// Return default response
	return m.DefaultResponse, nil
}

// ChatStream implements llm.LLMAdapter
func (m *MockLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	// For the mock, we'll just return a channel with a single chunk
	ch := make(chan llm.StreamChunk, 1)

	if m.ShouldError {
		errMsg := m.ErrorMessage
		if errMsg == "" {
			errMsg = "mock LLM error"
		}
		ch <- llm.StreamChunk{Done: true, Error: errors.New(errMsg)}
		close(ch)
		return ch, nil
	}

	resp, _ := m.Chat(ctx, req)
	ch <- llm.StreamChunk{
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		Done:      true,
		Error:     nil,
	}
	close(ch)
	return ch, nil
}

// Embed implements llm.LLMAdapter
func (m *MockLLM) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.ShouldError {
		errMsg := m.ErrorMessage
		if errMsg == "" {
			errMsg = "mock LLM error"
		}
		return nil, errors.New(errMsg)
	}

	// Return a simple mock embedding
	return []float32{0.1, 0.2, 0.3}, nil
}

// SetResponse sets a specific response for a given input message
func (m *MockLLM) SetResponse(userMessage string, response *llm.ChatResponse) {
	m.Responses[userMessage] = response
}

// SetupScenario configures the mock for common test scenarios
// Accepts a scenario name, a response, and an error
// If error is non-nil, the mock will return that error
func (m *MockLLM) SetupScenario(scenario string, response llm.ChatResponse, err error) {
	if err != nil {
		m.ShouldError = true
		m.ErrorMessage = err.Error()
		return
	}

	m.ShouldError = false
	m.Responses[scenario] = &response
	m.DefaultResponse = &response
}

// SetupScenarioByName configures the mock for common test scenarios by name
func (m *MockLLM) SetupScenarioByName(scenario string) {
	switch scenario {
	case "simple_question":
		m.DefaultResponse = &llm.ChatResponse{
			Content: "Kubernetes is a container orchestration platform.",
			Usage: llm.TokenUsage{
				InputTokens:  20,
				OutputTokens: 15,
				TotalTokens:  35,
			},
		}

	case "tool_call":
		m.Responses["read my config.yaml"] = &llm.ChatResponse{
			Content: "I'll read that file for you.",
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_1",
					Name: "read_file",
					Args: map[string]any{"path": "config.yaml"},
				},
			},
			Usage: llm.TokenUsage{
				InputTokens:  25,
				OutputTokens: 30,
				TotalTokens:  55,
			},
		}

	case "multi_turn":
		m.Responses["hello"] = &llm.ChatResponse{
			Content: "Hi! How can I help?",
		}
		m.Responses["what can you do?"] = &llm.ChatResponse{
			Content: "I can help with many tasks!",
		}
		m.Responses["thanks"] = &llm.ChatResponse{
			Content: "You're welcome!",
		}

	case "error":
		m.ShouldError = true
		m.ErrorMessage = "simulated LLM error"
	}
}
