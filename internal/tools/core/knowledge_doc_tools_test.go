package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/drift"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/tools/core"
)

// ---- SearchKnowledgeTool ----

type fakeKnowledgeSearchClient struct {
	fn func(ctx context.Context, query string, topK int, tierFilter []knowledge.Tier) ([]knowledge.SearchResult, error)
}

func (f *fakeKnowledgeSearchClient) SearchKnowledge(ctx context.Context, query string, topK int, tierFilter []knowledge.Tier) ([]knowledge.SearchResult, error) {
	return f.fn(ctx, query, topK, tierFilter)
}

func TestSearchKnowledgeTool(t *testing.T) {
	fake := &fakeKnowledgeSearchClient{
		fn: func(_ context.Context, query string, topK int, _ []knowledge.Tier) ([]knowledge.SearchResult, error) {
			if query == "fail" {
				return nil, errors.New("search error")
			}
			results := make([]knowledge.SearchResult, 0, topK)
			for i := 0; i < topK; i++ {
				results = append(results, knowledge.SearchResult{
					Entry:      knowledge.Entry{Title: "Runbook #1", Content: "step 1: do this", Tier: knowledge.TierCurated},
					Similarity: 0.95,
				})
			}
			return results, nil
		},
	}
	tool := core.NewSearchKnowledgeTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "search_knowledge" {
			t.Errorf("Name() = %q, want search_knowledge", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["query"]; !ok {
			t.Error("Parameters() missing query")
		}
	})

	t.Run("missing query", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("empty query", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"query": ""})
		if err == nil {
			t.Error("expected error for empty query")
		}
	})

	t.Run("success with default top_k", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"query": "payment timeout"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 5 {
			t.Errorf("count = %v, want 5 (default top_k)", m["count"])
		}
		if m["query"] != "payment timeout" {
			t.Errorf("query = %v, want payment timeout", m["query"])
		}
	})

	t.Run("success with custom top_k", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"query": "how to scale HPA",
			"top_k": float64(3),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 3 {
			t.Errorf("count = %v, want 3", m["count"])
		}
	})

	t.Run("success with tier filter", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"query":       "runbook",
			"tier_filter": []any{"curated", "synced"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("content truncation", func(t *testing.T) {
		longContent := strings.Repeat("word ", 300) // > 800 chars
		truncFake := &fakeKnowledgeSearchClient{
			fn: func(_ context.Context, _ string, _ int, _ []knowledge.Tier) ([]knowledge.SearchResult, error) {
				return []knowledge.SearchResult{
					{
						Entry:      knowledge.Entry{Title: "Long doc", Content: longContent, Tier: knowledge.TierDerived},
						Similarity: 0.8,
					},
				}, nil
			},
		}
		truncTool := core.NewSearchKnowledgeTool(truncFake)
		res, err := truncTool.Execute(context.Background(), map[string]any{"query": "long"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 1 {
			t.Errorf("count = %v, want 1", m["count"])
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"query": "fail"})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- DetectDocDriftTool ----

type fakeDocDriftClient struct {
	detectFn        func(ctx context.Context, sourceType knowledge.SourceType) ([]*drift.DriftReport, error)
	detectByEntryFn func(ctx context.Context, entryID string) (*drift.DriftReport, error)
}

func (f *fakeDocDriftClient) DetectDrift(ctx context.Context, sourceType knowledge.SourceType) ([]*drift.DriftReport, error) {
	return f.detectFn(ctx, sourceType)
}

func (f *fakeDocDriftClient) DetectDriftByEntry(ctx context.Context, entryID string) (*drift.DriftReport, error) {
	return f.detectByEntryFn(ctx, entryID)
}

func TestDetectDocDriftTool(t *testing.T) {
	fake := &fakeDocDriftClient{
		detectFn: func(_ context.Context, _ knowledge.SourceType) ([]*drift.DriftReport, error) {
			return []*drift.DriftReport{
				{EntryID: "entry-1", Title: "Runbook A", ExternalChanged: true},
				{EntryID: "entry-2", Title: "Runbook B", ExternalChanged: false},
			}, nil
		},
		detectByEntryFn: func(_ context.Context, entryID string) (*drift.DriftReport, error) {
			if entryID == "entry-1" {
				return &drift.DriftReport{EntryID: "entry-1", Title: "Runbook A", ExternalChanged: true}, nil
			}
			return nil, errors.New("entry not found")
		},
	}
	tool := core.NewDetectDocDriftTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "detect_doc_drift" {
			t.Errorf("Name() = %q, want detect_doc_drift", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["source_type"]; !ok {
			t.Error("Parameters() missing source_type")
		}
	})

	t.Run("detect all sources success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["drifted_count"].(int) != 2 {
			t.Errorf("drifted_count = %v, want 2", m["drifted_count"])
		}
	})

	t.Run("detect by source_type", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_type": "confluence",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["drifted_count"] == nil {
			t.Error("expected drifted_count in result")
		}
	})

	t.Run("detect by entry_id success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"entry_id": "entry-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["drifted_count"].(int) != 1 {
			t.Errorf("drifted_count = %v, want 1 (entry changed)", m["drifted_count"])
		}
	})

	t.Run("detect by entry_id not drifted", func(t *testing.T) {
		notDriftedFake := &fakeDocDriftClient{
			detectByEntryFn: func(_ context.Context, _ string) (*drift.DriftReport, error) {
				return &drift.DriftReport{EntryID: "entry-2", ExternalChanged: false}, nil
			},
		}
		notDriftedTool := core.NewDetectDocDriftTool(notDriftedFake)
		res, err := notDriftedTool.Execute(context.Background(), map[string]any{
			"entry_id": "entry-2",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["drifted_count"].(int) != 0 {
			t.Errorf("drifted_count = %v, want 0 (not changed)", m["drifted_count"])
		}
	})

	t.Run("detect by entry_id client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"entry_id": "missing-entry",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})

	t.Run("detect all sources client error", func(t *testing.T) {
		errFake := &fakeDocDriftClient{
			detectFn: func(_ context.Context, _ knowledge.SourceType) ([]*drift.DriftReport, error) {
				return nil, errors.New("connection failed")
			},
		}
		errTool := core.NewDetectDocDriftTool(errFake)
		_, err := errTool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error from client")
		}
	})

	t.Run("nil reports returns empty slice", func(t *testing.T) {
		nilFake := &fakeDocDriftClient{
			detectFn: func(_ context.Context, _ knowledge.SourceType) ([]*drift.DriftReport, error) {
				return nil, nil
			},
		}
		nilTool := core.NewDetectDocDriftTool(nilFake)
		res, err := nilTool.Execute(context.Background(), map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["drifted_count"].(int) != 0 {
			t.Errorf("drifted_count = %v, want 0", m["drifted_count"])
		}
	})
}

// ---- PublishDocUpdateTool ----

type fakePublishDocClient struct {
	fn func(ctx context.Context, id string) error
}

func (f *fakePublishDocClient) PublishProposal(ctx context.Context, id string) error {
	return f.fn(ctx, id)
}

func TestPublishDocUpdateTool(t *testing.T) {
	fake := &fakePublishDocClient{
		fn: func(_ context.Context, id string) error {
			if id == "prop-abc" {
				return nil
			}
			return errors.New("proposal not found or not approved")
		},
	}
	tool := core.NewPublishDocUpdateTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "publish_doc_update" {
			t.Errorf("Name() = %q, want publish_doc_update", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["proposal_id"]; !ok {
			t.Error("Parameters() missing proposal_id")
		}
	})

	t.Run("missing proposal_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error for missing proposal_id")
		}
	})

	t.Run("empty proposal_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"proposal_id": ""})
		if err == nil {
			t.Error("expected error for empty proposal_id")
		}
	})

	t.Run("success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{"proposal_id": "prop-abc"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["status"] != "published" {
			t.Errorf("status = %v, want published", m["status"])
		}
		if m["proposal_id"] != "prop-abc" {
			t.Errorf("proposal_id = %v, want prop-abc", m["proposal_id"])
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"proposal_id": "bad-proposal"})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- GenerateDocDraftTool ----

