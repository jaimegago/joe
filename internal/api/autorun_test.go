package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/autorun"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// autoRunLLM answers a pass: one graph_query call, then a final answer. The
// run's tool calls reach the accessor under the principal on the loop's
// context, which the audit log records per decision.
type autoRunLLM struct{ calls int }

func (a *autoRunLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	a.calls++
	if a.calls == 1 {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{ID: "tc-1", Name: "graph_query", Args: map[string]any{"query": "services"}}},
			Usage:     llm.TokenUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30},
		}, nil
	}
	return &llm.ChatResponse{
		Content: "Nothing in the graph for the scope.\nTURN-KIND: answer",
		Usage:   llm.TokenUsage{InputTokens: 50, OutputTokens: 20, TotalTokens: 70},
	}, nil
}

var autoRunTestBudget = autorun.Budget{TokenCeiling: 10_000, WallClock: 10 * time.Second, ReportReserveTokens: 500, ReportReserve: 5 * time.Second}

// setupAutoRunServer is setupTaskServer plus InstallAutoRun with a nil run
// engine (permit-all, RBAC disabled) and a started service.
func setupAutoRunServer(t *testing.T, llmAdapter llm.LLMAdapter) (*Server, *http.ServeMux, *autorun.Service, *store.Store) {
	t.Helper()
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	for _, id := range []string{"c1", "c2"} {
		if err := sqlStore.Components.Create(ctx, &store.Component{ID: id, Type: "kubernetes", Name: id, Config: []byte(`{}`)}); err != nil {
			t.Fatalf("create component: %v", err)
		}
	}
	services := &core.Services{
		Config:       &config.Config{Server: config.ServerConfig{Address: "localhost:7777"}},
		Graph:        graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:        sqlStore,
		SessionModel: sessionmodel.NewRepository(sqlStore.DB(), store.DriverSQLite),
		Adapters:     adapters.NewRegistry(),
		LLM:          llmAdapter,
		Audit:        audit.NewRepository(sqlStore.DB(), store.DriverSQLite),
	}
	srv := New(services, nil)
	svc := srv.InstallAutoRun(nil, autoRunTestBudget)
	if svc == nil {
		t.Fatal("InstallAutoRun returned nil with the session store wired")
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start auto run: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(stopCtx)
	})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux, svc, sqlStore
}

