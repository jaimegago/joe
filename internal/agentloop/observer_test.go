package agentloop

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/tools/local/echo"
)

// permissivePolicy returns a safety policy that allows all actions.
func permissivePolicy() *safety.SafetyPolicy {
	return &safety.SafetyPolicy{
		Version: 1,
		Record: safety.RecordPolicy{
			GraphMutations:     true,
			SourceRegistration: true,
			OnboardingFacts:    true,
			AutonomousRefresh:  true,
		},
		Act: safety.ActPolicy{
			WriteFile:           safety.WriteFilePolicy{Enabled: true},
			RunCommand:          safety.RunCommandPolicy{Enabled: true, AllowedCommands: []string{"*"}},
			K8sWrite:            safety.ActionToggle{Enabled: true},
			PagerdutyAck:        safety.ActionToggle{Enabled: true},
			AlertmanagerSilence: safety.ActionToggle{Enabled: true},
			GitPush:             safety.ActionToggle{Enabled: true},
		},
	}
}

func TestObserver_NoToolCalls(t *testing.T) {
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{Content: "Hello!", Usage: llm.TokenUsage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8}},
		},
	}
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, nil)
	observer := &SliceObserver{}
	agent := NewAgent(mockLLM, executor, registry, "test", WithObserver(observer))

	session := NewSession(nil)
	resp, err := agent.Run(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp != "Hello!" {
		t.Errorf("response = %q, want %q", resp, "Hello!")
	}

	if len(observer.Steps) != 1 {
		t.Fatalf("observer got %d steps, want 1", len(observer.Steps))
	}
	step := observer.Steps[0]
	if step.StepNumber != 1 {
		t.Errorf("step_number = %d, want 1", step.StepNumber)
	}
	if step.LLMResponse.Content != "Hello!" {
		t.Errorf("content = %q, want %q", step.LLMResponse.Content, "Hello!")
	}
	if step.LLMResponse.Usage.InputTokens != 5 {
		t.Errorf("input_tokens = %d, want 5", step.LLMResponse.Usage.InputTokens)
	}
	if step.ToolResults != nil {
		t.Error("tool_results should be nil for no-tool-call step")
	}
}

func TestObserver_WithToolCalls(t *testing.T) {
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "tc-1", Name: "echo", Args: map[string]any{"message": "ping"}},
				},
				Usage: llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			},
			{
				Content: "Done!",
				Usage:   llm.TokenUsage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28},
			},
		},
	}
	registry := tools.NewRegistry()
	registry.Register(echo.NewTool())
	executor := tools.NewExecutor(registry, nil, tools.WithPolicy(permissivePolicy()))
	observer := &SliceObserver{}
	agent := NewAgent(mockLLM, executor, registry, "test", WithObserver(observer))

	session := NewSession(nil)
	resp, err := agent.Run(context.Background(), session, "echo ping")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp != "Done!" {
		t.Errorf("response = %q, want %q", resp, "Done!")
	}

	if len(observer.Steps) != 2 {
		t.Fatalf("observer got %d steps, want 2", len(observer.Steps))
	}

	// First step: tool call
	s1 := observer.Steps[0]
	if s1.StepNumber != 1 {
		t.Errorf("step 1 number = %d", s1.StepNumber)
	}
	if len(s1.LLMResponse.ToolCalls) != 1 {
		t.Fatalf("step 1 tool_calls len = %d, want 1", len(s1.LLMResponse.ToolCalls))
	}
	if s1.LLMResponse.ToolCalls[0].Name != "echo" {
		t.Errorf("tool call name = %q, want %q", s1.LLMResponse.ToolCalls[0].Name, "echo")
	}
	if len(s1.ToolResults) != 1 {
		t.Fatalf("step 1 tool_results len = %d, want 1", len(s1.ToolResults))
	}
	if s1.ToolResults[0].Name != "echo" {
		t.Errorf("tool result name = %q, want %q", s1.ToolResults[0].Name, "echo")
	}
	// echo is not in the safety toolRegistry, so it's classified as T3:Act with
	// no policy key and always denied. The observer should still record the
	// error — that's the important part (the LLM handles it gracefully).
	if s1.ToolResults[0].Error == "" {
		t.Error("tool result error should be non-empty (echo blocked by safety)")
	}

	// Second step: final answer
	s2 := observer.Steps[1]
	if s2.StepNumber != 2 {
		t.Errorf("step 2 number = %d", s2.StepNumber)
	}
	if s2.LLMResponse.Content != "Done!" {
		t.Errorf("step 2 content = %q, want %q", s2.LLMResponse.Content, "Done!")
	}
	if s2.ToolResults != nil {
		t.Error("step 2 tool_results should be nil")
	}
}

func TestObserver_NilDoesNotPanic(t *testing.T) {
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{Content: "ok"},
		},
	}
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, nil)
	// No observer — should not panic
	agent := NewAgent(mockLLM, executor, registry, "test")

	session := NewSession(nil)
	_, err := agent.Run(context.Background(), session, "hi")
	if err != nil {
		t.Fatalf("Run without observer should not error: %v", err)
	}
}

func TestObserver_ToolsAvailable(t *testing.T) {
	mockLLM := &mockLLM{
		responses: []*llm.ChatResponse{
			{Content: "ok"},
		},
	}
	registry := tools.NewRegistry()
	registry.Register(echo.NewTool())
	executor := tools.NewExecutor(registry, nil)
	observer := &SliceObserver{}
	agent := NewAgent(mockLLM, executor, registry, "test", WithObserver(observer))

	session := NewSession(nil)
	agent.Run(context.Background(), session, "hi")

	if len(observer.Steps) == 0 {
		t.Fatal("no steps")
	}
	tools := observer.Steps[0].LLMRequest.ToolsAvailable
	if len(tools) != 1 || tools[0] != "echo" {
		t.Errorf("tools_available = %v, want [echo]", tools)
	}
}
