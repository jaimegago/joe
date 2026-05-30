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
// declares EXACTLY one method, Insert. There is no Update, Delete,
// Truncate, or other mutator. The database-side trigger
// (migrations/015_audit_log.up.sql) is the belt-and-suspenders half.
func TestRepositoryAPISurface_AppendOnly(t *testing.T) {
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
	if len(methodNames) != 1 || methodNames[0] != "Insert" {
		t.Fatalf("audit.Repository must expose exactly one method (Insert) — found %v. The append-only contract forbids Update/Delete on the API surface.", methodNames)
	}
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
