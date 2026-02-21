package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drift"
	"github.com/jaimegago/joe/internal/llm"
)

// DocDriftClient is the interface for drift detection.
type DocDriftClient interface {
	DetectDrift(ctx context.Context, sourceType knowledge.SourceType) ([]*drift.DriftReport, error)
	DetectDriftByEntry(ctx context.Context, entryID string) (*drift.DriftReport, error)
}

// DetectDocDriftTool checks for documentation drift between the knowledge store
// and external sources (Confluence, Notion).
// Tier: T1 (Observe) — read-only.
type DetectDocDriftTool struct {
	client DocDriftClient
}

// NewDetectDocDriftTool creates a new detect_doc_drift tool.
func NewDetectDocDriftTool(c DocDriftClient) *DetectDocDriftTool {
	return &DetectDocDriftTool{client: c}
}

func (t *DetectDocDriftTool) Name() string { return "detect_doc_drift" }

func (t *DetectDocDriftTool) Description() string {
	return "Detect documentation drift between Joe's knowledge store and external sources (Confluence, Notion). Returns entries where the external doc has changed since last sync."
}

func (t *DetectDocDriftTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_type": {
				Type:        "string",
				Description: "Optional filter: confluence | notion. Empty means check all synced sources.",
			},
			"entry_id": {
				Type:        "string",
				Description: "Optional: check drift for a single knowledge entry by ID.",
			},
		},
	}
}

func (t *DetectDocDriftTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	entryID, _ := args["entry_id"].(string)
	sourceTypeStr, _ := args["source_type"].(string)

	if entryID != "" {
		report, err := t.client.DetectDriftByEntry(ctx, entryID)
		if err != nil {
			return nil, fmt.Errorf("detect drift for entry %s: %w", entryID, err)
		}
		drifted := 0
		if report.ExternalChanged {
			drifted = 1
		}
		return map[string]any{
			"drifted_count": drifted,
			"reports":       []*drift.DriftReport{report},
		}, nil
	}

	sourceType := knowledge.SourceType(sourceTypeStr)
	reports, err := t.client.DetectDrift(ctx, sourceType)
	if err != nil {
		return nil, fmt.Errorf("detect doc drift: %w", err)
	}
	if reports == nil {
		reports = []*drift.DriftReport{}
	}
	return map[string]any{
		"drifted_count": len(reports),
		"reports":       reports,
	}, nil
}
