package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// TestTaskStatus_MaxIterations_ClassifiedByErrorsIs asserts the
// classifier maps the wrapped ErrMaxIterations sentinel to the
// "max_iterations_reached" status via errors.Is, not via substring
// matching on the message text. The previous classifier used a
// 15-character prefix match; this test guards against regressing back
// to that posture.
func TestTaskStatus_MaxIterations_ClassifiedByErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("max iterations (10) reached without final response: %w", agentloop.ErrMaxIterations)
	status, errMsg := taskStatus(context.Background(), wrapped)
	if status != "max_iterations_reached" {
		t.Errorf("status = %q, want %q", status, "max_iterations_reached")
	}
	if errMsg == "" {
		t.Error("errMsg empty; want the wrapped error message")
	}
	if !errors.Is(wrapped, agentloop.ErrMaxIterations) {
		t.Fatal("test wrapping is itself broken — sentinel not reachable via errors.Is")
	}
}

// TestTaskStatus_MaxIterations_RewordResilient ensures the classifier
// does NOT depend on the message text starting with "max iterations ".
// Wrapping the sentinel with an entirely different descriptive prefix
// must still classify as max_iterations_reached.
func TestTaskStatus_MaxIterations_RewordResilient(t *testing.T) {
	reworded := fmt.Errorf("iteration ceiling hit at step 7: %w", agentloop.ErrMaxIterations)
	status, _ := taskStatus(context.Background(), reworded)
	if status != "max_iterations_reached" {
		t.Errorf("status = %q for reworded wrap of ErrMaxIterations; want %q",
			status, "max_iterations_reached")
	}
}

// TestTaskStatus_Unrelated_NotMaxIterations ensures a generic error
// that mentions "max iterations" in its text (but does NOT wrap the
// sentinel) is NOT mis-classified. Under the old substring matcher
// this string was indistinguishable from a real exhaustion; under
// errors.Is it falls through to the "error" bucket.
func TestTaskStatus_Unrelated_NotMaxIterations(t *testing.T) {
	stray := errors.New("max iterations were configured incorrectly by caller")
	status, _ := taskStatus(context.Background(), stray)
	if status == "max_iterations_reached" {
		t.Errorf("status = %q for stray text-only error; should not bucket as max_iterations_reached",
			status)
	}
}

// TestTaskStatus_SessionTokenCeiling_RunawayTerminated asserts the G3a
// classifier maps the ErrSessionTokenCeiling sentinel to the
// "runaway_terminated" status via errors.Is, and that this bucket is
// distinct from the max_iterations, timeout, and generic-error buckets.
// The taskStatus switch is the only line that classifies the loop's
// terminal outcome — if this regresses, the streaming SSE final event
// will mis-label a runaway termination as a generic error.
func TestTaskStatus_SessionTokenCeiling_RunawayTerminated(t *testing.T) {
	wrapped := fmt.Errorf("session token ceiling (10000) exceeded at total 12345: %w",
		agentloop.ErrSessionTokenCeiling)

	status, errMsg := taskStatus(context.Background(), wrapped)
	if status != "runaway_terminated" {
		t.Errorf("status = %q, want %q", status, "runaway_terminated")
	}
	if errMsg == "" {
		t.Error("errMsg empty; want the wrapped error message")
	}
	if !errors.Is(wrapped, agentloop.ErrSessionTokenCeiling) {
		t.Fatal("test wrapping is itself broken — sentinel not reachable via errors.Is")
	}

	// Pairwise distinctness vs the buckets this case must not collide with.
	if status == "max_iterations_reached" {
		t.Error("runaway termination mis-bucketed as max_iterations_reached")
	}
	if status == "timeout" {
		t.Error("runaway termination mis-bucketed as timeout")
	}
	if status == "error" {
		t.Error("runaway termination mis-bucketed as the generic error bucket")
	}

	// A reworded wrap of the same sentinel must classify identically:
	// classification is by errors.Is, not by the message prefix.
	reworded := fmt.Errorf("agentic loop terminated by safety backstop: %w",
		agentloop.ErrSessionTokenCeiling)
	rewordedStatus, _ := taskStatus(context.Background(), reworded)
	if rewordedStatus != "runaway_terminated" {
		t.Errorf("reworded wrap classified as %q; want %q", rewordedStatus, "runaway_terminated")
	}

	// Cross-check: a wrap of ErrMaxIterations must NOT classify as
	// runaway_terminated, and a wrap of ErrSessionTokenCeiling must NOT
	// classify as max_iterations_reached.
	maxIter := fmt.Errorf("max iterations (10) reached without final response: %w",
		agentloop.ErrMaxIterations)
	if s, _ := taskStatus(context.Background(), maxIter); s == "runaway_terminated" {
		t.Error("max-iterations wrap mis-bucketed as runaway_terminated")
	}
	if s, _ := taskStatus(context.Background(), wrapped); s == "max_iterations_reached" {
		t.Error("ceiling wrap mis-bucketed as max_iterations_reached")
	}
}

