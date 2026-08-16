package git_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/store"
)

// Compile-time interface check
var _ gitadapter.GitAdapter = (*gitadapter.Adapter)(nil)

func writeTestFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// newTestRepo creates a temp git repo with two commits.
// Commit 1: README.md, main.go, cmd/app.go
// Commit 2: main.go modified
// Returns the repo, dir, and the two commit hashes.
func newTestRepo(t *testing.T) (*gogit.Repository, string, string, string) {
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

	sig := &object.Signature{Name: "test", Email: "test@test.com", When: time.Now().Add(-time.Hour)}

	writeTestFile(t, dir, "README.md", "# Test Repo\n")
	writeTestFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	writeTestFile(t, dir, "cmd/app.go", "package cmd\n")

	wt.Add(".")
	hash1, err := wt.Commit("initial commit", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("commit 1: %v", err)
	}

	sig2 := &object.Signature{Name: "test", Email: "test@test.com", When: time.Now()}
	writeTestFile(t, dir, "main.go", "package main\nfunc main() { println(\"hello\") }\n")
	wt.Add("main.go")
	hash2, err := wt.Commit("add hello", &gogit.CommitOptions{Author: sig2})
	if err != nil {
		t.Fatalf("commit 2: %v", err)
	}

	return repo, dir, hash1.String(), hash2.String()
}

func newTestAdapter(t *testing.T) (*gitadapter.Adapter, string, string) {
	t.Helper()
	repo, dir, hash1, hash2 := newTestRepo(t)
	return gitadapter.NewWithRepo(repo, dir), hash1, hash2
}

