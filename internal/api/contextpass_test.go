package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
)

// capturingChatLLM records the last ChatRequest the loop built so a test can
// assert the explicit output cap was stamped on it.
type capturingChatLLM struct {
	mu   sync.Mutex
	last llm.ChatRequest
}

func (c *capturingChatLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.mu.Lock()
	c.last = req
	c.mu.Unlock()
	return &llm.ChatResponse{Content: "done", Usage: llm.TokenUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}}, nil
}
func (c *capturingChatLLM) Embed(_ context.Context, _ string) ([]float32, error) { return nil, nil }

func (c *capturingChatLLM) lastReq() llm.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// setupContextPassServer builds a task server wired with a swappable adapter
// over a capturing LLM, the full llm-settings stack (so the context-budget
// fraction can be mutated live), and the storage-backed budget provider.
func setupContextPassServer(t *testing.T) (*Server, *capturingChatLLM, *llmsettings.MutationService) {
	t.Helper()
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cap := &capturingChatLLM{}
	settingsRepo := llmsettings.NewRepository(sqlStore.DB(), sqlStore.Driver())
	auditRepo := audit.NewRepository(sqlStore.DB(), sqlStore.Driver())
	svc := llmsettings.NewMutationService(settingsRepo, auditRepo)

	services := &core.Services{
		Config: &config.Config{
			Server: config.ServerConfig{Address: "localhost:7777"},
			LLM: config.LLMConfig{
				Current: "default",
				Available: map[string]config.ModelConfig{
					"default": {Provider: "claude", Model: "claude-sonnet-4-20250514"},
				},
			},
		},
		Graph:                 graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:                 sqlStore,
		Adapters:              adapters.NewRegistry(),
		LLM:                   llm.NewSwappableAdapter(cap, "default"),
		Audit:                 auditRepo,
		LLMSettings:           svc,
		ContextBudgetProvider: llmsettings.NewContextBudgetProvider(settingsRepo, agentloop.NewStaticContextBudget(), nil),
	}
	return New(services, TestingPolicyEngine(services)), cap, svc
}

// TestBuildTaskRun_OutputCapStampedFromTable asserts the request the loop
// builds carries the active model's table max-output (claude -> 4096), so the
// agentic path never relies on a provider's implicit default.
func TestBuildTaskRun_OutputCapStampedFromTable(t *testing.T) {
	srv, cap, _ := setupContextPassServer(t)
	h := &taskHandler{server: srv}

	prepared := h.buildTaskRun(context.Background(), taskRequest{Message: "hi"}, 1, &agentloop.SliceObserver{})
	if _, err := prepared.agent.Run(context.Background(), prepared.session, "hi"); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}
	if got := cap.lastReq().MaxTokens; got != 4096 {
		t.Errorf("ChatRequest.MaxTokens = %d, want 4096 (claude-sonnet-4 table max output)", got)
	}
}

// TestBuildTaskRun_ContextBudgetLiveAdjustable asserts the session budget
// reflects a fraction change written between two requests, without restart.
// window=200000, so the budget delta between 0.7 and 0.5 is exactly
// floor(0.7*200000) - floor(0.5*200000) = 40000 (output + overhead constant).
func TestBuildTaskRun_ContextBudgetLiveAdjustable(t *testing.T) {
	srv, _, svc := setupContextPassServer(t)
	h := &taskHandler{server: srv}

	// First request: store unset -> backstop fraction 0.7.
	p1 := h.buildTaskRun(context.Background(), taskRequest{Message: "hi"}, 1, &agentloop.SliceObserver{})
	b1 := p1.session.TokenBudget

	if err := svc.SetContextBudget(context.Background(), 0.5); err != nil {
		t.Fatalf("SetContextBudget: %v", err)
	}

	// Second request on the SAME server: budget reflects the new fraction.
	p2 := h.buildTaskRun(context.Background(), taskRequest{Message: "hi"}, 1, &agentloop.SliceObserver{})
	b2 := p2.session.TokenBudget

	if b2 <= 0 {
		t.Fatalf("second budget = %d, want positive", b2)
	}
	if b1-b2 != 40000 {
		t.Errorf("budget delta = %d (b1=%d b2=%d), want 40000 for 0.7->0.5 over a 200000 window", b1-b2, b1, b2)
	}
}

