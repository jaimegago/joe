package useragent

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/tools/local/echo"
)

// mockLLM is a mock LLM adapter for testing
type mockLLM struct {
	responses []*llm.ChatResponse
	callCount int
	lastReq   *llm.ChatRequest
}

func (m *mockLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.lastReq = &req

	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}

	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *mockLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (m *mockLLM) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("not implemented")
}

func TestNewAgent(t *testing.T) {
	mockLLM := &mockLLM{}
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry)
	systemPrompt := "You are a helpful assistant"

	agent := NewAgent(mockLLM, executor, registry, systemPrompt)

	if agent == nil {
		t.Fatal("NewAgent() returned nil")
	}
	if agent.llm != mockLLM {
		t.Error("NewAgent() llm not set correctly")
	}
	if agent.executor != executor {
		t.Error("NewAgent() executor not set correctly")
	}
	if agent.registry != registry {
		t.Error("NewAgent() registry not set correctly")
	}
	if agent.systemPrompt != systemPrompt {
		t.Error("NewAgent() systemPrompt not set correctly")
	}
	if agent.maxIterations != 10 {
		t.Errorf("NewAgent() maxIterations = %d, want 10", agent.maxIterations)
	}
}

func TestAgent_Run_NoToolCalls(t *testing.T) {
	// Mock LLM that returns a simple response without tool calls
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{
				Content:   "Hello! How can I help you?",
				ToolCalls: []llm.ToolCall{},
			},
		},
	}

	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	session := NewSession()
	response, err := agent.Run(context.Background(), session, "Hello")

	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	if response != "Hello! How can I help you?" {
		t.Errorf("Run() response = %q, want %q", response, "Hello! How can I help you?")
	}

	// Verify session has both user and assistant messages
	if len(session.Messages) != 2 {
		t.Errorf("Session has %d messages, want 2", len(session.Messages))
	}

	if session.Messages[0].Role != "user" || session.Messages[0].Content != "Hello" {
		t.Errorf("First message = %+v, want user message 'Hello'", session.Messages[0])
	}

	if session.Messages[1].Role != "assistant" {
		t.Errorf("Second message role = %s, want assistant", session.Messages[1].Role)
	}

	// Verify LLM was called with correct parameters
	if mockLLM.lastReq.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("LLM called with system prompt %q", mockLLM.lastReq.SystemPrompt)
	}
}

func TestAgent_Run_WithToolCall(t *testing.T) {
	// Mock LLM that:
	// 1. First call: returns a tool call to echo
	// 2. Second call: returns final response
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call-1",
						Name: "echo",
						Args: map[string]any{"message": "test message"},
					},
				},
			},
			{
				Content:   "I echoed your message!",
				ToolCalls: []llm.ToolCall{},
			},
		},
	}

	registry := tools.NewRegistry()
	registry.Register(echo.NewTool())
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	session := NewSession()
	response, err := agent.Run(context.Background(), session, "Echo 'test message'")

	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	if response != "I echoed your message!" {
		t.Errorf("Run() response = %q, want %q", response, "I echoed your message!")
	}

	// Verify session has: user message, tool result, assistant final response
	if len(session.Messages) < 3 {
		t.Errorf("Session has %d messages, want at least 3", len(session.Messages))
	}

	// Verify LLM was called twice (once for tool call, once for final response)
	if mockLLM.callCount != 2 {
		t.Errorf("LLM was called %d times, want 2", mockLLM.callCount)
	}
}

func TestAgent_Run_MultipleToolCalls(t *testing.T) {
	// Mock LLM that:
	// 1. First call: returns two tool calls
	// 2. Second call: returns final response
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call-1",
						Name: "echo",
						Args: map[string]any{"message": "first"},
					},
					{
						ID:   "call-2",
						Name: "echo",
						Args: map[string]any{"message": "second"},
					},
				},
			},
			{
				Content:   "Done!",
				ToolCalls: []llm.ToolCall{},
			},
		},
	}

	registry := tools.NewRegistry()
	registry.Register(echo.NewTool())
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	session := NewSession()
	response, err := agent.Run(context.Background(), session, "Test")

	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	if response != "Done!" {
		t.Errorf("Run() response = %q, want %q", response, "Done!")
	}

	// Verify both tool calls were executed (2 tool result messages)
	toolResultCount := 0
	for _, msg := range session.Messages {
		if msg.Role == "user" && msg.Content != "Test" {
			toolResultCount++
		}
	}

	if toolResultCount != 2 {
		t.Errorf("Session has %d tool results, want 2", toolResultCount)
	}
}

