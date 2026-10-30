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
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/runfindings"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// findingsLLM answers a pass with a joe-findings block naming a node under a
// scoped component, and records whether the system prompt it was handed
// carried the output contract.
type findingsLLM struct {
	calls         int
	sawContract   bool
	sawQuestions  bool
	findingsBlock string
}

func (a *findingsLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	a.calls++
	system := req.System.String()
	a.sawContract = a.sawContract || strings.Contains(system, runfindings.FindingsFence)
	a.sawQuestions = a.sawQuestions || strings.Contains(system, "THE THREE STANDING QUESTIONS")
	if a.calls == 1 {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{ID: "tc-1", Name: "graph_query", Args: map[string]any{"query": "services"}}},
			Usage:     llm.TokenUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30},
		}, nil
	}
	return &llm.ChatResponse{
		Content: "I took the scope.\n\n" + a.findingsBlock + "\nTURN-KIND: answer",
		Usage:   llm.TokenUsage{InputTokens: 50, OutputTokens: 20, TotalTokens: 70},
	}, nil
}

// setupAutoRunServerWithFindings is setupAutoRunServer with the run-findings
// repository wired, so InstallAutoRun wires the pipeline, and a graph node
// under component c1 for a finding to be about.
func setupAutoRunServerWithFindings(t *testing.T, llmAdapter llm.LLMAdapter) (*Server, *http.ServeMux, *store.Store) {
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
	g := graph.NewSQLiteStore(sqlStore.DB(), nil)
	if err := g.AddNode(ctx, graph.Node{ID: "k8s/default/svc-a", Type: "service", ComponentID: "c1", Metadata: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	services := &core.Services{
		Config:       &config.Config{Server: config.ServerConfig{Address: "localhost:7777"}},
		Graph:        g,
		Store:        sqlStore,
		SessionModel: sessionmodel.NewRepository(sqlStore.DB(), store.DriverSQLite),
		Adapters:     adapters.NewRegistry(),
		LLM:          llmAdapter,
		Audit:        audit.NewRepository(sqlStore.DB(), store.DriverSQLite),
		RunFindings:  runfindings.NewRepository(sqlStore.DB(), store.DriverSQLite),
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
	return srv, mux, sqlStore
}

// TestAutoRun_FindingsSeam_RunInvokesPipelineAndPlacesFindingsAboveLimits is
// the seam the two threads split on, closed: a run's final prose becomes
// records, the records become the findings section above the limits section
// in the one assistant turn, the findings endpoint reads them, and every read
// the pipeline made resolved to svc:agent:core at the accessor.
func TestAutoRun_FindingsSeam_RunInvokesPipelineAndPlacesFindingsAboveLimits(t *testing.T) {
	adapter := &findingsLLM{findingsBlock: "```" + runfindings.FindingsFence + "\n" +
		`{"findings": [{"type": "risk", "subject_node_id": "k8s/default/svc-a", "claim": "svc-a runs a single replica", "consequence": "would_be_unavailable", "condition": "a node drain takes the only pod down", "evidence_line": "replicas: 1"}]}` +
		"\n```"}
	srv, mux, sqlStore := setupAutoRunServerWithFindings(t, adapter)
	if srv.autorun == nil || srv.autorun.findings == nil {
		t.Fatal("InstallAutoRun did not wire the findings pipeline with the repository present")
	}

	w := doRequest(mux, "POST", "/api/v1/auto/runs", map[string]any{"scope": map[string]any{"components": []string{"c1", "c2"}}})
	if w.Code != http.StatusAccepted {
		t.Fatalf("trigger: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var started map[string]any
	_ = json.NewDecoder(w.Body).Decode(&started)
	id, _ := started["id"].(string)
	run := waitAutoRun(t, mux, id)
	if run["state"] != "completed" {
		t.Fatalf("run = %v, want completed", run)
	}
	sessionID, _ := run["session_id"].(string)

	if !adapter.sawContract || !adapter.sawQuestions {
		t.Errorf("the run's system prompt carried contract=%v questions=%v; want both", adapter.sawContract, adapter.sawQuestions)
	}

	// The one assistant turn: findings section, rendered from the record,
	// above the limits section, with the prose apart and labelled.
	wMsgs := doRequest(mux, "GET", "/api/v1/sessions/"+sessionID+"/messages", nil)
	var msgs struct {
		Count    int              `json:"count"`
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(wMsgs.Body).Decode(&msgs)
	if wMsgs.Code != http.StatusOK || msgs.Count != 1 {
		t.Fatalf("messages: %d / %d", wMsgs.Code, msgs.Count)
	}
	content, _ := msgs.Messages[0]["content"].(string)
	fi, li, pi := strings.Index(content, "## Findings"), strings.Index(content, "## Limits"), strings.Index(content, "## Pass notes (unverified)")
	if fi < 0 || li < 0 || pi < 0 || !(fi < pi && pi < li) {
		t.Fatalf("final turn sections out of order (findings %d, pass notes %d, limits %d):\n%s", fi, pi, li, content)
	}
	if !strings.Contains(content, "[new] risk** `k8s/default/svc-a` — svc-a runs a single replica") || strings.Contains(content, "## Pass (unverified)") {
		t.Errorf("final turn:\n%s", content)
	}

	// The record is readable through the findings endpoint, attached to the
	// run, keyed on the resolved set.
	wF := doRequest(mux, "GET", "/api/v1/run-findings?run_id="+id, nil)
	var out runFindingsResponse
	_ = json.NewDecoder(wF.Body).Decode(&out)
	if wF.Code != http.StatusOK || out.Count != 1 || out.Findings[0].Label != runfindings.LabelNew ||
		out.Findings[0].SessionID != sessionID || out.Runs[0].ScopeKey != runfindings.ScopeKey([]string{"c1", "c2"}) {
		t.Errorf("run-findings: %d %s", wF.Code, wF.Body.String())
	}

	// Every accessor decision — the pass's and the pipeline's graph reads —
	// was taken for svc:agent:core and no other principal.
	rows, err := sqlStore.DB().Query(`SELECT DISTINCT principal FROM audit_log WHERE kind = 'infra_access'`)
	if err != nil {
		t.Fatal(err)
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
}

// TestAutoRun_FindingsSeam_NoBlockKeepsTheRunAndRecordsNothing: a completed
// run whose model answered without the block still gets its limits section,
// the prose unverified, and the pipeline's error on the turn; no finding set
// is written, so no previous finding is wrongly resolved.
func TestAutoRun_FindingsSeam_NoBlockKeepsTheRunAndRecordsNothing(t *testing.T) {
	_, mux, sqlStore := setupAutoRunServerWithFindings(t, &findingsLLM{findingsBlock: "Nothing to report."})
	w := doRequest(mux, "POST", "/api/v1/auto/runs", map[string]any{"scope": map[string]any{"components": []string{"c1"}}})
	var started map[string]any
	_ = json.NewDecoder(w.Body).Decode(&started)
	id, _ := started["id"].(string)
	run := waitAutoRun(t, mux, id)
	if run["state"] != "completed" {
		t.Fatalf("run = %v", run)
	}
	sessionID, _ := run["session_id"].(string)
	wMsgs := doRequest(mux, "GET", "/api/v1/sessions/"+sessionID+"/messages", nil)
	var msgs struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(wMsgs.Body).Decode(&msgs)
	content, _ := msgs.Messages[0]["content"].(string)
	if !strings.Contains(content, "## Pass (unverified)") || !strings.Contains(content, "## Limits") || !strings.Contains(content, "emitted no joe-findings block") {
		t.Errorf("final turn:\n%s", content)
	}
	var n int
	_ = sqlStore.DB().QueryRow(`SELECT COUNT(*) FROM run_finding_sets`).Scan(&n)
	if n != 0 {
		t.Errorf("run_finding_sets rows = %d, want 0", n)
	}
}

// TestAutoRun_ReadsCarryFindingCountWhereASetExists pins the one field the
// Auto Joe page needs beyond the record (autojoe-ro-run-visibility): the run
// reads serve finding_count where the run persisted a finding set — the
// count, 0 for a clean run — and omit it where it persisted none, so the page
// links a run to its findings on a served fact and derives nothing.
func TestAutoRun_ReadsCarryFindingCountWhereASetExists(t *testing.T) {
	adapter := &findingsLLM{findingsBlock: "```" + runfindings.FindingsFence + "\n" +
		`{"findings": [{"type": "risk", "subject_node_id": "k8s/default/svc-a", "claim": "svc-a runs a single replica", "consequence": "would_be_unavailable", "condition": "a node drain takes the only pod down", "evidence_line": "replicas: 1"}]}` +
		"\n```"}
	_, mux, _ := setupAutoRunServerWithFindings(t, adapter)

	w := doRequest(mux, "POST", "/api/v1/auto/runs", map[string]any{"scope": map[string]any{"components": []string{"c1"}}})
	if w.Code != http.StatusAccepted {
		t.Fatalf("trigger: expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var started map[string]any
	_ = json.NewDecoder(w.Body).Decode(&started)
	if _, present := started["finding_count"]; present {
		t.Errorf("the trigger's accepted record carries finding_count before any set exists: %v", started)
	}
	withSet, _ := started["id"].(string)
	run := waitAutoRun(t, mux, withSet)
	if run["state"] != "completed" || run["finding_count"] != float64(1) {
		t.Fatalf("run with a set = %v, want completed with finding_count 1", run)
	}

	// A run whose prose carried no block persists no set: no finding_count.
	adapter.findingsBlock = "Nothing to report."
	w = doRequest(mux, "POST", "/api/v1/auto/runs", map[string]any{"scope": map[string]any{"components": []string{"c2"}}})
	_ = json.NewDecoder(w.Body).Decode(&started)
	withoutSet, _ := started["id"].(string)
	run = waitAutoRun(t, mux, withoutSet)
	if _, present := run["finding_count"]; run["state"] != "completed" || present {
		t.Fatalf("run without a set = %v, want completed and no finding_count", run)
	}

	// The list serves the same per run.
	wList := doRequest(mux, "GET", "/api/v1/auto/runs", nil)
	var list struct {
		Runs []map[string]any `json:"runs"`
	}
	_ = json.NewDecoder(wList.Body).Decode(&list)
	if wList.Code != http.StatusOK || len(list.Runs) != 2 {
		t.Fatalf("list: %d %s", wList.Code, wList.Body.String())
	}
	for _, r := range list.Runs {
		n, present := r["finding_count"]
		switch r["id"] {
		case withSet:
			if !present || n != float64(1) {
				t.Errorf("listed run with a set carries finding_count %v (present %v), want 1", n, present)
			}
		case withoutSet:
			if present {
				t.Errorf("listed run without a set carries finding_count %v", n)
			}
		}
	}
}
