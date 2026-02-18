package gitstatus

import (
	"context"
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
