package sessiongate_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInvariant5_NoRBACImportClosure is the named structural guard for
// Invariant 5 / §C2 — the §C captain-session gate is session-model-owned
// and must run UPSTREAM of the unchanged RBAC authorization layer. The
// gate must not import internal/rbac, directly or transitively, because
// importing it would invite future contributors to delegate the gate
// decision back into IsAllowed (which has no session/caller/incident
// parameter and cannot make this decision).
//
// Implementation: shell out to `go list -deps -json` against the
// sessiongate package and walk every entry's ImportPath. If
// github.com/jaimegago/joe/internal/rbac appears in the closure, fail
// loudly with an explanatory message. Equivalent in spirit to a
// golangci-lint depguard rule but self-contained in this test.
//
// When Change 10 wires the gate into the executor, the executor remains
// the layer that calls into rbac. The gate itself stays decoupled.
func TestInvariant5_NoRBACImportClosure(t *testing.T) {
	const forbidden = "github.com/jaimegago/joe/internal/rbac"
	const target = "github.com/jaimegago/joe/internal/sessiongate"

	cmd := exec.Command("go", "list", "-deps", "-json", target)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps -json %s: %v", target, err)
	}

	// `go list -json` emits a stream of JSON objects (not a JSON array).
	// Decode them one at a time.
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg struct {
			ImportPath string `json:"ImportPath"`
		}
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		if pkg.ImportPath == forbidden {
			t.Fatalf("Invariant 5 / §C2 violation: %s appears in the import "+
				"closure of %s.\n\nThe §C captain-session gate is session-model-"+
				"owned and runs UPSTREAM of the unchanged RBAC pipeline. It must "+
				"not import internal/rbac, directly or transitively. If a future "+
				"change needs to call rbac, do it from the EXECUTOR wrapper "+
				"(internal/coreagent/, Change 10) — not from sessiongate.",
				forbidden, target)
		}
	}
}

// TestC4_PositionalNotSemantic is the named structural guard for §C4.
// The Check function takes (ctx, repo, sessionID, principal, tier).
// It must NOT grow parameters named:
//
//   - sourceID  — would key on what is being touched, making the gate
//     source-aware (semantic).
//   - tool      — would key on which tool is firing.
//   - blast     — blast-radius computation; explicitly dropped in v1.
//   - radius    — same.
//
// Adding any of these would turn the §C gate from "which session is
// mutating?" into "what does the mutation touch?" — which is the §C4
// design failure mode. Adding a tier-like param is permitted (Tier
// short-circuits T1 reads only).
//
// Implementation: go/ast walk of sessiongate.go to find the Check
// FuncDecl and inspect its parameter identifiers. Any forbidden name
// fails the test with an explanation. The check looks at the IDENTIFIER
// the function declares, not the type — so renaming a param later is
// caught here even if its type stays the same.
func TestC4_PositionalNotSemantic(t *testing.T) {
	// Locate sessiongate.go relative to the test working dir (the
	// package directory; tests run there by default).
	path, err := filepath.Abs("sessiongate.go")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var checkFn *ast.FuncDecl
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "Check" && fn.Recv == nil {
			checkFn = fn
			break
		}
	}
	if checkFn == nil {
		t.Fatal("could not find Check function in sessiongate.go")
	}

	forbidden := map[string]bool{
		"sourceID": true, "sourceId": true, "component_id": true, "ComponentID": true,
		"tool": true, "toolName": true,
		"blast": true, "blastRadius": true,
		"radius": true,
	}

	// Enumerate parameters and assert none have a forbidden name.
	if checkFn.Type.Params == nil {
		t.Fatal("Check has no params field; unexpected")
	}
	for _, field := range checkFn.Type.Params.List {
		for _, name := range field.Names {
			if forbidden[name.Name] {
				t.Errorf("§C4 violation: Check parameter %q is forbidden — the gate "+
					"is POSITIONAL (which session the mutation arrives from), not "+
					"SEMANTIC (what it touches). Adding source/tool/blast/radius "+
					"parameters reintroduces semantic gating, which the v1 design "+
					"deliberately drops. If a new gate needs semantic data, build "+
					"it as a separate layer downstream of this one.", name.Name)
			}
		}
	}
}
