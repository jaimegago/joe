package git_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
)

// newSearchRepo builds a repo whose tree exercises every substrate rule:
// a binary file, files that sort around a path-prefix boundary (cmd vs
// cmdline), and a file whose content differs between the two commits.
//
// Tracked paths at HEAD, in the order Search must return them:
//
//	README.md, assets/blob.bin, cmd/app.go, cmdline/other.go,
//	internal/deep/util.go, version.txt
func newSearchRepo(t *testing.T) (*gitadapter.Adapter, string, string, string) {
	t.Helper()
	dir := t.TempDir()

	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	writeTestFile(t, dir, "README.md", "# Test Repo\nneedle one\n")
	writeTestFile(t, dir, "cmd/app.go", "package cmd\n\tneedle two\n")
	writeTestFile(t, dir, "cmdline/other.go", "package cmdline\nneedle three\n")
	writeTestFile(t, dir, "internal/deep/util.go", "package deep\nquiet\n")
	writeTestFile(t, dir, "assets/blob.bin", "\x00\x01needle four\n")
	writeTestFile(t, dir, "version.txt", "alpha\n")

	sig := &object.Signature{Name: "test", Email: "test@test.com", When: time.Now().Add(-time.Hour)}
	if err := wt.AddGlob("."); err != nil {
		t.Fatalf("add: %v", err)
	}
	hash1, err := wt.Commit("initial commit", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("commit 1: %v", err)
	}

	writeTestFile(t, dir, "version.txt", "beta\n")
	if _, err := wt.Add("version.txt"); err != nil {
		t.Fatalf("add version.txt: %v", err)
	}
	sig2 := &object.Signature{Name: "test", Email: "test@test.com", When: time.Now()}
	hash2, err := wt.Commit("bump version", &gogit.CommitOptions{Author: sig2})
	if err != nil {
		t.Fatalf("commit 2: %v", err)
	}

	return gitadapter.NewWithRepo(repo, dir), dir, hash1.String(), hash2.String()
}

func mustSearch(t *testing.T, a *gitadapter.Adapter, opts gitadapter.SearchOptions) *gitadapter.SearchResult {
	t.Helper()
	res, err := a.Search(context.Background(), opts)
	if err != nil {
		t.Fatalf("Search(%+v) error: %v", opts, err)
	}
	return res
}

// matchPaths renders the result as "path:line" so ordering assertions read as
// the contract states it: path in byte order, then line ascending.
func matchPaths(res *gitadapter.SearchResult) []string {
	out := make([]string, 0, len(res.Matches))
	for _, m := range res.Matches {
		out = append(out, m.Path+":"+strconv.Itoa(m.Line))
	}
	return out
}

func TestSearch_LiteralAcrossTree(t *testing.T) {
	a, _, _, head := newSearchRepo(t)

	res := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "needle", Literal: true})

	want := []string{"README.md:2", "cmd/app.go:2", "cmdline/other.go:2"}
	got := matchPaths(res)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("matches = %v, want %v (path byte order, then line)", got, want)
	}
	if res.Commit != head {
		t.Errorf("Commit = %q, want the resolved head %q", res.Commit, head)
	}
	if res.FilesInScope != 6 || res.FilesConsidered != 6 || res.FilesSearched != 5 {
		t.Errorf("in scope/considered/searched = %d/%d/%d, want 6/6/5",
			res.FilesInScope, res.FilesConsidered, res.FilesSearched)
	}
	if res.SkippedBinary != 1 {
		t.Errorf("SkippedBinary = %d, want 1 — assets/blob.bin holds a NUL byte and a matching line", res.SkippedBinary)
	}
	if res.MatchesTruncated || res.ScanIncomplete {
		t.Errorf("MatchesTruncated=%v ScanIncomplete=%v, want both false on a complete unbounded scan",
			res.MatchesTruncated, res.ScanIncomplete)
	}
	if res.Matches[1].Text != "\tneedle two" {
		t.Errorf("Text = %q, want the raw line including leading tab", res.Matches[1].Text)
	}
}

