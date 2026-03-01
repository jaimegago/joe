package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// GitHubCommentClient is the interface required by the github_comment tool.
type GitHubCommentClient interface {
	GitHubPostComment(ctx context.Context, sourceID, owner, repo string, number int, body string) error
}

// GitHubCommentTool posts a comment on a GitHub pull request (T2: Record).
type GitHubCommentTool struct {
	client GitHubCommentClient
}

// NewGitHubCommentTool creates a new github_comment tool.
func NewGitHubCommentTool(c GitHubCommentClient) *GitHubCommentTool {
	return &GitHubCommentTool{client: c}
}

func (t *GitHubCommentTool) Name() string { return "github_comment" }

func (t *GitHubCommentTool) Description() string {
	return "Post a review comment on a GitHub pull request. Use this to share findings, suggestions, or a review summary. This is T2 (Record) — it writes to GitHub but does not block the PR."
}

func (t *GitHubCommentTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {Type: "string", Description: "GitHub source ID registered in Joe"},
			"owner":     {Type: "string", Description: "Repository owner"},
			"repo":      {Type: "string", Description: "Repository name"},
			"pr_number": {Type: "number", Description: "Pull request number"},
			"body":      {Type: "string", Description: "Comment body (Markdown supported)"},
		},
		Required: []string{"source_id", "owner", "repo", "pr_number", "body"},
	}
}

func (t *GitHubCommentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, _ := args["source_id"].(string)
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	body, _ := args["body"].(string)
	if sourceID == "" || owner == "" || repo == "" || body == "" {
		return nil, fmt.Errorf("source_id, owner, repo, and body are required")
	}
	prNum, ok := args["pr_number"].(float64)
	if !ok || prNum <= 0 {
		return nil, fmt.Errorf("pr_number must be a positive integer")
	}

	if err := t.client.GitHubPostComment(ctx, sourceID, owner, repo, int(prNum), body); err != nil {
		return nil, fmt.Errorf("github_comment: %w", err)
	}
	return map[string]any{
		"posted": true,
		"owner":  owner,
		"repo":   repo,
		"pr":     int(prNum),
	}, nil
}