// waitAutoRun waits for a run to be delivered: its record out of running AND,
// when it has a session, its final turn written. The service writes the record
// before the findings pipeline and the turn (autorun.Service.execute), so a
// record that reads completed is not yet a delivered run; a caller that read
// the session on the record alone raced the turn and, under -cover's slower
// scheduling, lost (Post-Merge run 40721760377, "messages: 200 / 0").
func waitAutoRun(t *testing.T, mux *http.ServeMux, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := doRequest(mux, "GET", "/api/v1/auto/runs/"+id, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("get run: %d %s", w.Code, w.Body.String())
		}
		var run map[string]any
		_ = json.NewDecoder(w.Body).Decode(&run)
		if run["state"] != "running" {
			sessionID, _ := run["session_id"].(string)
			if sessionID == "" || autoRunTurnWritten(mux, sessionID) {
				return run
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s not delivered after 5s", id)
	return nil
}

func autoRunTurnWritten(mux *http.ServeMux, sessionID string) bool {
	w := doRequest(mux, "GET", "/api/v1/sessions/"+sessionID+"/messages", nil)
	var msgs struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(w.Body).Decode(&msgs)
	return w.Code == http.StatusOK && msgs.Count > 0
}

func TestAutoRun_RoutesAbsentUntilInstalled(t *testing.T) {
	_, mux := setupTaskServer(t, &taskStubLLM{response: "x"})
	w := doRequest(mux, "POST", "/api/v1/auto/runs", map[string]any{"scope": map[string]any{"components": []string{"c1"}}})
	if w.Code != http.StatusNotFound {
		t.Errorf("without InstallAutoRun the route must not exist; got %d", w.Code)
	}
}

func TestAutoRun_TriggerRunsAsAgentCoreAndWritesSession(t *testing.T) {
	_, mux, _, sqlStore := setupAutoRunServer(t, &autoRunLLM{})

	w := doRequest(mux, "POST", "/api/v1/auto/runs", map[string]any{"scope": map[string]any{"components": []string{"c1", "c2"}}})
	if w.Code != http.StatusAccepted {
		t.Fatalf("trigger: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var started map[string]any
	_ = json.NewDecoder(w.Body).Decode(&started)
	id, _ := started["id"].(string)
	if id == "" || started["state"] != "running" || started["principal"] != "svc:agent:core" {
		t.Fatalf("trigger response = %v", started)
	}
	if started["activator"] == "svc:agent:core" || started["activator"] == "" {
		t.Errorf("activator = %v, want the caller principal, never the run principal", started["activator"])
	}

	run := waitAutoRun(t, mux, id)
	if run["state"] != "completed" || run["stop_reason"] != nil {
		t.Fatalf("run = %v, want completed", run)
	}
	sessionID, _ := run["session_id"].(string)
	if sessionID == "" {
		t.Fatal("run has no session")
	}

	// The session is readable where every session is, carries one assistant
	// turn with the limits section, and was created by svc:agent:core.
	wMsgs := doRequest(mux, "GET", "/api/v1/sessions/"+sessionID+"/messages", nil)
	if wMsgs.Code != http.StatusOK {
		t.Fatalf("session messages: %d", wMsgs.Code)
	}
	var msgs struct {
		Count    int              `json:"count"`
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(wMsgs.Body).Decode(&msgs)
	if msgs.Count != 1 || msgs.Messages[0]["role"] != "assistant" {
		t.Fatalf("messages = %+v, want one assistant turn and no human turn", msgs)
	}
	content, _ := msgs.Messages[0]["content"].(string)
	if !strings.Contains(content, "## Limits") || !strings.Contains(content, "executed as svc:agent:core") {
		t.Errorf("final turn:\n%s", content)
	}
	var creator string
	if err := sqlStore.DB().QueryRow(`SELECT creator_principal FROM agent_sessions WHERE id = ?`, sessionID).Scan(&creator); err != nil || creator != "svc:agent:core" {
		t.Errorf("session creator = %q (%v), want svc:agent:core", creator, err)
	}

	// The run's reads resolved to svc:agent:core at the accessor: the audit log
	// records the principal per decision, and the graph_query the pass made
	// is there under that principal and under no other.
	rows, err := sqlStore.DB().Query(`SELECT DISTINCT principal FROM audit_log WHERE kind = 'infra_access'`)
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	defer rows.Close()
	var principals []string
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		principals = append(principals, p)
	}
	if len(principals) != 1 || principals[0] != "svc:agent:core" {
		t.Errorf("accessor decisions recorded under %v, want exactly svc:agent:core", principals)
	}

	// A run creates no agent_runs row, and /tasks creates no auto_runs row.
	var n int
	_ = sqlStore.DB().QueryRow(`SELECT COUNT(*) FROM agent_runs`).Scan(&n)
	if n != 0 {
		t.Errorf("agent_runs rows after a run = %d, want 0", n)
	}
	before := 0
	_ = sqlStore.DB().QueryRow(`SELECT COUNT(*) FROM auto_runs`).Scan(&before)
	if wt := doRequest(mux, "POST", "/api/v1/tasks", map[string]any{"message": "hello"}); wt.Code != http.StatusOK {
		t.Fatalf("tasks: %d", wt.Code)
	}
	after := 0
	_ = sqlStore.DB().QueryRow(`SELECT COUNT(*) FROM auto_runs`).Scan(&after)
	if after != before {
		t.Errorf("a /tasks call created an auto_runs row (%d → %d)", before, after)
	}

	// List and get read the record.
	wList := doRequest(mux, "GET", "/api/v1/auto/runs", nil)
	var list struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(wList.Body).Decode(&list)
	if wList.Code != http.StatusOK || list.Count != 1 {
		t.Errorf("list = %d / count %d", wList.Code, list.Count)
	}
	if w404 := doRequest(mux, "GET", "/api/v1/auto/runs/nope", nil); w404.Code != http.StatusNotFound {
		t.Errorf("unknown run: %d", w404.Code)
	}
}

func TestAutoRun_RefusalsAreVisibleToTheActivator(t *testing.T) {
	_, mux, _, _ := setupAutoRunServer(t, &autoRunLLM{})

	for _, tc := range []struct {
		name  string
		scope map[string]any
	}{
		{"empty", map[string]any{}},
		{"unknown component", map[string]any{"components": []string{"ghost"}}},
		{"zones without rbac", map[string]any{"zones": []string{"prod"}}},
	} {
		w := doRequest(mux, "POST", "/api/v1/auto/runs", map[string]any{"scope": tc.scope})
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", tc.name, w.Code, w.Body.String())
		}
	}
	if w := doRequest(mux, "POST", "/api/v1/auto/runs", "not json"); w.Code != http.StatusBadRequest {
		t.Errorf("invalid body: %d", w.Code)
	}
}

// blockingLLM holds the first call until released so a run stays live.
type blockingLLM struct {
	release chan struct{}
	calls   int
}

func (b *blockingLLM) Chat(ctx context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	b.calls++
	if b.calls == 1 {
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &llm.ChatResponse{Content: "done", Usage: llm.TokenUsage{TotalTokens: 1}}, nil
}

func TestAutoRun_SecondTriggerOnLiveScopeIs409(t *testing.T) {
	adapter := &blockingLLM{release: make(chan struct{})}
	_, mux, _, _ := setupAutoRunServer(t, adapter)
	scope := map[string]any{"scope": map[string]any{"components": []string{"c1"}}}

	w := doRequest(mux, "POST", "/api/v1/auto/runs", scope)
	if w.Code != http.StatusAccepted {
		t.Fatalf("first trigger: %d %s", w.Code, w.Body.String())
	}
	var first map[string]any
	_ = json.NewDecoder(w.Body).Decode(&first)

	w2 := doRequest(mux, "POST", "/api/v1/auto/runs", scope)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second trigger on a live scope: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "refused, not queued") {
		t.Errorf("409 body does not say the trigger was refused: %s", w2.Body.String())
	}
	close(adapter.release)
	waitAutoRun(t, mux, first["id"].(string))
}

func TestAutoRun_SchedulesInstallListRemove(t *testing.T) {
	_, mux, _, _ := setupAutoRunServer(t, &autoRunLLM{})

	w := doRequest(mux, "POST", "/api/v1/auto/schedules", map[string]any{
		"scope": map[string]any{"components": []string{"c1"}}, "interval_minutes": 0,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("zero interval: %d", w.Code)
	}
	w = doRequest(mux, "POST", "/api/v1/auto/schedules", map[string]any{
		"scope": map[string]any{"components": []string{"c1"}}, "interval_minutes": 15,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", w.Code, w.Body.String())
	}
	var sch map[string]any
	_ = json.NewDecoder(w.Body).Decode(&sch)
	if sch["interval_seconds"] != float64(900) || sch["token_ceiling"] != float64(autoRunTestBudget.TokenCeiling) {
		t.Errorf("schedule = %v", sch)
	}
	wList := doRequest(mux, "GET", "/api/v1/auto/schedules", nil)
	if wList.Code != http.StatusOK || !strings.Contains(wList.Body.String(), `"count":1`) {
		t.Errorf("list: %d %s", wList.Code, wList.Body.String())
	}
	if wDel := doRequest(mux, "DELETE", "/api/v1/auto/schedules/"+sch["id"].(string), nil); wDel.Code != http.StatusNoContent {
		t.Errorf("delete: %d", wDel.Code)
	}
	if wDel := doRequest(mux, "DELETE", "/api/v1/auto/schedules/"+sch["id"].(string), nil); wDel.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", wDel.Code)
	}
}

// TestAutoRun_WritesAreAdminGated: with RBAC enforced, a non-admin caller is
// refused on the trigger and the schedule install; reads stay open to any
// authenticated principal, as a run's session is.
func TestAutoRun_WritesAreAdminGated(t *testing.T) {
	srv, mux, _, sqlStore := setupAutoRunServer(t, &autoRunLLM{})
	srv.services.RBAC = rbac.NewRepository(sqlStore.DB(), store.DriverSQLite)
	srv.services.RBACEnabled = true

	scope := map[string]any{"scope": map[string]any{"components": []string{"c1"}}}
	if w := doRequest(mux, "POST", "/api/v1/auto/runs", scope); w.Code != http.StatusForbidden {
		t.Errorf("non-admin trigger: expected 403, got %d", w.Code)
	}
	if w := doRequest(mux, "POST", "/api/v1/auto/schedules", map[string]any{"scope": scope["scope"], "interval_minutes": 5}); w.Code != http.StatusForbidden {
		t.Errorf("non-admin schedule install: expected 403, got %d", w.Code)
	}
	if w := doRequest(mux, "DELETE", "/api/v1/auto/schedules/x", nil); w.Code != http.StatusForbidden {
		t.Errorf("non-admin schedule remove: expected 403, got %d", w.Code)
	}
	if w := doRequest(mux, "GET", "/api/v1/auto/runs", nil); w.Code != http.StatusOK {
		t.Errorf("read of runs: expected 200, got %d", w.Code)
	}
	var n int
	_ = sqlStore.DB().QueryRow(`SELECT COUNT(*) FROM auto_runs`).Scan(&n)
	if n != 0 {
		t.Errorf("a refused trigger recorded %d runs", n)
	}
}
