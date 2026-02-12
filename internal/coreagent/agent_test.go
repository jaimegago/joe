package coreagent

import (
	"context"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// mockLLMAdapter for testing
type mockLLMAdapter struct{}

func (m *mockLLMAdapter) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Content: "Mock response",
		Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (m *mockLLMAdapter) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Content: "Mock response", Done: true}
	close(ch)
	return ch, nil
}

func (m *mockLLMAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func TestNewCoreAgent(t *testing.T) {
	// Create in-memory database for testing
	sqlStore, err := store.New(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer sqlStore.Close()

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "info"},
		Refresh: config.RefreshConfig{IntervalMinutes: 1},
	}

	// Create mock services
	adapterRegistry := adapters.NewRegistry()
	services := core.New(cfg, sqlStore, sqlStore.DB(), adapterRegistry, nil)
	defer services.Close()

	// Create mock LLM adapter
	llmAdapter := &mockLLMAdapter{}

	// Create Core Agent
	agent := New(services, llmAdapter, nil)
	if agent == nil {
		t.Fatal("New() returned nil agent")
	}

	// Verify agent has the expected components
	if len(agent.GetAvailableTools()) == 0 {
		t.Error("Core Agent should have registered tools")
	}

	// Test that tools are registered correctly
	expectedTools := map[string]bool{
		"graph_add_node":       true,
		"graph_add_edge":       true,
		"graph_update_node":    true,
		"register_source":      true,
		"save_onboarding_fact": true,
	}

	tools := agent.GetAvailableTools()
	for _, tool := range tools {
		if expectedTools[tool.Name()] {
			delete(expectedTools, tool.Name())
		}
	}

	if len(expectedTools) > 0 {
		t.Errorf("Missing expected tools: %v", expectedTools)
	}
}

func TestCoreAgentStartStop(t *testing.T) {
	// Create in-memory database for testing
	sqlStore, err := store.New(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer sqlStore.Close()

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "info"},
		Refresh: config.RefreshConfig{IntervalMinutes: 1},
	}

	// Create mock services
	adapterRegistry := adapters.NewRegistry()
	services := core.New(cfg, sqlStore, sqlStore.DB(), adapterRegistry, nil)
	defer services.Close()

	// Create mock LLM adapter
	llmAdapter := &mockLLMAdapter{}

	// Create and start Core Agent
	agent := New(services, llmAdapter, nil)
	ctx := context.Background()

	if err := agent.Start(ctx); err != nil {
		t.Fatalf("failed to start Core Agent: %v", err)
	}

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop the agent
	if err := agent.Stop(ctx); err != nil {
		t.Fatalf("failed to stop Core Agent: %v", err)
	}
}

func TestCoreAgentProcessOnboarding(t *testing.T) {
	// Create in-memory database for testing
	sqlStore, err := store.New(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer sqlStore.Close()

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "info"},
		Refresh: config.RefreshConfig{IntervalMinutes: 1},
	}

	// Create mock services
	adapterRegistry := adapters.NewRegistry()
	services := core.New(cfg, sqlStore, sqlStore.DB(), adapterRegistry, nil)
	defer services.Close()

	// Create mock LLM adapter
	llmAdapter := &mockLLMAdapter{}

	// Create Core Agent
	agent := New(services, llmAdapter, nil)
	ctx := context.Background()

	// Test onboarding processing
	testInput := "I have a Kubernetes cluster with nginx pods"
	if err := agent.ProcessOnboarding(ctx, testInput); err != nil {
		t.Fatalf("failed to process onboarding input: %v", err)
	}

	// Verify that the fact was stored
	facts, err := sqlStore.Facts.GetByType(ctx, "user_input")
	if err != nil {
		t.Fatalf("failed to retrieve facts: %v", err)
	}

	if len(facts) == 0 {
		t.Error("expected onboarding fact to be stored")
	} else if facts[0].Content != testInput {
		t.Errorf("expected fact content %q, got %q", testInput, facts[0].Content)
	}
}
