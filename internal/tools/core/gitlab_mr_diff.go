package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// GitLabMRDiffClient is the interface required by the gitlab_mr_diff tool.
type GitLabMRDiffClient interface {
	GitLabGetMRDiff(ctx context.Context, sourceID, projectID string, iid int) (string, error)
}

// GitLabMRDiffTool fetches the unified diff of a GitLab merge request.
type GitLabMRDiffTool struct {
	client GitLabMRDiffClient
}

// NewGitLabMRDiffTool creates a new gitlab_mr_diff tool.
func NewGitLabMRDiffTool(c GitLabMRDiffClient) *GitLabMRDiffTool {
	return &GitLabMRDiffTool{client: c}
}

func (t *GitLabMRDiffTool) Name() string { return "gitlab_mr_diff" }

func (t *GitLabMRDiffTool) Description() string {
	return "Get the unified diff of a GitLab merge request. Returns the full code changes for review."
}

func (t *GitLabMRDiffTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "GitLab source ID registered in Joe"},
			"project_id":   {Type: "string", Description: "GitLab project ID or URL-encoded path"},
			"mr_iid":       {Type: "number", Description: "Merge request internal ID (iid)"},
		},
		Required: []string{"component_id", "project_id", "mr_iid"},
	}
}

func (t *GitLabMRDiffTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, _ := args["component_id"].(string)
	projectID, _ := args["project_id"].(string)
	if sourceID == "" || projectID == "" {
		return nil, fmt.Errorf("component_id and project_id are required")
	}
	mrIID, ok := args["mr_iid"].(float64)
	if !ok || mrIID <= 0 {
		return nil, fmt.Errorf("mr_iid must be a positive integer")
	}

	diff, err := t.client.GitLabGetMRDiff(ctx, sourceID, projectID, int(mrIID))
	if err != nil {
		return nil, fmt.Errorf("gitlab_mr_diff: %w", err)
	}
	return map[string]any{"diff": diff}, nil
}
