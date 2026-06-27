package access_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestEveryDispatchMethodDeclaresAnAction is the structural guard for
// docs/reference/joe-identity-design.md §2.8: the action for each gated operation is
// declared as a property of the method (adjacent to the delegated adapter
// call), not inferred from an HTTP verb.
//
// Operationally: every EXPORTED method on *Accessor that takes an
// rbac.Principal parameter (i.e. every principal-gated dispatch method) must
// reference one of the rbac.Action* constants in its body — that reference is
// the action declaration the accessor passes to guard/permit. Methods that do
// NOT take a principal are config resolvers (WebhookSecret) or pure helpers
// (GraphAvailable) and are intentionally exempt: they perform no RBAC
// decision.
//
// If a future contributor adds a principal-gated method that reaches an
// adapter or the graph store without declaring an action, this guard fails.
func TestEveryDispatchMethodDeclaresAnAction(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read access package dir: %v", err)
	}

	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			if !isAccessorReceiver(fn.Recv) {
				continue
			}
			if !fn.Name.IsExported() {
				continue
			}
			if !hasPrincipalParam(fn.Type.Params) {
				// Config resolver / pure helper — exempt by design.
				continue
			}
			checked++
			if !bodyReferencesAction(fn.Body) {
				t.Errorf("%s: Accessor.%s takes an rbac.Principal but declares no rbac.Action — "+
					"every principal-gated dispatch method must declare its action (read/query/"+
					"mutate/delete) adjacent to the delegated call (design §2.8).", name, fn.Name.Name)
			}
		}
	}

	if checked == 0 {
		t.Fatal("found no principal-gated Accessor methods to check — the guard is not exercising anything")
	}
}

func isAccessorReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.Name == "Accessor"
}

func hasPrincipalParam(params *ast.FieldList) bool {
	if params == nil {
		return false
	}
	for _, field := range params.List {
		sel, ok := field.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "rbac" && sel.Sel.Name == "Principal" {
			return true
		}
	}
	return false
}

func bodyReferencesAction(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "rbac" && strings.HasPrefix(sel.Sel.Name, "Action") {
			found = true
			return false
		}
		return true
	})
	return found
}