// finalScriptLLM emits a scripted sequence of responses, ignoring input. Once
// the script is exhausted it repeats the last response rather than running off
// the end: a no-tool-call response costs one EXTRA Chat call, because the loop
// probes it for an unfulfilled tool intent before accepting it as final (see
// agentloop.probeUnfulfilledToolIntent). Every script here ends on a text-only
// response, so repeating it models the probe faithfully — asked again, the model
// still calls no tool, and the loop keeps the original answer.
type finalScriptLLM struct {
	responses []*llm.ChatResponse
	i         int
}

func (s *finalScriptLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	r := s.responses[min(s.i, len(s.responses)-1)]
	s.i++
	return r, nil
}
func (s *finalScriptLLM) Embed(context.Context, string) ([]float32, error) { return nil, nil }

// bigResultTool registers under a T1 (read-only) name so the default executor
// policy permits it, and returns a large payload to drive result truncation.
type bigResultTool struct{ payload string }

func (t *bigResultTool) Name() string        { return "list_components" }
func (t *bigResultTool) Description() string { return "big" }
func (t *bigResultTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{Type: "object", Properties: map[string]llm.Property{}}
}
func (t *bigResultTool) Execute(context.Context, map[string]any) (any, error) {
	return map[string]string{"data": t.payload}, nil
}

// TestFinalizeTaskResponse_TruncationCounters_OnFinal asserts that a turn with
// two oversized tool results and an oversized user message reports
// tool_results_truncated=2 and user_message_truncated=true on the final
// response, and that a clean turn reports zero/false. This exercises the full
// ingestion → session-counter → finalize path.
func TestFinalizeTaskResponse_TruncationCounters_OnFinal(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&bigResultTool{payload: strings.Repeat("Z", 40000)})
	exec := tools.NewExecutor(reg, nil)

	// First turn: two oversized tool results then a final answer.
	llmTrunc := &finalScriptLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "list_components", Args: map[string]any{}},
			{ID: "c2", Name: "list_components", Args: map[string]any{}},
		}},
		{Content: "done"},
	}}
	agent := agentloop.NewAgent(llmTrunc, exec, reg, "sys")
	session := agentloop.NewSession(nil)
	session.TokenBudget = 1000 // floor-governed: 2000-token caps

	if _, err := agent.Run(context.Background(), session, strings.Repeat("Q", 40000)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	resp := finalizeTaskResponse("t1", "s1", "completed", "", "done", nil, session, 200000, time.Second)
	if resp.ContextWindowTokens != 200000 {
		t.Errorf("context_window_tokens = %d, want 200000", resp.ContextWindowTokens)
	}
	if resp.ToolResultsTruncated != 2 {
		t.Errorf("tool_results_truncated = %d, want 2", resp.ToolResultsTruncated)
	}
	if !resp.UserMessageTruncated {
		t.Error("user_message_truncated = false, want true")
	}

	// Clean turn: nothing oversized.
	cleanLLM := &finalScriptLLM{responses: []*llm.ChatResponse{{Content: "ok"}}}
	cleanReg := tools.NewRegistry()
	cleanExec := tools.NewExecutor(cleanReg, nil)
	cleanAgent := agentloop.NewAgent(cleanLLM, cleanExec, cleanReg, "sys")
	cleanSession := agentloop.NewSession(nil)
	cleanSession.TokenBudget = 100000
	if _, err := cleanAgent.Run(context.Background(), cleanSession, "small"); err != nil {
		t.Fatalf("clean Run: %v", err)
	}
	resp2 := finalizeTaskResponse("t2", "s2", "completed", "", "ok", nil, cleanSession, 200000, time.Second)
	if resp2.ToolResultsTruncated != 0 || resp2.UserMessageTruncated {
		t.Errorf("clean turn: tool_results_truncated=%d user_message_truncated=%v, want 0/false",
			resp2.ToolResultsTruncated, resp2.UserMessageTruncated)
	}
}

