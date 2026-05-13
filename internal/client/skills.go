package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

const (
	apiSkillsReloadPath  = "/api/v1/skills/reload"
	apiSkillsListPath    = "/api/v1/skills"
	apiSkillsApprovePath = "/api/v1/skills/approve"
	apiSkillsRejectPath  = "/api/v1/skills/reject"
)

// SkillsReloadResult mirrors the JSON shape returned by POST /skills/reload.
// The before/after counts plus the diff slices give CLI and CI/CD callers a
// concise summary of what changed in the registry without a follow-up list
// call.
type SkillsReloadResult struct {
	Status  string   `json:"status"`
	Trigger string   `json:"trigger"`
	Before  int      `json:"before"`
	After   int      `json:"after"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Updated []string `json:"updated,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// ReloadSkills triggers a synchronous rescan of joe-core's ~/.joe/skills/
// directory and atomic registry swap. Returns the reload summary.
func (c *Client) ReloadSkills(ctx context.Context) (*SkillsReloadResult, error) {
	var result SkillsReloadResult
	if err := c.doJSON(ctx, "POST", c.baseURL+apiSkillsReloadPath, nil, http.StatusOK, &result, "skills reload"); err != nil {
		return nil, err
	}
	return &result, nil
}

// SkillStatusEntry mirrors the JSON shape returned by GET /skills. One row
// per installed skill.
type SkillStatusEntry struct {
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	Repo             string `json:"repo"`
	Ref              string `json:"ref,omitempty"`
	Commit           string `json:"commit,omitempty"`
	Status           string `json:"status"`
	QuarantineReason string `json:"quarantine_reason,omitempty"`
	Hash             string `json:"hash,omitempty"`
}

// SkillsListResult is the GET /skills payload, split between active and
// quarantined so callers can render "needs approval" without extra
// filtering.
type SkillsListResult struct {
	Active      []SkillStatusEntry `json:"active"`
	Quarantined []SkillStatusEntry `json:"quarantined"`
}

// ListSkills fetches every installed skill from joe-core, split into active
// and quarantined buckets.
func (c *Client) ListSkills(ctx context.Context) (*SkillsListResult, error) {
	var result SkillsListResult
	if err := c.doJSON(ctx, "GET", c.baseURL+apiSkillsListPath, nil, http.StatusOK, &result, "skills list"); err != nil {
		return nil, err
	}
	return &result, nil
}

// SkillsApprovalResult mirrors the JSON shape returned by /skills/approve
// and /skills/reject — both endpoints share the same response shape so the
// client only needs one type.
type SkillsApprovalResult struct {
	Status string   `json:"status"`
	Name   string   `json:"name"`
	Repo   string   `json:"repo,omitempty"`
	Commit string   `json:"commit,omitempty"`
	Skills []string `json:"skills"`
}

// ApproveSkill asks joe-core to move a quarantined skill into the active
// registry. The name parameter identifies any skill in the quarantined
// install; the whole install is approved as a unit.
func (c *Client) ApproveSkill(ctx context.Context, name string) (*SkillsApprovalResult, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	var result SkillsApprovalResult
	if err := c.doJSON(ctx, "POST", c.baseURL+apiSkillsApprovePath, bytes.NewReader(body), http.StatusOK, &result, "skills approve"); err != nil {
		return nil, err
	}
	return &result, nil
}

// RejectSkill asks joe-core to delete a quarantined install. Returns the
// names of every skill that was removed (one install can carry multiple).
func (c *Client) RejectSkill(ctx context.Context, name string) (*SkillsApprovalResult, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	var result SkillsApprovalResult
	if err := c.doJSON(ctx, "POST", c.baseURL+apiSkillsRejectPath, bytes.NewReader(body), http.StatusOK, &result, "skills reject"); err != nil {
		return nil, err
	}
	return &result, nil
}