// TestTaskStatus_CostLimitExceeded_DistinctBucket asserts the G3b
// classifier maps the ErrCostLimitExceeded sentinel to the
// "cost_limit_exceeded" status via errors.Is, and that this bucket is
// distinct from runaway_terminated, max_iterations_reached, timeout,
// and the generic error bucket. The taskStatus switch is the only
// line that classifies the loop's terminal outcome; if this regresses,
// the streaming SSE final event will mis-label a cost-window refusal
// as a generic error.
func TestTaskStatus_CostLimitExceeded_DistinctBucket(t *testing.T) {
	wrapped := fmt.Errorf("cost-window gate refused: hourly window observed 12345 >= limit 10000 (currency USD): %w",
		llmusage.ErrCostLimitExceeded)

	status, errMsg := taskStatus(context.Background(), wrapped)
	if status != "cost_limit_exceeded" {
		t.Errorf("status = %q, want %q", status, "cost_limit_exceeded")
	}
	if errMsg == "" {
		t.Error("errMsg empty; want the wrapped error message")
	}
	if !errors.Is(wrapped, llmusage.ErrCostLimitExceeded) {
		t.Fatal("test wrapping is itself broken — sentinel not reachable via errors.Is")
	}

	// Pairwise distinctness vs the four buckets this case must not collide with.
	for _, bucket := range []string{"max_iterations_reached", "runaway_terminated", "timeout", "error"} {
		if status == bucket {
			t.Errorf("cost-limit refusal mis-bucketed as %q", bucket)
		}
	}

	// A reworded wrap of the same sentinel must classify identically:
	// classification is by errors.Is, not by the message prefix.
	reworded := fmt.Errorf("budget tripwire hit, refusing call: %w", llmusage.ErrCostLimitExceeded)
	if s, _ := taskStatus(context.Background(), reworded); s != "cost_limit_exceeded" {
		t.Errorf("reworded wrap classified as %q; want %q", s, "cost_limit_exceeded")
	}

	// Cross-check: wraps of the OTHER terminal sentinels must NOT
	// classify as cost_limit_exceeded.
	otherSentinels := []error{agentloop.ErrMaxIterations, agentloop.ErrSessionTokenCeiling}
	for _, sentinel := range otherSentinels {
		w := fmt.Errorf("descriptive prefix: %w", sentinel)
		if s, _ := taskStatus(context.Background(), w); s == "cost_limit_exceeded" {
			t.Errorf("wrap of %v mis-bucketed as cost_limit_exceeded", sentinel)
		}
	}
}

