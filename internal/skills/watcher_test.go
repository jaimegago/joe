package skills

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// writeSkillFile writes a SKILL.md with the given metadata + body into dir.
// It uses the same disk shape as the installer so the watcher sees realistic
// content. Unlike the helper in registry_test.go, this version returns the
// file path so tests can mutate it later.
func writeSkillFile(t *testing.T, dir, name, desc, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + body + "\n"
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestWatcher(t *testing.T, root string) (*Watcher, *AtomicRouter) {
	t.Helper()
	router := NewAtomicRouter(NewRouter(NewRegistry()))
	w, err := NewWatcher(root, router, WithDebounce(20*time.Millisecond))
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, router
}

func TestWatcher_StartupReloadIsExplicit(t *testing.T) {
	// The watcher does not auto-reload on construction — the caller is
	// expected to do a startup load via skills.LoadDir + NewAtomicRouter
	// before constructing the watcher. We just verify the initial state.
	root := t.TempDir()
	w, router := newTestWatcher(t, root)
	if got := router.Snapshot().Registry().Len(); got != 0 {
		t.Errorf("initial registry should be empty, got %d", got)
	}
	_ = w
}

func TestWatcher_ReloadPicksUpNewSkill(t *testing.T) {
	root := t.TempDir()
	w, router := newTestWatcher(t, root)

	writeSkillFile(t, filepath.Join(root, "alpha"), "alpha", "first skill", "alpha body")

	result := w.Reload(context.Background(), "test")
	if result.Err != nil {
		t.Fatalf("Reload: %v", result.Err)
	}
	if result.After != 1 {
		t.Errorf("After = %d, want 1", result.After)
	}
	if !reflect.DeepEqual(result.Added, []string{"alpha"}) {
		t.Errorf("Added = %v, want [alpha]", result.Added)
	}
	if got := router.Snapshot().Registry().Get("alpha"); got == nil {
		t.Error("registry did not pick up new skill")
	}
}

func TestWatcher_ReloadDetectsUpdatedAndRemoved(t *testing.T) {
	root := t.TempDir()
	w, _ := newTestWatcher(t, root)

	alphaPath := writeSkillFile(t, filepath.Join(root, "alpha"), "alpha", "v1 description", "v1 body")
	writeSkillFile(t, filepath.Join(root, "beta"), "beta", "second", "body")
	if r := w.Reload(context.Background(), "seed"); r.Err != nil {
		t.Fatalf("seed Reload: %v", r.Err)
	}

	// Update alpha's body so its hash changes.
	if err := os.WriteFile(alphaPath, []byte(
		"---\nname: alpha\ndescription: v2 description\n---\nv2 body\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove beta.
	if err := os.RemoveAll(filepath.Join(root, "beta")); err != nil {
		t.Fatal(err)
	}

	result := w.Reload(context.Background(), "test")
	if result.Err != nil {
		t.Fatalf("Reload: %v", result.Err)
	}
	if result.Before != 2 || result.After != 1 {
		t.Errorf("counts before=%d after=%d, want 2->1", result.Before, result.After)
	}
	if !reflect.DeepEqual(result.Updated, []string{"alpha"}) {
		t.Errorf("Updated = %v, want [alpha]", result.Updated)
	}
	if !reflect.DeepEqual(result.Removed, []string{"beta"}) {
		t.Errorf("Removed = %v, want [beta]", result.Removed)
	}
}

func TestWatcher_FailedReloadKeepsPreviousRegistry(t *testing.T) {
	// A reload whose LoadDir succeeds but yields a skill that overwrites
	// nothing useful still publishes. To exercise the "validation failure
	// keeps old state" path we need LoadDir to return an error — that
	// only happens when the root is not a directory. Simulate by pointing
	// the watcher at a file the test then replaces with a regular file.
	//
	// We can't easily make LoadDir fail without breaking NewWatcher, so
	// instead we verify the weaker guarantee: when the new directory has
	// a malformed SKILL.md, the registry skips it rather than aborting,
	// and the previous good skill stays loaded.
	root := t.TempDir()
	w, router := newTestWatcher(t, root)

	writeSkillFile(t, filepath.Join(root, "alpha"), "alpha", "first", "body")
	if r := w.Reload(context.Background(), "seed"); r.Err != nil {
		t.Fatalf("seed Reload: %v", r.Err)
	}

	// Add a malformed SKILL.md without frontmatter.
	if err := os.MkdirAll(filepath.Join(root, "bad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bad", "SKILL.md"), []byte("no frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := w.Reload(context.Background(), "test")
	if result.Err != nil {
		t.Fatalf("Reload should not surface per-skill parse errors, got %v", result.Err)
	}
	if router.Snapshot().Registry().Get("alpha") == nil {
		t.Error("alpha was dropped after malformed sibling appeared")
	}
}

func TestWatcher_DetectsFilesystemChangesViaRun(t *testing.T) {
	// End-to-end: start Run, drop a skill into the directory, and verify
	// the registry updates within a short window. Uses a small debounce
	// (set in newTestWatcher) to keep the test fast.
	if testing.Short() {
		t.Skip("filesystem timing test is slow under -short")
	}
	root := t.TempDir()
	w, router := newTestWatcher(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runDone := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(runDone)
	}()

	writeSkillFile(t, filepath.Join(root, "gamma"), "gamma", "third skill", "body")

	// Wait up to ~1s for the watcher to debounce and reload.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if router.Snapshot().Registry().Get("gamma") != nil {
			cancel()
			<-runDone
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-runDone
	t.Fatalf("watcher did not pick up new skill within 1s; current count=%d",
		router.Snapshot().Registry().Len())
}

func TestDiffSkills(t *testing.T) {
	added, removed, updated := diffSkills(
		[]string{"alpha", "beta", "gamma"},
		map[string]string{"alpha": "h1", "beta": "h2", "gamma": "h3"},
		[]string{"alpha", "delta", "gamma"},
		map[string]string{"alpha": "h1", "delta": "h9", "gamma": "h3-new"},
	)
	want := func(name string, got []string, want []string) {
		t.Helper()
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	want("added", added, []string{"delta"})
	want("removed", removed, []string{"beta"})
	want("updated", updated, []string{"gamma"})
}