func TestAgent_Run_LLMError(t *testing.T) {
	// Mock LLM that returns an error
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{},
	}

	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	session := NewSession()
	_, err := agent.Run(context.Background(), session, "Hello")

	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}

	if !contains(err.Error(), "llm chat failed") {
		t.Errorf("Run() error = %v, want error containing 'llm chat failed'", err)
	}
}

func TestAgent_Run_ToolNotFound(t *testing.T) {
	// Mock LLM that calls a non-existent tool
	// The agent should handle tool errors gracefully by passing them to the LLM
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{
				Content: "",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call-1",
						Name: "nonexistent_tool",
						Args: map[string]any{},
					},
				},
			},
			// LLM receives the tool error and provides a final response
			{
				Content:   "Sorry, that tool doesn't exist",
				ToolCalls: []llm.ToolCall{},
			},
		},
	}

	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	session := NewSession()
	response, err := agent.Run(context.Background(), session, "Test")

	// The agent should complete successfully, handling the tool error gracefully
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}

	if response != "Sorry, that tool doesn't exist" {
		t.Errorf("Run() response = %q, want %q", response, "Sorry, that tool doesn't exist")
	}

	// Verify the tool error was added to the session as a message
	hasErrorMessage := false
	for _, msg := range session.Messages {
		if contains(msg.Content, "failed to get tool") || contains(msg.Content, "tool not found") {
			hasErrorMessage = true
			break
		}
	}

	if !hasErrorMessage {
		t.Error("Session should contain tool error message")
	}

	// Verify LLM was called twice (once for tool call, once after error)
	if mockLLM.callCount != 2 {
		t.Errorf("LLM was called %d times, want 2", mockLLM.callCount)
	}
}

func TestAgent_Run_MaxIterations(t *testing.T) {
	// Mock LLM that always returns tool calls (infinite loop scenario)
	responses := make([]*llm.ChatResponse, 15)
	for i := range responses {
		responses[i] = &llm.ChatResponse{
			Content: "",
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call-1",
					Name: "echo",
					Args: map[string]any{"message": "loop"},
				},
			},
		}
	}

	mockLLM := &mockLLM{
		responses: responses,
	}

	registry := tools.NewRegistry()
	registry.Register(echo.NewTool())
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	session := NewSession()
	_, err := agent.Run(context.Background(), session, "Test")

	if err == nil {
		t.Fatal("Run() expected error for max iterations, got nil")
	}

	if !contains(err.Error(), "max iterations") {
		t.Errorf("Run() error = %v, want error containing 'max iterations'", err)
	}
}

func TestAgent_Run_ContextCancellation(t *testing.T) {
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{
				Content:   "Response",
				ToolCalls: []llm.ToolCall{},
			},
		},
	}

	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	session := NewSession()
	_, err := agent.Run(ctx, session, "Test")

	if err == nil {
		t.Fatal("Run() expected error for cancelled context, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestAgent_Run_ToolDefinitionsIncluded(t *testing.T) {
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{
				Content:   "Done",
				ToolCalls: []llm.ToolCall{},
			},
		},
	}

	registry := tools.NewRegistry()
	registry.Register(echo.NewTool())
	executor := tools.NewExecutor(registry)
	agent := NewAgent(mockLLM, executor, registry, "You are a helpful assistant")

	session := NewSession()
	_, err := agent.Run(context.Background(), session, "Test")

	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Verify tools were passed to LLM
	if len(mockLLM.lastReq.Tools) != 1 {
		t.Errorf("LLM received %d tools, want 1", len(mockLLM.lastReq.Tools))
	}

	if mockLLM.lastReq.Tools[0].Name != "echo" {
		t.Errorf("LLM received tool %q, want 'echo'", mockLLM.lastReq.Tools[0].Name)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
