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
// structural guard for docs/reference/joe-identity-design.md §2.5 / §5-Invariant-2,
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
// Phase E (docs/reference/joe-identity-design.md §3) tightened this invariant. The
// agent-loop execution path (internal/api/tasks.go → in-process accessor
// client → accessor) is NOT in the allowlist and is covered by the guard —
// proving that the loop now reaches infra through the accessor with the real
// caller principal, not through a loopback HTTP self-call. This is the key
// signal Phase E achieved its purpose.
//
// The allowlist is now SPLIT BY ACCESS KIND (A001-COREGOV CC-08): a package may
// be exempt for graph access without being exempt for adapter reads. See
// adapterGetAllowed and graphAccessAllowed below.
//
// Allowlisted packages:
//
//   - internal/access  — the guarded accessor itself (its whole purpose).
//     Exempt for both kinds, though it reaches the registry via its own
//     a.registry.Get field, never services.Adapters.Get.
//
//   - internal/coreagent — the Core Agent's background refresh path, which is
//     timer-driven and runs under the synthetic agent:core principal (no user
//     request, no edge auth). Exempt for GRAPH ACCESS ONLY (graphAccessAllowed),
//     NOT for adapter reads.
//
//     coreagent is NO LONGER exempt for the ADAPTER READ. Since A001-COREGOV
//     CC-05 the refresh resolves every component's adapter through
//     access.ResolveAdapter under the agent:core principal at rbac.ActionRead
//     (internal/coreagent/refresh.go resolveAdapter -> r.accessor.ResolveAdapter),
//     and CC-08 removed the last raw fallback: a nil accessor now FAILS CLOSED
//     (returns ErrPermissionDenied, no registry.Get) instead of reading the raw
//     registry. With no raw services.Adapters.Get left on any coreagent path,
//     coreagent is dropped from adapterGetAllowed — so the guard is RE-ARMED:
//     any reintroduced raw services.Adapters.Get in coreagent now FAILS this
//     test (it is no longer adapterExempt, so the Adapters.Get branch records a
//     violation). Production boot in cmd/joe/server.go always wires the accessor.
//
//     What coreagent's graph exemption DOES still cover is the Core Agent's graph WRITE half:
//     ApplyGraphDelta -> raw services.Graph.AddNode/AddEdge/Delete* in the
//     *_refresh.go files, plus the onboarding-tool graph mutations in agent.go.
//     This is INTENTIONAL, not a deferred or not-yet-governed gap: it is an
//     internal Tier-3 knowledge write, governed upstream by the agent:core read
//     floor (A001-COREGOV). A component whose refresh adapter read is denied
//     yields no delta, so the write is fenced by the floored read; the write
//     carries the agent:core principal for audit and takes no write permit by
//     design. The orphan-write enumeration (every ApplyGraphDelta call site is
//     reachable only downstream of the floored access.ResolveAdapter) was verified
//     during the coreagent refresh-governance review.
//
//     The refresh side remains READ-ONLY on customer infrastructure by invariant
//     (every *_refresh.go file calls List/Get/Describe/Status only — no
//     Create/Update/Delete/Apply/Post/Put/Patch on any adapter). Should the
//     refresh path ever gain a mutating INFRASTRUCTURE call, that is a different
//     matter from this intentional internal graph write and must be moved under
//     the accessor (or under captaingate with a synthetic caller principal).
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

	// Two SEPARATE allowlists, one per access kind (A001-COREGOV CC-08). Before
	// CC-08 a single prefix list exempted internal/coreagent for BOTH adapter
	// reads and graph writes, so the guard could no longer catch a reintroduced
	// raw services.Adapters.Get in coreagent. CC-08 fail-closed the refresh
	// resolve (it now flows through access.ResolveAdapter), so coreagent no longer
	// needs — and must NOT keep — an adapter-read exemption: dropping it from
	// adapterGetAllowed re-arms the guard so any new raw services.Adapters.Get in
	// coreagent (or anywhere outside the accessor) FAILS this test. The intentional
	// graph-WRITE half (ApplyGraphDelta -> services.Graph.AddNode/AddEdge/Delete*,
	// plus agent.go onboarding writes) stays exempt via graphAccessAllowed.
	//
	// internal/access is the guarded accessor itself; it reaches the registry via
	// its own a.registry.Get field (NOT services.Adapters.Get), so it does not even
	// need the adapter-read exemption — it is listed only for defensive clarity.
	adapterGetAllowed := []string{
		filepath.FromSlash("internal/access/"),
	}
	// Graph access stays exempt for the accessor, the Core Agent's intentional
	// Tier-3 graph write (governed upstream by the CC-05 floored read), and the
	// composition root's process-level graph.Summary telemetry gauge.
	graphAccessAllowed := []string{
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
		// A file may be exempt for one access kind but not the other (e.g.
		// coreagent is exempt for graph writes but NOT for adapter reads since
		// CC-08), so the prefix check is applied per-violation below, not as a
		// whole-file skip.
		hasPrefix := func(prefixes []string) bool {
			for _, p := range prefixes {
				if strings.HasPrefix(rel, p) {
					return true
				}
			}
			return false
		}
		adapterExempt := hasPrefix(adapterGetAllowed)
		graphExempt := hasPrefix(graphAccessAllowed)
		// Skip parsing only when the file is exempt for BOTH kinds.
		if adapterExempt && graphExempt {
			return nil
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
			// services.Adapters.Get(...) — resolve-for-use path. Exempt only for
			// adapterGetAllowed (NOT coreagent since CC-08).
			if recv.Sel.Name == "Adapters" && sel.Sel.Name == "Get" && !adapterExempt {
				violations = append(violations, violation{rel, line,
					"services.Adapters.Get(...) — resolve an adapter through the guarded accessor instead"})
				return true
			}
			// services.Graph.<GraphStoreMethod>(...) — graph access. Exempt for
			// graphAccessAllowed (includes coreagent's intentional Tier-3 write).
			if recv.Sel.Name == "Graph" && graphMethods[sel.Sel.Name] && !graphExempt {
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
			"  exception, add its package prefix to the matching allowlist in this test\n"+
			"  (adapterGetAllowed for services.Adapters.Get, graphAccessAllowed for graph\n"+
			"  methods). NOTE (CC-08): coreagent is intentionally NOT in adapterGetAllowed —\n"+
			"  a raw adapter read there must flow through access.ResolveAdapter, not be\n"+
			"  re-exempted.",
			v.rel, v.line, v.what)
	}
}
