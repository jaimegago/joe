package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/tools/core"
)

type mockDocDraftClient struct {
	proposal *proposals.Proposal
	err      error
}

func (m *mockDocDraftClient) CreateProposal(ctx context.Context, req drafts.GenerateRequest) (*proposals.Proposal, error) {
	return m.proposal, m.err
}

func TestGenerateDocDraftTool_Name(t *testing.T) {
	tool := core.NewGenerateDocDraftTool(&mockDocDraftClient{})
	if tool.Name() != "generate_doc_draft" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "generate_doc_draft")
	}
}

func TestGenerateDocDraftTool_Execute_Success(t *testing.T) {
	client := &mockDocDraftClient{
		proposal: &proposals.Proposal{
			ID:              "prop-1",
			Title:           "My Service Runbook",
			Status:          proposals.StatusPending,
			TargetType:      proposals.TargetConfluence,
			TargetID:        "page-123",
			ProposedContent: "New content here",
			Diff:            "--- old\n+++ new\n",
		},
	}
	tool := core.NewGenerateDocDraftTool(client)

	result, err := tool.Execute(context.Background(), map[string]any{
		"topic":       "My Service Runbook",
		"target_type": "confluence",
		"target_id":   "page-123",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("Execute() result type = %T, want map[string]any", result)
	}
	if m["proposal_id"] != "prop-1" {
		t.Errorf("proposal_id = %v, want %q", m["proposal_id"], "prop-1")
	}
	if m["status"] != "pending" {
		t.Errorf("status = %v, want %q", m["status"], "pending")
	}
}

func TestGenerateDocDraftTool_Execute_MissingParams(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing topic", map[string]any{"target_type": "confluence", "target_id": "p1"}},
		{"missing target_type", map[string]any{"topic": "foo", "target_id": "p1"}},
		{"missing target_id", map[string]any{"topic": "foo", "target_type": "confluence"}},
	}

	tool := core.NewGenerateDocDraftTool(&mockDocDraftClient{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tt.args)
			if err == nil {
				t.Error("Execute() should return error for missing required params")
			}
		})
	}
}

func TestGenerateDocDraftTool_Execute_InvalidTargetType(t *testing.T) {
	tool := core.NewGenerateDocDraftTool(&mockDocDraftClient{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"topic":       "foo",
		"target_type": "invalid",
		"target_id":   "p1",
	})
	if err == nil {
		t.Error("Execute() should return error for invalid target_type")
	}
}

func TestGenerateDocDraftTool_Execute_ClientError(t *testing.T) {
	client := &mockDocDraftClient{err: errors.New("LLM unavailable")}
	tool := core.NewGenerateDocDraftTool(client)
	_, err := tool.Execute(context.Background(), map[string]any{
		"topic":       "foo",
		"target_type": "confluence",
		"target_id":   "p1",
	})
	if err == nil {
		t.Error("Execute() should propagate client error")
	}
}

func TestGenerateDocDraftTool_Execute_DiffTruncation(t *testing.T) {
	longDiff := make([]byte, 3000)
	for i := range longDiff {
		longDiff[i] = 'x'
	}
	client := &mockDocDraftClient{
		proposal: &proposals.Proposal{
			ID:              "p-trunc",
			Title:           "T",
			Status:          proposals.StatusPending,
			TargetType:      proposals.TargetGit,
			TargetID:        "docs/foo.md",
			ProposedContent: "content",
			Diff:            string(longDiff),
		},
	}
	tool := core.NewGenerateDocDraftTool(client)
	result, err := tool.Execute(context.Background(), map[string]any{
		"topic":       "foo",
		"target_type": "git",
		"target_id":   "docs/foo.md",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	preview, ok := m["diff_preview"].(string)
	if !ok {
		t.Fatal("diff_preview should be a string")
	}
	if len(preview) > 2100 {
		t.Errorf("diff_preview length = %d, expected truncation at ~2000", len(preview))
	}
}
