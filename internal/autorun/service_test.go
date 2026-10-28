package autorun

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
)

// scriptedLLM serves canned responses in order. onChat runs after each call
// with the request's context, so a test can block, cancel, or record.
type scriptedLLM struct {
	mu        sync.Mutex
	responses []*llm.ChatResponse
	calls     int
	requests  []llm.ChatRequest
	onChat    func(ctx context.Context, n int)
}

func (m *scriptedLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	n := m.calls
	m.calls++
	m.requests = append(m.requests, req)
	var resp *llm.ChatResponse
	if n < len(m.responses) {
		resp = m.responses[n]
	}
	m.mu.Unlock()
	if m.onChat != nil {
		m.onChat(ctx, n)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("scripted llm: no more responses")
	}
	return resp, nil
}

// probeTool is a read tool that names a component, so a successful call
// covers it. It records the principal each call ran under. It wears a
// registered read-class tool name: safety.ClassifyTool denies an unknown name
// as a Mutate by default, which is the right default and not what this fixture
// is testing.
type probeTool struct {
	mu   sync.Mutex
	seen []rbac.Principal
}

func (p *probeTool) Name() string        { return "k8s_get" }
func (p *probeTool) Description() string { return "reads a component's self-report" }
func (p *probeTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{Type: "object", Properties: map[string]llm.Property{
		"component_id": {Type: "string", Description: "component"},
	}, Required: []string{"component_id"}}
}
func (p *probeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	p.mu.Lock()
	p.seen = append(p.seen, rbac.PrincipalFromContext(ctx))
	p.mu.Unlock()
	id, _ := args["component_id"].(string)
	if id == "" {
		return nil, errors.New("component_id required")
	}
	return map[string]string{"component": id, "status": "Ready"}, nil
}

type harness struct {
	t        *testing.T
	store    *store.Store
	sessions *sessionmodel.SQLRepository
	repo     *SQLRepository
	probe    *probeTool
	llm      *scriptedLLM
	svc      *Service
	now      time.Time
	nowMu    sync.Mutex
}

func newHarness(t *testing.T, llmAdapter *scriptedLLM, budget Budget) *harness {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	for _, id := range []string{"c1", "c2", "c3"} {
		if err := s.Components.Create(ctx, &store.Component{ID: id, Type: "kubernetes", Name: id, Config: []byte(`{}`)}); err != nil {
			t.Fatalf("create component %s: %v", id, err)
		}
	}
	h := &harness{
		t:        t,
		store:    s,
		sessions: sessionmodel.NewRepository(s.DB(), store.DriverSQLite),
		repo:     NewRepository(s.DB(), store.DriverSQLite),
		probe:    &probeTool{},
		llm:      llmAdapter,
		now:      time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
	}
	svc, err := New(Config{
		Runs:          h.repo,
		Sessions:      h.sessions,
		Components:    s.Components,
		Budget:        budget,
		Loop:          h.buildLoop,
		SchedulerTick: time.Hour, // tests drive Tick directly
		Now:           h.clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.svc = svc
	return h
}

func (h *harness) clock() time.Time {
	h.nowMu.Lock()
	defer h.nowMu.Unlock()
	return h.now
}

func (h *harness) advance(d time.Duration) {
	h.nowMu.Lock()
	h.now = h.now.Add(d)
	h.nowMu.Unlock()
}

// buildLoop mirrors what internal/api's factory does, over a registry holding
// the probe tool and an executor enforcing the run's resolved scope.
func (h *harness) buildLoop(_ context.Context, run *Run, observer agentloop.RunObserver) (*Loop, error) {
	registry := tools.NewRegistry()
	registry.Register(h.probe)
	executor := tools.NewExecutor(registry, nil, tools.WithAllowedComponents(run.ResolvedComponents))
	agent := agentloop.NewAgent(h.llm, executor, registry, llm.StaticSystem("test"),
		agentloop.WithObserver(observer),
		agentloop.WithSessionLimits(ceiling(run.TokenCeiling)),
	)
	agent.SetMaxIterations(PassIterationCap)
	session := agentloop.NewSession(nil)
	return &Loop{Agent: agent, Session: session, Close: session.Close}, nil
}

type ceiling int

func (c ceiling) SessionTokenCeiling() int { return int(c) }

func (h *harness) start() {
	h.t.Helper()
	if err := h.svc.Start(context.Background()); err != nil {
		h.t.Fatalf("Start: %v", err)
	}
	h.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.svc.Stop(ctx)
	})
}

