package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// GitLabCommentClient is the interface required by the gitlab_comment tool.
type GitLabCommentClient interface {
	GitLabPostNote(ctx context.Context, sourceID, projectID string, iid int, body string) error
}

// GitLabCommentTool posts a note on a GitLab merge request (T2: Record).
type GitLabCommentTool struct {
	client GitLabCommentClient
}

// NewGitLabCommentTool creates a new gitlab_comment tool.
func NewGitLabCommentTool(c GitLabCommentClient) *GitLabCommentTool {
	return &GitLabCommentTool{client: c}
}

func (t *GitLabCommentTool) Name() string { return "gitlab_comment" }

func (t *GitLabCommentTool) Description() string {
	return "Post a note on a GitLab merge request. Use this to share findings, suggestions, or a review summary. This is a Mutate action — it writes to GitLab but does not block the MR."
}

func (t *GitLabCommentTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "GitLab component ID registered in Joe"},
			"project_id":   {Type: "string", Description: "GitLab project ID or URL-encoded path"},
			"mr_iid":       {Type: "number", Description: "Merge request internal ID (iid)"},
			"body":         {Type: "string", Description: "Note body (Markdown supported)"},
		},
		Required: []string{"component_id", "project_id", "mr_iid", "body"},
	}
}

func (t *GitLabCommentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, _ := args["component_id"].(string)
	projectID, _ := args["project_id"].(string)
	body, _ := args["body"].(string)
	if sourceID == "" || projectID == "" || body == "" {
		return nil, fmt.Errorf("component_id, project_id, and body are required")
	}
	mrIID, ok := args["mr_iid"].(float64)
	if !ok || mrIID <= 0 {
		return nil, fmt.Errorf("mr_iid must be a positive integer")
	}

	if err := t.client.GitLabPostNote(ctx, sourceID, projectID, int(mrIID), body); err != nil {
		return nil, fmt.Errorf("gitlab_comment: %w", err)
	}
	return map[string]any{
		"posted":     true,
		"project_id": projectID,
		"mr_iid":     int(mrIID),
	}, nil
}
