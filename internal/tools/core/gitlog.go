package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// GitLogTool retrieves commit history from a Git repository source.
type GitLogTool struct {
	client *client.Client
}

// NewGitLogTool creates a new git_log tool.
func NewGitLogTool(c *client.Client) *GitLogTool {
	return &GitLogTool{client: c}
}

func (t *GitLogTool) Name() string { return "git_log" }

func (t *GitLogTool) Description() string {
	return "Get recent commit history from a connected Git repository source."
}

func (t *GitLogTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {Type: "string", Description: "ID of the Git source."},
			"limit":     {Type: "integer", Description: "Maximum number of commits to return. Defaults to 20."},
		},
		Required: []string{"source_id"},
	}
}

func (t *GitLogTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	commits, err := t.client.GitLog(ctx, sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	return map[string]any{
		"commits":   commits,
		"count":     len(commits),
		"source_id": sourceID,
	}, nil
}