// TestTaskStatus_ContextOverflow_DistinctBucket asserts the classifier maps a
// wrapped llm.ErrContextOverflow (as the agentic loop produces it: an adapter
// error wrapped in "llm chat failed: %w") to the "context_overflow" status via
// errors.Is, with a friendly wire message that is NOT the raw provider text,
// and that this bucket is distinct from the other terminal buckets.
func TestTaskStatus_ContextOverflow_DistinctBucket(t *testing.T) {
	// Mirror the real chain: adapter wrap → loop's "llm chat failed: %w".
	adapterErr := fmt.Errorf("Claude rejected the request: prompt is too long: 215024 tokens > 200000 maximum: %w", llm.ErrContextOverflow)
	wrapped := fmt.Errorf("llm chat failed: %w", adapterErr)

	status, errMsg := taskStatus(context.Background(), wrapped)
	if status != "context_overflow" {
		t.Errorf("status = %q, want %q", status, "context_overflow")
	}
	if !strings.Contains(errMsg, "too large for the model's context window") {
		t.Errorf("errMsg = %q, want the friendly context-window message", errMsg)
	}
	if strings.Contains(errMsg, "215024") {
		t.Errorf("errMsg leaks raw provider text: %q", errMsg)
	}

	// Distinct from every other terminal bucket.
	for _, bucket := range []string{"max_iterations_reached", "runaway_terminated", "cost_limit_exceeded", "timeout", "error"} {
		if status == bucket {
			t.Errorf("context overflow mis-bucketed as %q", bucket)
		}
	}

	// A non-overflow generic provider error stays "error".
	generic := fmt.Errorf("llm chat failed: %w", errors.New("invalid request to Claude API: unsupported parameter"))
	if s, _ := taskStatus(context.Background(), generic); s != "error" {
		t.Errorf("generic provider error classified as %q; want %q", s, "error")
	}
}

// taskStubLLM returns a canned response with no tool calls (single iteration).
type taskStubLLM struct {
	response string
}

func (s *taskStubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Content: s.response,
		Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (s *taskStubLLM) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (s *taskStubLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// taskToolLLM returns a tool call on the first call, then a final answer.
type taskToolLLM struct {
	callCount int
}

func (t *taskToolLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	t.callCount++
	if t.callCount == 1 {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{
				{ID: "tc-1", Name: "graph_query", Args: map[string]any{"query": "services"}},
			},
			Usage: llm.TokenUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30},
		}, nil
	}
	return &llm.ChatResponse{
		Content: "Found 3 services in the graph.",
		Usage:   llm.TokenUsage{InputTokens: 50, OutputTokens: 20, TotalTokens: 70},
	}, nil
}

func (t *taskToolLLM) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (t *taskToolLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// maxIterLLM always returns tool calls, never a final answer.
type maxIterLLM struct{}

func (m *maxIterLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		ToolCalls: []llm.ToolCall{
			{ID: "tc-loop", Name: "graph_query", Args: map[string]any{"query": "loop"}},
		},
		Usage: llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (m *maxIterLLM) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (m *maxIterLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

func setupTaskServer(t *testing.T, llmAdapter llm.LLMAdapter) (*Server, *http.ServeMux) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	services := &core.Services{
		Config: &config.Config{
			Server: config.ServerConfig{
				Address: "localhost:7777",
			},
		},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: adapters.NewRegistry(),
		LLM:      llmAdapter,
	}

	srv := New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

// TestTaskEndpoint_RouteRegistered ensures the route doesn't 404.
func TestTaskEndpoint_RouteRegistered(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "hello"})
	if w.Code == http.StatusNotFound {
		t.Error("POST /api/v1/tasks returned 404 — route not registered")
	}
}

// TestTaskEndpoint_CompletedStatus verifies a simple prompt returns completed status.
func TestTaskEndpoint_CompletedStatus(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "Hello from Joe!"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "hello"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp taskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != "completed" {
		t.Errorf("status = %q, want %q", resp.Status, "completed")
	}
	if resp.FinalAnswer != "Hello from Joe!" {
		t.Errorf("final_answer = %q, want %q", resp.FinalAnswer, "Hello from Joe!")
	}
	if resp.TaskID == "" {
		t.Error("task_id should not be empty")
	}
	if resp.SessionID == "" {
		t.Error("session_id should not be empty")
	}
	if len(resp.Steps) < 1 {
		t.Error("steps array should have at least one entry")
	}
	if resp.DurationMs < 0 {
		t.Error("duration_ms should be non-negative")
	}
}

// TestTaskEndpoint_StepsHaveContent checks step structure.
func TestTaskEndpoint_StepsHaveContent(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "result"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "test"})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Steps) == 0 {
		t.Fatal("no steps returned")
	}
	step := resp.Steps[0]
	if step.StepNumber != 1 {
		t.Errorf("step_number = %d, want 1", step.StepNumber)
	}
	if step.LLMRequest.MessageCount == 0 {
		t.Error("llm_request.message_count should be > 0")
	}
	if step.LLMResponse.Content != "result" {
		t.Errorf("llm_response.content = %q, want %q", step.LLMResponse.Content, "result")
	}
}

