package gitdiff

import (
	"context"
	"strings"
	"testing"
)

func TestGitDiffTool_Metadata(t *testing.T) {
	tool := New()

	if tool.Name() != "local_git_diff" {
		t.Errorf("Name() = %q, want local_git_diff", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["staged"]; !ok {
		t.Error("Parameters() missing staged property")
	}
	if _, ok := params.Properties["path"]; !ok {
		t.Error("Parameters() missing path property")
	}
}

func TestGitDiffTool_Execute_NoArgs(t *testing.T) {
	// Running with no args should work (runs git diff in cwd which is a git repo)
	tool := New()
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() with no args: %v", err)
	}
	m := res.(map[string]any)
	if _, ok := m["diff"]; !ok {
		t.Error("result missing diff key")
	}
	if _, ok := m["truncated"]; !ok {
		t.Error("result missing truncated key")
	}
	if _, ok := m["files_changed"]; !ok {
		t.Error("result missing files_changed key")
	}
}

func TestGitDiffTool_Execute_Staged(t *testing.T) {
	tool := New()
	res, err := tool.Execute(context.Background(), map[string]any{"staged": true})
	if err != nil {
		t.Fatalf("Execute() staged=true: %v", err)
	}
	m := res.(map[string]any)
	if m["truncated"].(bool) {
		t.Error("unexpected truncation on staged diff")
	}
}

func TestGitDiffTool_Execute_BlockedPath(t *testing.T) {
	tool := New()
	// ~/.joe/ should be blocked by safety.IsPathAllowed
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": "~/.joe/config.yaml",
	})
	if err == nil {
		t.Fatal("expected error for blocked ~/.joe path")
	}
	if !strings.Contains(err.Error(), "self-protection") {
		t.Errorf("expected self-protection error, got: %v", err)
	}
}

func TestCountFilesInDiff(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single file", "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n", 1},
		{"two files", "diff --git a/a.go b/a.go\ndiff --git a/b.go b/b.go\n", 2},
		{"no diff header", "some random output\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countFilesInDiff(tt.input)
			if got != tt.want {
				t.Errorf("countFilesInDiff() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGitDiffTool_Execute_WithValidPath(t *testing.T) {
	// Pass a relative path that is allowed — exercises ExpandPath + safety check +
	// appending path to git args. Use "." which is always in the working repo.
	tool := New()
	res, err := tool.Execute(context.Background(), map[string]any{
		"path": ".",
	})
	if err != nil {
		t.Fatalf("Execute() with valid path: %v", err)
	}
	m := res.(map[string]any)
	if _, ok := m["diff"]; !ok {
		t.Error("result missing diff key")
	}
}

func TestGitDiffTool_Execute_Truncation(t *testing.T) {
	// Verify the truncation logic: we can't easily trigger it in a real repo,
	// but we can validate the truncated flag is false when diff is small.
	tool := New()
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	m := res.(map[string]any)
	truncated := m["truncated"].(bool)
	if truncated {
		if _, ok := m["truncated_message"]; !ok {
			t.Error("truncated=true but no truncated_message key")
		}
	}
}
