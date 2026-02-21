package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// PublishDocClient is the interface for publishing an approved proposal.
type PublishDocClient interface {
	PublishProposal(ctx context.Context, id string) error
}

// PublishDocUpdateTool publishes an approved doc proposal to its target system.
// Tier: T3 (Act) — mutates an external system (Confluence, Notion, or Git).
type PublishDocUpdateTool struct {
	client PublishDocClient
}

// NewPublishDocUpdateTool creates a new publish_doc_update tool.
func NewPublishDocUpdateTool(c PublishDocClient) *PublishDocUpdateTool {
	return &PublishDocUpdateTool{client: c}
}

func (t *PublishDocUpdateTool) Name() string { return "publish_doc_update" }

func (t *PublishDocUpdateTool) Description() string {
	return "Publish an approved documentation proposal to its target system (Confluence, Notion, or Git). The proposal must already be approved by a human. This writes to an external system."
}

func (t *PublishDocUpdateTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"proposal_id": {
				Type:        "string",
				Description: "ID of the approved proposal to publish",
			},
		},
		Required: []string{"proposal_id"},
	}
}

func (t *PublishDocUpdateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	proposalID, _ := args["proposal_id"].(string)
	if proposalID == "" {
		return nil, fmt.Errorf("missing required parameter: proposal_id")
	}

	if err := t.client.PublishProposal(ctx, proposalID); err != nil {
		return nil, fmt.Errorf("publish doc update: %w", err)
	}

	return map[string]any{
		"status":      "published",
		"proposal_id": proposalID,
		"message":     "Documentation proposal published successfully.",
	}, nil
}