func (h *harness) waitDone(id string) *Run {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.repo.GetRun(context.Background(), id)
		if err != nil {
			h.t.Fatalf("GetRun: %v", err)
		}
		if !run.Live() {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("run %s still live after 5s", id)
	return nil
}

func probeCall(id, component string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: "k8s_get", Args: map[string]any{"component_id": component}}
}

func usage(n int) llm.TokenUsage {
	return llm.TokenUsage{InputTokens: n, OutputTokens: 0, TotalTokens: n}
}

var defaultBudget = Budget{TokenCeiling: 10_000, WallClock: 10 * time.Second, ReportReserveTokens: 500, ReportReserve: 5 * time.Second}

func TestService_CompletedPass_RecordsCoverageAndWritesSession(t *testing.T) {
	adapter := &scriptedLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{probeCall("1", "c1"), probeCall("2", "c2")}, Usage: usage(100)},
		{Content: "c1 and c2 are Ready. c3 was not examined.\nTURN-KIND: answer", Usage: usage(50)},
	}}
	h := newHarness(t, adapter, defaultBudget)
	h.start()

	run, err := h.svc.Trigger(context.Background(), "user:op@example.com", Selection{Components: []string{"c1", "c2", "c3"}})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if run.Principal != "svc:agent:core" || run.Activator != "user:op@example.com" {
		t.Fatalf("principal/activator = %q/%q", run.Principal, run.Activator)
	}
	if run.TokenCeiling != 10_000 || run.WallClockSeconds != 10 || run.ReportReserveTokens != 500 {
		t.Errorf("budget not recorded on the run: %+v", run)
	}
	done := h.waitDone(run.ID)

	if done.State != StateCompleted || done.StopReason != "" {
		t.Errorf("state/stop = %q/%q, want completed with no stop reason", done.State, done.StopReason)
	}
	if strings.Join(done.Covered, ",") != "c1,c2" || strings.Join(done.NotCovered, ",") != "c3" {
		t.Errorf("coverage = %v / %v", done.Covered, done.NotCovered)
	}
	if done.TokensUsed != 150 || done.ReportTokens != 0 {
		t.Errorf("tokens = %d/%d, want 150/0", done.TokensUsed, done.ReportTokens)
	}
	if done.EndedAt == nil || done.SessionID == "" {
		t.Fatalf("ended_at/session not recorded: %+v", done)
	}

	// Every tool call ran as svc:agent:core — the activator never reached the
	// context the tools execute under.
	for _, p := range h.probe.seen {
		if p != rbac.Principal("svc:agent:core") {
			t.Errorf("tool ran as %q, want svc:agent:core", p)
		}
	}
	if len(h.probe.seen) != 2 {
		t.Errorf("probe calls = %d, want 2", len(h.probe.seen))
	}

	// The session: created by svc:agent:core, one assistant turn and no human
	// turn, carrying the limits section read from the record.
	sess, err := h.sessions.GetSession(context.Background(), done.SessionID)
	if err != nil || sess == nil {
		t.Fatalf("session: %v %v", sess, err)
	}
	if sess.CreatorPrincipal != "svc:agent:core" {
		t.Errorf("session creator = %q, want svc:agent:core", sess.CreatorPrincipal)
	}
	if sess.Title == nil || !strings.Contains(*sess.Title, "Auto Joe") {
		t.Errorf("session title = %v", sess.Title)
	}
	msgs, err := h.sessions.ListChatMessages(context.Background(), done.SessionID)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != "assistant" {
		t.Fatalf("messages = %d (%v), want exactly one assistant turn", len(msgs), msgs)
	}
	for _, want := range []string{"## Pass (unverified)", "c1 and c2 are Ready", "## Limits", "Covered (2): c1, c2", "Not looked at (1): c3"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Errorf("final turn missing %q:\n%s", want, msgs[0].Content)
		}
	}
	if strings.Contains(msgs[0].Content, "TURN-KIND") {
		t.Errorf("turn-kind marker leaked into the final turn")
	}

	// A run creates no agent_runs row: that table stays the sessions API's.
	var n int
	if err := h.store.DB().QueryRow(`SELECT COUNT(*) FROM agent_runs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("agent_runs rows = %d (err %v), want 0", n, err)
	}
}

func TestService_TokenCeiling_StopsPassAndReportsFromReserve(t *testing.T) {
	adapter := &scriptedLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{probeCall("1", "c1")}, Usage: usage(600)},
		// This call crosses the ceiling (1000): the loop stops before acting on
		// its tool calls, so c2 is never read.
		{ToolCalls: []llm.ToolCall{probeCall("2", "c2")}, Usage: usage(600)},
		// The reserve-funded report call.
		{Content: "Examined c1: Ready. Did not reach c2 or c3.", Usage: usage(120)},
	}}
	budget := defaultBudget
	budget.TokenCeiling = 1000
	budget.ReportReserveTokens = 300
	h := newHarness(t, adapter, budget)
	h.start()

	run, err := h.svc.Trigger(context.Background(), "user:op", Selection{Components: []string{"c1", "c2", "c3"}})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	done := h.waitDone(run.ID)
	if done.State != StateCompleted || done.StopReason != agentloop.StopReasonTokenCeiling {
		t.Fatalf("state/stop = %q/%q, want completed/token_ceiling (error %q)", done.State, done.StopReason, done.Error)
	}
	if strings.Join(done.Covered, ",") != "c1" || strings.Join(done.NotCovered, ",") != "c2,c3" {
		t.Errorf("coverage = %v / %v", done.Covered, done.NotCovered)
	}
	if done.TokensUsed != 1200 || done.ReportTokens != 120 {
		t.Errorf("tokens used/report = %d/%d, want 1200/120 (the report's spend is outside the ceiling)", done.TokensUsed, done.ReportTokens)
	}
	// The report call offered no tools and capped its output at the reserve.
	last := adapter.requests[len(adapter.requests)-1]
	if len(last.Tools) != 0 {
		t.Errorf("report call offered %d tools, want none", len(last.Tools))
	}
	if last.MaxTokens != 300 {
		t.Errorf("report call MaxTokens = %d, want the 300-token reserve", last.MaxTokens)
	}
	if !strings.Contains(last.Messages[len(last.Messages)-1].Content, "budget for this pass is exhausted") {
		t.Errorf("report call did not carry the budget synthesis instruction")
	}
	msgs, _ := h.sessions.ListChatMessages(context.Background(), done.SessionID)
	if len(msgs) != 1 || msgs[0].StopReason != agentloop.StopReasonTokenCeiling {
		t.Fatalf("final turn stop_reason = %v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "Did not reach c2 or c3") || !strings.Contains(msgs[0].Content, "stopped on the token ceiling (1000 tokens) with 1 of 3") {
		t.Errorf("final turn:\n%s", msgs[0].Content)
	}
}

func TestService_WallClock_StopsPassAndReportsUnderFreshDeadline(t *testing.T) {
	adapter := &scriptedLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{probeCall("1", "c1")}, Usage: usage(10)},
		nil, // the second call blocks until the pass deadline
		{Content: "Examined c1 only; the clock ran out.", Usage: usage(5)},
	}}
	adapter.onChat = func(ctx context.Context, n int) {
		if n == 1 {
			<-ctx.Done()
		}
	}
	budget := defaultBudget
	budget.WallClock = 150 * time.Millisecond
	h := newHarness(t, adapter, budget)
	h.start()

	run, err := h.svc.Trigger(context.Background(), "user:op", Selection{Components: []string{"c1", "c2"}})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	done := h.waitDone(run.ID)
	if done.State != StateCompleted || done.StopReason != agentloop.StopReasonWallClock {
		t.Fatalf("state/stop = %q/%q (error %q), want completed/wall_clock", done.State, done.StopReason, done.Error)
	}
	if strings.Join(done.Covered, ",") != "c1" || strings.Join(done.NotCovered, ",") != "c2" {
		t.Errorf("coverage = %v / %v", done.Covered, done.NotCovered)
	}
	if done.ReportTokens != 5 {
		t.Errorf("report tokens = %d, want 5 (the report ran under its own deadline after the pass's expired)", done.ReportTokens)
	}
	msgs, _ := h.sessions.ListChatMessages(context.Background(), done.SessionID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "stopped on the wall-clock ceiling") {
		t.Errorf("final turn: %v", msgs)
	}
}

func TestService_OneLiveRunPerScope_RefusesNotQueues(t *testing.T) {
	release := make(chan struct{})
	adapter := &scriptedLLM{responses: []*llm.ChatResponse{
		{Content: "done", Usage: usage(1)},
		{Content: "done", Usage: usage(1)},
		{Content: "done", Usage: usage(1)},
	}}
	adapter.onChat = func(_ context.Context, n int) {
		if n == 0 {
			<-release
		}
	}
	h := newHarness(t, adapter, defaultBudget)
	h.start()

	first, err := h.svc.Trigger(context.Background(), "user:a", Selection{Components: []string{"c1", "c2"}})
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	// Same scope, differently spelled: refused while the first is live.
	_, err = h.svc.Trigger(context.Background(), "user:b", Selection{Components: []string{"c2", "c1", "c1"}})
	if !errors.Is(err, ErrRunLive) {
		t.Fatalf("second trigger on a live scope: err = %v, want ErrRunLive", err)
	}
	// A different scope is not held by the first.
	other, err := h.svc.Trigger(context.Background(), "user:b", Selection{Components: []string{"c3"}})
	if err != nil {
		t.Fatalf("trigger on another scope: %v", err)
	}
	close(release)
	h.waitDone(first.ID)
	h.waitDone(other.ID)
	// Once the first run is done the scope is free again.
	if _, err := h.svc.Trigger(context.Background(), "user:b", Selection{Components: []string{"c1", "c2"}}); err != nil {
		t.Fatalf("trigger after the live run ended: %v", err)
	}
	runs, err := h.repo.ListRuns(context.Background(), 10)
	if err != nil || len(runs) != 3 {
		t.Errorf("runs = %d (%v), want 3: the refused trigger left no record", len(runs), err)
	}
}

func TestService_ScopeErrorsAreRefusedBeforeAnyRecord(t *testing.T) {
	h := newHarness(t, &scriptedLLM{}, defaultBudget)
	h.start()
	if _, err := h.svc.Trigger(context.Background(), "user:a", Selection{}); !errors.Is(err, ErrEmptyScope) {
		t.Errorf("empty: %v", err)
	}
	if _, err := h.svc.Trigger(context.Background(), "user:a", Selection{Components: []string{"nope"}}); !errors.Is(err, ErrUnknownComponent) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := h.svc.Trigger(context.Background(), "user:a", Selection{Zones: []string{"z"}}); !errors.Is(err, ErrZonesUnavailable) {
		t.Errorf("zones without rbac: %v", err)
	}
	runs, _ := h.repo.ListRuns(context.Background(), 10)
	if len(runs) != 0 {
		t.Errorf("refused triggers left %d records", len(runs))
	}
	var n int
	_ = h.store.DB().QueryRow(`SELECT COUNT(*) FROM agent_sessions`).Scan(&n)
	if n != 0 {
		t.Errorf("refused triggers created %d sessions", n)
	}
}

func TestService_TriggerBeforeStartIsRefused(t *testing.T) {
	h := newHarness(t, &scriptedLLM{}, defaultBudget)
	if _, err := h.svc.Trigger(context.Background(), "user:a", Selection{Components: []string{"c1"}}); !errors.Is(err, ErrNotStarted) {
		t.Errorf("err = %v, want ErrNotStarted", err)
	}
}

func TestService_Schedule_FiresAndRecordsRefusal(t *testing.T) {
	release := make(chan struct{})
	adapter := &scriptedLLM{responses: []*llm.ChatResponse{
		{Content: "done", Usage: usage(1)},
		{Content: "done", Usage: usage(1)},
	}}
	adapter.onChat = func(_ context.Context, n int) {
		if n == 0 {
			<-release
		}
	}
	h := newHarness(t, adapter, defaultBudget)
	h.start()
	ctx := context.Background()

	if _, err := h.svc.InstallSchedule(ctx, "user:admin", Selection{Components: []string{"c1"}}, 30*time.Second); err == nil {
		t.Fatal("an interval under a minute must be refused")
	}
	if _, err := h.svc.InstallSchedule(ctx, "user:admin", Selection{Components: []string{"nope"}}, time.Hour); !errors.Is(err, ErrUnknownComponent) {
		t.Fatalf("unresolvable schedule: %v", err)
	}
	sch, err := h.svc.InstallSchedule(ctx, "user:admin", Selection{Components: []string{"c1"}}, 10*time.Minute)
	if err != nil {
		t.Fatalf("InstallSchedule: %v", err)
	}
	if sch.TokenCeiling != defaultBudget.TokenCeiling || sch.WallClockSeconds != 10 || sch.InstalledBy != "user:admin" {
		t.Errorf("schedule did not copy the deployment budget: %+v", sch)
	}
	if !sch.NextRunAt.Equal(h.clock().Add(10 * time.Minute)) {
		t.Errorf("next_run_at = %v", sch.NextRunAt)
	}

	// Not due yet: nothing fires.
	h.svc.Tick(ctx)
	if runs, _ := h.repo.ListRuns(ctx, 10); len(runs) != 0 {
		t.Fatalf("tick before due started %d runs", len(runs))
	}

	// Due: fires, activator is the schedule, budget is the schedule's.
	h.advance(10 * time.Minute)
	h.svc.Tick(ctx)
	runs, _ := h.repo.ListRuns(ctx, 10)
	if len(runs) != 1 {
		t.Fatalf("tick at due started %d runs, want 1", len(runs))
	}
	if runs[0].Activator != "schedule:"+sch.ID || runs[0].Principal != "svc:agent:core" {
		t.Errorf("activator/principal = %q/%q", runs[0].Activator, runs[0].Principal)
	}
	got, _ := h.repo.GetSchedule(ctx, sch.ID)
	if !strings.HasPrefix(got.LastOutcome, "started "+runs[0].ID) || got.LastRunAt == nil {
		t.Errorf("schedule outcome = %q / last_run_at %v", got.LastOutcome, got.LastRunAt)
	}
	if !got.NextRunAt.Equal(h.clock().Add(10 * time.Minute)) {
		t.Errorf("next_run_at not advanced: %v", got.NextRunAt)
	}

	// Due again while the first run is still live: refused, and the refusal
	// is visible on the schedule.
	h.advance(10 * time.Minute)
	h.svc.Tick(ctx)
	runs, _ = h.repo.ListRuns(ctx, 10)
	if len(runs) != 1 {
		t.Fatalf("tick on a live scope started a run: %d", len(runs))
	}
	got, _ = h.repo.GetSchedule(ctx, sch.ID)
	if !strings.HasPrefix(got.LastOutcome, "refused:") {
		t.Errorf("refusal not recorded on the schedule: %q", got.LastOutcome)
	}
	close(release)
	h.waitDone(runs[0].ID)

	if err := h.svc.RemoveSchedule(ctx, sch.ID); err != nil {
		t.Fatalf("RemoveSchedule: %v", err)
	}
	if err := h.svc.RemoveSchedule(ctx, sch.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second remove: %v", err)
	}
	if list, _ := h.svc.ListSchedules(ctx); len(list) != 0 {
		t.Errorf("schedules after remove = %d", len(list))
	}
}

func TestService_StartClosesRunsLeftLiveByAPreviousProcess(t *testing.T) {
	h := newHarness(t, &scriptedLLM{}, defaultBudget)
	ctx := context.Background()
	if _, err := h.repo.CreateRun(ctx, Run{
		ID: "stale", Activator: "user:x", Principal: "svc:agent:core",
		Selection: Selection{Components: []string{"c1"}}, ResolvedComponents: []string{"c1"},
		TokenCeiling: 1, WallClockSeconds: 1, ReportReserveTokens: 1,
	}); err != nil {
		t.Fatalf("seed stale run: %v", err)
	}
	h.start()
	run, _ := h.repo.GetRun(ctx, "stale")
	if run.State != StateFailed || run.StopReason != StopReasonInterrupted || run.EndedAt == nil {
		t.Errorf("stale run after Start = %+v, want failed/interrupted", run)
	}
	// And the scope is free again.
	if _, err := h.svc.Trigger(ctx, "user:a", Selection{Components: []string{"c1"}}); err != nil {
		t.Errorf("trigger after the sweep: %v", err)
	}
}

func TestService_LLMErrorFailsTheRunAndStillWritesLimits(t *testing.T) {
	h := newHarness(t, &scriptedLLM{}, defaultBudget) // no responses: the first call errors
	h.start()
	run, err := h.svc.Trigger(context.Background(), "user:a", Selection{Components: []string{"c1"}})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	done := h.waitDone(run.ID)
	if done.State != StateFailed || done.StopReason != StopReasonError || !strings.Contains(done.Error, "no more responses") {
		t.Errorf("run = %+v, want failed/error", done)
	}
	msgs, _ := h.sessions.ListChatMessages(context.Background(), done.SessionID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "## Limits") || !strings.Contains(msgs[0].Content, "failed:") {
		t.Errorf("a failed run still owes its limits section: %v", msgs)
	}
}

func TestService_StopCancelsLiveRun(t *testing.T) {
	adapter := &scriptedLLM{responses: []*llm.ChatResponse{{Content: "x", Usage: usage(1)}}}
	adapter.onChat = func(ctx context.Context, n int) { <-ctx.Done() }
	h := newHarness(t, adapter, defaultBudget)
	if err := h.svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := h.svc.Trigger(context.Background(), "user:a", Selection{Components: []string{"c1"}})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.svc.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	done, _ := h.repo.GetRun(context.Background(), run.ID)
	if done.State != StateFailed || done.StopReason != agentloop.StopReasonCancelled {
		t.Errorf("run after Stop = %q/%q, want failed/cancelled", done.State, done.StopReason)
	}
}
