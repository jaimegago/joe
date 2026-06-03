package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

// TestAdminRoutes_AllRequireAdminGate is the structural guard for the
// security fix in ADMIN_SURFACE_AUDIT.md Launch Blocker 1 (privilege
// escalation): EVERY handler registered under the /api/v1/admin/ prefix
// must admin-gate via server.requireAdmin.
//
// Style mirrors the identity refactor's single-implementation guards —
// captaingate.TestPhaseG_SingleSharedCaptainGateImplementation and
// sessiongate's import_guard_test — which parse the AST and assert a
// structural property no human review can be trusted to re-check on every
// change. Here the property is: the set of adminHandler methods wired into
// the mux by registerAdminRoutes is a SUBSET of the set of adminHandler
// methods that call requireAdmin in their body.
//
// Why a structural test and not just the regression tests: the regression
// tests (admin_gate_test.go) pin the behaviour of the endpoints that exist
// TODAY. This test pins the invariant for endpoints that DON'T EXIST YET —
// someone adding `POST /api/v1/admin/admins` next quarter and forgetting
// the gate gets a failing build naming the ungated handler, not a silent
// re-opened privilege escalation. The gate is the single mechanism
// (admingate.go); this test guarantees it is applied uniformly.
func TestAdminRoutes_AllRequireAdminGate(t *testing.T) {
	fset := token.NewFileSet()
	// admin.go lives in this package's directory; the test working dir IS
	// that directory, so a bare filename parses it (same locality trick the
	// repo's other source-walking guards use, minus the repo-root walk —
	// the surface under audit is a single file).
	f, err := parser.ParseFile(fset, "admin.go", nil, 0)
	if err != nil {
		t.Fatalf("parse admin.go: %v", err)
	}

	registered := registeredAdminHandlers(t, f)
	gated := gatedAdminHandlers(f)

	if len(registered) == 0 {
		t.Fatal("found no admin handlers registered in registerAdminRoutes — " +
			"the guard cannot be vacuously green; did the route-registration " +
			"shape in admin.go change?")
	}

	var ungated []string
	for _, name := range registered {
		if !gated[name] {
			ungated = append(ungated, name)
		}
	}
	sort.Strings(ungated)

	for _, name := range ungated {
		t.Errorf("admin handler %q is registered under /api/v1/admin/ but its "+
			"body never calls requireAdmin — this re-opens the privilege "+
			"escalation fixed per ADMIN_SURFACE_AUDIT.md Blocker 1. Add "+
			"`if _, gated := h.server.requireAdmin(w, r); gated { return }` at "+
			"the top of %s, the same gate every other admin handler uses "+
			"(admingate.go). Do NOT route an admin endpoint around the gate.",
			name, name)
	}
}

// registeredAdminHandlers returns the adminHandler method names passed as the
// handler argument to each mux.HandleFunc call inside registerAdminRoutes.
// The handler argument is always a selector of the form `h.<method>`.
func registeredAdminHandlers(t *testing.T, f *ast.File) []string {
	t.Helper()
	var reg *ast.FuncDecl
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == "registerAdminRoutes" {
			reg = fd
			break
		}
	}
	if reg == nil {
		t.Fatal("registerAdminRoutes not found in admin.go — the guard relies " +
			"on it being the single admin-route registration site")
	}

	var names []string
	ast.Inspect(reg.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" {
			return true
		}
		// mux.HandleFunc(pattern, h.<method>) — the handler is the last arg.
		if len(call.Args) == 0 {
			return true
		}
		hsel, ok := call.Args[len(call.Args)-1].(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := hsel.X.(*ast.Ident); ok && ident.Name == "h" {
			names = append(names, hsel.Sel.Name)
		}
		return true
	})
	return names
}

// gatedAdminHandlers returns the set of adminHandler method names whose body
// contains a call to a selector named requireAdmin (e.g.
// h.server.requireAdmin(...)).
func gatedAdminHandlers(f *ast.File) map[string]bool {
	gated := map[string]bool{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil {
			continue
		}
		if !isAdminHandlerReceiver(fd.Recv) {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "requireAdmin" {
				gated[fd.Name.Name] = true
				return false
			}
			return true
		})
	}
	return gated
}

// isAdminHandlerReceiver reports whether the receiver list is `(h *adminHandler)`.
func isAdminHandlerReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "adminHandler"
}
