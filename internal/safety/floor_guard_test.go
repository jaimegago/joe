package safety_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWriteFloor_NoRuntimeLoweringPath is the load-bearing immutability guard
// (D-0018 point 2). The write floor's immutability is guaranteed by the lowering
// operation NOT EXISTING in the running program — not by permission checks. This
// test walks every production .go file in the repo and fails if any of the
// former mutable-safe-mode mechanism reappears:
//
//   - safeModeActive    — the process-global mutable boolean (deleted)
//   - ActivateSafeMode  — the boot setter (replaced by the boot-resolved value)
//   - DeactivateSafeMode — the LIVE down-transition (deleted; this is the one
//     whose absence is the guarantee)
//   - IsSafeModeActive  — the live read of the mutable boolean (replaced by the
//     sealed WriteFloor value)
//   - safety.Reset / func Reset — the in-process panic-flag reset the old unlock
//     path called to lower state (deleted; recovery is restart)
//
// Reintroducing any of these would route the floor decision back through
// runtime-mutable state. The legitimate way to relax this is to update D-0018
// and this guard in the same diff.
func TestWriteFloor_NoRuntimeLoweringPath(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Each pattern is a forbidden runtime-lowering identifier. Word-bounded so a
	// substring (e.g. a longer identifier) does not false-positive.
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`\bsafeModeActive\b`),
		regexp.MustCompile(`\bActivateSafeMode\b`),
		regexp.MustCompile(`\bDeactivateSafeMode\b`),
		regexp.MustCompile(`\bIsSafeModeActive\b`),
		regexp.MustCompile(`\bErrSafeModeActive\b`),
		// The panic-flag Reset() the old unlock path called — gone; recovery is
		// restart. Matches both the definition and any caller.
		regexp.MustCompile(`\bfunc Reset\(`),
		regexp.MustCompile(`\bsafety\.Reset\(`),
	}

	skipDir := func(name string) bool {
		if name == ".git" || name == "node_modules" || name == "vendor" ||
			name == "dist" || name == "build" || name == ".joe" {
			return true
		}
		if strings.HasPrefix(name, ".") && name != "." {
			return true
		}
		return false
	}

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil // production-code rule; test files exempt
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			return nil
		}
		content := string(data)
		rel, _ := filepath.Rel(repoRoot, path)
		for _, re := range forbidden {
			if loc := re.FindStringIndex(content); loc != nil {
				line := 1 + strings.Count(content[:loc[0]], "\n")
				t.Errorf("%s:%d reintroduces a runtime floor-lowering identifier matching %q. "+
					"The write floor is a boot-resolved, runtime-immutable value (D-0018): "+
					"the down-transition must NOT EXIST in the production binary. Recovery is "+
					"restart, not a live transition. Update D-0018 and this guard together if "+
					"this is intentional.", rel, line, re.String())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

// TestWriteFloor_NotReDerivedFromDiskInExecutor asserts the enforcement path does
// not re-read the persisted panic state mid-process: the tool executor must not
// reference ReadPanicState or the panic state file. The floor is read from the
// single boot-sealed value (D-0018), so a local `joe unlock` clearing the file
// cannot lower a running floor.
func TestWriteFloor_NotReDerivedFromDiskInExecutor(t *testing.T) {
	repoRoot := findRepoRoot(t)
	executor := filepath.Join(repoRoot, "internal", "tools", "executor.go")
	data, err := os.ReadFile(executor)
	if err != nil {
		t.Fatalf("read executor.go: %v", err)
	}
	content := string(data)
	// The floor must be read from the boot-sealed WriteFloor value — never
	// re-derived from the (now-deleted) panic.state file NOR from the panic DB
	// row mid-process. Forbid both the file readers and the DB-row readers here.
	for _, bad := range []string{
		"ReadPanicState", "panic.state", "panicStateFile",
		"PanicStore", "IsPanicked", "PanicInfo", "cluster_panic_state",
	} {
		if strings.Contains(content, bad) {
			t.Errorf("executor.go references %q — the floor must be read from the "+
				"boot-sealed WriteFloor value, never re-derived from disk or DB "+
				"mid-process (D-0018).", bad)
		}
	}
}

// TestPanicState_SingleHomeNoFileConcept is the consolidation break-test (D-0018
// follow-up): panic state has ONE home, the cluster_panic_state DB row, and the
// panic.state FILE does not exist as a concept anywhere in production code. It
// walks every production .go file and fails if any file writer/reader/clearer or
// the path constant reappears. Analogous to the no-runtime-lowering guard: the
// guarantee is that the second store does not exist, not that it is guarded.
// The legitimate way to relax this is to update D-0018 and this guard together.
func TestPanicState_SingleHomeNoFileConcept(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Forbid the file API identifiers and the path used as a code literal. The
	// path is matched only as a quoted string ("panic.state") so this guard
	// catches a re-added file constant, not the prose in comments (including this
	// guard's own) that explains the file was removed.
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`\bWritePanicState\b`),
		regexp.MustCompile(`\bReadPanicState\b`),
		regexp.MustCompile(`\bClearPanicState\b`),
		regexp.MustCompile(`\bAcknowledgePanic\b`),
		regexp.MustCompile(`\bpanicStateFile\b`),
		regexp.MustCompile(`"panic\.state"`),
	}

	skipDir := func(name string) bool {
		if name == ".git" || name == "node_modules" || name == "vendor" ||
			name == "dist" || name == "build" || name == ".joe" {
			return true
		}
		if strings.HasPrefix(name, ".") && name != "." {
			return true
		}
		return false
	}

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil // production-code rule; test files exempt
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			return nil
		}
		content := string(data)
		rel, _ := filepath.Rel(repoRoot, path)
		for _, re := range forbidden {
			if loc := re.FindStringIndex(content); loc != nil {
				line := 1 + strings.Count(content[:loc[0]], "\n")
				t.Errorf("%s:%d reintroduces the panic.state FILE concept matching %q. "+
					"Panic state has ONE home — the cluster_panic_state DB row (D-0018 "+
					"consolidation). The file writer/reader/clearer and its path were "+
					"deleted; recovery clears the DB row via `joe unlock`. Update D-0018 "+
					"and this guard together if this is intentional.", rel, line, re.String())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

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
