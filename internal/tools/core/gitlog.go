package core

import (
	"context"
	"fmt"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/llm"
)

// GitLogClient defines the subset of client.Client needed for GitLogTool.
type GitLogClient interface {
	GitLog(ctx context.Context, sourceID string, limit int) ([]gitadapter.CommitInfo, error)
}

// GitLogTool retrieves commit history from a Git repository component.
type GitLogTool struct {
	client GitLogClient
}

// NewGitLogTool creates a new git_log tool.
func NewGitLogTool(c GitLogClient) *GitLogTool {
	return &GitLogTool{client: c}
}

func (t *GitLogTool) Name() string { return "git_log" }

func (t *GitLogTool) Description() string {
	return "Get recent commit history from a connected Git repository component."
}

func (t *GitLogTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "ID of the Git component."},
			"limit":        {Type: "integer", Description: "Maximum number of commits to return. Defaults to 20."},
		},
		Required: []string{"component_id"},
	}
}

func (t *GitLogTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
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
		"commits":      commits,
		"count":        len(commits),
		"component_id": sourceID,
	}, nil
}