// TestTaskEndpoint_TokenUsage verifies aggregated token usage is returned.
func TestTaskEndpoint_TokenUsage(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "hi"})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.TotalTokens.InputTokens == 0 {
		t.Error("total_tokens.input_tokens should be > 0")
	}
	if resp.TotalTokens.OutputTokens == 0 {
		t.Error("total_tokens.output_tokens should be > 0")
	}
}

// TestTaskEndpoint_SessionPersisted verifies the session is persisted and messages are retrievable.
func TestTaskEndpoint_SessionPersisted(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "persisted answer"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "query"})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	// Fetch messages via sessions endpoint
	wMsgs := doRequest(mux, "GET", "/api/v1/sessions/"+resp.SessionID+"/messages", nil)
	if wMsgs.Code != http.StatusOK {
		t.Fatalf("get messages: expected 200, got %d", wMsgs.Code)
	}

	var msgsResp map[string]any
	json.NewDecoder(wMsgs.Body).Decode(&msgsResp)
	count := int(msgsResp["count"].(float64))
	if count < 2 {
		t.Errorf("expected at least 2 messages (user + assistant), got %d", count)
	}
}

// TestTaskEndpoint_CustomSessionID verifies providing a session_id works.
func TestTaskEndpoint_CustomSessionID(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message":    "hello",
		"session_id": "my-custom-session",
	})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.SessionID != "my-custom-session" {
		t.Errorf("session_id = %q, want %q", resp.SessionID, "my-custom-session")
	}
}

// TestTaskEndpoint_MaxIterationsLimit verifies the loop stops at the configured limit.
func TestTaskEndpoint_MaxIterationsLimit(t *testing.T) {
	_, mux := setupTaskServer(t, &maxIterLLM{})
	maxIter := 2
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "loop forever",
		"config": map[string]any{
			"max_iterations": maxIter,
		},
	})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "max_iterations_reached" {
		t.Errorf("status = %q, want %q", resp.Status, "max_iterations_reached")
	}
	if resp.Iterations != maxIter {
		t.Errorf("iterations = %d, want %d", resp.Iterations, maxIter)
	}
}

// TestTaskEndpoint_TimeoutBehavior verifies timeout produces correct status.
func TestTaskEndpoint_TimeoutBehavior(t *testing.T) {
	_, mux := setupTaskServer(t, &maxIterLLM{})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "slow task",
		"config": map[string]any{
			"timeout":        "1ms",
			"max_iterations": 1000,
		},
	})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	// Should be either timeout or max_iterations_reached depending on timing
	if resp.Status != "timeout" && resp.Status != "max_iterations_reached" && resp.Status != "error" {
		t.Errorf("status = %q, want timeout or max_iterations_reached or error", resp.Status)
	}
}

