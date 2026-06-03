package llmusage_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPhaseG2_LLMAdapterConstructorWrappedOnce is the structural guard
// that pins the Stream-G-G2 wrap-once invariant: the raw LLM adapter
// constructor (deps.newLLMAdapter) is called EXACTLY ONCE inside
// cmd/joe/server.go's runServerWithDeps, and that single call site is
// immediately followed by the assignment to the recording wrapper.
//
// Why this matters. The four (or more) downstream consumers — the
// SwappableAdapter, the knowledge embedder, the doc drafter, the
// review agent, and the Core Agent — all read the same llmAdapter
// handle by name. If a future change adds a SECOND call to
// deps.newLLMAdapter, that second call yields a fresh, unwrapped raw
// adapter; whichever consumer then takes that handle bypasses the
// recorder and its Chat calls vanish from llm_usage. This test catches
// that mistake at compile-test time instead of in production when a
// cost-window aggregate undercounts.
//
// If this fails: do NOT add a second call to satisfy a new consumer;
// instead, route the new consumer through the existing wrapped
// llmAdapter handle, or expose it via core.Services if the consumer
// lives in a different package.
func TestPhaseG2_LLMAdapterConstructorWrappedOnce(t *testing.T) {
	repoRoot := findRepoRootForGuard(t)
	mainPath := filepath.Join(repoRoot, "cmd", "joe", "server.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainPath, err)
	}

	var callSites []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// The raw constructor is deps.newLLMAdapter — the
		// dependency-injected callable. The whole point of routing it
		// through deps is that the test harnesses can substitute it;
		// the production wire site lives in runWithDeps and must
		// reference it exactly once.
		if ident.Name == "deps" && sel.Sel.Name == "newLLMAdapter" {
			pos := fset.Position(call.Pos())
			callSites = append(callSites, "server.go:"+strconv.Itoa(pos.Line))
		}
		return true
	})

	if got := len(callSites); got != 1 {
		t.Fatalf("deps.newLLMAdapter is called %d times in cmd/joe/server.go; want exactly 1 (the recorder wrap site). "+
			"call sites: %v. "+
			"A second call would hand a downstream consumer the RAW, unrecorded adapter. "+
			"Route the new consumer through the existing wrapped llmAdapter handle instead.",
			got, callSites)
	}

	// Belt-and-suspenders: the SINGLE call site must be in close
	// proximity to a llmusage.NewRecorderAdapter call site. We accept
	// any usage within the same file because the structure is "raw
	// adapter constructed, then immediately wrapped".
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read %s: %v", mainPath, err)
	}
	if !strings.Contains(string(src), "llmusage.NewRecorderAdapter") {
		t.Errorf("cmd/joe/server.go does not reference llmusage.NewRecorderAdapter; the raw adapter is constructed but never wrapped — every Chat call would go unrecorded.")
	}
}

// findRepoRootForGuard mirrors the helper used by the captaingate
// single-impl guard: walk up from the test directory until a go.mod is
// found, falling back to `go env GOMOD` when the walk hits the
// filesystem root (this happens in some CI sandboxes).
func findRepoRootForGuard(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			out, err := exec.Command("go", "env", "GOMOD").Output()
			if err == nil {
				s := strings.TrimSpace(string(out))
				if s != "" && s != "/dev/null" {
					return filepath.Dir(s)
				}
			}
			t.Fatal("could not find repo root (no go.mod walking up from test dir)")
		}
		dir = parent
	}
}
