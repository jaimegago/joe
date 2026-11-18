package access_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// actAccessor builds an accessor over one kubernetes component alice may read,
// recording every audit row.
func actAccessor(t *testing.T) (*access.Accessor, *recordingAudit, *bool) {
	t.Helper()
	repo := newFakeRepo()
	repo.assign("s-k8s", "z-read")
	repo.grant("alice", "z-read")
	called := false
	reg := adapters.NewRegistry()
	reg.Register("s-k8s", fakeK8s{called: &called})
	rec := &recordingAudit{}
	return access.New(reg, nil, rbac.NewPolicyEngine(repo), rec), rec, &called
}

func loopCtx(call audit.Call) context.Context {
	ctx := audit.WithScope(context.Background(), audit.Scope{
		SessionID: "sess-1",
		TaskID:    "task-1",
		Trigger:   audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/task-1"},
	})
	return audit.WithCall(ctx, call)
}

func decodeAct(t *testing.T, e audit.Event) audit.Act {
	t.Helper()
	var act audit.Act
	if err := json.Unmarshal([]byte(e.Context), &act); err != nil {
		t.Fatalf("row context %q is not an act: %v", e.Context, err)
	}
	return act
}

// TestAct_KubernetesDispatchRecordsWhatAnAuditEventRecords: each Kubernetes
// dispatch leaves one row whose operation carries the verb, the API group and
// plural resource the API server sees, the subresource, the object name and
// the namespace — and, inside a loop, the session, the model's reason and the
// trigger.
func TestAct_KubernetesDispatchRecordsWhatAnAuditEventRecords(t *testing.T) {
	a, rec, _ := actAccessor(t)
	ctx := loopCtx(audit.Call{Tool: "k8s_get", CallID: "call-7", Reason: "The operator asked about api."})

	if _, err := a.K8sGetResource(ctx, "alice", "s-k8s", "Deployments", "default", "api"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := a.K8sListResources(ctx, "alice", "s-k8s", "pods", ""); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := a.K8sGetPodLogs(ctx, "alice", "s-k8s", "default", "api-0", "app", 50); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if _, err := a.K8sListResources(ctx, "alice", "s-k8s", "cert-manager.io/v1/certificates", "prod"); err != nil {
		t.Fatalf("crd list: %v", err)
	}

	want := []audit.Operation{
		{Verb: "get", APIGroup: "apps", Resource: "deployments", Name: "api", Namespace: "default"},
		{Verb: "list", Resource: "pods"},
		{Verb: "get", Resource: "pods", Subresource: "log", Name: "api-0", Namespace: "default"},
		{Verb: "list", APIGroup: "cert-manager.io", Resource: "certificates", Namespace: "prod"},
	}
	rows := rec.snapshot()
	if len(rows) != len(want) {
		t.Fatalf("%d audit rows for %d acts; every act leaves exactly one: %#v", len(rows), len(want), rows)
	}
	for i, row := range rows {
		act := decodeAct(t, row)
		if act.Operation != want[i] {
			t.Errorf("act %d operation = %+v, want %+v", i, act.Operation, want[i])
		}
		if row.SessionID != "sess-1" || act.SessionID != "sess-1" || act.TaskID != "task-1" {
			t.Errorf("act %d session = column %q / context %q, task %q; want sess-1, task-1", i, row.SessionID, act.SessionID, act.TaskID)
		}
		if act.Reasoning != "The operator asked about api." {
			t.Errorf("act %d reasoning = %q", i, act.Reasoning)
		}
		if act.Tool != "k8s_get" || act.ToolCallID != "call-7" {
			t.Errorf("act %d tool = %q, call = %q", i, act.Tool, act.ToolCallID)
		}
		if act.Trigger == nil || act.Trigger.Type != audit.TriggerOperatorRequest || act.Trigger.Ref != "task/task-1" {
			t.Errorf("act %d trigger = %+v", i, act.Trigger)
		}
		if row.Kind != audit.KindInfraAccess || row.Decision != audit.DecisionAllow || row.ComponentID != "s-k8s" {
			t.Errorf("act %d row = %+v", i, row)
		}
	}
}

// TestAct_CallWithNoReasonIsMadeAndRecordsAnEmptyReason — D-0170. The model
// gave no reason: the call reaches the adapter, and the entry says so with an
// empty reasoning. Nothing is written in the model's place.
func TestAct_CallWithNoReasonIsMadeAndRecordsAnEmptyReason(t *testing.T) {
	a, rec, called := actAccessor(t)
	ctx := loopCtx(audit.Call{Tool: "k8s_get", CallID: "call-1"})

	if _, err := a.K8sListResources(ctx, "alice", "s-k8s", "pods", "default"); err != nil {
		t.Fatalf("a call that stated no reason was refused: %v", err)
	}
	if !*called {
		t.Fatal("the adapter was not reached; a call with no reason must still be made")
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	if act := decodeAct(t, rows[0]); act.Reasoning != "" {
		t.Errorf("reasoning = %q; want empty when the model gave none", act.Reasoning)
	}
	if !strings.Contains(rows[0].Context, `"reasoning":""`) {
		t.Errorf("context %s does not record the empty reasoning explicitly", rows[0].Context)
	}
}

// TestAct_SessionComesFromTheLoopScopeNotTheRequestHeader: the session id
// agentctx carries is also set from a caller-supplied header on every route.
// A row is filed under a session only when the code that started a loop
// stamped the audit scope; a request that merely names a session adds nothing
// to that session's trail.
func TestAct_SessionComesFromTheLoopScopeNotTheRequestHeader(t *testing.T) {
	a, rec, _ := actAccessor(t)
	ctx := agentctx.WithSessionID(context.Background(), "someone-elses-session")

	if _, err := a.K8sListResources(ctx, "alice", "s-k8s", "pods", "default"); err != nil {
		t.Fatalf("list: %v", err)
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	if rows[0].SessionID != "" {
		t.Errorf("row filed under session %q from the request context; only the loop scope names a session", rows[0].SessionID)
	}
	act := decodeAct(t, rows[0])
	if act.SessionID != "" || act.Trigger != nil {
		t.Errorf("act = %+v; want no session and no trigger outside a loop", act)
	}
	// The operation is still recorded: a direct read says what it read.
	if want := (audit.Operation{Verb: "list", Resource: "pods", Namespace: "default"}); act.Operation != want {
		t.Errorf("operation = %+v, want %+v", act.Operation, want)
	}
}

// TestAct_RefusedActRecordsWhatWasAttempted: a denied act leaves its one row
// too, with the operation that was refused, and reaches no adapter.
func TestAct_RefusedActRecordsWhatWasAttempted(t *testing.T) {
	a, rec, called := actAccessor(t)
	ctx := loopCtx(audit.Call{Tool: "k8s_get", Reason: "look at the secret"})

	_, err := a.K8sGetResource(ctx, "mallory", "s-k8s", "secrets", "default", "db")
	if !errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
	if *called {
		t.Fatal("the adapter was reached on a denied act")
	}
	rows := rec.snapshot()
	if len(rows) != 1 || rows[0].Decision != audit.DecisionDeny || rows[0].SessionID != "sess-1" {
		t.Fatalf("rows = %#v; want one deny row in the session", rows)
	}
	if want := (audit.Operation{Verb: "get", Resource: "secrets", Name: "db", Namespace: "default"}); decodeAct(t, rows[0]).Operation != want {
		t.Errorf("operation = %+v, want %+v", decodeAct(t, rows[0]).Operation, want)
	}
}

// TestAct_GraphDecisionIsNotAnAct: a read of Joe's own graph reaches no
// managed system. Its row is the bare decision — no act, no session — even
// inside a loop, so it never appears in a session's trail.
func TestAct_GraphDecisionIsNotAnAct(t *testing.T) {
	repo := newFakeRepo()
	repo.grant("alice", "unassigned")
	rec := &recordingAudit{}
	graphCalled := false
	a := access.New(adapters.NewRegistry(), fakeGraph{called: &graphCalled}, rbac.NewPolicyEngine(repo), rec)

	if _, err := a.GraphSummary(loopCtx(audit.Call{Tool: "graph_query", Reason: "orient"}), "alice"); err != nil {
		t.Fatalf("graph summary: %v", err)
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	if rows[0].SessionID != "" || rows[0].Context != "" {
		t.Errorf("graph row = session %q, context %q; want neither", rows[0].SessionID, rows[0].Context)
	}
	if rows[0].ComponentID != access.GraphComponentID {
		t.Errorf("component = %q, want %q", rows[0].ComponentID, access.GraphComponentID)
	}
}

// TestAct_EmptyKubernetesResourceIsRefusedBeforeAnyDecision: a list or get
// that names no resource type is an invalid request, not an act. It writes no
// row and reaches no adapter.
func TestAct_EmptyKubernetesResourceIsRefusedBeforeAnyDecision(t *testing.T) {
	a, rec, called := actAccessor(t)
	if _, err := a.K8sListResources(context.Background(), "alice", "s-k8s", "", "default"); err == nil {
		t.Error("list with no resource type returned no error")
	}
	if _, err := a.K8sGetResource(context.Background(), "alice", "s-k8s", "", "default", "x"); err == nil {
		t.Error("get with no resource type returned no error")
	}
	if *called || len(rec.snapshot()) != 0 {
		t.Errorf("adapter called = %v, rows = %d; want neither", *called, len(rec.snapshot()))
	}
}

// TestGuard_EveryDispatchDeclaresItsOperation walks the accessor's source and
// requires every call that gates an adapter — guard[...] and observeResolve —
// to declare a literal operation with a non-empty verb and resource (or the
// Kubernetes helper with a literal verb). The compiler already requires the
// argument; this is what stops it being passed empty, which the accessor
// would refuse at run time on the first call instead of here.
//
// It fails loudly rather than vacuously if it finds no call sites.
func TestGuard_EveryDispatchDeclaresItsOperation(t *testing.T) {
	fset := token.NewFileSet()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	pkgs, err := parser.ParseDir(fset, wd, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse access package: %v", err)
	}

	sites := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var opArg ast.Expr
				switch fun := call.Fun.(type) {
				case *ast.IndexExpr: // guard[T](a, ctx, principal, sourceID, action, op, typeName)
					if id, ok := fun.X.(*ast.Ident); ok && id.Name == "guard" && len(call.Args) == 7 {
						opArg = call.Args[5]
					}
				case *ast.SelectorExpr: // a.observeResolve(ctx, principal, sourceID, action, op)
					if fun.Sel.Name == "observeResolve" && len(call.Args) == 5 {
						opArg = call.Args[4]
					}
				}
				if opArg == nil {
					return true
				}
				sites++
				pos := filepath.Base(name) + ":" + strconv.Itoa(fset.Position(call.Pos()).Line)
				if problem := undeclaredOperation(opArg); problem != "" {
					t.Errorf("%s: %s", pos, problem)
				}
				return true
			})
		}
	}
	if sites == 0 {
		t.Fatal("found no guard or observeResolve call sites; the guard is checking nothing")
	}
}

// undeclaredOperation returns "" when arg is a literal operation that names a
// verb and a resource, and otherwise what is wrong with it.
func undeclaredOperation(arg ast.Expr) string {
	switch v := arg.(type) {
	case *ast.CompositeLit:
		fields := map[string]ast.Expr{}
		for _, elt := range v.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok {
					fields[key.Name] = kv.Value
				}
			}
		}
		for _, required := range []string{"Verb", "Resource"} {
			lit, ok := fields[required].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return "operation does not declare " + required + " as a string literal"
			}
			if s, err := strconv.Unquote(lit.Value); err != nil || s == "" {
				return "operation declares an empty " + required
			}
		}
		return ""
	case *ast.CallExpr:
		if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "k8sOperation" && len(v.Args) == 5 {
			lit, ok := v.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return "k8sOperation verb is not a string literal"
			}
			if s, err := strconv.Unquote(lit.Value); err != nil || s == "" {
				return "k8sOperation declares an empty verb"
			}
			return ""
		}
		return "operation is built by a call other than k8sOperation"
	}
	return "operation is not a literal audit.Operation"
}
