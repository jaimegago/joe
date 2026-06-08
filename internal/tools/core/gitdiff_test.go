package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/tools/core"
)

type fakeGitClient struct {
	gitDiffFunc func(ctx context.Context, sourceID, from, to string) (string, error)
}

func (f *fakeGitClient) GitDiff(ctx context.Context, sourceID, from, to string) (string, error) {
	return f.gitDiffFunc(ctx, sourceID, from, to)
}

func TestGitDiffTool_Execute_Success(t *testing.T) {
	tool := core.NewGitDiffTool(&fakeGitClient{
		gitDiffFunc: func(ctx context.Context, sourceID, from, to string) (string, error) {
			if sourceID == "src" && from == "a" && to == "b" {
				return "diff output", nil
			}
			return "", errors.New("unexpected args")
		},
	})
	res, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "src",
		"from":         "a",
		"to":           "b",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := res.(map[string]any)
	if m["diff"] != "diff output" {
		t.Errorf("diff = %v, want diff output", m["diff"])
	}
}

func TestGitDiffTool_Execute_MissingParams(t *testing.T) {
	tool := core.NewGitDiffTool(&fakeGitClient{})
	cases := []map[string]any{
		{},
		{"from": "a", "to": "b"},
		{"component_id": "src", "to": "b"},
		{"component_id": "src", "from": "a"},
	}
	for _, args := range cases {
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Errorf("expected error for args: %v", args)
		}
	}
}

func TestGitDiffTool_Execute_ErrorFromClient(t *testing.T) {
	tool := core.NewGitDiffTool(&fakeGitClient{
		gitDiffFunc: func(ctx context.Context, sourceID, from, to string) (string, error) {
			return "", errors.New("fail")
		},
	})
	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "src",
		"from":         "a",
		"to":           "b",
	})
	if err == nil || err.Error() == "" {
		t.Errorf("expected error from client, got: %v", err)
	}
}
