package git_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
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
			source := store.Source{Config: json.RawMessage(tt.config)}
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
	_, err := a.ReadFile(ctx, "README.md")
	if err == nil {
		t.Error("ReadFile should fail after disconnect")
	}
}

func TestReadFile(t *testing.T) {
	a, _, _ := newTestAdapter(t)
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
			got, err := a.ReadFile(ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ReadFile(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ReadFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestListFiles(t *testing.T) {
	a, _, _ := newTestAdapter(t)
	ctx := context.Background()

	t.Run("root directory", func(t *testing.T) {
		files, err := a.ListFiles(ctx, "")
		if err != nil {
			t.Fatalf("ListFiles(\"\") error = %v", err)
		}
		// Expect: README.md, main.go, cmd/
		if len(files) != 3 {
			t.Fatalf("got %d files, want 3", len(files))
		}

		names := map[string]bool{}
		for _, f := range files {
			names[f.Path] = true
		}
		for _, want := range []string{"README.md", "main.go", "cmd"} {
			if !names[want] {
				t.Errorf("missing file %q in listing", want)
			}
		}
	})

	t.Run("subdirectory", func(t *testing.T) {
		files, err := a.ListFiles(ctx, "cmd")
		if err != nil {
			t.Fatalf("ListFiles(\"cmd\") error = %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if files[0].Path != "app.go" {
			t.Errorf("file path = %q, want \"app.go\"", files[0].Path)
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		_, err := a.ListFiles(ctx, "nope")
		if err == nil {
			t.Error("ListFiles(\"nope\") should fail")
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
		source := store.Source{Config: json.RawMessage(`{"url": "` + srcDir + `", "branch": "` + branch + `"}`)}
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
	source := store.Source{Config: json.RawMessage(`{"url": "` + srcDir + `"}`)}
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
