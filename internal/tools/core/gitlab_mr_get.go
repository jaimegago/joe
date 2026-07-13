package core

import (
	"context"
	"fmt"

	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/llm"
)

// GitLabMRGetClient is the interface required by the gitlab_mr_get tool.
type GitLabMRGetClient interface {
	GitLabGetMR(ctx context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error)
}

// GitLabMRGetTool fetches GitLab merge request metadata.
type GitLabMRGetTool struct {
	client GitLabMRGetClient
}

// NewGitLabMRGetTool creates a new gitlab_mr_get tool.
func NewGitLabMRGetTool(c GitLabMRGetClient) *GitLabMRGetTool {
	return &GitLabMRGetTool{client: c}
}

func (t *GitLabMRGetTool) Name() string { return "gitlab_mr_get" }

func (t *GitLabMRGetTool) Description() string {
	return "Get metadata for a GitLab merge request (title, state, author, source/target branch, SHA)."
}

func (t *GitLabMRGetTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "GitLab component ID registered in Joe"},
			"project_id":   {Type: "string", Description: "GitLab project ID or URL-encoded path (e.g. '42' or 'group%2Fproject')"},
			"mr_iid":       {Type: "number", Description: "Merge request internal ID (iid)"},
		},
		Required: []string{"component_id", "project_id", "mr_iid"},
	}
}

func (t *GitLabMRGetTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, _ := args["component_id"].(string)
	projectID, _ := args["project_id"].(string)
	if sourceID == "" || projectID == "" {
		return nil, fmt.Errorf("component_id and project_id are required")
	}
	mrIID, ok := args["mr_iid"].(float64)
	if !ok || mrIID <= 0 {
		return nil, fmt.Errorf("mr_iid must be a positive integer")
	}

	mr, err := t.client.GitLabGetMR(ctx, sourceID, projectID, int(mrIID))
	if err != nil {
		return nil, fmt.Errorf("gitlab_mr_get: %w", err)
	}
	return mr, nil
}
