package openaicompat_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llm/openaicompat"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/tools"
)

// denyAllMutateTool is a registered managed-system mutate. classified as
// ActionMutate by safety.ClassifyTool ("publish_doc_update_git"). Its Execute
// must never run in this test: the write floor must deny the call first.
type denyAllMutateTool struct{ t *testing.T }

func (d *denyAllMutateTool) Name() string { return "publish_doc_update_git" }
func (d *denyAllMutateTool) Description() string {
	return "publish a doc update (managed-system mutate)"
}
func (d *denyAllMutateTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{Type: "object"}
}
func (d *denyAllMutateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	d.t.Fatal("tool executed: the write floor must deny this mutate before it runs")
	return nil, nil
}

// TestGovernance_AdapterToolCall_DeniedByWriteFloor is the required governance
// break-test. It proves the openai-compat adapter creates NO ungoverned tool
// path: a tool call PRODUCED BY THE ADAPTER (parsed from a real OpenAI-shaped
// response) is denied by the write floor in observation mode when executed as
// a managed-system mutate — exactly as it would be for the native providers.
//
// The adapter only emits neutral llm.ToolCall values; it has no execution
// authority of its own. Execution flows solely through tools.Executor, whose
// gate order (floor > incident > RBAC) is unchanged by adding the provider.
// The test fails if the adapter's tool call can reach execution: the mock
// tool's Execute calls t.Fatal.
func TestGovernance_AdapterToolCall_DeniedByWriteFloor(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	// 1. Stand up a compatible endpoint that returns a mutate tool call,
	//    exactly as a model would when asking Joe to perform a managed-system mutate.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"content":"","tool_calls":[
				{"id":"call_x","type":"function","function":{"name":"publish_doc_update_git","arguments":"{\"component_id\":\"cluster-a\",\"path\":\"/etc/x\"}"}}
			]}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer srv.Close()

	client, err := openaicompat.NewClient("test-model", srv.URL+"/v1")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "please write /etc/x"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("adapter produced %d tool calls, want 1", len(resp.ToolCalls))
	}
	call := resp.ToolCalls[0]

	// 2. Build the SAME executor the rest of Joe uses, with the write floor
	//    UP in observation mode — nothing about the provider bypasses it.
	registry := tools.NewRegistry()
	registry.Register(&denyAllMutateTool{t: t})

	floor := safety.ResolveWriteFloor(false /*panicState*/, true /*observation*/)
	executor := tools.NewExecutor(registry, nil,
		tools.WithWriteFloor(floor),
		tools.WithPolicy(safety.DefaultPolicy()),
	)

	// 3. Execute the ADAPTER-PRODUCED tool call through the executor.
	_, execErr := executor.Execute(context.Background(), call.Name, call.Args)
	if execErr == nil {
		t.Fatal("adapter tool call reached execution ungoverned: expected a write-floor denial")
	}
	if !errors.Is(execErr, safety.ErrWriteFloor) {
		t.Fatalf("expected ErrWriteFloor denial, got %T: %v", execErr, execErr)
	}
	var floorErr *safety.WriteFloorError
	if !errors.As(execErr, &floorErr) || floorErr.Reason != safety.FloorReasonObservation {
		t.Fatalf("expected observation write-floor reason, got %T: %v", execErr, execErr)
	}
}
