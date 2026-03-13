package gitstatus

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestGitStatusTool_Metadata(t *testing.T) {
	tool := New()

	if tool.Name() != "local_git_status" {
		t.Errorf("Name() = %q, want local_git_status", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["path"]; !ok {
		t.Error("Parameters() missing path property")
	}
}

func TestGitStatusTool_Execute_DefaultCWD(t *testing.T) {
	// Running with no args uses CWD, which is a git repository.
	tool := New()
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() with no args: %v", err)
	}
	m := res.(map[string]any)
	if _, ok := m["branch"]; !ok {
		t.Error("result missing branch key")
	}
	if _, ok := m["is_clean"]; !ok {
		t.Error("result missing is_clean key")
	}
	if _, ok := m["staged"]; !ok {
		t.Error("result missing staged key")
	}
	if _, ok := m["unstaged"]; !ok {
		t.Error("result missing unstaged key")
	}
	if _, ok := m["untracked"]; !ok {
		t.Error("result missing untracked key")
	}
}

func TestGitStatusTool_Execute_ExplicitPath(t *testing.T) {
	// Pass "." which resolves to CWD — exercises the path argument branch.
	tool := New()
	res, err := tool.Execute(context.Background(), map[string]any{"path": "."})
	if err != nil {
		t.Fatalf("Execute() with path='.': %v", err)
	}
	m := res.(map[string]any)
	if _, ok := m["branch"]; !ok {
		t.Error("result missing branch key")
	}
}

func TestGitStatusTool_Execute_BlockedPath(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": "~/.joe",
	})
	if err == nil {
		t.Fatal("expected error for blocked ~/.joe path")
	}
	if !strings.Contains(err.Error(), "self-protection") {
		t.Errorf("expected self-protection error, got: %v", err)
	}
}

func TestGitStatusTool_Execute_NotARepo(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("expected 'not a git repository' error, got: %v", err)
	}
}

func TestParseStatusCode(t *testing.T) {
	tests := []struct {
		code byte
		want string
	}{
		{'M', "modified"},
		{'A', "added"},
		{'D', "deleted"},
		{'R', "renamed"},
		{'C', "copied"},
		{'U', "U"},
	}
	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			got := parseStatusCode(tt.code)
			if got != tt.want {
				t.Errorf("parseStatusCode(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestGitStatusTool_ParseOutput(t *testing.T) {
	// Validate parsing through Execute by checking that is_clean is true
	// when status output is empty (clean repo).
	// We cannot directly control git output, so we use CWD as it's likely clean in CI.
	// The real coverage comes from the parseStatusCode tests + status parsing logic.
	tool := New()
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	m := res.(map[string]any)
	isClean := m["is_clean"].(bool)
	staged := m["staged"].([]FileStatus)
	unstaged := m["unstaged"].([]FileStatus)
	untracked := m["untracked"].([]FileStatus)

	// is_clean should be consistent with the file lists
	expectedClean := len(staged) == 0 && len(unstaged) == 0 && len(untracked) == 0
	if isClean != expectedClean {
		t.Errorf("is_clean = %v, but staged/unstaged/untracked counts say %v", isClean, expectedClean)
	}
}

func TestGitStatusTool_Execute_InvalidPath(t *testing.T) {
	// A path with a null byte cannot be expanded/stat'd and triggers an error.
	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": string([]byte{0}),
	})
	if err == nil {
		t.Fatal("expected error for invalid path (null byte)")
	}
}

func TestGitStatusTool_Execute_PathWithTilde(t *testing.T) {
	// Path using ~ should expand correctly (to home directory); it may or may
	// not be a git repo, but we just need the expand-path branch to be exercised
	// without a panic.
	tool := New()
	_, _ = tool.Execute(context.Background(), map[string]any{"path": "~"})
}

// --- injected-runner tests to cover branches unreachable via real git ---

func TestGitStatusTool_Execute_BranchError(t *testing.T) {
	// Runner fails on the first call (branch --show-current).
	calls := 0
	tool := newWithRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		calls++
		return "", fmt.Errorf("git error: %v", args)
	})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error when branch command fails")
	}
	if calls != 1 {
		t.Errorf("expected 1 git call, got %d", calls)
	}
}

func TestGitStatusTool_Execute_StatusError(t *testing.T) {
	// Runner succeeds for branch but fails for status.
	calls := 0
	tool := newWithRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "main\n", nil // branch --show-current
		}
		return "", fmt.Errorf("git status error")
	})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error when status command fails")
	}
	if !strings.Contains(err.Error(), "git status error") {
		t.Errorf("error = %v, want 'git status error'", err)
	}
}

func TestGitStatusTool_Execute_EmptyStatusLine(t *testing.T) {
	// Status output containing an empty line — exercises the line=="" continue branch.
	tool := newWithRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "branch" {
			return "main\n", nil
		}
		// Porcelain output: one empty line, one valid untracked line.
		return "\n?? newfile.go\n", nil
	})
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	m := res.(map[string]any)
	untracked := m["untracked"].([]FileStatus)
	if len(untracked) != 1 {
		t.Errorf("len(untracked) = %d, want 1", len(untracked))
	}
}

func TestGitStatusTool_Execute_ShortStatusLine(t *testing.T) {
	// Status output with a line shorter than 4 chars — exercises the len<4 continue branch.
	tool := newWithRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "branch" {
			return "main\n", nil
		}
		// "M " is only 2 chars — should be skipped.
		return "M \n?? realfile.go\n", nil
	})
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	m := res.(map[string]any)
	untracked := m["untracked"].([]FileStatus)
	if len(untracked) != 1 {
		t.Errorf("len(untracked) = %d, want 1", len(untracked))
	}
}

func TestGitStatusTool_Execute_MixedStatus(t *testing.T) {
	// Exercises staged, unstaged, and untracked parsing paths together.
	tool := newWithRunner(func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "branch" {
			return "feature\n", nil
		}
		// M_ = staged modified, _M = unstaged modified, ?? = untracked
		return "M  staged.go\n M unstaged.go\n?? untracked.go\n", nil
	})
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	m := res.(map[string]any)
	if m["branch"] != "feature" {
		t.Errorf("branch = %v, want feature", m["branch"])
	}
	if m["is_clean"].(bool) {
		t.Error("is_clean = true, want false")
	}
	staged := m["staged"].([]FileStatus)
	if len(staged) != 1 {
		t.Errorf("len(staged) = %d, want 1", len(staged))
	}
	unstaged := m["unstaged"].([]FileStatus)
	if len(unstaged) != 1 {
		t.Errorf("len(unstaged) = %d, want 1", len(unstaged))
	}
	untracked := m["untracked"].([]FileStatus)
	if len(untracked) != 1 {
		t.Errorf("len(untracked) = %d, want 1", len(untracked))
	}
}