func TestSearch_RegexIsRE2(t *testing.T) {
	a, _, _, _ := newSearchRepo(t)

	res := mustSearch(t, a, gitadapter.SearchOptions{Pattern: `needle (one|three)`})
	if got, want := len(res.Matches), 2; got != want {
		t.Fatalf("match count = %d, want %d (%v)", got, want, matchPaths(res))
	}

	// A backreference is not RE2 syntax and must fail to compile rather than
	// silently matching nothing.
	if _, err := a.Search(context.Background(), gitadapter.SearchOptions{Pattern: `(needle)\1`}); err == nil {
		t.Error("Search with a backreference returned nil error; an unsupported pattern must fail explicitly")
	}
}

func TestSearch_InvalidPatternIsAnError(t *testing.T) {
	a, _, _, _ := newSearchRepo(t)

	res, err := a.Search(context.Background(), gitadapter.SearchOptions{Pattern: "needle("})
	if err == nil {
		t.Fatalf("Search with an unbalanced group returned (%+v, nil); it must fail explicitly, never as zero hits", res)
	}
	if !strings.Contains(err.Error(), "invalid search pattern") {
		t.Errorf("error = %q, want it to name the invalid pattern", err)
	}
}

func TestSearch_PinnedCommit(t *testing.T) {
	a, _, first, head := newSearchRepo(t)

	atFirst := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "alpha", Literal: true, Commit: first})
	if len(atFirst.Matches) != 1 || atFirst.Matches[0].Path != "version.txt" {
		t.Errorf("at %s: matches = %v, want version.txt only", first[:7], matchPaths(atFirst))
	}
	if atFirst.Commit != first {
		t.Errorf("Commit = %q, want %q — the answer must report the commit it was pinned to", atFirst.Commit, first)
	}

	atHead := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "alpha", Literal: true})
	if len(atHead.Matches) != 0 {
		t.Errorf("at head: matches = %v, want none — the string was replaced in the second commit", matchPaths(atHead))
	}
	if atHead.Commit != head {
		t.Errorf("empty result reported Commit = %q, want %q — an empty answer still reports its commit", atHead.Commit, head)
	}

	if _, err := a.Search(context.Background(), gitadapter.SearchOptions{
		Pattern: "needle",
		Commit:  "0000000000000000000000000000000000000000",
	}); err == nil {
		t.Error("Search at an unknown commit returned nil error; it must fail rather than answer at a different one")
	}
}

func TestSearch_PathScopeStopsAtSegmentBoundary(t *testing.T) {
	a, _, _, _ := newSearchRepo(t)

	res := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "needle", Literal: true, PathPrefix: "/cmd/"})
	if got := matchPaths(res); len(got) != 1 || got[0] != "cmd/app.go:2" {
		t.Errorf("matches = %v, want cmd/app.go only — cmdline/ is a different path segment", got)
	}
	if res.PathPrefix != "cmd" {
		t.Errorf("PathPrefix = %q, want the normalized %q reported back", res.PathPrefix, "cmd")
	}
	if res.FilesInScope != 1 {
		t.Errorf("FilesInScope = %d, want 1 — the scope narrows the substrate, not just the output", res.FilesInScope)
	}
}