// TestTaskEndpoint_SafetyTierOverride verifies safety_tier parameter is accepted.
func TestTaskEndpoint_SafetyTierOverride(t *testing.T) {
	tiers := []string{"observe", "record", "act"}
	for _, tier := range tiers {
		t.Run(tier, func(t *testing.T) {
			_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
			w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
				"message": "hello",
				"config": map[string]any{
					"safety_tier": tier,
				},
			})
			if w.Code != http.StatusOK {
				t.Errorf("expected 200 with safety_tier=%s, got %d: %s", tier, w.Code, w.Body.String())
			}
		})
	}
}

// TestTaskEndpoint_MissingMessage returns 400.
func TestTaskEndpoint_MissingMessage(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty message, got %d", w.Code)
	}
}

// TestTaskEndpoint_InvalidJSON returns 400.
func TestTaskEndpoint_InvalidJSON(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	req := httptest.NewRequest("POST", "/api/v1/tasks", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// TestTaskEndpoint_InvalidTimeout returns 400.
func TestTaskEndpoint_InvalidTimeout(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "hello",
		"config": map[string]any{
			"timeout": "not-a-duration",
		},
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid timeout, got %d", w.Code)
	}
}

// TestTaskEndpoint_InvalidMaxIterations returns 400.
func TestTaskEndpoint_InvalidMaxIterations(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	zero := 0
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "hello",
		"config": map[string]any{
			"max_iterations": zero,
		},
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for max_iterations=0, got %d", w.Code)
	}
}

// TestTaskEndpoint_NoLLM returns 503.
func TestTaskEndpoint_NoLLM(t *testing.T) {
	_, mux := setupTaskServer(t, nil)
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "hello"})
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without LLM, got %d", w.Code)
	}
}

// TestTaskEndpoint_ToolCallsInSteps verifies tool calls appear in step data.
func TestTaskEndpoint_ToolCallsInSteps(t *testing.T) {
	_, mux := setupTaskServer(t, &taskToolLLM{})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "list services"})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "completed" {
		t.Fatalf("status = %q, want completed", resp.Status)
	}

	// Should have 2 steps: one with tool calls, one with final answer
	if len(resp.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(resp.Steps))
	}

	// First step should have tool calls
	if len(resp.Steps[0].LLMResponse.ToolCalls) == 0 {
		t.Error("first step should have tool calls")
	}
	if resp.Steps[0].LLMResponse.ToolCalls[0].Name != "graph_query" {
		t.Errorf("tool call name = %q, want %q", resp.Steps[0].LLMResponse.ToolCalls[0].Name, "graph_query")
	}

	// Tool results should be present (will show error since graph_query tool isn't
	// actually registered, but the structure should be there)
	if len(resp.Steps[0].ToolResults) == 0 {
		t.Error("first step should have tool results")
	}

	// Final answer
	if resp.FinalAnswer != "Found 3 services in the graph." {
		t.Errorf("final_answer = %q, want %q", resp.FinalAnswer, "Found 3 services in the graph.")
	}
}

// setupTaskServerWithRBAC creates a task test server with RBAC configured.
// It creates zone-a with source "k8s-frontend" and zone-b with source "k8s-payments".
func setupTaskServerWithRBAC(t *testing.T, llmAdapter llm.LLMAdapter) (*Server, *http.ServeMux) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed sources (FK target for zone assignments)
	_, err = sqlStore.DB().Exec(`
		INSERT INTO sources (id, type, name, config) VALUES ('k8s-frontend', 'kubernetes', 'Frontend K8s', '{}');
		INSERT INTO sources (id, type, name, config) VALUES ('k8s-payments', 'kubernetes', 'Payments K8s', '{}');
	`)
	if err != nil {
		t.Fatalf("seed sources: %v", err)
	}

	rbacRepo := rbac.NewRepository(sqlStore.DB(), "sqlite")
	ctx := context.Background()

	// Create zones
	_, err = rbacRepo.CreateZone(ctx, rbac.Zone{
		ID: "zone-a", Name: "Frontend Zone",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery},
	})
	if err != nil {
		t.Fatalf("create zone-a: %v", err)
	}
	_, err = rbacRepo.CreateZone(ctx, rbac.Zone{
		ID: "zone-b", Name: "Payments Zone",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery},
	})
	if err != nil {
		t.Fatalf("create zone-b: %v", err)
	}

	// Assign sources to zones
	if err := rbacRepo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-frontend", ZoneID: "zone-a", AssignedBy: "test",
	}); err != nil {
		t.Fatalf("assign k8s-frontend: %v", err)
	}
	if err := rbacRepo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-payments", ZoneID: "zone-b", AssignedBy: "test",
	}); err != nil {
		t.Fatalf("assign k8s-payments: %v", err)
	}

	services := &core.Services{
		Config: &config.Config{
			Server: config.ServerConfig{Address: "localhost:7777"},
		},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: adapters.NewRegistry(),
		LLM:      llmAdapter,
		RBAC:     rbacRepo,
	}

	srv := New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

