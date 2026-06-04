package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

// TestAdminRoutes_AllAuditOnAllow is the structural guard for D-0013 (the
// admin-audit gap): EVERY handler registered under the /api/v1/admin/ prefix
// must write an audit row in its allow path — i.e. its body must call
// h.recordAdminAudit, the single KindAdminAccess writer (admin.go).
//
// It is the sibling of TestAdminRoutes_AllRequireAdminGate
// (admin_gate_guard_test.go), which pins the GATE invariant from D-0012.
// Together they pin both halves of the admin-RBAC-surface contract: every
// admin endpoint (a) admin-gates AND (b) leaves a durable audit trail.
//
// Style mirrors the identity refactor's single-implementation guards —
// captaingate.TestPhaseG_SingleSharedCaptainGateImplementation and
// sessiongate's import guard — which parse the AST and assert a structural
// property no human review can be trusted to re-check on every change. Here
// the property is: the set of adminHandler methods wired into the mux by
// registerAdminRoutes is a SUBSET of the set of adminHandler methods that
// call recordAdminAudit in their body.
//
// Why a structural test and not just the regression tests: the regression
// tests (llmadmin_test.go / admin_audit_test.go) pin the behaviour of the
// endpoints that exist TODAY. This test pins the invariant for endpoints
// that DON'T EXIST YET — someone adding `POST /api/v1/admin/admins` next
// quarter and forgetting the audit write gets a failing build naming the
// unaudited handler, not a silently re-opened audit gap (the most
// authorization-critical mutations in the system going unrecorded, which is
// exactly the D-0012 finding that D-0013 closed). recordAdminAudit is the
// single writer mechanism; this test guarantees it is applied uniformly.
func TestAdminRoutes_AllAuditOnAllow(t *testing.T) {
	fset := token.NewFileSet()
	// admin.go lives in this package's directory; the test working dir IS
	// that directory, so a bare filename parses it (same locality trick the
	// gate guard and the repo's other source-walking guards use).
	f, err := parser.ParseFile(fset, "admin.go", nil, 0)
	if err != nil {
		t.Fatalf("parse admin.go: %v", err)
	}

	registered := registeredAdminHandlers(t, f)
	auditing := auditingAdminHandlers(f)

	if len(registered) == 0 {
		t.Fatal("found no admin handlers registered in registerAdminRoutes — " +
			"the guard cannot be vacuously green; did the route-registration " +
			"shape in admin.go change?")
	}

	var unaudited []string
	for _, name := range registered {
		if !auditing[name] {
			unaudited = append(unaudited, name)
		}
	}
	sort.Strings(unaudited)

	for _, name := range unaudited {
		t.Errorf("admin handler %q is registered under /api/v1/admin/ but its "+
			"body never calls recordAdminAudit — this re-opens the admin-audit "+
			"gap closed per DECISIONS.md D-0013 (authorization-config mutations "+
			"unrecorded). Add an `h.recordAdminAudit(...)` call in %s's allow "+
			"path: fail-closed (check the returned error, abort the mutation) "+
			"for a mutating action, fail-open (discard the return) for a read. "+
			"Do NOT route an admin endpoint around the audit writer.",
			name, name)
	}
}

// auditingAdminHandlers returns the set of adminHandler method names whose
// body contains a call to a selector named recordAdminAudit (e.g.
// h.recordAdminAudit(...)). The mechanics mirror gatedAdminHandlers in
// admin_gate_guard_test.go, which detects requireAdmin the same way.
func auditingAdminHandlers(f *ast.File) map[string]bool {
	auditing := map[string]bool{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil {
			continue
		}
		if !isAdminHandlerReceiver(fd.Recv) {
			continue
		}
		// recordAdminAudit is itself an adminHandler method; its body calls
		// Insert, not recordAdminAudit, so it is correctly NOT counted as
		// auditing itself (and it is not registered as a route either).
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "recordAdminAudit" {
				auditing[fd.Name.Name] = true
				return false
			}
			return true
		})
	}
	return auditing
}