func TestSearch_OutputBoundIsNotTheWorkBound(t *testing.T) {
	a, _, _, _ := newSearchRepo(t)

	out := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "needle", Literal: true, MaxMatches: 2})
	if got, want := matchPaths(out), []string{"README.md:2", "cmd/app.go:2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("bounded matches = %v, want the prefix %v", got, want)
	}
	if !out.MatchesTruncated {
		t.Error("MatchesTruncated = false after the output bound bit")
	}
	if out.ScanIncomplete {
		t.Error("ScanIncomplete = true when only the OUTPUT bound bit — the two markers mean opposite things and must not collapse")
	}

	work := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "needle", Literal: true, MaxFilesScanned: 2})
	if !work.ScanIncomplete {
		t.Error("ScanIncomplete = false after the work bound bit; an absence of hits would read as a clean empty result")
	}
	if work.MatchesTruncated {
		t.Error("MatchesTruncated = true when only the WORK bound bit")
	}
	if work.FilesConsidered != 2 || work.FilesInScope != 6 {
		t.Errorf("considered/in scope = %d/%d, want 2/6", work.FilesConsidered, work.FilesInScope)
	}
	if got := matchPaths(work); len(got) != 1 || got[0] != "README.md:2" {
		t.Errorf("matches = %v, want only the files the bound allowed", got)
	}
}

func TestSearch_BoundsCannotBeRaisedPastTheCeiling(t *testing.T) {
	a, _, _, _ := newSearchRepo(t)

	res := mustSearch(t, a, gitadapter.SearchOptions{
		Pattern:         "needle",
		Literal:         true,
		MaxMatches:      1 << 20,
		MaxFilesScanned: 1 << 20,
	})
	if res.MaxMatches >= 1<<20 {
		t.Errorf("MaxMatches = %d; a caller must not be able to raise the output bound past the ceiling", res.MaxMatches)
	}
	if res.MaxFilesScanned >= 1<<20 {
		t.Errorf("MaxFilesScanned = %d; a caller must not be able to raise the work bound past the ceiling", res.MaxFilesScanned)
	}
}

// TestSearch_SubstrateIsTheTreeNotTheWorktree pins the property that makes the
// reported commit meaningful: a hit must be reproducible by a git_read at the
// same commit, so uncommitted state on disk is not searched.
func TestSearch_SubstrateIsTheTreeNotTheWorktree(t *testing.T) {
	a, dir, _, _ := newSearchRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("needle five\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("needle six\nneedle seven\n"), 0o644); err != nil {
		t.Fatalf("overwrite tracked file: %v", err)
	}

	res := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "needle", Literal: true})
	if res.FilesInScope != 6 {
		t.Errorf("FilesInScope = %d, want 6 — an untracked file is not part of the substrate", res.FilesInScope)
	}
	for _, m := range res.Matches {
		if m.Text == "needle five" || m.Text == "needle six" || m.Text == "needle seven" {
			t.Errorf("matched worktree content %q; the substrate is the tree at the pinned commit", m.Text)
		}
	}
}

// TestSearch_LargeFileIsExcludedAndCounted pins the size rule. The threshold is
// git_read's own: a hit in a file git_read refuses could never be turned into a
// citation, so it is excluded from the substrate rather than offered as an
// unfollowable lead — and, like every exclusion, it is counted.
func TestSearch_LargeFileIsExcludedAndCounted(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	big := strings.Repeat("padding line\n", 100000) + "needle in a haystack\n"
	if len(big) <= 1<<20 {
		t.Fatalf("fixture is %d bytes, expected over 1 MiB", len(big))
	}
	writeTestFile(t, dir, "big.txt", big)
	writeTestFile(t, dir, "small.txt", "needle in a small file\n")

	if err := wt.AddGlob("."); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := wt.Commit("add fixtures", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com", When: time.Now()},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	a := gitadapter.NewWithRepo(repo, dir)
	res := mustSearch(t, a, gitadapter.SearchOptions{Pattern: "needle", Literal: true})

	if res.SkippedLarge != 1 {
		t.Errorf("SkippedLarge = %d, want 1 — an exclusion the caller cannot see breaks their knowledge of what was searched", res.SkippedLarge)
	}
	if got := matchPaths(res); len(got) != 1 || got[0] != "small.txt:1" {
		t.Errorf("matches = %v, want small.txt only", got)
	}
	if res.ScanIncomplete {
		t.Error("ScanIncomplete = true; a substrate exclusion is not an incomplete scan and must not be reported as one")
	}
}
