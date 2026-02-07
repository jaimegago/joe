package repl

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/useragent"
)

// mockLLM is a simple mock for testing
type mockLLM struct {
	response string
}

func (m *mockLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Content:   m.response,
		ToolCalls: []llm.ToolCall{},
	}, nil
}

func (m *mockLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (m *mockLLM) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, nil
}

func TestNew(t *testing.T) {
	mockLLM := &mockLLM{response: "test"}
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry)
	agentInstance := useragent.NewAgent(mockLLM, executor, registry, "test prompt")

	repl := New(agentInstance)

	if repl == nil {
		t.Fatal("New() returned nil")
	}

	if repl.agent == nil {
		t.Error("New() did not set agent")
	}

	if repl.session == nil {
		t.Error("New() did not initialize session")
	}
}

// Note: Testing Run() requires mocking stdin/stdout which is complex
// For now, we test that the REPL can be created successfully
// Manual testing is the primary verification method for REPL functionality
