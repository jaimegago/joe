package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- ExpandPath (pathutil.go) ----

func TestExpandPath_Absolute(t *testing.T) {
	tmpDir := t.TempDir()
	got, err := ExpandPath(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tmpDir {
		t.Errorf("ExpandPath(%q) = %q, want %q", tmpDir, got, tmpDir)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home dir: %v", err)
	}
	got, err := ExpandPath("~")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != home {
		t.Errorf("ExpandPath(~) = %q, want %q", got, home)
	}
}

func TestExpandPath_TildeSubpath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home dir: %v", err)
	}
	got, err := ExpandPath("~/Documents")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "Documents")
	if got != want {
		t.Errorf("ExpandPath(~/Documents) = %q, want %q", got, want)
	}
}

func TestExpandPath_Relative(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	got, err := ExpandPath("somedir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(cwd, "somedir")
	if got != want {
		t.Errorf("ExpandPath(somedir) = %q, want %q", got, want)
	}
}

// ---- RunGit (gitutil.go) ----

func TestRunGit_Success(t *testing.T) {
	// Use the current repo directory (joe is a git repo)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Walk up to find a .git dir
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not inside a git repository")
		}
		dir = parent
	}

	out, err := RunGit(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatalf("RunGit rev-parse: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("expected non-empty output from rev-parse")
	}
}

func TestRunGit_NotARepo(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := RunGit(context.Background(), tmpDir, "status")
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunGit_CommandError(t *testing.T) {
	// Use a git repo dir but a bad subcommand to trigger a git error (not "not a git repo")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not inside a git repository")
		}
		dir = parent
	}

	_, err = RunGit(context.Background(), dir, "log", "--invalid-flag-xyz")
	if err == nil {
		t.Fatal("expected error for invalid git flag")
	}
}
