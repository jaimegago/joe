package rbac_test

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

// TestGuard_PolicyEngineConstructedOnlyAtCompositionRoot is the rbac-engine-split
// structural guard, in the style of internal/api/regime_invariant_test.go. Every
// rbac.PolicyEngine must be constructed at the composition root
// (cmd/joe/server.go — buildHTTPHandler for the transport engine, the CC-05
// refresh-engine wiring for the agent:core engine) and injected into its
// consumer; NO other production file may call an rbac.NewPolicyEngine*
// constructor.
//
// This is what makes the transport/accessor engine drift this session fixed
// structurally impossible: the drift was a SECOND, bare engine (with neither the
// read-posture nor the auto_promote resolver) built inside internal/api's
// api.New, whose consumer — the guarded accessor — then enforced with it while the
// governance-wired engine fed only the demoted EnforcementMiddleware. Forbidding
// engine construction anywhere but the composition root keeps the one
// governance-wired engine the only engine on each path.
//
// cmd/joe (the composition root) and _test.go files (which legitimately build
// engines to drive handlers under test — the exemption the pin and the api tests
// rely on) are exempt. Adding a production call site outside cmd/joe fails the
// build with a message explaining the rule.
func TestGuard_PolicyEngineConstructedOnlyAtCompositionRoot(t *testing.T) {
	repoRoot := findRepoRootFromRBAC(t)

	type site struct {
		fileRel string
		line    int
		fn      string
	}
	var sites []site

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == "dist" || name == "build" || name == ".joe" ||
				(strings.HasPrefix(name, ".") && name != ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		// The composition root is the ONE allowed construction site.
		if strings.HasPrefix(filepath.ToSlash(rel), "cmd/joe/") {
			return nil
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// Match both a qualified call (rbac.NewPolicyEngine…) from another
			// package and an unqualified call (NewPolicyEngine…) from within
			// package rbac. The constructor DEFINITIONS in policy.go are FuncDecls,
			// not CallExprs, so they are never flagged.
			if strings.HasPrefix(calleeName(call.Fun), "NewPolicyEngine") {
				sites = append(sites, site{fileRel: rel, line: fset.Position(call.Pos()).Line, fn: calleeName(call.Fun)})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	for _, s := range sites {
		t.Errorf("rbac-engine-split violation: %s constructor called at %s:%d — every "+
			"rbac.PolicyEngine must be built at the composition root (cmd/joe) and injected. "+
			"Constructing one elsewhere reintroduces the transport/accessor engine drift this "+
			"guard prevents; move the construction to cmd/joe/server.go and pass the engine in.",
			s.fn, s.fileRel, s.line)
	}
}

// calleeName returns the called function's identifier for either a qualified
// (pkg.NewPolicyEngine) or unqualified (NewPolicyEngine) call expression, and ""
// for any other callee shape.
func calleeName(fun ast.Expr) string {
	switch v := fun.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// findRepoRootFromRBAC walks up from the working directory until it finds a
// go.mod (the package dir is a few levels below the repo root).
func findRepoRootFromRBAC(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 10; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
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
