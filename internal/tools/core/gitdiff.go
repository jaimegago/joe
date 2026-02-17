package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// GitDiffClient defines the subset of client.Client needed for GitDiffTool.
type GitDiffClient interface {
	GitDiff(ctx context.Context, sourceID, from, to string) (string, error)
}

// GitDiffTool shows the diff between two refs in a Git repository source.
type GitDiffTool struct {
	client GitDiffClient
}

// NewGitDiffTool creates a new git_diff tool.
func NewGitDiffTool(c GitDiffClient) *GitDiffTool {
	return &GitDiffTool{client: c}
}

func (t *GitDiffTool) Name() string { return "git_diff" }

func (t *GitDiffTool) Description() string {
	return "Show the diff between two Git refs (branches, tags, or commit hashes) in a connected Git repository source."
}

func (t *GitDiffTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {Type: "string", Description: "ID of the Git source."},
			"from":      {Type: "string", Description: "Starting ref (branch, tag, or commit hash)."},
			"to":        {Type: "string", Description: "Ending ref (branch, tag, or commit hash)."},
		},
		Required: []string{"source_id", "from", "to"},
	}
}

func (t *GitDiffTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	from, ok := args["from"].(string)
	if !ok || from == "" {
		return nil, fmt.Errorf("missing required parameter: from")
	}

	to, ok := args["to"].(string)
	if !ok || to == "" {
		return nil, fmt.Errorf("missing required parameter: to")
	}

	diff, err := t.client.GitDiff(ctx, sourceID, from, to)
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	return map[string]any{
		"diff":      diff,
		"from":      from,
		"to":        to,
		"source_id": sourceID,
	}, nil
}