type fakeDocDraftClient struct {
	fn func(ctx context.Context, req drafts.GenerateRequest) (*proposals.Proposal, error)
}

func (f *fakeDocDraftClient) CreateProposal(ctx context.Context, req drafts.GenerateRequest) (*proposals.Proposal, error) {
	return f.fn(ctx, req)
}

func TestGenerateDocDraftTool(t *testing.T) {
	fake := &fakeDocDraftClient{
		fn: func(_ context.Context, req drafts.GenerateRequest) (*proposals.Proposal, error) {
			if req.Topic == "fail" {
				return nil, errors.New("llm error")
			}
			return &proposals.Proposal{
				ID:         "prop-new",
				Title:      "Draft: " + req.Topic,
				Status:     proposals.StatusPending,
				TargetType: req.TargetType,
				TargetID:   req.TargetID,
				Diff:       "diff content here",
			}, nil
		},
	}
	tool := core.NewGenerateDocDraftTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "generate_doc_draft" {
			t.Errorf("Name() = %q, want generate_doc_draft", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["topic"]; !ok {
			t.Error("Parameters() missing topic")
		}
	})

	t.Run("missing topic", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"target_type": "confluence",
			"target_id":   "12345",
		})
		if err == nil {
			t.Error("expected error for missing topic")
		}
	})

	t.Run("missing target_type", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"topic":     "payment service runbook",
			"target_id": "12345",
		})
		if err == nil {
			t.Error("expected error for missing target_type")
		}
	})

	t.Run("missing target_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"topic":       "payment service runbook",
			"target_type": "confluence",
		})
		if err == nil {
			t.Error("expected error for missing target_id")
		}
	})

	t.Run("invalid target_type", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"topic":       "payment service runbook",
			"target_type": "invalid-system",
			"target_id":   "12345",
		})
		if err == nil {
			t.Error("expected error for invalid target_type")
		}
	})

	t.Run("success confluence", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"topic":       "payment service runbook",
			"target_type": "confluence",
			"target_id":   "12345",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["proposal_id"] != "prop-new" {
			t.Errorf("proposal_id = %v, want prop-new", m["proposal_id"])
		}
		if m["status"] != "pending" {
			t.Errorf("status = %v, want pending", m["status"])
		}
	})

	t.Run("success notion", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"topic":       "k8s scaling guide",
			"target_type": "notion",
			"target_id":   "page-xyz",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["target_type"] != "notion" {
			t.Errorf("target_type = %v, want notion", m["target_type"])
		}
	})

	t.Run("success git", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"topic":       "deployment guide",
			"target_type": "git",
			"target_id":   "docs/deploy.md",
			"context":     "focus on rollback steps",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["target_type"] != "git" {
			t.Errorf("target_type = %v, want git", m["target_type"])
		}
	})

	t.Run("long diff gets truncated", func(t *testing.T) {
		longDiff := strings.Repeat("x", 3000)
		longFake := &fakeDocDraftClient{
			fn: func(_ context.Context, req drafts.GenerateRequest) (*proposals.Proposal, error) {
				return &proposals.Proposal{
					ID:         "prop-long",
					Title:      "Long proposal",
					Status:     proposals.StatusPending,
					TargetType: proposals.TargetGit,
					TargetID:   "docs/big.md",
					Diff:       longDiff,
				}, nil
			},
		}
		longTool := core.NewGenerateDocDraftTool(longFake)
		res, err := longTool.Execute(context.Background(), map[string]any{
			"topic":       "big doc",
			"target_type": "git",
			"target_id":   "docs/big.md",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		preview, _ := m["diff_preview"].(string)
		if len(preview) >= len(longDiff) {
			t.Error("expected diff to be truncated")
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"topic":       "fail",
			"target_type": "confluence",
			"target_id":   "12345",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}
