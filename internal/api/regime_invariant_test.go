package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInvariant4_RegimeResolveSingleCallSite is the named structural guard
// for §R5 / Invariant 4 (incident-mode entry may be automated; exit may
// not). The body of work that flips system_regime back to 'normal' lives
// inside sessionmodel.Repository.ResolveIncidentRegime. This test asserts
// that calls to that method appear in exactly one production-code
// function across the entire repository — the human-resolve handler
// (*regimeHandler).resolve in internal/api/regime.go.
//
// Any second production call site fails the build with a message
// explaining the rule. Test files (*_test.go) are exempt — tests
// legitimately exercise the function via different setup paths and the
// "no auto-resolve" invariant is about production code, not test code.
// The unexported "WithHook" variant is also exempt: it's the test seam
// exposed only for the single-transaction rollback assertion.
//
// When Change 12 lands and adds the autonomous-resolve inert seam, the
// seam returns 403 BEFORE any call to ResolveIncidentRegime. If a future
// contributor enables the seam (against the rules of the incremental-
// autonomy pattern) and routes through this function, the assertion
// would fire because there would now be a second call site. The
// decomposition pins this guard as the structural enforcement of
// "no auto-resolve via confirm_close".
func TestInvariant4_RegimeResolveSingleCallSite(t *testing.T) {
	repoRoot := findRepoRoot(t)

	type callSite struct {
		path    string
		line    int
		fnName  string
		fileRel string
	}
	var sites []callSite

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip vendor, node_modules, build artifacts, and the docs tree.
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == "dist" || name == "build" || name == ".joe" ||
				strings.HasPrefix(name, ".") && name != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Test files are exempt — see test-level doc comment.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}

		// Track the enclosing function for each call expression.
		var enclosing []string
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				enclosing = append(enclosing, funcDeclName(node))
				// Visit children, then pop on return.
				defer func() { enclosing = enclosing[:len(enclosing)-1] }()
				ast.Inspect(node, func(child ast.Node) bool {
					call, ok := child.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					// Match ResolveIncidentRegime ONLY — not the
					// WithHook variant (test seam) and not other names.
					if sel.Sel.Name != "ResolveIncidentRegime" {
						return true
					}
					rel, _ := filepath.Rel(repoRoot, path)
					sites = append(sites, callSite{
						path:    path,
						fileRel: rel,
						line:    fset.Position(call.Pos()).Line,
						fnName:  funcDeclName(node),
					})
					return true
				})
				return false // we already walked this FuncDecl's body
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Whitelist of allowed production-code call sites for ResolveIncidentRegime.
	// Adding entries here is a STRUCTURAL CHANGE that must be reviewed —
	// it expands the surface that can flip regime to 'normal'.
	type allowed struct {
		fileRel string
		fnName  string
		reason  string
	}
	allowedSites := []allowed{
		{
			fileRel: "internal/api/regime.go",
			fnName:  "(*regimeHandler).resolve",
			reason:  "the human-resolve handler — the only production caller per §R5 / Invariant 4",
		},
	}

	// Every observed site must match one in the allowlist.
	for _, s := range sites {
		matched := false
		for _, a := range allowedSites {
			if s.fileRel == a.fileRel && s.fnName == a.fnName {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("§R5 / Invariant 4 violation: unexpected production-code call to "+
				"ResolveIncidentRegime at %s:%d in %s\n\n"+
				"  Only the human-resolve handler may transition regime to 'normal'. "+
				"To add a new call site, extend the allowlist in this test in the "+
				"same commit and justify against §R5 (incident-mode exit may not be "+
				"automated).", s.fileRel, s.line, s.fnName)
		}
	}

	// Every entry in the allowlist must actually be present (catches typos
	// and dead allowlist entries that would silently weaken the guard).
	for _, a := range allowedSites {
		found := false
		for _, s := range sites {
			if s.fileRel == a.fileRel && s.fnName == a.fnName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("allowlist entry %s in %s is not present in code — guard is now weaker "+
				"than intended. Either remove the entry or restore the call site.",
				a.fnName, a.fileRel)
		}
	}
}

// funcDeclName produces a printable name for a function declaration,
// including the receiver type if present. e.g. "(*regimeHandler).resolve",
// or just "newRegimeServer" for a package-level function.
func funcDeclName(fn *ast.FuncDecl) string {
	if fn == nil {
		return "<nil>"
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	// Recover the receiver type as text.
	recvType := exprText(fn.Recv.List[0].Type)
	return "(" + recvType + ")." + fn.Name.Name
}

// exprText turns an ast.Expr into the source-level text it represents.
// Sufficient for our needs: identifiers and pointer-to-identifier.
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return "*" + exprText(v.X)
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.IndexExpr:
		return exprText(v.X) + "[" + exprText(v.Index) + "]"
	}
	return "<unknown>"
}

// findRepoRoot walks up from the working directory until it finds a go.mod.
// Tests are typically invoked from the package dir; the repo root is a few
// levels up.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod walking up from %s", wd)
	return ""
}
