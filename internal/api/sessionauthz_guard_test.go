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

// TestInvariant_SessionMutationGoesThroughSeam is the load-bearing structural
// guard for DESIGN-CHAT-SESSIONS.md §12.7 / ledger node B003: the dedicated
// session-authorization seam (internal/sessionauthz, reached from the api layer
// via (*Server).sessionAccess) is the SINGLE enforcement point for session
// authorization. Concretely, no production code may mutate a session in the
// session-model store — UpdateSessionTitle, LinkSessionToIncident, or
// DeleteSession on sessionmodel.Repository — outside the allowlist below, and
// every user-initiated owner-mutate handler in that allowlist must ALSO call
// (*Server).sessionAccess in the same function.
//
// Modelled on the incident-exit guard (regime_invariant_test.go) and the
// ungoverned-adapter guard (access_guard_test.go); it reuses their funcDeclName
// / findRepoRoot helpers from this package.
//
// Method-name matching follows the regime guard. UpdateSessionTitle and
// LinkSessionToIncident are unique names. DeleteSession collides with the
// UNRELATED auth login-session method (internal/auth), so internal/auth is
// skipped — that DeleteSession is not a chat-session mutation and is out of this
// invariant's scope.
//
// Two allowlisted call sites are deliberately NOT seam-gated, each a recorded
// exception rather than a silent gap:
//   - (*taskHandler).generateTitleAsync — the LLM auto-title path is a SYSTEM
//     actor (no human request principal to authorize), the §12.7 sweeper-style
//     bypass. It must never grow a user-facing mutation.
//   - (*sessionsHandler).delete — the legacy /api/v1/agent-sessions team-global
//     delete, which carries NO ownership check today and is REMOVED with its
//     whole namespace in B005. It is pinned here so it cannot multiply before
//     B005 deletes it.
//
// Adding any entry expands the surface that can mutate a session without the
// seam and must be justified against §12.7 in the same commit.
func TestInvariant_SessionMutationGoesThroughSeam(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Session-model mutation methods this guard pins.
	mutationMethods := map[string]bool{
		"UpdateSessionTitle":    true,
		"LinkSessionToIncident": true,
		"DeleteSession":         true,
	}

	type site struct {
		fileRel string
		line    int
		fnName  string
	}
	var sites []site
	// Set of "fileRel::fnName" whose body contains a (*Server).sessionAccess call.
	seamGated := map[string]bool{}

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
		// The auth package's DeleteSession is an unrelated login-session method.
		if strings.HasPrefix(rel, filepath.FromSlash("internal/auth/")) {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", rel, perr)
			return nil
		}

		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			fnName := funcDeclName(fn)
			key := rel + "::" + fnName
			ast.Inspect(fn, func(child ast.Node) bool {
				call, ok := child.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name == "sessionAccess" {
					seamGated[key] = true
					return true
				}
				if mutationMethods[sel.Sel.Name] {
					sites = append(sites, site{
						fileRel: rel,
						line:    fset.Position(call.Pos()).Line,
						fnName:  fnName,
					})
				}
				return true
			})
			return false // already walked this FuncDecl's body
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	type allowed struct {
		fileRel     string
		fnName      string
		requireSeam bool
		reason      string
	}
	allowlist := []allowed{
		{
			fileRel:     filepath.FromSlash("internal/api/webui.go"),
			fnName:      "(*webUIHandler).handleUpdateSession",
			requireSeam: true,
			reason:      "per-user rename — owner-mutate 'write' through the seam",
		},
		{
			fileRel:     filepath.FromSlash("internal/api/webui.go"),
			fnName:      "(*webUIHandler).handleDeleteSession",
			requireSeam: true,
			reason:      "per-user delete — owner-mutate 'soft_delete' through the seam",
		},
		{
			fileRel:     filepath.FromSlash("internal/api/webui.go"),
			fnName:      "(*webUIHandler).handleLinkIncident",
			requireSeam: true,
			reason:      "per-user link-incident — owner-mutate 'write' through the seam",
		},
		{
			fileRel:     filepath.FromSlash("internal/api/sessiontitle.go"),
			fnName:      "(*taskHandler).generateTitleAsync",
			requireSeam: false,
			reason:      "LLM auto-title is a system actor (no request principal); §12.7 sweeper-style bypass",
		},
		{
			fileRel:     filepath.FromSlash("internal/api/sessions.go"),
			fnName:      "(*sessionsHandler).delete",
			requireSeam: false,
			reason:      "legacy /agent-sessions team-global delete (no ownership check); removed with the namespace in B005",
		},
	}

	inAllowlist := func(s site) (allowed, bool) {
		for _, a := range allowlist {
			if a.fileRel == s.fileRel && a.fnName == s.fnName {
				return a, true
			}
		}
		return allowed{}, false
	}

	// 1. Every observed mutation site must be allowlisted.
	for _, s := range sites {
		if _, ok := inAllowlist(s); !ok {
			t.Errorf("§12.7 violation: session mutation at %s:%d in %s reaches the store "+
				"without going through the session seam.\n\n"+
				"  Authorize the action via (*Server).sessionAccess and add this call site "+
				"to the allowlist in this test (in the same commit), justified against §12.7. "+
				"The seam — not an inline check — must be the single enforcement point.",
				s.fileRel, s.line, s.fnName)
		}
	}

	// 2. Seam-gated allowlist entries must actually call (*Server).sessionAccess.
	//    This is what proves the handler enforces via the seam, not in prose.
	for _, a := range allowlist {
		if !a.requireSeam {
			continue
		}
		if !seamGated[a.fileRel+"::"+a.fnName] {
			t.Errorf("§12.7 violation: %s in %s mutates a session but does NOT call "+
				"(*Server).sessionAccess — the seam is no longer its enforcement point.",
				a.fnName, a.fileRel)
		}
	}

	// 3. Every allowlist entry must be present in code (no dead/stale entries
	//    that would silently weaken the guard).
	for _, a := range allowlist {
		found := false
		for _, s := range sites {
			if s.fileRel == a.fileRel && s.fnName == a.fnName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("allowlist entry %s in %s is not present in code — the guard is now "+
				"weaker than intended. Remove the entry or restore the call site. (%s)",
				a.fnName, a.fileRel, a.reason)
		}
	}
}
