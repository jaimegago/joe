package webui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory until it finds go.mod,
// so the contract test can read ui/vite.config.ts and the Makefile regardless
// of where `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (go.mod) above test directory")
		}
		dir = parent
	}
}

func readRepoFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(b)
}

// TestEmbedSourceMatchesViteOutDir pins the invariant that nothing else asserts:
// the directory Vite builds into is the same directory the Makefile copies into
// the embed tree (and that //go:embed all:dist then reads).
//
// The chain is: Vite build.outDir (relative to ui/) -> ui/dist -> Makefile
// `cp -R ui/dist/. internal/webui/dist/` -> //go:embed all:dist. Today every
// link is the Vite default ("dist"). If someone overrode Vite's outDir (e.g.
// build: { outDir: 'build' }) the Makefile would copy an empty ui/dist and ship
// a UI-less binary with no other warning — this test fails before that lands.
func TestEmbedSourceMatchesViteOutDir(t *testing.T) {
	root := repoRoot(t)

	// 1. Vite must not redirect build.outDir away from its default "dist".
	//    If build.outDir is set at all, it must resolve to "dist" (i.e. ui/dist).
	vite := readRepoFile(t, root, "ui", "vite.config.ts")
	outDir := regexp.MustCompile(`outDir\s*:\s*['"]([^'"]*)['"]`)
	if m := outDir.FindStringSubmatch(vite); m != nil {
		got := strings.TrimPrefix(strings.TrimSpace(m[1]), "./")
		if got != "dist" {
			t.Fatalf("vite build.outDir = %q, but the Makefile copies ui/dist; "+
				"a non-default outDir would ship a UI-less binary", got)
		}
	}

	// 2. The Makefile's embed dir and copy source must still be the ui/dist that
	//    Vite's default outDir produces and that the embed reads.
	mk := readRepoFile(t, root, "Makefile")
	if !strings.Contains(mk, "EMBED_UI_DIR := internal/webui/dist") {
		t.Fatal("Makefile EMBED_UI_DIR drifted from internal/webui/dist (the //go:embed all:dist path)")
	}
	if !strings.Contains(mk, "cp -R ui/dist/. $(EMBED_UI_DIR)/") {
		t.Fatal("Makefile no longer copies ui/dist into the embed dir; the embed source/outDir contract has drifted")
	}
}
