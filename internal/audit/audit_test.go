package audit_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// TestRepositoryAPISurface_AppendOnly is the code-level half of the
// dual append-only enforcement (docs/joe-identity-design.md §2.6, Phase F
// req 2a). It parses the audit package source and asserts that the
// Repository interface — the surface every audit caller depends on —
// declares EXACTLY the two insert-shaped methods on the allow-list
// (Insert, InsertTx) and nothing else. The two-method allow-list, not a
// raw "must equal Insert" check, is the load-bearing structure: Stream G
// phase G4 added InsertTx so a settings mutation and its audit row can
// share one transaction, but every method name is still insert-shaped
// and the count must match the allow-list size. There is no Update,
// Delete, Truncate, or other mutator. The database-side trigger
// (migrations/015_audit_log.up.sql, preserved by 017) is the
// belt-and-suspenders half.
//
// To regress this guard a future maintainer would have to:
//
//   - add a third method to the interface (the count check fires); OR
//   - rename one of the existing methods to something not on the
//     allow-list (the per-method check fires).
//
// The separate mutator-name guard
// (TestRepositoryAPISurface_NoMutatorPackageFunctions) keeps top-level
// package functions from sneaking in Update/Delete/Truncate/Purge/Erase/
// Remove — InsertTx is insert-shaped so it does not collide.
func TestRepositoryAPISurface_AppendOnly(t *testing.T) {
	// Allow-list of permitted method names on audit.Repository. Both
	// names are insert-shaped: the row is appended; there is no caller
	// path to mutate or remove a row.
	allow := map[string]bool{
		"Insert":   true,
		"InsertTx": true,
	}

	fset := token.NewFileSet()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	auditDir := wd // tests run in the package directory

	pkgs, err := parser.ParseDir(fset, auditDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse audit package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("no Go packages found under %s", auditDir)
	}

	found := false
	var methodNames []string
	for _, pkg := range pkgs {
		ast.Inspect(pkg, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name == nil || ts.Name.Name != "Repository" {
				return true
			}
			iface, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				t.Fatalf("Repository is not an interface")
			}
			found = true
			for _, m := range iface.Methods.List {
				for _, name := range m.Names {
					methodNames = append(methodNames, name.Name)
				}
			}
			return false
		})
	}
	if !found {
		t.Fatalf("audit.Repository interface not found")
	}
	// Count must equal the allow-list size — guards against a stray
	// third method being added.
	if len(methodNames) != len(allow) {
		t.Fatalf("audit.Repository must expose exactly %d method(s) (the insert-shaped allow-list %v) — found %d: %v. The append-only contract forbids Update/Delete on the API surface; new methods may be added only if they are themselves insert-shaped and the allow-list is updated together.",
			len(allow), keysSorted(allow), len(methodNames), methodNames)
	}
	// Every method must be on the allow-list — guards against a rename
	// of either method to something non-insert-shaped.
	for _, name := range methodNames {
		if !allow[name] {
			t.Fatalf("audit.Repository method %q is not on the insert-shaped allow-list %v. The append-only contract forbids Update/Delete on the API surface.", name, keysSorted(allow))
		}
	}
}

// keysSorted returns the keys of m in sorted order so the failure
// message above is stable.
func keysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Tiny inline sort to avoid pulling sort into the test for two
	// elements; the allow-list is small and rarely changes.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// TestRepositoryAPISurface_NoMutatorPackageFunctions asserts the audit
// package exports no top-level mutator functions either — only the
// constructor, FailurePosture, and helpers. A future maintainer who
// added a stray `func Erase(...)` would trip this guard.
func TestRepositoryAPISurface_NoMutatorPackageFunctions(t *testing.T) {
	fset := token.NewFileSet()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	pkgs, err := parser.ParseDir(fset, wd, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse audit package: %v", err)
	}

	forbidden := []string{"Update", "Delete", "Truncate", "Purge", "Erase", "Remove"}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if fn.Name == nil {
					continue
				}
				name := fn.Name.Name
				for _, bad := range forbidden {
					if name == bad || strings.HasPrefix(name, bad) {
						t.Errorf("audit package exports forbidden mutator %q (file %s); the audit log is append-only — no removal surface should exist",
							name, fset.Position(fn.Pos()).Filename)
					}
				}
			}
		}
	}
}

// TestFailurePosture_FailOpenOnRead verifies the §4 split for reads:
// a failed audit write does NOT block the read (the action proceeds).
func TestFailurePosture_FailOpenOnRead(t *testing.T) {
	auditErr := errors.New("disk full")
	for _, action := range []string{string(rbac.ActionRead), string(rbac.ActionQuery)} {
		got := audit.FailurePosture(context.Background(), action, auditErr, "test:read")
		if got != nil {
			t.Errorf("FailurePosture(%q, auditErr) returned %v; reads must fail-open per §4", action, got)
		}
	}
}

// TestFailurePosture_FailClosedOnMutate verifies the §4 split for
// mutating actions and transition verbs: a failed audit write blocks the
// action (the auditErr is returned to the caller).
func TestFailurePosture_FailClosedOnMutate(t *testing.T) {
	auditErr := errors.New("disk full")
	mutating := []string{
		string(rbac.ActionMutate),
		string(rbac.ActionDelete),
		audit.ActionDeclareIncident,
		audit.ActionResolveIncident,
		audit.ActionCaptainAttach,
		audit.ActionCaptainTransferBegin,
		audit.ActionCaptainTransferConfirm,
		audit.ActionCaptainTransferCancel,
	}
	for _, action := range mutating {
		got := audit.FailurePosture(context.Background(), action, auditErr, "test:mutate")
		if got == nil {
			t.Errorf("FailurePosture(%q, auditErr) returned nil; mutating actions must fail-closed per §4", action)
		}
	}
}

// TestFailurePosture_NoErrorReturnsNil — the happy path doesn't lie.
func TestFailurePosture_NoErrorReturnsNil(t *testing.T) {
	for _, action := range []string{
		string(rbac.ActionRead),
		string(rbac.ActionMutate),
		audit.ActionDeclareIncident,
	} {
		if got := audit.FailurePosture(context.Background(), action, nil, "test"); got != nil {
			t.Errorf("FailurePosture(%q, nil) = %v; want nil", action, got)
		}
	}
}

// TestNoopRepository accepts any event and never errors.
func TestNoopRepository(t *testing.T) {
	r := audit.NewNoopRepository()
	if err := r.Insert(context.Background(), audit.Event{
		Principal: "user:alice",
		Action:    "read",
		Decision:  audit.DecisionAllow,
		Kind:      audit.KindInfraAccess,
	}); err != nil {
		t.Fatalf("noop Insert returned %v; want nil", err)
	}
}
