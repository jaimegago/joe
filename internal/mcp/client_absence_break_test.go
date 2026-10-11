package mcp

import (
	"bufio"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// client_absence_break_test.go pins D-0067 STRUCTURALLY: Joe is an MCP server and
// never an MCP client. It consumes no external MCP server's tools, because the
// protocol carries no enforceable mutation classification for them.
//
// The guard is an allowlist over MCP-library imports, not a denylist of client
// paths. Every production import whose path names MCP and lies outside this module
// must be one of allowedMCPImports — the mcp-go server and its shared protocol
// types. So the mcp-go client package fails here, and so does any second MCP
// library (whose client may share a package with its server), until someone edits
// the allowlist deliberately.

// allowedMCPImports is the complete set of external MCP packages production code
// may import. Adding an entry is a decision against D-0067, not a fix to this test.
var allowedMCPImports = map[string]bool{
	"github.com/mark3labs/mcp-go/mcp":    true, // protocol types, shared by both sides
	"github.com/mark3labs/mcp-go/server": true, // the joe mcp stdio server
}

// serverImport is the import the guard must observe, so a walk that sees no MCP
// import at all fails rather than passing vacuously.
const serverImport = "github.com/mark3labs/mcp-go/server"

// modulePath reads the module directive from the repository's go.mod.
func modulePath(t *testing.T, root string) string {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("open go.mod: %v", err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("no module directive in go.mod")
	return ""
}

// isExternalMCPImport reports whether path is an MCP package from outside this
// module — any path segment naming MCP, the module's own packages excluded.
func isExternalMCPImport(path, module string) bool {
	if path == module || strings.HasPrefix(path, module+"/") {
		return false
	}
	return strings.Contains(strings.ToLower(path), "mcp") ||
		strings.Contains(path, "modelcontextprotocol")
}

// TestNoMCPClientInProduction walks every production .go file in the repository and
// fails on any external MCP import outside allowedMCPImports.
func TestNoMCPClientInProduction(t *testing.T) {
	root := filepath.Join("..", "..")
	module := modulePath(t, root)
	sawServer := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip node_modules, testdata, and any hidden dir (.git, and
			// .claude/worktrees, whose stale copies would be scanned). The root is
			// exempt: its name is "..", which would otherwise read as hidden.
			n := d.Name()
			if path == root {
				return nil
			}
			if n == "node_modules" || n == "testdata" || (len(n) > 1 && strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !isExternalMCPImport(p, module) {
				continue
			}
			if p == serverImport {
				sawServer = true
			}
			if !allowedMCPImports[p] {
				t.Errorf("%s imports %s — Joe is an MCP server only, never an MCP client (D-0067)", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	if !sawServer {
		t.Errorf("no production file imports %s — the guard is vacuous; update serverImport and allowedMCPImports to the MCP library actually in use", serverImport)
	}
}

// TestIsExternalMCPImport pins the classifier the guard rests on, so a guard that
// stops recognising a client import fails here rather than going quietly green.
func TestIsExternalMCPImport(t *testing.T) {
	const module = "github.com/jaimegago/joe"
	cases := map[string]bool{
		"github.com/mark3labs/mcp-go/client":           true,
		"github.com/mark3labs/mcp-go/client/transport": true,
		"github.com/mark3labs/mcp-go/mcptest":          true,
		"github.com/modelcontextprotocol/go-sdk/mcp":   true,
		"github.com/jaimegago/joe/internal/mcp":        false,
		"github.com/jaimegago/joe":                     false,
		"k8s.io/client-go/kubernetes":                  false,
		"github.com/prometheus/client_golang/api":      false,
		"github.com/mark3labs/mcp-go/server":           true,
	}
	for path, want := range cases {
		if got := isExternalMCPImport(path, module); got != want {
			t.Errorf("isExternalMCPImport(%q) = %v, want %v", path, got, want)
		}
	}
	for _, client := range []string{"github.com/mark3labs/mcp-go/client", "github.com/mark3labs/mcp-go/mcptest"} {
		if allowedMCPImports[client] {
			t.Errorf("%s is allowlisted — that admits an MCP client", client)
		}
	}
}
