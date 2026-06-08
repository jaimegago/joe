package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// GitHubPRDiffClient is the interface required by the github_pr_diff tool.
type GitHubPRDiffClient interface {
	GitHubGetPRDiff(ctx context.Context, sourceID, owner, repo string, number int) (string, error)
}

// GitHubPRDiffTool fetches the unified diff for a GitHub pull request.
type GitHubPRDiffTool struct {
	client GitHubPRDiffClient
}

// NewGitHubPRDiffTool creates a new github_pr_diff tool.
func NewGitHubPRDiffTool(c GitHubPRDiffClient) *GitHubPRDiffTool {
	return &GitHubPRDiffTool{client: c}
}

func (t *GitHubPRDiffTool) Name() string { return "github_pr_diff" }

func (t *GitHubPRDiffTool) Description() string {
	return "Get the unified diff for a GitHub pull request."
}

func (t *GitHubPRDiffTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "GitHub source ID registered in Joe"},
			"owner":        {Type: "string", Description: "Repository owner"},
			"repo":         {Type: "string", Description: "Repository name"},
			"pr_number":    {Type: "number", Description: "Pull request number"},
		},
		Required: []string{"component_id", "owner", "repo", "pr_number"},
	}
}

func (t *GitHubPRDiffTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	diff, err := t.client.GitHubGetPRDiff(ctx, sourceID, owner, repo, int(prNum))
	if err != nil {
		return nil, fmt.Errorf("github_pr_diff: %w", err)
	}
	return map[string]any{
		"diff":         diff,
		"component_id": sourceID,
		"owner":        owner,
		"repo":         repo,
		"pr_number":    int(prNum),
	}, nil
}
