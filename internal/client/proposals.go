package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
)

const apiKnowledgeProposalsPath = "/api/v1/knowledge/proposals"

// CreateProposal generates a new documentation proposal via the joecored API.
func (c *Client) CreateProposal(ctx context.Context, req drafts.GenerateRequest) (*proposals.Proposal, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal create proposal request: %w", err)
	}

	var p proposals.Proposal
	if err := c.doJSON(ctx, http.MethodPost, c.baseURL+apiKnowledgeProposalsPath,
		bytes.NewReader(body), http.StatusCreated, &p, "create proposal"); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetProposal returns a single proposal by ID.
func (c *Client) GetProposal(ctx context.Context, id string) (*proposals.Proposal, error) {
	u := fmt.Sprintf("%s%s/%s", c.baseURL, apiKnowledgeProposalsPath, url.PathEscape(id))
	var p proposals.Proposal
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &p, "get proposal"); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProposals returns proposals, optionally filtered by status and target type.
func (c *Client) ListProposals(ctx context.Context, statusFilter proposals.ProposalStatus, targetType proposals.TargetType) ([]*proposals.Proposal, error) {
	u := c.baseURL + apiKnowledgeProposalsPath
	q := url.Values{}
	if statusFilter != "" {
		q.Set("status", string(statusFilter))
	}
	if targetType != "" {
		q.Set("target_type", string(targetType))
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var result struct {
		Proposals []*proposals.Proposal `json:"proposals"`
		Count     int                   `json:"count"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "list proposals"); err != nil {
		return nil, err
	}
	return result.Proposals, nil
}

// ApproveProposal approves a pending proposal by ID.
func (c *Client) ApproveProposal(ctx context.Context, id string) error {
	u := fmt.Sprintf("%s%s/%s/approve", c.baseURL, apiKnowledgeProposalsPath, url.PathEscape(id))
	return c.doJSON(ctx, http.MethodPost, u, nil, http.StatusOK, nil, "approve proposal")
}

// PublishProposal publishes an approved proposal to its target system.
func (c *Client) PublishProposal(ctx context.Context, id string) error {
	u := fmt.Sprintf("%s%s/%s/publish", c.baseURL, apiKnowledgeProposalsPath, url.PathEscape(id))
	return c.doJSON(ctx, http.MethodPost, u, nil, http.StatusOK, nil, "publish proposal")
}

// RejectProposal rejects a pending proposal with an optional reason.
func (c *Client) RejectProposal(ctx context.Context, id, reason string) error {
	u := fmt.Sprintf("%s%s/%s/reject", c.baseURL, apiKnowledgeProposalsPath, url.PathEscape(id))
	body, _ := json.Marshal(map[string]string{"reason": reason})
	return c.doJSON(ctx, http.MethodPost, u, bytes.NewReader(body), http.StatusOK, nil, "reject proposal")
}
