package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestInvariant_NoUngovernedAdapterOrGraphAccess is the load-bearing
// structural guard for docs/joe-identity-design.md §2.5 / §5-Invariant-2,
// Phase A: the guarded accessor (internal/access) is the ONLY path to an
// infrastructure adapter or the graph store. Concretely, no production code
// outside the allowlist may:
//
//   - resolve an adapter for use via services.Adapters.Get(...), or
//   - call a graph-store method via services.Graph.<Method>(...).
//
// Registry lifecycle (services.Adapters.Register/Unregister/List) and the
// nil-check `services.Graph == nil` are NOT adapter/graph ACCESS and are
// allowed; passing services.Adapters / services.Graph as constructor
// arguments (e.g. access.New(...)) is likewise fine.
//
// Phase E (docs/joe-identity-design.md §3) tightened this invariant. The
// agent-loop execution path (internal/api/tasks.go → in-process accessor
// client → accessor) is NOT in the allowlist and is covered by the guard —
// proving that the loop now reaches infra through the accessor with the real
// caller principal, not through a loopback HTTP self-call. This is the key
// signal Phase E achieved its purpose.
//
// Remaining allowlisted packages:
//
//   - internal/access  — the guarded accessor itself (its whole purpose).
//
//   - internal/coreagent — the Core Agent's background refresh path, which is
//     timer-driven and runs WITHOUT a caller principal (no user request,
//     no edge auth). The accessor requires a principal, so the refresh path
//     stays structurally outside it. This is NOT the loop path that Phase E
//     refactored — that is internal/api, which is covered by this guard.
//
//     Phase G (D-0010) re-verified this exception against every refresh path
//     and the Core Agent's own onboarding tools. The audit verdict was
//     VERDICT-A: the refresh side is READ-ONLY on customer infrastructure
//     (every *_refresh.go file calls List/Get/Describe/Status only — no
//     Create/Update/Delete/Apply/Post/Put/Patch on any adapter), and the
//     onboarding side mutates only the INTERNAL graph store via
//     graph_add_node / graph_add_edge tools. No code path on the coreagent
//     side issues an infrastructure mutation. The allowlist therefore stays;
//     should the refresh path ever gain a mutating call, that fact should
//     surface here as a new violation, this comment should be updated, and
//     the refresh should be moved under the accessor (or under captaingate
//     with a synthetic caller principal).
//
//   - cmd/joe — the composition root. Its only access is a process-level
//     OpenTelemetry business-metrics gauge that reads graph.Summary; this is
//     server-internal telemetry with no caller principal, so it is not a
//     principal-gated request/loop path and the accessor (which requires a
//     principal) is the wrong home for it.
//
// Modelled on the incident-exit AST guard in regime_invariant_test.go.
func TestInvariant_NoUngovernedAdapterOrGraphAccess(t *testing.T) {
	repoRoot := findRepoRoot(t)

	allowedPrefixes := []string{
		filepath.FromSlash("internal/access/"),
		filepath.FromSlash("internal/coreagent/"),
		filepath.FromSlash("cmd/joe/"),
	}

	// GraphStore methods that constitute graph ACCESS (graph.GraphStore).
	graphMethods := map[string]bool{
		"AddNode": true, "AddEdge": true, "GetNode": true, "Query": true,
		"Related": true, "Path": true, "DeleteNode": true, "DeleteEdge": true,
		"Summary": true, "ListNodesByComponent": true, "ListEdgesForNodes": true,
		"ListAll": true,
	}

	type violation struct {
		rel  string
		line int
		what string
	}
	var violations []violation

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
		for _, p := range allowedPrefixes {
			if strings.HasPrefix(rel, p) {
				return nil
			}
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", rel, perr)
			return nil
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.SelectorExpr) // receiver is itself a selector: X.Adapters / X.Graph
			if !ok {
				return true
			}
			line := fset.Position(call.Pos()).Line
			// services.Adapters.Get(...) — resolve-for-use path.
			if recv.Sel.Name == "Adapters" && sel.Sel.Name == "Get" {
				violations = append(violations, violation{rel, line,
					"services.Adapters.Get(...) — resolve an adapter through the guarded accessor instead"})
				return true
			}
			// services.Graph.<GraphStoreMethod>(...) — graph access.
			if recv.Sel.Name == "Graph" && graphMethods[sel.Sel.Name] {
				violations = append(violations, violation{rel, line,
					"services.Graph." + sel.Sel.Name + "(...) — reach the graph store through the guarded accessor instead"})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	for _, v := range violations {
		t.Errorf("ungoverned adapter/graph access at %s:%d — %s\n\n"+
			"  Phase A invariant: the only path to an adapter or the graph store is the\n"+
			"  guarded accessor (internal/access). If this is the accessor or a documented\n"+
			"  Phase-E exception, add its package prefix to allowedPrefixes in this test.",
			v.rel, v.line, v.what)
	}
}