// zoneViolationLLM returns a tool call targeting k8s-payments (zone-b) source.
type zoneViolationLLM struct {
	callCount int
}

func (z *zoneViolationLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	z.callCount++
	if z.callCount == 1 {
		// Agent tries to access k8s-payments (zone-b)
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{
				{ID: "tc-zone", Name: "k8s_get", Args: map[string]any{
					"source_id": "k8s-payments",
					"resource":  "pods",
					"namespace": "payments",
				}},
			},
			Usage: llm.TokenUsage{InputTokens: 20, OutputTokens: 10},
		}, nil
	}
	return &llm.ChatResponse{
		Content: "I cannot access that resource.",
		Usage:   llm.TokenUsage{InputTokens: 50, OutputTokens: 20},
	}, nil
}

func (z *zoneViolationLLM) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (z *zoneViolationLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// TestTaskEndpoint_ZoneViolationBlocked verifies that when allowed_zones is set,
// tool calls targeting sources outside those zones are blocked at the executor level.
func TestTaskEndpoint_ZoneViolationBlocked(t *testing.T) {
	_, mux := setupTaskServerWithRBAC(t, &zoneViolationLLM{})

	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "check pods in payments namespace",
		"config": map[string]any{
			"allowed_zones": []string{"zone-a"}, // only zone-a (k8s-frontend)
		},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp taskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != "completed" {
		t.Fatalf("status = %q, want completed", resp.Status)
	}

	// The first step should show the tool call was attempted but resulted in an error
	if len(resp.Steps) < 1 {
		t.Fatal("expected at least 1 step")
	}

	// Check that the tool result contains a zone violation error
	step := resp.Steps[0]
	if len(step.ToolResults) == 0 {
		t.Fatal("first step should have tool results")
	}
	tr := step.ToolResults[0]
	if tr.Error == "" {
		t.Error("tool result should contain a zone violation error")
	}
	if !strings.Contains(tr.Error, "outside scope") {
		t.Errorf("tool error = %q, want zone violation message containing 'outside scope'", tr.Error)
	}
}

// TestTaskEndpoint_AllowedZonesAccepted verifies the allowed_zones parameter is accepted.
func TestTaskEndpoint_AllowedZonesAccepted(t *testing.T) {
	_, mux := setupTaskServerWithRBAC(t, &taskStubLLM{response: "ok"})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "hello",
		"config": map[string]any{
			"allowed_zones": []string{"zone-a"},
		},
	})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with allowed_zones, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTaskEndpoint_ToolsUsedList verifies deduplicated tools_used list.
func TestTaskEndpoint_ToolsUsedList(t *testing.T) {
	_, mux := setupTaskServer(t, &taskToolLLM{})
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "list services"})

	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.ToolsUsed) == 0 {
		t.Error("tools_used should not be empty when tools were called")
	}
	found := false
	for _, name := range resp.ToolsUsed {
		if name == "graph_query" {
			found = true
		}
	}
	if !found {
		t.Errorf("tools_used = %v, expected to contain 'graph_query'", resp.ToolsUsed)
	}
}
