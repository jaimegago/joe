package core

import (
	"context"
	"fmt"

	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	"github.com/jaimegago/joe/internal/llm"
)

// GitHubPRGetClient is the interface required by the github_pr_get tool.
type GitHubPRGetClient interface {
	GitHubGetPR(ctx context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error)
}

// GitHubPRGetTool fetches GitHub pull request metadata.
type GitHubPRGetTool struct {
	client GitHubPRGetClient
}

// NewGitHubPRGetTool creates a new github_pr_get tool.
func NewGitHubPRGetTool(c GitHubPRGetClient) *GitHubPRGetTool {
	return &GitHubPRGetTool{client: c}
}

func (t *GitHubPRGetTool) Name() string { return "github_pr_get" }

func (t *GitHubPRGetTool) Description() string {
	return "Get metadata for a GitHub pull request (title, state, author, head/base SHA)."
}

func (t *GitHubPRGetTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "GitHub component ID registered in Joe"},
			"owner":        {Type: "string", Description: "Repository owner (org or user)"},
			"repo":         {Type: "string", Description: "Repository name"},
			"pr_number":    {Type: "number", Description: "Pull request number"},
		},
		Required: []string{"component_id", "owner", "repo", "pr_number"},
	}
}

func (t *GitHubPRGetTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, _ := args["component_id"].(string)
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	if sourceID == "" || owner == "" || repo == "" {
		return nil, fmt.Errorf("component_id, owner, and repo are required")
	}
	prNum, ok := args["pr_number"].(float64)
	if !ok || prNum <= 0 {
		return nil, fmt.Errorf("pr_number must be a positive integer")
	}

	pr, err := t.client.GitHubGetPR(ctx, sourceID, owner, repo, int(prNum))
	if err != nil {
		return nil, fmt.Errorf("github_pr_get: %w", err)
	}
	return pr, nil
}
