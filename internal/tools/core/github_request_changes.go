package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// GitHubRequestChangesClient is the interface required by the github_request_changes tool.
type GitHubRequestChangesClient interface {
	GitHubRequestChanges(ctx context.Context, sourceID, owner, repo string, number int, body string) error
}

// GitHubRequestChangesTool submits a "request changes" review on a GitHub PR (T3: Act).
// This blocks the PR from merging until the author addresses the review.
type GitHubRequestChangesTool struct {
	client GitHubRequestChangesClient
}

// NewGitHubRequestChangesTool creates a new github_request_changes tool.
func NewGitHubRequestChangesTool(c GitHubRequestChangesClient) *GitHubRequestChangesTool {
	return &GitHubRequestChangesTool{client: c}
}

func (t *GitHubRequestChangesTool) Name() string { return "github_request_changes" }

func (t *GitHubRequestChangesTool) Description() string {
	return "Submit a review requesting changes on a GitHub pull request. This blocks the PR from merging. Use only for blocking issues (security risks, infrastructure misconfigurations, breaking changes). This is T3 (Act) — requires explicit policy opt-in."
}

func (t *GitHubRequestChangesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {Type: "string", Description: "GitHub source ID registered in Joe"},
			"owner":     {Type: "string", Description: "Repository owner"},
			"repo":      {Type: "string", Description: "Repository name"},
			"pr_number": {Type: "number", Description: "Pull request number"},
			"body":      {Type: "string", Description: "Review body explaining what needs to be changed"},
		},
		Required: []string{"source_id", "owner", "repo", "pr_number", "body"},
	}
}

func (t *GitHubRequestChangesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
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

	if err := t.client.GitHubRequestChanges(ctx, sourceID, owner, repo, int(prNum), body); err != nil {
		return nil, fmt.Errorf("github_request_changes: %w", err)
	}
	return map[string]any{
		"changes_requested": true,
		"owner":             owner,
		"repo":              repo,
		"pr":                int(prNum),
	}, nil
}