func TestConnect_ConfigErrors(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "missing url",
			config: `{"branch": "main"}`,
		},
		{
			name:   "ssh auth without key path",
			config: `{"url": "git@github.com:foo/bar.git", "auth_type": "ssh"}`,
		},
		{
			name:   "https auth without token",
			config: `{"url": "https://github.com/foo/bar.git", "auth_type": "https"}`,
		},
		{
			name:   "unknown auth type",
			config: `{"url": "https://github.com/foo/bar.git", "auth_type": "badauth"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := gitadapter.New()
			source := store.Component{Config: json.RawMessage(tt.config)}
			if err := a.Connect(context.Background(), source); err == nil {
				t.Errorf("Connect() expected error, got nil")
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   json.RawMessage
		wantURL string
		wantErr bool
	}{
		{
			name:    "full config",
			input:   json.RawMessage(`{"url":"https://github.com/foo/bar.git","branch":"main","auth_type":"https","http_token":"tok"}`),
			wantURL: "https://github.com/foo/bar.git",
		},
		{
			name:    "minimal config",
			input:   json.RawMessage(`{"url":"https://github.com/foo/bar.git"}`),
			wantURL: "https://github.com/foo/bar.git",
		},
		{
			name:    "missing url",
			input:   json.RawMessage(`{"branch":"main"}`),
			wantErr: true,
		},
		{
			name:    "empty json",
			input:   json.RawMessage(`{}`),
			wantErr: true,
		},
		{
			name:  "nil input",
			input: nil,
		},
		{
			name:    "invalid json",
			input:   json.RawMessage(`not json`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := gitadapter.ParseConfig(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantURL != "" && cfg.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", cfg.URL, tt.wantURL)
			}
		})
	}
}

func TestAdapterStatus(t *testing.T) {
	t.Run("new adapter is disconnected", func(t *testing.T) {
		a := gitadapter.New()
		s := a.Status()
		if s.Connected {
			t.Error("new adapter should not be connected")
		}
	})

	t.Run("test adapter is connected", func(t *testing.T) {
		a, _, _ := newTestAdapter(t)
		s := a.Status()
		if !s.Connected {
			t.Error("test adapter should be connected")
		}
	})
}

func TestDisconnect(t *testing.T) {
	a, _, _ := newTestAdapter(t)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	s := a.Status()
	if s.Connected {
		t.Error("should be disconnected after Disconnect()")
	}

	ctx := context.Background()
	_, err := a.ReadFile(ctx, "README.md", "")
	if err == nil {
		t.Error("ReadFile should fail after disconnect")
	}
}

func TestReadFile(t *testing.T) {
	a, hash1, hash2 := newTestAdapter(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "existing file",
			path: "README.md",
			want: "# Test Repo\n",
		},
		{
			name: "modified file reads latest",
			path: "main.go",
			want: "package main\nfunc main() { println(\"hello\") }\n",
		},
		{
			name: "file in subdirectory",
			path: "cmd/app.go",
			want: "package cmd\n",
		},
		{
			name:    "nonexistent file",
			path:    "nope.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.ReadFile(ctx, tt.path, "")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ReadFile(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Content != tt.want {
				t.Errorf("ReadFile(%q) = %q, want %q", tt.path, got.Content, tt.want)
			}
			// An absent commit still answers at one, and says which. Without
			// this the caller can pass a commit and never learn whether it was
			// honoured — the same blindness the argument was added to remove.
			if got.Commit != hash2 {
				t.Errorf("ReadFile(%q) reported commit %q, want the resolved head %q", tt.path, got.Commit, hash2)
			}
		})
	}

	// The property that makes repo_search's citation rule performable: a read at
	// a named commit answers THERE, not at whatever the clone's head happens to
	// be. main.go differs between the two commits, so a read that quietly
	// resolved head would return the wrong bytes rather than merely the wrong
	// label.
	t.Run("a named commit answers at that commit", func(t *testing.T) {
		got, err := a.ReadFile(ctx, "main.go", hash1)
		if err != nil {
			t.Fatalf("ReadFile at %s error = %v", hash1, err)
		}
		if want := "package main\nfunc main() {}\n"; got.Content != want {
			t.Errorf("ReadFile(main.go, %s) = %q, want the content at that commit %q", hash1, got.Content, want)
		}
		if got.Commit != hash1 {
			t.Errorf("reported commit = %q, want %q", got.Commit, hash1)
		}
	})

	// "Answer at exactly that revision or fail, never silently at a different
	// one." Falling back to head here is the failure mode the whole pin exists
	// to prevent, and it would be invisible in the answer.
	t.Run("an unresolvable commit fails rather than falling back to head", func(t *testing.T) {
		if _, err := a.ReadFile(ctx, "README.md", "no-such-revision"); err == nil {
			t.Error("ReadFile at an unresolvable revision returned no error; it must fail, never answer elsewhere")
		}
	})
}

func TestListFiles(t *testing.T) {
	a, hash1, hash2 := newTestAdapter(t)
	ctx := context.Background()

	t.Run("root directory", func(t *testing.T) {
		res, err := a.ListFiles(ctx, "", "")
		if err != nil {
			t.Fatalf("ListFiles(\"\") error = %v", err)
		}
		// Expect: README.md, main.go, cmd/
		if len(res.Files) != 3 {
			t.Fatalf("got %d files, want 3", len(res.Files))
		}

		names := map[string]bool{}
		for _, f := range res.Files {
			names[f.Path] = true
		}
		for _, want := range []string{"README.md", "main.go", "cmd"} {
			if !names[want] {
				t.Errorf("missing file %q in listing", want)
			}
		}
		if res.Commit != hash2 {
			t.Errorf("reported commit = %q, want the resolved head %q", res.Commit, hash2)
		}
	})

	t.Run("subdirectory", func(t *testing.T) {
		res, err := a.ListFiles(ctx, "cmd", "")
		if err != nil {
			t.Fatalf("ListFiles(\"cmd\") error = %v", err)
		}
		if len(res.Files) != 1 {
			t.Fatalf("got %d files, want 1", len(res.Files))
		}
		if res.Files[0].Path != "app.go" {
			t.Errorf("file path = %q, want \"app.go\"", res.Files[0].Path)
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		_, err := a.ListFiles(ctx, "nope", "")
		if err == nil {
			t.Error("ListFiles(\"nope\") should fail")
		}
	})

	// List mode takes the commit on the same terms as read mode. Honouring it
	// in one mode and not the other would leave git_read accepting a commit and
	// silently answering at a different one — exactly the fiction the pin exists
	// to prevent, moved rather than removed.
	t.Run("a named commit is honoured and reported in list mode too", func(t *testing.T) {
		res, err := a.ListFiles(ctx, "", hash1)
		if err != nil {
			t.Fatalf("ListFiles at %s error = %v", hash1, err)
		}
		if res.Commit != hash1 {
			t.Errorf("reported commit = %q, want %q", res.Commit, hash1)
		}
	})

	t.Run("an unresolvable commit fails rather than falling back to head", func(t *testing.T) {
		if _, err := a.ListFiles(ctx, "", "no-such-revision"); err == nil {
			t.Error("ListFiles at an unresolvable revision returned no error; it must fail, never answer elsewhere")
		}
	})
}

func TestLog(t *testing.T) {
	a, _, _ := newTestAdapter(t)
	ctx := context.Background()

	t.Run("returns commits in order", func(t *testing.T) {
		commits, err := a.Log(ctx, 10)
		if err != nil {
			t.Fatalf("Log() error = %v", err)
		}
		if len(commits) != 2 {
			t.Fatalf("got %d commits, want 2", len(commits))
		}
		// Most recent first
		if commits[0].Message != "add hello" {
			t.Errorf("first commit message = %q, want %q", commits[0].Message, "add hello")
		}
		if commits[1].Message != "initial commit" {
			t.Errorf("second commit message = %q, want %q", commits[1].Message, "initial commit")
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		commits, err := a.Log(ctx, 1)
		if err != nil {
			t.Fatalf("Log() error = %v", err)
		}
		if len(commits) != 1 {
			t.Fatalf("got %d commits, want 1", len(commits))
		}
	})

	t.Run("default limit", func(t *testing.T) {
		commits, err := a.Log(ctx, 0)
		if err != nil {
			t.Fatalf("Log() error = %v", err)
		}
		if len(commits) != 2 {
			t.Fatalf("got %d commits, want 2", len(commits))
		}
	})
}

func TestDiff(t *testing.T) {
	a, hash1, hash2 := newTestAdapter(t)
	ctx := context.Background()

	t.Run("diff between commits", func(t *testing.T) {
		diff, err := a.Diff(ctx, hash1, hash2)
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if diff == "" {
			t.Error("Diff() returned empty string")
		}
		// Should mention main.go since that's what changed
		if !contains(diff, "main.go") {
			t.Error("diff should mention main.go")
		}
	})

	t.Run("diff HEAD vs first commit", func(t *testing.T) {
		diff, err := a.Diff(ctx, hash1, "HEAD")
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if diff == "" {
			t.Error("Diff() returned empty string")
		}
	})

	t.Run("same commit no diff", func(t *testing.T) {
		diff, err := a.Diff(ctx, hash2, hash2)
		if err != nil {
			t.Fatalf("Diff() error = %v", err)
		}
		if diff != "" {
			t.Errorf("Diff() of same commit = %q, want empty", diff)
		}
	})

	t.Run("invalid ref", func(t *testing.T) {
		_, err := a.Diff(ctx, "badref", hash2)
		if err == nil {
			t.Error("Diff() with bad ref should fail")
		}
	})

	t.Run("invalid to ref", func(t *testing.T) {
		_, err := a.Diff(ctx, hash1, "badtoref")
		if err == nil {
			t.Error("Diff() with bad toRef should fail")
		}
	})
}

func TestConnect_LocalRepoWithBranch(t *testing.T) {
	// Override HOME so clones go to temp dir instead of ~/.joe/repos/
	t.Setenv("HOME", t.TempDir())

	_, srcDir, _, _ := newTestRepo(t)

	// Try with an explicit branch to exercise the branch condition in Connect
	for _, branch := range []string{"master", "main"} {
		a := gitadapter.New()
		source := store.Component{Config: json.RawMessage(`{"url": "` + srcDir + `", "branch": "` + branch + `"}`)}
		if err := a.Connect(context.Background(), source); err == nil {
			if !a.Status().Connected {
				t.Errorf("adapter should be connected after Connect() with branch %q", branch)
			}
			return
		}
	}
	t.Skip("neither master nor main branch could be cloned")
}

func TestConnect_LocalRepo(t *testing.T) {
	// Override HOME so clones go to temp dir instead of ~/.joe/repos/
	t.Setenv("HOME", t.TempDir())

	_, srcDir, _, _ := newTestRepo(t)

	a := gitadapter.New()
	source := store.Component{Config: json.RawMessage(`{"url": "` + srcDir + `"}`)}
	if err := a.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("adapter should be connected after successful Connect()")
	}
}

func TestLog_MaxLimitCapped(t *testing.T) {
	a, _, _ := newTestAdapter(t)
	ctx := context.Background()

	// 2 commits in repo; asking for > maxLogLimit (500) should still return 2
	commits, err := a.Log(ctx, 501)
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}
	if len(commits) != 2 {
		t.Errorf("Log(501) = %d commits, want 2", len(commits))
	}
}

func TestDiff_BranchName(t *testing.T) {
	a, hash1, _ := newTestAdapter(t)
	ctx := context.Background()

	// Exercise resolveCommit branch-reference path; try both common default names
	diff, err := a.Diff(ctx, hash1, "master")
	if err != nil {
		diff, err = a.Diff(ctx, hash1, "main")
		if err != nil {
			t.Skipf("neither master nor main branch found: %v", err)
		}
	}
	if diff == "" {
		t.Error("expected non-empty diff when comparing against branch head")
	}
}

func TestCommitAndPush_SSHMissingKey(t *testing.T) {
	// buildDocAuth should fail: ssh auth without key path.
	err := gitadapter.CommitAndPush(
		context.Background(),
		"file:///nonexistent",
		"",
		"file.md",
		"content",
		"msg",
		gitadapter.DocAuthConfig{AuthType: "ssh"},
	)
	if err == nil {
		t.Error("expected error for ssh auth without key path")
	}
}

func TestCommitAndPush_HTTPSMissingToken(t *testing.T) {
	// buildDocAuth should fail: https auth without token.
	err := gitadapter.CommitAndPush(
		context.Background(),
		"https://invalid.example.invalid/repo.git",
		"",
		"file.md",
		"content",
		"msg",
		gitadapter.DocAuthConfig{AuthType: "https"},
	)
	if err == nil {
		t.Error("expected error for https auth without token")
	}
}

func TestCommitAndPush_HTTPSCloneError(t *testing.T) {
	// buildDocAuth succeeds (returns BasicAuth), but clone fails with invalid URL.
	err := gitadapter.CommitAndPush(
		context.Background(),
		"https://invalid.example.invalid/repo.git",
		"",
		"file.md",
		"content",
		"msg",
		gitadapter.DocAuthConfig{AuthType: "https", HTTPToken: "tok"},
	)
	if err == nil {
		t.Error("expected error for clone failure with https auth")
	}
}

func TestCommitAndPush_LocalBareRepo(t *testing.T) {
	// Create a source repo with commits.
	_, srcDir, _, _ := newTestRepo(t)

	// Create a bare clone to act as "remote".
	bareDir := t.TempDir()
	_, err := gogit.PlainClone(bareDir, true, &gogit.CloneOptions{
		URL: srcDir,
	})
	if err != nil {
		t.Fatalf("bare clone: %v", err)
	}

	// CommitAndPush should succeed: clone bare, write file, commit, push back.
	err = gitadapter.CommitAndPush(
		context.Background(),
		bareDir,
		"",
		"docs/test.md",
		"# Test\n",
		"docs: add test.md",
		gitadapter.DocAuthConfig{AuthType: "none"},
	)
	if err != nil {
		t.Fatalf("CommitAndPush() error = %v", err)
	}
}

func TestConnect_PullExisting(t *testing.T) {
	// Override HOME so clones go to temp dir.
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, srcDir, _, _ := newTestRepo(t)

	// First connect: clones the repo.
	a := gitadapter.New()
	src := store.Component{Config: json.RawMessage(`{"url":"` + srcDir + `"}`)}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}

	// Second connect with same HOME: PlainOpen succeeds → exercises the pull path.
	b := gitadapter.New()
	if err := b.Connect(context.Background(), src); err != nil {
		t.Fatalf("second Connect() (pull path) error = %v", err)
	}
	if !b.Status().Connected {
		t.Error("expected connected after second Connect()")
	}
}

func TestDiff_TagRef(t *testing.T) {
	repo, dir, hash1, _ := newTestRepo(t)

	// Create a lightweight tag at the first commit.
	tagName := "v0.1.0"
	h := plumbing.NewHash(hash1)
	tagRef := plumbing.NewHashReference(plumbing.NewTagReferenceName(tagName), h)
	if err := repo.Storer.SetReference(tagRef); err != nil {
		t.Fatalf("create tag: %v", err)
	}

	a := gitadapter.NewWithRepo(repo, dir)
	diff, err := a.Diff(context.Background(), tagName, "HEAD")
	if err != nil {
		t.Fatalf("Diff(tag) error = %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff from tag to HEAD")
	}
}

func TestConnect_HTTPSAuth(t *testing.T) {
	// Override HOME so clones go to temp dir.
	t.Setenv("HOME", t.TempDir())
	_, srcDir, _, _ := newTestRepo(t)

	// https auth with a token — buildAuth should succeed returning BasicAuth.
	// The local file URL doesn't require auth so clone succeeds despite fake token.
	a := gitadapter.New()
	src := store.Component{Config: json.RawMessage(
		`{"url":"` + srcDir + `","auth_type":"https","http_token":"fake-token"}`,
	)}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Skipf("Connect with https auth on local dir not supported: %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected after Connect() with https auth")
	}
}

// --- Disconnected adapter error paths ---

func TestReadFile_Disconnected(t *testing.T) {
	a := gitadapter.New()
	_, err := a.ReadFile(context.Background(), "README.md", "")
	if err == nil {
		t.Error("ReadFile() on disconnected adapter should return error")
	}
}

func TestListFiles_Disconnected(t *testing.T) {
	a := gitadapter.New()
	_, err := a.ListFiles(context.Background(), "", "")
	if err == nil {
		t.Error("ListFiles() on disconnected adapter should return error")
	}
}

func TestLog_Disconnected(t *testing.T) {
	a := gitadapter.New()
	_, err := a.Log(context.Background(), 10)
	if err == nil {
		t.Error("Log() on disconnected adapter should return error")
	}
}

func TestDiff_Disconnected(t *testing.T) {
	a := gitadapter.New()
	_, err := a.Diff(context.Background(), "HEAD", "HEAD")
	if err == nil {
		t.Error("Diff() on disconnected adapter should return error")
	}
}

// --- CommitAndPush additional paths ---

func TestCommitAndPush_DefaultCommitMsg(t *testing.T) {
	// Commit message empty — exercises the default message branch.
	_, srcDir, _, _ := newTestRepo(t)

	bareDir := t.TempDir()
	_, err := gogit.PlainClone(bareDir, true, &gogit.CloneOptions{URL: srcDir})
	if err != nil {
		t.Fatalf("bare clone: %v", err)
	}

	err = gitadapter.CommitAndPush(
		context.Background(),
		bareDir,
		"",
		"docs/auto.md",
		"auto content",
		"", // empty message → default
		gitadapter.DocAuthConfig{AuthType: "none"},
	)
	if err != nil {
		t.Fatalf("CommitAndPush() with default msg error = %v", err)
	}
}

func TestCommitAndPush_WithBranch(t *testing.T) {
	// Clone with an explicit branch name to exercise the branch path.
	_, srcDir, _, _ := newTestRepo(t)

	bareDir := t.TempDir()
	_, cloneErr := gogit.PlainClone(bareDir, true, &gogit.CloneOptions{URL: srcDir})
	if cloneErr != nil {
		t.Fatalf("bare clone: %v", cloneErr)
	}

	// Determine which branch the test repo uses (master or main).
	tmpRepo, openErr := gogit.PlainOpen(srcDir)
	if openErr != nil {
		t.Fatalf("open src repo: %v", openErr)
	}
	head, headErr := tmpRepo.Head()
	if headErr != nil {
		t.Fatalf("get HEAD: %v", headErr)
	}
	branchName := head.Name().Short()

	err := gitadapter.CommitAndPush(
		context.Background(),
		bareDir,
		branchName,
		"docs/branch.md",
		"branch content",
		"docs: branch test",
		gitadapter.DocAuthConfig{AuthType: "none"},
	)
	if err != nil {
		t.Fatalf("CommitAndPush() with branch error = %v", err)
	}
}

func TestCommitAndPush_SSHWithKey(t *testing.T) {
	// buildDocAuth ssh path with a key path that doesn't exist → load error.
	err := gitadapter.CommitAndPush(
		context.Background(),
		"file:///nonexistent",
		"",
		"file.md",
		"content",
		"msg",
		gitadapter.DocAuthConfig{AuthType: "ssh", SSHKeyPath: "/nonexistent/key"},
	)
	if err == nil {
		t.Error("expected error for ssh auth with nonexistent key file")
	}
}

func TestReadFile_LargeFile(t *testing.T) {
	// Create a repo with a file larger than maxFileSize (1 MB) to hit the truncation guard.
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	// Write a 2 MB file.
	bigContent := make([]byte, 2<<20)
	for i := range bigContent {
		bigContent[i] = 'x'
	}
	largePath := filepath.Join(dir, "large.bin")
	if err := os.WriteFile(largePath, bigContent, 0644); err != nil {
		t.Fatalf("write large file: %v", err)
	}
	wt.Add("large.bin")
	sig := &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()}
	if _, err := wt.Commit("add large file", &gogit.CommitOptions{Author: sig}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	a := gitadapter.NewWithRepo(repo, dir)
	_, err = a.ReadFile(context.Background(), "large.bin", "")
	if err == nil {
		t.Error("ReadFile() expected error for file larger than maxFileSize")
	}
}

func TestListFiles_RootWithSlash(t *testing.T) {
	// Exercise the "/" dir path branch in ListFiles.
	a, _, _ := newTestAdapter(t)
	res, err := a.ListFiles(context.Background(), "/", "")
	if err != nil {
		t.Fatalf("ListFiles(\"/\") error = %v", err)
	}
	if len(res.Files) == 0 {
		t.Error("ListFiles(\"/\") should return files")
	}
}

func TestListFiles_DotDir(t *testing.T) {
	// Exercise the "." dir path branch in ListFiles.
	a, _, _ := newTestAdapter(t)
	res, err := a.ListFiles(context.Background(), ".", "")
	if err != nil {
		t.Fatalf("ListFiles(\".\") error = %v", err)
	}
	if len(res.Files) == 0 {
		t.Error("ListFiles(\".\") should return files")
	}
}

func TestConnect_InvalidConfig(t *testing.T) {
	a := gitadapter.New()
	src := store.Component{Config: json.RawMessage(`not-json`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for invalid config JSON")
	}
}

// writeTempSSHKey generates an ECDSA key and writes it to a temp file in PEM format.
func writeTempSSHKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "id_ecdsa")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, block); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func TestConnect_SSHAuthWithKey(t *testing.T) {
	// buildAuth ssh path: key file exists → NewPublicKeysFromFile succeeds,
	// then connect will fail (no ssh remote) but auth is built.
	keyPath := writeTempSSHKey(t)
	a := gitadapter.New()
	// HOME doesn't matter here — it will fail at clone, not at auth build.
	t.Setenv("HOME", t.TempDir())
	src := store.Component{Config: json.RawMessage(
		`{"url":"git@github.example.invalid:org/repo.git","auth_type":"ssh","ssh_key_path":"` + keyPath + `"}`,
	)}
	err := a.Connect(context.Background(), src)
	// We expect clone to fail, but buildAuth should have succeeded.
	// The error should mention clone/network, not key loading.
	if err == nil {
		t.Skip("unexpected success connecting to fake SSH remote")
	}
	// Ensure error is from clone, not from auth
	errStr := err.Error()
	for _, bad := range []string{"load ssh key", "ssh_key_path required"} {
		if containsHelper(errStr, bad) {
			t.Errorf("unexpected auth error: %v", err)
		}
	}
}

func TestDiff_Truncation(t *testing.T) {
	// Create a repo with a diff that exceeds maxDiffSize (1 MB).
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	sig := &object.Signature{Name: "test", Email: "t@t.com", When: time.Now().Add(-time.Hour)}

	// Commit 1: empty file.
	writeTestFile(t, dir, "big.txt", "")
	wt.Add("big.txt")
	hash1, err := wt.Commit("empty", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("commit1: %v", err)
	}

	// Commit 2: file with >1MB of unique lines so the diff is large.
	bigContent := make([]byte, 2<<20)
	for i := range bigContent {
		bigContent[i] = byte('a' + (i % 26))
	}
	writeTestFile(t, dir, "big.txt", string(bigContent))
	wt.Add("big.txt")
	sig2 := &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()}
	hash2, err := wt.Commit("big", &gogit.CommitOptions{Author: sig2})
	if err != nil {
		t.Fatalf("commit2: %v", err)
	}

	a := gitadapter.NewWithRepo(repo, dir)
	diff, err := a.Diff(context.Background(), hash1.String(), hash2.String())
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if !containsHelper(diff, "truncated") {
		t.Error("Diff() expected truncation marker for large diff")
	}
}

func TestCommitAndPush_SSHWithKeyLoadError(t *testing.T) {
	// buildDocAuth: ssh with an existing path but not a valid key → load error.
	keyPath := writeTempSSHKey(t)
	// Overwrite with garbage so it parses as a file but fails as an SSH key.
	if err := os.WriteFile(keyPath, []byte("not a pem key"), 0600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}
	err := gitadapter.CommitAndPush(
		context.Background(),
		"file:///nonexistent",
		"",
		"file.md",
		"content",
		"msg",
		gitadapter.DocAuthConfig{AuthType: "ssh", SSHKeyPath: keyPath},
	)
	if err == nil {
		t.Error("expected error for ssh auth with bad key file content")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