// TestFinalizeTaskResponse_TrimFlags asserts the final response carries
// history_trimmed/messages_dropped from the session's pruning record, and
// reports false/0 when nothing was dropped.
func TestFinalizeTaskResponse_TrimFlags(t *testing.T) {
	// Trimmed session: a tiny budget forces dropping older messages.
	trimmed := agentloop.NewSession(nil)
	trimmed.TokenBudget = 5
	trimmed.AddMessages(context.Background(), []llm.Message{
		{Role: "user", Content: strings.Repeat("a", 40)},
		{Role: "assistant", Content: strings.Repeat("b", 40)},
		{Role: "user", Content: strings.Repeat("c", 40)},
	})
	resp := finalizeTaskResponse("t1", "s1", "completed", "", "answer", nil, trimmed, 200000, time.Second)
	if !resp.HistoryTrimmed || resp.MessagesDropped == 0 {
		t.Errorf("trimmed turn: history_trimmed=%v messages_dropped=%d, want true/>0", resp.HistoryTrimmed, resp.MessagesDropped)
	}

	// Untrimmed session: nothing dropped.
	clean := agentloop.NewSession(nil)
	clean.TokenBudget = 100000
	clean.MaxMessages = 100
	clean.AddMessages(context.Background(), []llm.Message{{Role: "user", Content: "hi"}})
	resp2 := finalizeTaskResponse("t2", "s2", "completed", "", "answer", nil, clean, 200000, time.Second)
	if resp2.HistoryTrimmed || resp2.MessagesDropped != 0 {
		t.Errorf("clean turn: history_trimmed=%v messages_dropped=%d, want false/0", resp2.HistoryTrimmed, resp2.MessagesDropped)
	}
}

// --- context-budget HTTP endpoint ---

func TestSetContextBudget_AdminMutatesAuditsAndGETReflects(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	before := f.countAudit(audit.ActionLLMSetContextBudget)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/context-budget", `{"fraction": 0.5}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin set-context-budget: status=%d body=%s", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionLLMSetContextBudget); n != before+1 {
		t.Errorf("audit rows = %d, want %d (atomic persist + audit)", n, before+1)
	}

	w2 := f.do(http.MethodGet, "/api/v1/llm/settings", "", "user:bob")
	if w2.Code != http.StatusOK {
		t.Fatalf("get settings: status=%d body=%s", w2.Code, w2.Body.String())
	}
	var resp struct {
		ContextBudget struct {
			StoredRaw float64 `json:"stored_raw"`
			State     string  `json:"state"`
			Effective float64 `json:"effective"`
		} `json:"context_budget"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ContextBudget.StoredRaw != 0.5 || resp.ContextBudget.State != LimitStateConfigured || resp.ContextBudget.Effective != 0.5 {
		t.Errorf("GET context_budget = %+v, want stored 0.5 / configured / effective 0.5", resp.ContextBudget)
	}
}

func TestSetContextBudget_InvalidFractionRejected(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	before := f.countAudit(audit.ActionLLMSetContextBudget)

	for _, body := range []string{`{"fraction": 0}`, `{"fraction": -0.2}`, `{"fraction": 1.5}`} {
		w := f.do(http.MethodPost, "/api/v1/llm/settings/context-budget", body, "user:alice")
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s: status=%d, want 400", body, w.Code)
		}
	}
	// Rejected writes must not audit.
	if n := f.countAudit(audit.ActionLLMSetContextBudget); n != before {
		t.Errorf("audit rows after rejected writes = %d, want %d", n, before)
	}
	// The boundary value 1.0 is accepted.
	w := f.do(http.MethodPost, "/api/v1/llm/settings/context-budget", `{"fraction": 1.0}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Errorf("fraction 1.0: status=%d, want 200 (inclusive upper bound)", w.Code)
	}
}

func TestSetContextBudget_NonAdminForbiddenNoAudit(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	before := f.countAudit(audit.ActionLLMSetContextBudget)
	w := f.do(http.MethodPost, "/api/v1/llm/settings/context-budget", `{"fraction": 0.5}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: status=%d, want 403", w.Code)
	}
	if n := f.countAudit(audit.ActionLLMSetContextBudget); n != before {
		t.Errorf("non-admin write audited: rows=%d, want %d", n, before)
	}
}

func TestSettingsGet_ContextBudgetUnsetReportsBackstop(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	w := f.do(http.MethodGet, "/api/v1/llm/settings", "", "user:bob")
	if w.Code != http.StatusOK {
		t.Fatalf("get settings: status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ContextBudget struct {
			StoredRaw float64 `json:"stored_raw"`
			State     string  `json:"state"`
			Effective float64 `json:"effective"`
		} `json:"context_budget"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ContextBudget.StoredRaw != 0 || resp.ContextBudget.State != LimitStateBackstop {
		t.Errorf("unset context_budget = %+v, want stored 0 / backstop", resp.ContextBudget)
	}
	if resp.ContextBudget.Effective != agentloop.DefaultContextBudgetFraction {
		t.Errorf("unset effective = %v, want backstop %v", resp.ContextBudget.Effective, agentloop.DefaultContextBudgetFraction)
	}
}
