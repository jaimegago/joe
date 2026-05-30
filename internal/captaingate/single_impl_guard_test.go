package captaingate_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhaseG_SingleSharedCaptainGateImplementation is the named
// structural guard for the Phase G requirement: "there is a single
// shared §C gate implementation (not two divergent copies on the Core
// Agent and agentloop paths)" (D-0010).
//
// What we assert: across the entire repo, the function
// sessiongate.Check is invoked from EXACTLY ONE production package —
// internal/captaingate — and from that package's tests. No other
// production code (notably internal/coreagent, internal/api,
// internal/agentloop) may call sessiongate.Check, because doing so
// would mean a second gate implementation exists and the two can
// drift.
//
// The pre-Phase-G layout had the gate inside
// internal/coreagent/executor_durable.go (the only call site at the
// time). Phase G moved it to the shared internal/captaingate package
// and composed it around BOTH the Core Agent and the user task loop.
// If a future change reintroduces a sessiongate.Check call outside
// internal/captaingate, this test fails with an instruction to
// compose captaingate.Wrapper instead.
func TestPhaseG_SingleSharedCaptainGateImplementation(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Production callers ONLY (skip *_test.go files).
	allowed := filepath.FromSlash("internal/captaingate/")

	var violations []string
	walkErr := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == "dist" || name == "build" ||
				(strings.HasPrefix(name, ".") && name != ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		if strings.HasPrefix(rel, allowed) {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parse %s: %v", rel, perr)
			return nil
		}
		// Only inspect files that actually import sessiongate; otherwise
		// the call cannot exist by name.
		imports := false
		for _, imp := range f.Imports {
			if imp.Path != nil && imp.Path.Value == `"github.com/jaimegago/joe/internal/sessiongate"` {
				imports = true
				break
			}
		}
		if !imports {
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
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "sessiongate" && sel.Sel.Name == "Check" {
				violations = append(violations,
					rel+":"+itoa(fset.Position(call.Pos()).Line))
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}

	for _, v := range violations {
		t.Errorf("sessiongate.Check call outside internal/captaingate at %s — "+
			"Phase G invariant: the §C gate has exactly one production "+
			"implementation, in internal/captaingate.Wrapper. If you need to "+
			"gate a new execution path, compose captaingate.New(inner, "+
			"sessRepo, auditRepo) — do NOT call sessiongate.Check directly, "+
			"because doing so would split the gate logic into two copies "+
			"that can drift.", v)
	}
}

// findRepoRoot walks up from the test directory until it finds a
// go.mod (the standard "I'm at the repo root" probe).
func findRepoRoot(t *testing.T) string {
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
			// Fall back to `go env GOMOD`'s parent.
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

// itoa is the same as strconv.Itoa; inlined to avoid a strconv import
// just for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
