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
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
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
		Graph:        graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:        sqlStore,
		SessionModel: sessionmodel.NewRepository(sqlStore.DB(), store.DriverSQLite),
		Adapters:     adapters.NewRegistry(),
		LLM:          llmAdapter,
		// Wire the real audit repository so limit-hit audit rows (e.g. the
		// iteration-cap row, Session: loop-budget-exhaustion) are actually
		// written and can be asserted via srv.services.Store.DB(). Additive and
		// inert for tests that never trip a limit.
		Audit: audit.NewRepository(sqlStore.DB(), store.DriverSQLite),
	}

	srv := New(services, TestingPolicyEngine(services))
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

// TestTaskOwnership_CrossUserSessionRejected guards the §11 Phase 1 send/continue
// isolation rule on the task path: a turn that targets another user's session is
// refused with 404 (not run, not persisted, no history seeded). Without the
// guard, seedHistory would load alice's prior messages into bob's model context.
func TestTaskOwnership_CrossUserSessionRejected(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	srv, mux := setupTaskServer(t, &recordingLLM{answer: "ok"})

	if _, err := srv.services.SessionModel.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: "owned-by-alice", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: alice,
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	w := reqAsPrincipal(mux, "POST", "/api/v1/tasks", bob,
		map[string]any{"message": "hi", "session_id": "owned-by-alice"})
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-user task: got %d, want 404", w.Code)
	}

	// alice may continue her own session.
	if ok := reqAsPrincipal(mux, "POST", "/api/v1/tasks", alice,
		map[string]any{"message": "hi", "session_id": "owned-by-alice"}); ok.Code != http.StatusOK {
		t.Errorf("owner task: got %d, want 200", ok.Code)
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

// TestTaskAutoTitle_GeneratedOnFirstMessage verifies a freshly-started session
// is auto-titled from its opening message. The title is written asynchronously
// (a background LLM call, claude.ai-style — there is no synchronous first-words
// heuristic), so this polls until it lands rather than asserting it exists the
// instant the turn returns.
func TestTaskAutoTitle_GeneratedOnFirstMessage(t *testing.T) {
	const alice = "user:alice@example.com"
	srv, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	ctx := context.Background()

	w := reqAsPrincipal(mux, "POST", "/api/v1/tasks", alice,
		map[string]any{"message": "why is the payment service crashlooping in prod"})
	if w.Code != http.StatusOK {
		t.Fatalf("task: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	var title string
	for range 200 {
		sess, err := srv.services.SessionModel.GetSession(ctx, resp.SessionID)
		if err != nil || sess == nil {
			t.Fatalf("GetSession: %v", err)
		}
		if sess.Title != nil && *sess.Title != "" {
			title = *sess.Title
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if title == "" {
		t.Fatalf("session has no auto-title after first message (waited 2s)")
	}
}

// titleStubLLM simulates a reasoning model (e.g. Gemini 2.5): a title request —
// identified by the ChatTitleSystem prompt — returns empty content unless given
// enough output budget that the model's "thinking" does not starve the reply.
// Regular agent-loop calls always answer "ok". Guards the regression where a
// 32-token title cap produced empty titles (NULL in the DB) on Gemini.
type titleStubLLM struct {
	minTitleTokens int
}

func (s *titleStubLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if req.SystemPrompt == prompts.ChatTitleSystem {
		if req.MaxTokens < s.minTitleTokens {
			return &llm.ChatResponse{Content: ""}, nil // thinking ate the budget
		}
		return &llm.ChatResponse{Content: "Payment Service Crash Loop"}, nil
	}
	return &llm.ChatResponse{
		Content: "ok",
		Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (s *titleStubLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// TestTaskAutoTitle_SurvivesReasoningModelThinkingBudget verifies the title call
// is given enough output budget that a reasoning model still emits a title after
// its thinking — i.e. the cap is not regressed back to a starving value.
func TestTaskAutoTitle_SurvivesReasoningModelThinkingBudget(t *testing.T) {
	const alice = "user:alice@example.com"
	srv, mux := setupTaskServer(t, &titleStubLLM{minTitleTokens: 256})
	ctx := context.Background()

	w := reqAsPrincipal(mux, "POST", "/api/v1/tasks", alice,
		map[string]any{"message": "why is the payment service crashlooping in prod"})
	if w.Code != http.StatusOK {
		t.Fatalf("task: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	var title string
	for range 200 {
		sess, err := srv.services.SessionModel.GetSession(ctx, resp.SessionID)
		if err != nil || sess == nil {
			t.Fatalf("GetSession: %v", err)
		}
		if sess.Title != nil && *sess.Title != "" {
			title = *sess.Title
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if title != "Payment Service Crash Loop" {
		t.Fatalf("title = %q, want the model-generated title (a starving token cap regressed?)", title)
	}
}

// TestTaskAutoTitle_DoesNotOverwriteExistingTitle verifies the auto-titler skips
// a session that already has a title — a later turn, or a user-set name, is left
// intact. Using a pre-set title makes this deterministic (the skip branch fires
// before any heuristic/async work).
func TestTaskAutoTitle_DoesNotOverwriteExistingTitle(t *testing.T) {
	const alice = "user:alice@example.com"
	srv, mux := setupTaskServer(t, &taskStubLLM{response: "ok"})
	ctx := context.Background()

	custom := "User Picked This Name"
	if _, err := srv.services.SessionModel.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "titled", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: alice, Title: &custom,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	w := reqAsPrincipal(mux, "POST", "/api/v1/tasks", alice,
		map[string]any{"message": "completely different topic", "session_id": "titled"})
	if w.Code != http.StatusOK {
		t.Fatalf("task: got %d, want 200", w.Code)
	}

	sess, _ := srv.services.SessionModel.GetSession(ctx, "titled")
	if sess.Title == nil || *sess.Title != custom {
		t.Errorf("title = %v, want preserved %q", sess.Title, custom)
	}
}

// sentinelTitleStubLLM returns the "New chat" sentinel for a title request — the
// reply the prompt mandates for a meaningless opening message. Regular agent-loop
// calls answer "ok".
type sentinelTitleStubLLM struct{}

func (s *sentinelTitleStubLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if req.SystemPrompt == prompts.ChatTitleSystem {
		return &llm.ChatResponse{Content: "New chat"}, nil
	}
	return &llm.ChatResponse{
		Content: "ok",
		Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (s *sentinelTitleStubLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// TestTaskAutoTitle_SentinelLeavesSessionUntitled verifies that when the title
// model returns the "New chat" sentinel (meaningless opening message), it is NOT
// persisted as the title. Persisting it would freeze the session at "New chat"
// forever (maybeAutoTitle only runs while the title is nil), so the row must stay
// nil — letting the placeholder show and a later turn re-title the session.
func TestTaskAutoTitle_SentinelLeavesSessionUntitled(t *testing.T) {
	const alice = "user:alice@example.com"
	srv, mux := setupTaskServer(t, &sentinelTitleStubLLM{})
	ctx := context.Background()

	w := reqAsPrincipal(mux, "POST", "/api/v1/tasks", alice, map[string]any{"message": "hi"})
	if w.Code != http.StatusOK {
		t.Fatalf("task: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp taskResponse
	json.NewDecoder(w.Body).Decode(&resp)

	// The title write is async; give the background goroutine ample time to run
	// (and skip), then assert the session is still untitled.
	time.Sleep(300 * time.Millisecond)
	sess, err := srv.services.SessionModel.GetSession(ctx, resp.SessionID)
	if err != nil || sess == nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Title != nil {
		t.Errorf("title = %q, want nil (the 'New chat' sentinel must not be persisted)", *sess.Title)
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

// taskSynthLLM drives the loop to its iteration cap, then answers on the
// tool-less forced-synthesis call. It recognises that call by its appended final
// user message (prompts.MaxIterationsSynthesis) and returns non-empty content;
// every in-loop call returns a tool call so the loop runs to exhaustion. This is
// the mock shape the Phase 1 item 9 note prescribed for the success path.
type taskSynthLLM struct{}

func (t *taskSynthLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Content == prompts.MaxIterationsSynthesis {
		return &llm.ChatResponse{
			Content: "Synthesized answer from the evidence gathered so far.",
			Usage:   llm.TokenUsage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28},
		}, nil
	}
	return &llm.ChatResponse{
		ToolCalls: []llm.ToolCall{{ID: "tc-loop", Name: "graph_query", Args: map[string]any{"query": "loop"}}},
		Usage:     llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (t *taskSynthLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// countTaskAuditRows returns the audit_log row count for an action, read from
// the same store the server writes to (audit.Repository exposes no list method).
func countTaskAuditRows(t *testing.T, srv *Server, action string) int {
	t.Helper()
	var n int
	if err := srv.services.Store.DB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM audit_log WHERE action = ?", action).Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// TestTaskEndpoint_MaxIterations_ForcedSynthesisCompletes proves the
// loop-budget-exhaustion happy path end-to-end: loop exhaustion followed by a
// successful forced synthesis yields terminal status "completed" (NOT
// max_iterations_reached), a persisted assistant message carrying stop_reason
// "max_iterations", the same stop_reason on the response, an iteration count
// equal to the cap (the synthesis call is observer-silent), and exactly one
// llm_max_iterations_reached audit row.
func TestTaskEndpoint_MaxIterations_ForcedSynthesisCompletes(t *testing.T) {
	srv, mux := setupTaskServer(t, &taskSynthLLM{})
	const maxIter = 2
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "investigate the failing service",
		"config":  map[string]any{"max_iterations": maxIter},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp taskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("status = %q, want completed", resp.Status)
	}
	if resp.StopReason != agentloop.StopReasonMaxIterations {
		t.Errorf("stop_reason = %q, want %q", resp.StopReason, agentloop.StopReasonMaxIterations)
	}
	if resp.FinalAnswer == "" {
		t.Error("expected a synthesized final answer, got empty")
	}
	if resp.Iterations != maxIter {
		t.Errorf("iterations = %d, want %d (synthesis call must be observer-silent)", resp.Iterations, maxIter)
	}

	// The assistant message persisted with the truncation marker so a reload can
	// still render the notice.
	msgs, err := srv.services.SessionModel.ListChatMessages(context.Background(), resp.SessionID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	var assistant *sessionmodel.ChatMessage
	for i := range msgs {
		if msgs[i].Role == "assistant" {
			assistant = &msgs[i]
		}
	}
	if assistant == nil {
		t.Fatal("no assistant message persisted")
	}
	if assistant.Content == "" {
		t.Error("persisted assistant message content is empty")
	}
	if assistant.StopReason != agentloop.StopReasonMaxIterations {
		t.Errorf("persisted stop_reason = %q, want %q", assistant.StopReason, agentloop.StopReasonMaxIterations)
	}

	if n := countTaskAuditRows(t, srv, audit.ActionLLMMaxIterationsReached); n != 1 {
		t.Errorf("llm_max_iterations_reached audit rows = %d, want 1", n)
	}
}

// TestTaskEndpoint_MaxIterations_SynthesisFailureReported proves the fallback
// path: when the forced-synthesis call cannot produce an answer (maxIterLLM
// returns a tool call with empty content on the synthesis call too), the turn
// reports terminal status "max_iterations_reached" with no stop_reason, and the
// audit row is STILL written exactly once.
func TestTaskEndpoint_MaxIterations_SynthesisFailureReported(t *testing.T) {
	srv, mux := setupTaskServer(t, &maxIterLLM{})
	const maxIter = 2
	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{
		"message": "loop forever",
		"config":  map[string]any{"max_iterations": maxIter},
	})

	var resp taskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "max_iterations_reached" {
		t.Errorf("status = %q, want max_iterations_reached", resp.Status)
	}
	if resp.StopReason != "" {
		t.Errorf("stop_reason = %q, want empty on synthesis failure", resp.StopReason)
	}
	if n := countTaskAuditRows(t, srv, audit.ActionLLMMaxIterationsReached); n != 1 {
		t.Errorf("llm_max_iterations_reached audit rows = %d, want 1 (written whether or not synthesis succeeds)", n)
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

// floorMutateLLM issues a github_comment (Mutate) tool call on its first turn, then
// a final answer. Used to drive a managed-system mutation through the user-task
// loop so the write floor's denial can be observed end-to-end.
type floorMutateLLM struct{ calls int }

func (m *floorMutateLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	m.calls++
	if m.calls == 1 {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{
				{ID: "tc-w", Name: "github_comment", Args: map[string]any{"path": "/tmp/joe-floor-test", "content": "x"}},
			},
			Usage: llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		}, nil
	}
	return &llm.ChatResponse{
		Content: "I could not write the file.",
		Usage:   llm.TokenUsage{InputTokens: 5, OutputTokens: 2, TotalTokens: 7},
	}, nil
}

func (m *floorMutateLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// TestTaskEndpoint_WriteFloorBlocksMutate closes the D-0022 floor-coverage hole:
// the user-task executor (internal/api/tasks.go) must carry the boot-resolved
// write floor (D-0018) so a Mutate (github_comment) is denied when the floor is up,
// exactly like the Core Agent executor. With the floor up in observation mode, a
// github_comment tool call must be refused with the floor reason — surfaced as the
// stable "observation" write-failure code (classifyWriteFailure maps the typed
// *safety.WriteFloorError, so a non-empty observation code proves the executor
// returned that typed error). Before the fix the executor carried no floor and
// the mutation slipped through.
func TestTaskEndpoint_WriteFloorBlocksMutate(t *testing.T) {
	srv, mux := setupTaskServer(t, &floorMutateLLM{})
	// Floor up via observation mode (JOE_MODE=observation at boot). The handler
	// reads services.WriteFloor at request time, so setting it on the shared
	// services pointer after setup is sufficient.
	srv.services.WriteFloor = safety.ResolveWriteFloor(false, true)

	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "write a file"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp taskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Steps) < 1 || len(resp.Steps[0].ToolResults) == 0 {
		t.Fatalf("expected a tool result for the github_comment call; steps=%+v", resp.Steps)
	}
	tr := resp.Steps[0].ToolResults[0]
	if tr.Name != "github_comment" {
		t.Fatalf("tool result name = %q, want github_comment", tr.Name)
	}
	if tr.Error == "" {
		t.Error("github_comment under an up floor should be denied (non-empty error)")
	}
	if tr.ErrorCode != errorCodeObservation {
		t.Errorf("tool result error_code = %q, want %q (write floor not enforced on the user-task path)",
			tr.ErrorCode, errorCodeObservation)
	}
	// Turn-level code is the first per-tool write-failure code.
	if resp.ErrorCode != errorCodeObservation {
		t.Errorf("turn error_code = %q, want %q", resp.ErrorCode, errorCodeObservation)
	}
}

// TestTaskEndpoint_WriteFloorAllowsReads asserts the floor only denies Mutates:
// with the floor up, a Read tool call on the user-task path must NOT be
// floor-blocked (its result carries no floor write-failure code). Reads must
// always flow — observation/safe mode must not freeze ordinary queries.
func TestTaskEndpoint_WriteFloorAllowsReads(t *testing.T) {
	srv, mux := setupTaskServer(t, &taskToolLLM{}) // issues graph_query (a Read)
	srv.services.WriteFloor = safety.ResolveWriteFloor(false, true)

	w := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "list services"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp taskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Steps) < 1 || len(resp.Steps[0].ToolResults) == 0 {
		t.Fatalf("expected a tool result for the graph_query call; steps=%+v", resp.Steps)
	}
	tr := resp.Steps[0].ToolResults[0]
	if tr.Name != "graph_query" {
		t.Fatalf("tool result name = %q, want graph_query", tr.Name)
	}
	// The read must not be denied BY THE FLOOR (it may fail for unrelated
	// reasons in this harness, but never with a floor write-failure code).
	if tr.ErrorCode == errorCodeObservation || tr.ErrorCode == errorCodeSafeMode {
		t.Errorf("read tool result error_code = %q; a Read must not be floor-blocked", tr.ErrorCode)
	}
	if resp.ErrorCode == errorCodeObservation || resp.ErrorCode == errorCodeSafeMode {
		t.Errorf("turn error_code = %q; a read-only turn must not carry a floor code", resp.ErrorCode)
	}
}

// stubFloorTool is a no-op tool whose action class is decided by its NAME via
// safety.ClassifyTool (github_comment → Mutate, list_components → Read). It lets the
// executor-seam test below exercise the floor without the real tool plumbing.
type stubFloorTool struct{ name string }

func (s stubFloorTool) Name() string                    { return s.name }
func (s stubFloorTool) Description() string             { return "stub" }
func (s stubFloorTool) Parameters() llm.ParameterSchema { return llm.ParameterSchema{Type: "object"} }
func (s stubFloorTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return "ok", nil
}

// TestUserTaskExecutorFloor_ErrorsIs is the typed-error guard for the user-task
// executor seam. tasks.go builds its executor with tools.WithWriteFloor; this
// test constructs the same executor and asserts a Mutate is denied with an error
// that satisfies errors.Is(err, safety.ErrWriteFloor) (i.e. a
// *safety.WriteFloorError), while a Read passes through untouched. This is the
// errors.Is contract the api write-failure classifier depends on.
func TestUserTaskExecutorFloor_ErrorsIs(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(stubFloorTool{name: "github_comment"})  // Mutate by classification
	registry.Register(stubFloorTool{name: "list_components"}) // Read by classification

	exec := tools.NewExecutor(registry, nil, tools.WithWriteFloor(safety.ResolveWriteFloor(false, true)))
	ctx := context.Background()

	_, err := exec.Execute(ctx, "github_comment", map[string]any{"path": "/tmp/x", "content": "y"})
	if err == nil {
		t.Fatal("github_comment under an up floor returned nil error; want a write-floor denial")
	}
	if !errors.Is(err, safety.ErrWriteFloor) {
		t.Errorf("errors.Is(err, ErrWriteFloor) = false for %v; want true", err)
	}
	var floorErr *safety.WriteFloorError
	if !errors.As(err, &floorErr) {
		t.Fatalf("errors.As(err, *WriteFloorError) = false for %v", err)
	}
	if floorErr.Reason != safety.FloorReasonObservation {
		t.Errorf("floor reason = %q, want %q", floorErr.Reason, safety.FloorReasonObservation)
	}

	// A Read must not be floor-blocked.
	if _, err := exec.Execute(ctx, "list_components", map[string]any{"path": "/tmp/x"}); err != nil {
		t.Errorf("list_components under an up floor returned %v; a Read must pass the floor", err)
	}
}

// declareTestIncident puts the server's session model into incident regime,
// owned by alice. Every user-task request below runs in a DIFFERENT,
// non-captain session, so its Mutate is gate-refused (incident_mode) while the
// regime is active and the floor is down.
func declareTestIncident(t *testing.T, srv *Server) {
	t.Helper()
	// Promote-in-place (§12.3): create alice's 'default' session, then promote
	// it to the incident master — declaration no longer mints a fresh row.
	const principal = "user:alice@example.com"
	sid := uuid.NewString()
	if _, err := srv.services.SessionModel.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: sid, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: principal,
	}); err != nil {
		t.Fatalf("create default session: %v", err)
	}
	if _, _, err := srv.services.SessionModel.DeclareIncidentRegime(
		context.Background(), principal, sid, sessionmodel.RegimeKindHuman); err != nil {
		t.Fatalf("declare incident regime: %v", err)
	}
}

// firstMutateToolResult returns the first step's first tool result, failing if
// the loop produced none (e.g. the LLM never issued the expected tool call).
func firstMutateToolResult(t *testing.T, resp taskResponse) taskToolResult {
	t.Helper()
	if len(resp.Steps) < 1 || len(resp.Steps[0].ToolResults) == 0 {
		t.Fatalf("expected a tool result for the github_comment call; steps=%+v", resp.Steps)
	}
	return resp.Steps[0].ToolResults[0]
}

// TestTaskEndpoint_FloorPrecedesIncidentOnUserTaskPath pins the
// captaingate.WithFloor wiring on the user-task path. When BOTH the write floor
// is up (observation) AND an incident regime is active, a Mutate driven through
// the user-task loop must surface the FLOOR reason (observation), NOT the
// captain gate's incident_mode code — the floor > incident precedence (D-0022 /
// D-0019 decision 9) realized on the chat path. It exists to handle exactly this
// co-occurrence, and would regress to incident_mode if captaingate.WithFloor
// were removed from tasks.go: the §C gate would then run before any floor check.
//
// The first sub-run is a positive control. With the SAME incident regime but the
// floor DOWN, the very same Mutate is refused by the captain gate (incident_mode)
// — proving the incident regime is genuinely active on this path so the
// precedence assertion is non-vacuous. Each sub-run uses its own server because
// floorMutateLLM carries a per-instance call counter (it issues github_comment only
// on its first Chat call), so a fresh instance is needed to re-issue the Mutate.
func TestTaskEndpoint_FloorPrecedesIncidentOnUserTaskPath(t *testing.T) {
	// Control: floor DOWN + incident active → captain gate refuses (incident_mode).
	srvCtl, muxCtl := setupTaskServer(t, &floorMutateLLM{})
	declareTestIncident(t, srvCtl)

	wCtl := doRequest(muxCtl, "POST", "/api/v1/tasks", map[string]any{"message": "write a file"})
	if wCtl.Code != http.StatusOK {
		t.Fatalf("control: expected 200, got %d: %s", wCtl.Code, wCtl.Body.String())
	}
	var respCtl taskResponse
	if err := json.NewDecoder(wCtl.Body).Decode(&respCtl); err != nil {
		t.Fatalf("control decode: %v", err)
	}
	trCtl := firstMutateToolResult(t, respCtl)
	if trCtl.ErrorCode != errorCodeIncidentMode {
		t.Fatalf("control: tool error_code = %q, want %q — incident regime not active on the "+
			"user-task path, so the precedence assertion below would be vacuous", trCtl.ErrorCode, errorCodeIncidentMode)
	}

	// Precedence: floor UP (observation) + incident active → the floor wins.
	srvPre, muxPre := setupTaskServer(t, &floorMutateLLM{})
	declareTestIncident(t, srvPre)
	srvPre.services.WriteFloor = safety.ResolveWriteFloor(false, true) // observation

	wPre := doRequest(muxPre, "POST", "/api/v1/tasks", map[string]any{"message": "write a file"})
	if wPre.Code != http.StatusOK {
		t.Fatalf("precedence: expected 200, got %d: %s", wPre.Code, wPre.Body.String())
	}
	var respPre taskResponse
	if err := json.NewDecoder(wPre.Body).Decode(&respPre); err != nil {
		t.Fatalf("precedence decode: %v", err)
	}
	trPre := firstMutateToolResult(t, respPre)
	if trPre.ErrorCode == errorCodeIncidentMode {
		t.Error("precedence: Mutate surfaced incident_mode under an up floor — floor > incident " +
			"precedence broken on the user-task path (captaingate.WithFloor missing?)")
	}
	if trPre.ErrorCode != errorCodeObservation {
		t.Errorf("precedence: tool error_code = %q, want %q (the floor must outrank the incident gate)",
			trPre.ErrorCode, errorCodeObservation)
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

	// Seed components (FK target for zone assignments)
	_, err = sqlStore.DB().Exec(`
		INSERT INTO components (id, type, name, config) VALUES ('k8s-frontend', 'kubernetes', 'Frontend K8s', '{}');
		INSERT INTO components (id, type, name, config) VALUES ('k8s-payments', 'kubernetes', 'Payments K8s', '{}');
	`)
	if err != nil {
		t.Fatalf("seed components: %v", err)
	}

	rbacRepo := rbac.NewRepository(sqlStore.DB(), "sqlite")
	ctx := context.Background()

	// Create zones
	_, err = rbacRepo.CreateZone(ctx, rbac.Zone{
		ID: "zone-a", Name: "Frontend Zone",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery},
	}, "test")
	if err != nil {
		t.Fatalf("create zone-a: %v", err)
	}
	_, err = rbacRepo.CreateZone(ctx, rbac.Zone{
		ID: "zone-b", Name: "Payments Zone",
		AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery},
	}, "test")
	if err != nil {
		t.Fatalf("create zone-b: %v", err)
	}

	// Assign components to zones
	if err := rbacRepo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "k8s-frontend", ZoneID: "zone-a", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign k8s-frontend: %v", err)
	}
	if err := rbacRepo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "k8s-payments", ZoneID: "zone-b", AssignedBy: "test",
	}, "test"); err != nil {
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

	srv := New(services, TestingPolicyEngine(services))
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
					"component_id": "k8s-payments",
					"resource":     "pods",
					"namespace":    "payments",
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

func (z *zoneViolationLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1}, nil
}

// TestTaskEndpoint_ZoneViolationBlocked verifies that when allowed_zones is set,
// tool calls targeting components outside those zones are blocked at the executor level.
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
