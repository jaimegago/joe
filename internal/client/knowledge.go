package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jaimegago/joe/internal/knowledge"
)

// --- Knowledge entries ---

// CreateKnowledgeEntry creates a new knowledge entry in joecored.
func (c *Client) CreateKnowledgeEntry(ctx context.Context, e *knowledge.Entry) (*knowledge.Entry, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("marshal entry: %w", err)
	}

	var created knowledge.Entry
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiKnowledgeEntriesPath,
		bytes.NewReader(body), http.StatusCreated, &created, "create knowledge entry"); err != nil {
		return nil, err
	}
	return &created, nil
}

// GetKnowledgeEntry returns a single knowledge entry by ID.
func (c *Client) GetKnowledgeEntry(ctx context.Context, id string) (*knowledge.Entry, error) {
	u := fmt.Sprintf("%s%s/%s", c.baseURL, apiKnowledgeEntriesPath, url.PathEscape(id))
	var e knowledge.Entry
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &e, "get knowledge entry"); err != nil {
		return nil, err
	}
	return &e, nil
}

// ListKnowledgeEntries returns knowledge entries, optionally filtered.
func (c *Client) ListKnowledgeEntries(ctx context.Context, tier knowledge.Tier, sourceType knowledge.SourceType) ([]*knowledge.Entry, error) {
	u := c.baseURL + apiKnowledgeEntriesPath
	q := url.Values{}
	if tier != "" {
		q.Set("tier", string(tier))
	}
	if sourceType != "" {
		q.Set("source_type", string(sourceType))
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var result struct {
		Entries []*knowledge.Entry `json:"entries"`
		Count   int                `json:"count"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "list knowledge entries"); err != nil {
		return nil, err
	}
	return result.Entries, nil
}

// DeleteKnowledgeEntry deletes a knowledge entry by ID.
func (c *Client) DeleteKnowledgeEntry(ctx context.Context, id string) error {
	u := fmt.Sprintf("%s%s/%s", c.baseURL, apiKnowledgeEntriesPath, url.PathEscape(id))
	return c.doJSON(ctx, http.MethodDelete, u, nil, http.StatusOK, nil, "delete knowledge entry")
}

// --- Semantic search ---

type knowledgeSearchRequest struct {
	Query         string           `json:"query"`
	TopK          int              `json:"top_k,omitempty"`
	TierFilter    []knowledge.Tier `json:"tier_filter,omitempty"`
	MinConfidence float64          `json:"min_confidence,omitempty"`
}

// SearchKnowledge performs semantic search over the knowledge store.
func (c *Client) SearchKnowledge(ctx context.Context, query string, topK int, tierFilter []knowledge.Tier) ([]knowledge.SearchResult, error) {
	req := knowledgeSearchRequest{
		Query:      query,
		TopK:       topK,
		TierFilter: tierFilter,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal search request: %w", err)
	}

	var result struct {
		Results []knowledge.SearchResult `json:"results"`
		Count   int                      `json:"count"`
		Query   string                   `json:"query"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiKnowledgeSearchPath,
		bytes.NewReader(body), http.StatusOK, &result, "knowledge search"); err != nil {
		return nil, err
	}
	return result.Results, nil
}

// --- Knowledge sources ---

// CreateKnowledgeSource registers a new external knowledge source.
func (c *Client) CreateKnowledgeSource(ctx context.Context, src *knowledge.KnowledgeSource) (*knowledge.KnowledgeSource, error) {
	body, err := json.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("marshal source: %w", err)
	}

	var created knowledge.KnowledgeSource
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiKnowledgeSourcesPath,
		bytes.NewReader(body), http.StatusCreated, &created, "create knowledge source"); err != nil {
		return nil, err
	}
	return &created, nil
}

// ListKnowledgeSources returns all registered knowledge sources.
func (c *Client) ListKnowledgeSources(ctx context.Context) ([]*knowledge.KnowledgeSource, error) {
	var result struct {
		Sources []*knowledge.KnowledgeSource `json:"sources"`
		Count   int                          `json:"count"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.baseURL+apiKnowledgeSourcesPath,
		nil, http.StatusOK, &result, "list knowledge sources"); err != nil {
		return nil, err
	}
	return result.Sources, nil
}

// DeleteKnowledgeSource removes a knowledge source by ID.
func (c *Client) DeleteKnowledgeSource(ctx context.Context, id string) error {
	u := fmt.Sprintf("%s%s/%s", c.baseURL, apiKnowledgeSourcesPath, url.PathEscape(id))
	return c.doJSON(ctx, http.MethodDelete, u, nil, http.StatusOK, nil, "delete knowledge source")
}

// TriggerKnowledgeSync requests a manual sync of a knowledge source.
func (c *Client) TriggerKnowledgeSync(ctx context.Context, id string) error {
	u := fmt.Sprintf("%s%s/%s/sync", c.baseURL, apiKnowledgeSourcesPath, url.PathEscape(id))
	return c.doJSON(ctx, http.MethodPost, u, nil, http.StatusAccepted, nil, "trigger knowledge sync")
}
