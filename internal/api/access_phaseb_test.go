package api_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/rbac"
)

// TestPhaseB_ContextPrincipalReachesAccessorDecision proves the Phase B
// requirement-2 property at the accessor seam directly: the accessor's
// allow/deny decision tracks the principal it is handed, with a non-nil policy
// engine and no HTTP transport involved.
//
//   - "alice" (granted prod-readonly) → the accessor allows and reaches the
//     adapter.
//   - "mallory" (no grant)            → the accessor denies with
//     ErrPermissionDenied and performs no infrastructure call (the
//     security-load-bearing deny assertion, previously the ungranted→403 case).
//
// This was historically a behavioural HTTP test that drove a managed-system
// route to reach the accessor. That route is gone; the decision is exercised
// here directly with constructed principals — the accessor is the RBAC
// chokepoint Phase E moved enforcement onto, so this is the authoritative place
// to assert it. The complementary static guard
// TestPhaseB_AccessorCallersDerivePrincipalFromContext below proves the HTTP
// handlers feed that accessor a principal derived from the request context (the
// transport half of the original property), so no coverage is lost.
func TestPhaseB_ContextPrincipalReachesAccessorDecision(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateComponent(t, sqlStore, "s-allow")

	repo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "s-allow", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign s-allow: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant alice: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-allow", apiFakeK8s{})

	// Non-nil policy engine ⇒ the accessor enforces, exactly as api.New wires it
	// when a service account is configured.
	acc := access.New(registry, nil, rbac.NewPolicyEngine(repo), nil)

	// alice holds the grant → allowed, reaches the adapter.
	if _, err := acc.K8sListResources(ctx, "alice", "s-allow", "pods", ""); err != nil {
		t.Errorf("principal alice (granted) should be allowed by the accessor, got: %v", err)
	}
	// mallory holds no grant → denied with ErrPermissionDenied; no infra call.
	if _, err := acc.K8sListResources(ctx, "mallory", "s-allow", "pods", ""); !errors.Is(err, access.ErrPermissionDenied) {
		t.Errorf("principal mallory (ungranted) should be denied with ErrPermissionDenied, got: %v", err)
	}
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
