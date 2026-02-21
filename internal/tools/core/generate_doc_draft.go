package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/llm"
)

// DocDraftClient is the interface for generating documentation draft proposals.
type DocDraftClient interface {
	CreateProposal(ctx context.Context, req drafts.GenerateRequest) (*proposals.Proposal, error)
}

// GenerateDocDraftTool generates a documentation draft proposal from the knowledge store.
// Tier: T2 (Record) — creates a proposal in Joe's internal state.
type GenerateDocDraftTool struct {
	client DocDraftClient
}

// NewGenerateDocDraftTool creates a new generate_doc_draft tool.
func NewGenerateDocDraftTool(c DocDraftClient) *GenerateDocDraftTool {
	return &GenerateDocDraftTool{client: c}
}

func (t *GenerateDocDraftTool) Name() string { return "generate_doc_draft" }

func (t *GenerateDocDraftTool) Description() string {
	return "Generate a documentation draft proposal from the knowledge store. Creates a pending proposal that requires human approval before publishing. Use when asked to draft or update documentation."
}

func (t *GenerateDocDraftTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"topic": {
				Type:        "string",
				Description: "What to document — used to search the knowledge store for relevant context",
			},
			"target_type": {
				Type:        "string",
				Description: "Target system: confluence | notion | git",
			},
			"target_id": {
				Type:        "string",
				Description: "Page ID (Confluence/Notion) or file path (git) to update",
			},
			"context": {
				Type:        "string",
				Description: "Optional extra guidance for the LLM (e.g. 'focus on runbook steps')",
			},
		},
		Required: []string{"topic", "target_type", "target_id"},
	}
}

func (t *GenerateDocDraftTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	topic, _ := args["topic"].(string)
	targetTypeStr, _ := args["target_type"].(string)
	targetID, _ := args["target_id"].(string)
	context, _ := args["context"].(string)

	if topic == "" {
		return nil, fmt.Errorf("missing required parameter: topic")
	}
	if targetTypeStr == "" {
		return nil, fmt.Errorf("missing required parameter: target_type")
	}
	if targetID == "" {
		return nil, fmt.Errorf("missing required parameter: target_id")
	}

	targetType := proposals.TargetType(targetTypeStr)
	if targetType != proposals.TargetConfluence && targetType != proposals.TargetNotion && targetType != proposals.TargetGit {
		return nil, fmt.Errorf("invalid target_type %q: must be confluence, notion, or git", targetTypeStr)
	}

	proposal, err := t.client.CreateProposal(ctx, drafts.GenerateRequest{
		Topic:      topic,
		TargetType: targetType,
		TargetID:   targetID,
		Context:    context,
	})
	if err != nil {
		return nil, fmt.Errorf("generate doc draft: %w", err)
	}

	// Return a preview (truncated diff to avoid overwhelming the LLM).
	diffPreview := proposal.Diff
	if len(diffPreview) > 2000 {
		diffPreview = diffPreview[:2000] + "\n... (diff truncated)"
	}

	return map[string]any{
		"proposal_id":  proposal.ID,
		"title":        proposal.Title,
		"status":       string(proposal.Status),
		"target_type":  string(proposal.TargetType),
		"target_id":    proposal.TargetID,
		"diff_preview": diffPreview,
		"message":      "Proposal created. A human must approve it before publishing.",
	}, nil
}
