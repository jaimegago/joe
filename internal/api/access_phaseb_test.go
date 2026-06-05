package api_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
)

// TestPhaseB_ContextPrincipalReachesAccessorDecision is the Phase B
// requirement-2 proof: the principal the accessor enforces on is the one
// IdentityMiddleware placed in the request context, NOT a constant or a
// server/default principal.
//
// To isolate the accessor as the decision-maker, the outer HTTP
// EnforcementMiddleware is deliberately omitted here — so the ONLY RBAC gate is
// the accessor inside the handler. The accessor's engine is non-nil (API key
// configured), and the request carries an injected, non-default principal in
// its context (the value IdentityMiddleware would set). The decision must then
// track that injected principal's grants:
//   - "alice" (granted prod-readonly) → the accessor allows → 200.
//   - "mallory" (no grant)            → the accessor denies → 403.
//
// This is the behavioural form of the §B "static" acceptance criterion
// (joe-identity-phase-plan.md Phase B): a precise AST data-flow assertion is
// brittle against Phase A's explicit-principal signature, so per the prompt we
// prove the same property behaviourally — a context-injected principal reaches
// the accessor's decision. A complementary lightweight static guard lives in
// TestPhaseB_AccessorCallersDerivePrincipalFromContext below.
func TestPhaseB_ContextPrincipalReachesAccessorDecision(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateSource(t, sqlStore, "s-allow")

	repo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "s-allow", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign s-allow: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant alice: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-allow", apiFakeK8s{})

	// Service account set ⇒ newPolicyEngine builds a non-nil engine ⇒ the
	// accessor enforces. RBAC repo + adapters wired so the handler can resolve
	// and call.
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{ServiceAccounts: []config.ServiceAccount{{Name: "operator", Key: "secret"}}}},
		Store:    sqlStore,
		RBAC:     repo,
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// No EnforcementMiddleware: the accessor is the sole gate. The principal is
	// injected straight into the request context (as IdentityMiddleware would),
	// so the outcome is driven purely by what the handler reads from context and
	// hands to the accessor.
	call := func(principal rbac.Principal) int {
		r := httptest.NewRequest("GET", "/api/v1/k8s/s-allow/resources?resource=pods", nil)
		r = r.WithContext(rbac.WithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}

	if code := call("alice"); code != http.StatusOK {
		t.Errorf("context principal alice (granted) should be allowed by the accessor: got %d, want 200", code)
	}
	if code := call("mallory"); code != http.StatusForbidden {
		t.Errorf("context principal mallory (ungranted) should be denied by the accessor: got %d, want 403", code)
	}

	// Belt-and-suspenders: if the handler ignored context and used a constant,
	// both calls above would return the SAME status. They differ, which proves
	// the decision follows the context-injected principal.
}

// accessorMethodName returns the method name when call is an invocation of a
// method on a `.accessor` receiver (e.g. s.accessor.K8sListResources(...) or
// h.server.accessor.GraphSummary(...)); ok is false otherwise.
func accessorMethodName(call *ast.CallExpr) (name string, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", false
	}
	recv, isSel := sel.X.(*ast.SelectorExpr) // receiver is itself a selector ending in `.accessor`
	if !isSel || recv.Sel.Name != "accessor" {
		return "", false
	}
	return sel.Sel.Name, true
}

// isConstantPrincipal reports whether expr is a hardcoded principal value: a
// string literal, rbac.Unknown, or rbac.Principal("literal"). A principal
// sourced from context (the identifier assigned from PrincipalFromContext, or
// an inline rbac.PrincipalFromContext(...) call) is none of these.
func isConstantPrincipal(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		return ok && pkg.Name == "rbac" && e.Sel.Name == "Unknown"
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "rbac" || sel.Sel.Name != "Principal" || len(e.Args) != 1 {
			return false
		}
		lit, ok := e.Args[0].(*ast.BasicLit)
		return ok && lit.Kind == token.STRING
	}
	return false
}

// TestPhaseB_AccessorCallersDerivePrincipalFromContext is the lightweight
// static companion to the behavioural proof above. It asserts that every
// principal-gated accessor call site in internal/api obtains its principal from
// the request context, not a constant. Concretely, for each non-test function
// that invokes a principal-gated accessor method:
//   - the function must reference rbac.PrincipalFromContext (positive: the
//     principal is context-derived), and
//   - the principal argument (after ctx) must not be a string literal,
//     rbac.Unknown, or rbac.Principal("literal") (negative: not hardcoded).
//
// Principal-less accessor methods (documented in DECISIONS.md D-0004 — webhook
// secret resolvers run pre-auth via HMAC; GraphAvailable is a pure helper) take
// no principal and are exempt. A new principal-less method is added to the
// exempt set here, mirroring the allowlist convention in access_guard_test.go.
func TestPhaseB_AccessorCallersDerivePrincipalFromContext(t *testing.T) {
	principalLess := map[string]bool{
		"GitHubWebhookSecret": true,
		"GitLabWebhookSecret": true,
		"GraphAvailable":      true,
	}

	repoRoot := findRepoRoot(t)
	apiDir := filepath.Join(repoRoot, "internal", "api")
	entries, err := os.ReadDir(apiDir)
	if err != nil {
		t.Fatalf("read internal/api dir: %v", err)
	}

	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(apiDir, name), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			// Positive signal: does this function read the principal from context?
			refsCtxPrincipal := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if ok && pkg.Name == "rbac" && sel.Sel.Name == "PrincipalFromContext" {
					refsCtxPrincipal = true
					return false
				}
				return true
			})

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				method, isAccessor := accessorMethodName(call)
				if !isAccessor || principalLess[method] {
					return true
				}
				checked++
				if !refsCtxPrincipal {
					t.Errorf("%s: %s() calls accessor.%s (principal-gated) but never reads "+
						"rbac.PrincipalFromContext — the caller principal must derive from the "+
						"request context (IdentityMiddleware), not a constant (Phase B req 2, "+
						"design §2.5).", name, fn.Name.Name, method)
				}
				if len(call.Args) >= 2 && isConstantPrincipal(call.Args[1]) {
					t.Errorf("%s: %s() passes a hardcoded principal to accessor.%s — the principal "+
						"must derive from the request context, not a constant/default.",
						name, fn.Name.Name, method)
				}
				return true
			})
		}
	}

	if checked == 0 {
		t.Fatal("found no principal-gated accessor call sites in internal/api — the guard is not exercising anything")
	}
}
