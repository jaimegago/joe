package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drift"
)

const apiKnowledgeDriftPath = "/api/v1/knowledge/drift"

// DetectDrift checks drift for all Tier 2 entries, optionally filtered by source type.
// Implements the DocDriftClient interface for the detect_doc_drift tool.
func (c *Client) DetectDrift(ctx context.Context, sourceType knowledge.SourceType) ([]*drift.DriftReport, error) {
	u := c.baseURL + apiKnowledgeDriftPath
	q := url.Values{}
	if sourceType != "" {
		q.Set("source_type", string(sourceType))
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var result struct {
		Reports []*drift.DriftReport `json:"reports"`
		Count   int                  `json:"count"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "detect drift"); err != nil {
		return nil, err
	}
	return result.Reports, nil
}

// DetectDriftByEntry checks drift for a single knowledge entry by ID.
// Implements the DocDriftClient interface for the detect_doc_drift tool.
func (c *Client) DetectDriftByEntry(ctx context.Context, entryID string) (*drift.DriftReport, error) {
	u := fmt.Sprintf("%s%s/%s", c.baseURL, apiKnowledgeDriftPath, url.PathEscape(entryID))
	var report drift.DriftReport
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &report, "detect drift by entry"); err != nil {
		return nil, err
	}
	return &report, nil
}
