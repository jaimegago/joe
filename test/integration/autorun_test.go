//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/autorun"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/promotereads"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// autoRunPassLLM drives one pass: a k8s_get on the scoped component on its
// first call, then the report on every later call (the loop's unfulfilled-
// intent probe is one of those, and must see no tool call). Reset between
// passes.
type autoRunPassLLM struct{ calls int }

func (a *autoRunPassLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	a.calls++
	if a.calls == 1 {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{ID: "tc-1", Name: "k8s_get", Args: map[string]any{"component_id": "local-k8s", "resource": "pods"}}},
			Usage:     llm.TokenUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30},
		}, nil
	}
	return &llm.ChatResponse{
		Content: "Pass complete.\nTURN-KIND: answer",
		Usage:   llm.TokenUsage{InputTokens: 50, OutputTokens: 20, TotalTokens: 70},
	}, nil
}

// TestIntegration_AutoRun_ReadsAreAdmittedByTheRefreshersAuthority is the
// acceptance test for the run side's principal contract (joe-pm
// docs/backlog/autojoe-ro.md § Principal): a run's reads resolve to
// svc:agent:core at the accessor and are admitted by exactly the authority the
// refresher has — grants plus the per-type auto_promote_reads admit — and the
// activator's own authority never carries into the run.
//
// Setup: one kubernetes component in zone "prod"; the activator holds a
// policy on that zone; svc:agent:core holds none. Under the run path's
// promote-aware engine (the refresher's), with auto_promote_reads OFF for
// kubernetes, the run's k8s_get is DENIED — the activator's grant does not
// reach it, so the component is recorded as not covered and the audit log
// carries a deny for svc:agent:core. With the promotion switched ON (an
// operator act on the admin surface), the same pass is admitted and the
// component is covered.
func TestIntegration_AutoRun_ReadsAreAdmittedByTheRefreshersAuthority(t *testing.T) {
	testStore, rbacRepo := newRBACStore(t)
	ctx := context.Background()
	const activator = "user:admin@example.com"

	if err := testStore.Components.Create(ctx, &store.Component{
		ID: "local-k8s", Type: store.ComponentTypeKubernetes, Name: "Local Kind", Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := rbacRepo.CreateZone(ctx, rbac.Zone{
		ID: "prod", Name: "prod", AllowedActions: []rbac.Action{rbac.ActionRead},
	}, "test"); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if err := rbacRepo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "local-k8s", ZoneID: "prod", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}
	// The activator may read prod. The run must not inherit that.
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{Principal: activator, ZoneID: "prod"}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	auditRepo := audit.NewRepository(testStore.DB(), testStore.Driver())
	promoteRepo := promotereads.NewRepository(testStore.DB(), testStore.Driver())
	llmAdapter := &autoRunPassLLM{}
	services := &core.Services{
		Config:       &config.Config{},
		Graph:        graph.NewSQLiteStore(testStore.DB(), nil),
		Store:        testStore,
		SessionModel: sessionmodel.NewRepository(testStore.DB(), testStore.Driver()),
		Adapters:     registryWithK8s("local-k8s"),
		LLM:          llmAdapter,
		Audit:        auditRepo,
		RBAC:         rbacRepo,
	}
	server := api.New(services, nil)
	// The run path's engine is the refresher's: promote-aware, no read posture
	// (cmd/joe/server.go builds exactly this for InstallAutoRun).
	svc := server.InstallAutoRun(rbac.NewPolicyEngineWithPromote(rbacRepo, promoteRepo), autorun.Budget{
		TokenCeiling: 10_000, WallClock: 10 * time.Second, ReportReserveTokens: 500, ReportReserve: 5 * time.Second,
	})
	if svc == nil {
		t.Fatal("InstallAutoRun returned nil")
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(stopCtx)
	})

	runPass := func() *autorun.Run {
		t.Helper()
		run, err := svc.Trigger(ctx, activator, autorun.Selection{Components: []string{"local-k8s"}})
		if err != nil {
			t.Fatalf("Trigger: %v", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			got, err := svc.GetRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if !got.Live() {
				return got
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("run still live")
		return nil
	}
	decisionsFor := func(component string) []string {
		t.Helper()
		rows, err := testStore.DB().Query(
			`SELECT decision || ':' || COALESCE(principal, '') FROM audit_log WHERE kind = 'infra_access' AND component_id = ? ORDER BY id`, component)
		if err != nil {
			t.Fatalf("audit query: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			_ = rows.Scan(&s)
			out = append(out, s)
		}
		return out
	}

	// 1. Promotion OFF: the run is denied the component the activator may read.
	denied := runPass()
	if denied.State != autorun.StateCompleted {
		t.Fatalf("run 1 state = %q (%s)", denied.State, denied.Error)
	}
	if denied.Principal != "svc:agent:core" || denied.Activator != activator {
		t.Fatalf("run 1 principal/activator = %q/%q", denied.Principal, denied.Activator)
	}
	if len(denied.Covered) != 0 || strings.Join(denied.NotCovered, ",") != "local-k8s" {
		t.Errorf("run 1 coverage = %v / %v; the activator's grant must not carry into the run", denied.Covered, denied.NotCovered)
	}
	if d := decisionsFor("local-k8s"); len(d) != 1 || d[0] != "deny:svc:agent:core" {
		t.Errorf("run 1 accessor decisions = %v, want exactly one deny for svc:agent:core", d)
	}
	sess, err := services.SessionModel.GetSession(ctx, denied.SessionID)
	if err != nil || sess == nil || sess.CreatorPrincipal != "svc:agent:core" {
		t.Errorf("run 1 session creator = %v (%v), want svc:agent:core", sess, err)
	}

	// 2. Promotion ON for kubernetes: the same pass is admitted, by the same
	// admit the refresher reads under.
	if err := promotereads.NewMutationService(promoteRepo, auditRepo).SetPromoted(
		rbac.WithPrincipal(ctx, rbac.Principal(activator)), store.ComponentTypeKubernetes, true); err != nil {
		t.Fatalf("SetPromoted: %v", err)
	}
	llmAdapter.calls = 0
	admitted := runPass()
	if admitted.State != autorun.StateCompleted {
		t.Fatalf("run 2 state = %q (%s)", admitted.State, admitted.Error)
	}
	if strings.Join(admitted.Covered, ",") != "local-k8s" || len(admitted.NotCovered) != 0 {
		t.Errorf("run 2 coverage = %v / %v, want local-k8s covered", admitted.Covered, admitted.NotCovered)
	}
	if d := decisionsFor("local-k8s"); len(d) != 2 || d[1] != "allow:svc:agent:core" {
		t.Errorf("run 2 accessor decisions = %v, want the second an allow for svc:agent:core", d)
	}

	// The two records compare on their resolved sets, and both carry the
	// limits section in the session they wrote.
	msgs, err := services.SessionModel.ListChatMessages(ctx, admitted.SessionID)
	if err != nil || len(msgs) != 1 || !strings.Contains(msgs[0].Content, "## Limits") || !strings.Contains(msgs[0].Content, "Covered (1): local-k8s") {
		t.Errorf("run 2 final turn = %v (%v)", msgs, err)
	}
}
