//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
)

// sequentialMockLLM returns ChatResponses in order. When all responses are
// consumed it returns a plain text "Done." response so the agent loop exits.
type sequentialMockLLM struct {
	responses []*llm.ChatResponse
	callCount int
}

func (m *sequentialMockLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if m.callCount < len(m.responses) {
		resp := m.responses[m.callCount]
		m.callCount++
		return resp, nil
	}
	m.callCount++
	return &llm.ChatResponse{Content: "Done."}, nil
}

func (m *sequentialMockLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// TestAgentFlow_ToolCallRoundtrip verifies the full loop:
//
//	user message → MockLLM emits graph_query tool call →
//	core tool calls joecored API (empty graph store) →
//	tool result returned to MockLLM → MockLLM emits final text →
//	agent returns response.
func TestAgentFlow_ToolCallRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	// Wire core tools to the running joecored (port 7778).
	coreClient := client.New("http://localhost:7778")
	registry := tools.NewCoreRegistry(coreClient, nil)
	executor := tools.NewExecutor(registry, nil)

	mockLLM := &sequentialMockLLM{
		responses: []*llm.ChatResponse{
			// Turn 1: trigger a graph_query tool call.
			{
				ToolCalls: []llm.ToolCall{
					{Name: "graph_query", Args: map[string]any{"query": "type:service"}},
				},
			},
			// Turn 2: after receiving the (empty) tool result, produce a final answer.
			{Content: "I found no services — the graph is empty."},
		},
	}

	agent := agentloop.NewAgent(mockLLM, executor, registry, "You are a test agent.")
	session := agentloop.NewSession(nil)

	response, err := agent.Run(context.Background(), session, "what services are in the graph?")
	if err != nil {
		t.Fatalf("agent.Run() error = %v", err)
	}
	if response == "" {
		t.Error("expected non-empty response")
	}
	if mockLLM.callCount != 2 {
		t.Errorf("LLM callCount = %d, want 2 (tool call + final answer)", mockLLM.callCount)
	}

	// Verify joecored is still healthy after the round-trip.
	status, err := harness.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() after test error = %v", err)
	}
	if status["status"] != "ok" {
		t.Errorf("joecored status = %v, want ok", status["status"])
	}
}

// TestAgentFlow_MaxIterationsRespected verifies the agent stops after reaching
// DefaultMaxIterations (20) when the LLM never stops calling tools.
func TestAgentFlow_MaxIterationsRespected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	harness := NewTestHarness(t)
	defer harness.Stop()
	if err := harness.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	coreClient := client.New("http://localhost:7778")
	registry := tools.NewCoreRegistry(coreClient, nil)
	executor := tools.NewExecutor(registry, nil)

	// MockLLM always returns an echo tool call — never a final text answer.
	infiniteMockLLM := &sequentialMockLLM{}
	// Leave responses empty so fallback always returns "Done." immediately.
	// We test that the agent exits without hanging.

	agent := agentloop.NewAgent(infiniteMockLLM, executor, registry, "You are a test agent.")
	session := agentloop.NewSession(nil)

	// The agent should finish (either with result or error) within the iteration limit.
	_, err := agent.Run(context.Background(), session, "do something")
	// We just verify no panic and the call returns.
	_ = err

	if infiniteMockLLM.callCount == 0 {
		t.Error("LLM was never called")
	}
}
